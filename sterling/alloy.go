package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// alloyCommand is one of the run/check commands in an Alloy model.
type alloyCommand struct {
	index int
	label string
}

type alloyProvider struct {
	opts    options
	model   string
	alloy   string
	host    string
	port    int
	timeout time.Duration

	mu       sync.Mutex
	commands []alloyCommand
	clients  map[*wsConn]struct{}
	cache    map[int]string
	nextID   int
	runMu    sync.Mutex
}

// runAlloy serves the Alloy-flavored UI and, when a model was supplied, starts
// a local Sterling provider backed by the Alloy Analyzer.
func runAlloy(opts options, staticPort int, staticDir string, alloyWS *atomic.Int64) (int, error) {
	if opts.serveOnly {
		// Preserve the historical "serve the UI only" mode: an externally
		// started provider is expected on Alloy's conventional port unless the
		// caller explicitly chose one.
		providerPort := opts.providerPort
		if providerPort == 0 {
			providerPort = 4000
		}
		alloyWS.Store(int64(providerPort))
		if err := emitReady(opts, staticPort, nil, staticDir); err != nil {
			return 0, err
		}
		openBrowser(opts, visualizerURL(opts, staticPort, nil))
		waitForSignal()
		return 0, nil
	}

	modelPath, err := filepath.Abs(opts.model)
	if err != nil {
		return 0, err
	}

	provider, err := startAlloyProvider(opts, modelPath)
	if err != nil {
		return 0, err
	}
	defer provider.close()

	providerPort := provider.port
	alloyWS.Store(int64(providerPort))
	if err := emitReady(opts, staticPort, &providerPort, staticDir); err != nil {
		return 0, err
	}
	openBrowser(opts, visualizerURL(opts, staticPort, &providerPort))
	waitForSignal()
	return 0, nil
}

func startAlloyProvider(opts options, model string) (*alloyProvider, error) {
	if _, err := exec.LookPath(opts.alloy); err != nil {
		return nil, fmt.Errorf("could not find Alloy executable %q: %w", opts.alloy, err)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(opts.host, strconv.Itoa(opts.providerPort)))
	if err != nil {
		return nil, fmt.Errorf("could not listen for Alloy provider on %s:%d: %w", opts.host, opts.providerPort, err)
	}

	p := &alloyProvider{
		opts:    opts,
		model:   model,
		alloy:   opts.alloy,
		host:    opts.host,
		port:    listener.Addr().(*net.TCPAddr).Port,
		timeout: opts.timeout,
		clients: make(map[*wsConn]struct{}),
		cache:   make(map[int]string),
	}
	p.commands = p.loadCommands()

	server := &http.Server{Handler: http.HandlerFunc(p.handleHTTP)}
	go func() { _ = server.Serve(listener) }()

	// Start the default command in the background so the graph is ready as soon
	// as the UI asks for its first datum.
	go func() {
		if _, err := p.instanceFor(p.defaultIndex()); err != nil {
			logf(opts, "could not run Alloy command: %v", err)
		}
	}()

	return p, nil
}

func (p *alloyProvider) close() {
	p.mu.Lock()
	clients := make([]*wsConn, 0, len(p.clients))
	for client := range p.clients {
		clients = append(clients, client)
	}
	p.clients = make(map[*wsConn]struct{})
	p.mu.Unlock()

	for _, client := range clients {
		client.close()
	}
}

func (p *alloyProvider) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketRequest(r) {
		http.NotFound(w, r)
		return
	}
	conn, err := upgradeWebSocket(w, r)
	if err != nil {
		return
	}

	p.mu.Lock()
	p.clients[conn] = struct{}{}
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.clients, conn)
			p.mu.Unlock()
			conn.close()
		}()

		for {
			op, data, err := conn.readMessage()
			if err != nil {
				return
			}
			if op == wsText {
				p.handleMessage(conn, string(data))
			}
		}
	}()
}

func (p *alloyProvider) handleMessage(conn *wsConn, message string) {
	if message == "ping" {
		_ = conn.writeText("pong")
		return
	}

	var msg struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(message), &msg); err != nil {
		return
	}

	switch msg.Type {
	case "meta":
		p.sendMeta(conn)
	case "data":
		p.sendData(conn, p.defaultIndex())
	case "click":
		var payload struct {
			OnClick string `json:"onClick"`
			Context struct {
				GeneratorName string `json:"generatorName"`
			} `json:"context"`
		}
		_ = json.Unmarshal(msg.Payload, &payload)
		index := p.defaultIndex()
		if payload.Context.GeneratorName != "" {
			if command, ok := p.commandByName(payload.Context.GeneratorName); ok {
				index = command.index
			}
		}
		p.sendData(conn, index)
	case "eval":
		var payload struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(msg.Payload, &payload)
		p.sendJSON(conn, map[string]any{
			"type":    "eval",
			"version": 1,
			"payload": map[string]any{
				"id":     payload.ID,
				"result": "Evaluation is not supported by this launcher.",
			},
		})
	}
}

