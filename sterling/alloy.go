package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
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
	opts         options
	model        string
	commandModel string
	cleanup      func()
	alloy        string
	host         string
	port         int
	timeout      time.Duration

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

	// Alloy always opens util/integer implicitly and resolves it relative to
	// the model's module path. If a different file named integer.als happens to
	// live there (as in the alloytools/models repository), Alloy recursively
	// imports that file until it reports a circular dependency. Run a staged
	// copy of the model with the built-in integer module left to Alloy's jar.
	commandModel, cleanup, err := stageAlloyModel(model)
	if err != nil {
		logf(opts, "could not stage Alloy model; running it in place: %v", err)
		commandModel = model
		cleanup = nil
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(opts.host, strconv.Itoa(opts.providerPort)))
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("could not listen for Alloy provider on %s:%d: %w", opts.host, opts.providerPort, err)
	}

	p := &alloyProvider{
		opts:         opts,
		model:        model,
		commandModel: commandModel,
		cleanup:      cleanup,
		alloy:        opts.alloy,
		host:         opts.host,
		port:         listener.Addr().(*net.TCPAddr).Port,
		timeout:      opts.timeout,
		clients:      make(map[*wsConn]struct{}),
		cache:        make(map[int]string),
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

	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

var alloyModulePattern = regexp.MustCompile(`(?m)^[ \t]*module[ \t]+([^ \t\r\n\[\]]+)`)

// stageAlloyModel copies a model and its local modules into a temporary tree
// whose layout follows the model's module declaration. Alloy resolves imports
// from the module name rather than the physical directory, so this also avoids
// accidentally resolving its implicit "open util/integer" to an unrelated
// integer.als file next to the original model.
func stageAlloyModel(model string) (string, func(), error) {
	content, err := os.ReadFile(model)
	if err != nil {
		return "", nil, err
	}

	tempDir, err := os.MkdirTemp("", "sterling-alloy-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	staged, err := stagedAlloyPath(tempDir, model, string(content))
	if err != nil {
		cleanup()
		return "", nil, err
	}
	stageDir := filepath.Dir(staged)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := copyAlloyTree(filepath.Dir(model), stageDir); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.WriteFile(staged, content, 0o644); err != nil {
		cleanup()
		return "", nil, err
	}
	return staged, cleanup, nil
}

func stagedAlloyPath(tempDir, model, content string) (string, error) {
	ext := ".als"
	if strings.EqualFold(filepath.Ext(model), ".md") {
		ext = ".md"
	}

	match := alloyModulePattern.FindStringSubmatch(content)
	if len(match) == 2 {
		module := path.Clean(strings.ReplaceAll(match[1], "\\", "/"))
		if module != "." && module != ".." && !strings.HasPrefix(module, "../") && !strings.HasPrefix(module, "/") {
			return filepath.Join(tempDir, filepath.FromSlash(module+ext)), nil
		}
	}

	base := filepath.Base(model)
	if strings.EqualFold(base, "integer.als") {
		// Alloy skips its implicit integer import for a file whose path ends
		// with util/integer.als, which is exactly what this file defines.
		return filepath.Join(tempDir, "util", "integer.als"), nil
	}
	return filepath.Join(tempDir, base), nil
}

func copyAlloyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(src string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if src == srcDir {
			return nil
		}

		rel, err := filepath.Rel(srcDir, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		// Alloy only loads .als files and Markdown (.md) models. Skip the
		// implicit integer module so that Alloy falls back to the copy bundled
		// in its own jar instead of recursively importing this one.
		if entry.Name() == "integer.als" || entry.Name() == "integer.md" || !isAlloySource(entry.Name()) {
			return nil
		}
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

func isAlloySource(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".als", ".md":
		return true
	default:
		return false
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
	if xml == "" {
		// The command is unsatisfiable (for example, "run ... expect 0").
		// Alloy exits successfully without producing an instance.
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

	cmd := exec.CommandContext(ctx, p.alloy, "commands", p.commandModel)
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
	args = append(args, p.commandModel)

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
		// An unsatisfiable command exits successfully without writing XML.
		return "", nil
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