func (p *alloyProvider) sendMeta(conn *wsConn) {
	generators := make([]string, 0, len(p.commands))
	for _, command := range p.commands {
		generators = append(generators, command.label)
	}
	p.sendJSON(conn, map[string]any{
		"type":    "meta",
		"version": 1,
		"payload": map[string]any{
			"name":       "Alloy",
			"views":      []string{"graph", "table", "script"},
			"generators": generators,
		},
	})
}

func (p *alloyProvider) sendData(conn *wsConn, index int) {
	xml, err := p.instanceFor(index)
	if err != nil {
		logf(p.opts, "could not visualize Alloy command: %v", err)
		p.sendJSON(conn, map[string]any{
			"type":    "data",
			"version": 1,
			"payload": map[string]any{"enter": []any{}},
		})
		return
	}

	p.mu.Lock()
	p.nextID++
	id := fmt.Sprintf("alloy-%d", p.nextID)
	p.mu.Unlock()

	command := p.commandByIndex(index)
	p.sendJSON(conn, map[string]any{
		"type":    "data",
		"version": 1,
		"payload": map[string]any{
			"enter": []any{map[string]any{
				"id":            id,
				"generatorName": command.label,
				"format":        "alloy",
				"data":          xml,
				"evaluator":     false,
			}},
		},
	})
}

func (p *alloyProvider) sendJSON(conn *wsConn, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = conn.writeText(string(data))
}

func (p *alloyProvider) defaultIndex() int {
	if len(p.commands) == 0 {
		return 0
	}
	return p.commands[0].index
}

func (p *alloyProvider) commandByIndex(index int) alloyCommand {
	for _, command := range p.commands {
		if command.index == index {
			return command
		}
	}
	return alloyCommand{index: index, label: "Run"}
}

func (p *alloyProvider) commandByName(name string) (alloyCommand, bool) {
	for _, command := range p.commands {
		if command.label == name {
			return command, true
		}
	}
	for _, command := range p.commands {
		if strings.Contains(command.label, name) {
			return command, true
		}
	}
	return alloyCommand{}, false
}

func (p *alloyProvider) loadCommands() []alloyCommand {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.alloy, "commands", p.model)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		logf(p.opts, "could not list Alloy commands: %v%s", err, formatStderr(stderr.String()))
		return []alloyCommand{{index: 0, label: "Run"}}
	}

	var commands []alloyCommand
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		separator := strings.Index(line, " . ")
		if separator < 0 {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSpace(line[:separator]))
		if err != nil {
			continue
		}
		label := strings.TrimSpace(line[separator+3:])
		if label == "" {
			continue
		}
		commands = append(commands, alloyCommand{index: index, label: label})
	}
	if len(commands) == 0 {
		return []alloyCommand{{index: 0, label: "Run"}}
	}
	return commands
}

func (p *alloyProvider) instanceFor(index int) (string, error) {
	p.runMu.Lock()
	defer p.runMu.Unlock()

	p.mu.Lock()
	if xml, ok := p.cache[index]; ok {
		p.mu.Unlock()
		return xml, nil
	}
	p.mu.Unlock()

	xml, err := p.runCommand(index)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.cache[index] = xml
	p.mu.Unlock()
	return xml, nil
}

func (p *alloyProvider) runCommand(index int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	args := []string{
		"exec",
		"-c", strconv.Itoa(index),
		"-t", "xml",
		"-o", "-",
	}
	args = append(args, p.opts.extraArgs...)
	args = append(args, p.model)

	cmd := exec.CommandContext(ctx, p.alloy, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Alloy timed out after %ds", int(p.timeout.Seconds()))
		}
		return "", fmt.Errorf("%v%s", err, formatStderr(stderr.String()))
	}

	xml := extractAlloyXML(string(out))
	if xml == "" {
		return "", fmt.Errorf("Alloy produced no instance%s", formatStderr(stderr.String()))
	}
	return setAlloyFilename(xml, p.model), nil
}

// setAlloyFilename supplies the model path that Cope and Drag expects in the
// instance XML. Alloy's command-line exec subcommand leaves it empty.
func setAlloyFilename(xml, model string) string {
	escaped := strings.NewReplacer(
		"&", "&amp;",
		"\"", "&quot;",
		"<", "&lt;",
		">", "&gt;",
	).Replace(model)
	return strings.Replace(xml, `filename=""`, `filename="`+escaped+`"`, 1)
}

func extractAlloyXML(output string) string {
	start := strings.Index(output, "<alloy")
	end := strings.LastIndex(output, "</alloy>")
	if start < 0 || end < start {
		return ""
	}
	return output[start : end+len("</alloy>")]
}

func formatStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}
