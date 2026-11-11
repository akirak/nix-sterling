// Command sterling-forge serves the locally built Cope and Drag (Sterling) UI
// and, when a model is supplied, starts Forge in Sterling provider mode.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	appName    = envOr("STERLING_APP_NAME", "sterling-forge")
	flavor     = envOr("STERLING_FLAVOR", defaultFlavor(appName))
	allowModel = os.Getenv("STERLING_ALLOW_MODEL") != "0"
)

const defaultTimeout = 120

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return usageError{message: fmt.Sprintf(format, args...)}
}

type options struct {
	host         string
	staticPort   int
	providerPort int
	timeout      time.Duration
	staticDir    string
	racket       string
	alloy        string
	readyFile    string
	model        string
	browser      string
	noBrowser    bool
	json         bool
	quiet        bool
	verbose      bool
	serveOnly    bool
	extraArgs    []string
}

type forgeProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	stdin   *os.File
	stdinW  *os.File
	mu      sync.Mutex
	state   *os.ProcessState
	waitErr error
}

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintf(os.Stderr, "%s: %s\n\n%s", appName, usage.message, helpText())
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(argv []string) (int, error) {
	extraArgs, argv := splitExtraArgs(argv)

	staticPort, err := envPort("STERLING_STATIC_PORT", 0)
	if err != nil {
		return 0, err
	}
	providerPort, err := envPort("STERLING_PROVIDER_PORT", 0)
	if err != nil {
		return 0, err
	}
	timeout, err := envPositiveInt("STERLING_TIMEOUT", defaultTimeout)
	if err != nil {
		return 0, err
	}

	var opts options
	var showHelp, showVersion bool

	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.host, "host", envOr("STERLING_HOST", "127.0.0.1"), "")
	fs.IntVar(&opts.staticPort, "static-port", staticPort, "")
	fs.IntVar(&opts.providerPort, "provider-port", providerPort, "")
	fs.IntVar(&opts.providerPort, "p", providerPort, "")
	fs.IntVar(&timeout, "timeout", timeout, "")
	fs.StringVar(&opts.staticDir, "static-dir", defaultStaticDir(), "")
	fs.StringVar(&opts.racket, "racket", envOr("STERLING_RACKET", "racket"), "")
	fs.StringVar(&opts.alloy, "alloy", envOr("STERLING_ALLOY", "alloy6"), "")
	fs.StringVar(&opts.readyFile, "ready-file", os.Getenv("STERLING_READY_FILE"), "")
	fs.StringVar(&opts.model, "model", envModel(), "")
	fs.StringVar(&opts.browser, "browser", os.Getenv("STERLING_BROWSER"), "")
	fs.BoolVar(&opts.noBrowser, "no-browser", false, "")
	fs.BoolVar(&opts.json, "json", false, "")
	fs.BoolVar(&opts.quiet, "quiet", false, "")
	fs.BoolVar(&opts.quiet, "q", false, "")
	fs.BoolVar(&opts.verbose, "verbose", false, "")
	fs.BoolVar(&opts.verbose, "v", false, "")
	fs.BoolVar(&opts.serveOnly, "serve-only", defaultServeOnly(), "")
	fs.BoolVar(&opts.serveOnly, "static-only", defaultServeOnly(), "")
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showVersion, "version", false, "")
	fs.BoolVar(&showVersion, "V", false, "")

	positionals, err := parseFlags(fs, argv)
	if err != nil {
		return 0, usagef("%v", err)
	}
	if showHelp {
		fmt.Print(helpText())
		return 0, nil
	}
	if showVersion {
		fmt.Printf("%s %s\n", appName, version())
		return 0, nil
	}

	if err := validatePort(opts.staticPort, "--static-port"); err != nil {
		return 0, err
	}
	if err := validatePort(opts.providerPort, "--provider-port"); err != nil {
		return 0, err
	}
	if timeout <= 0 {
		return 0, usagef("--timeout must be a positive number of seconds")
	}
	opts.timeout = time.Duration(timeout) * time.Second

	if len(positionals) > 2 {
		return 0, usagef("expected a model and optional legacy tag, got: %s", strings.Join(positionals, " "))
	}
	if len(positionals) > 0 {
		if opts.model != "" {
			return 0, usagef("model specified both positionally and with --model")
		}
		opts.model = positionals[0]
	}
	if len(positionals) == 2 {
		logf(opts, "ignoring release tag %s; serving the locally-built UI", positionals[1])
	}

	opts.serveOnly = opts.serveOnly || !allowModel
	if flavor == "alloy" && opts.model == "" {
		// The Alloy launcher historically served the UI alone, allowing an
		// externally started provider to connect. Keep that behavior when no
		// model is given.
		opts.serveOnly = true
	}
	if opts.serveOnly && opts.model != "" {
		return 0, usagef("--serve-only cannot be combined with a model")
	}
	if !opts.serveOnly && opts.model == "" {
		return 0, usagef("missing %s (or use --serve-only)", modelFileForFlavor())
	}
	opts.extraArgs = extraArgs

	staticDir, err := filepath.Abs(opts.staticDir)
	if err != nil {
		return 0, err
	}
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		return 0, fmt.Errorf("no index.html in static directory %s; build the project or pass --static-dir", staticDir)
	}

	var alloyWS *atomic.Int64
	if flavor == "alloy" {
		alloyWS = new(atomic.Int64)
	}

	server := &http.Server{Handler: staticHandler(staticDir, opts.verbose, alloyWS)}
	listener, err := net.Listen("tcp", net.JoinHostPort(opts.host, strconv.Itoa(opts.staticPort)))
	if err != nil {
		return 0, fmt.Errorf("could not listen on %s:%d: %w", opts.host, opts.staticPort, err)
	}
	staticPort = listener.Addr().(*net.TCPAddr).Port
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	if flavor == "alloy" {
		return runAlloy(opts, staticPort, staticDir, alloyWS)
	}

	if opts.serveOnly {
		if err := emitReady(opts, staticPort, nil, staticDir); err != nil {
			return 0, err
		}
		openBrowser(opts, visualizerURL(opts, staticPort, nil))
		waitForSignal()
		return 0, nil
	}

	providerPort = opts.providerPort
	if providerPort <= 0 {
		providerPort, err = freePort(opts.host)
		if err != nil {
			return 0, err
		}
	}

	child, err := startForge(opts, providerPort)
	if err != nil {
		return 0, err
	}

	sigCh, stopSignals := notifyShutdownSignals()
	defer stopSignals()
	go func() {
		sig := <-sigCh
		child.stop()
		server.Close()
		if s, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(s))
		}
		os.Exit(1)
	}()

	if err := waitForProvider(child, opts.host, providerPort, opts.timeout); err != nil {
		child.stop()
		return 0, err
	}

	if err := emitReady(opts, staticPort, &providerPort, staticDir); err != nil {
		child.stop()
		return 0, err
	}
	openBrowser(opts, visualizerURL(opts, staticPort, &providerPort))

	waitErr := child.wait()
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return exitErr.ExitCode(), nil
		}
		return 0, waitErr
	}
	return 0, nil
}

// parseFlags allows options before or after positional arguments, matching the
// launcher's historical command line.
func parseFlags(fs *flag.FlagSet, argv []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(argv); err != nil {
			return nil, err
		}
		args := fs.Args()
		if len(args) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, args[0])
		argv = args[1:]
	}
}

func splitExtraArgs(argv []string) ([]string, []string) {
	for i, arg := range argv {
		if arg == "--" {
			return argv[i+1:], argv[:i]
		}
	}
	return nil, argv
}

func startForge(opts options, providerPort int) (*forgeProcess, error) {
	args := []string{
		opts.model,
		"-O", "run_sterling",
		"serve",
		"-O", "sterling_port", strconv.Itoa(providerPort),
	}
	args = append(args, opts.extraArgs...)

	cmd := exec.Command(opts.racket, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	process := &forgeProcess{cmd: cmd, done: make(chan struct{})}
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		cmd.Stdin = os.Stdin
	} else {
		// Keep the pipe open until Forge exits so it does not see EOF and
		// immediately stop in non-interactive environments.
		stdin, stdinW, err := os.Pipe()
		if err == nil {
			cmd.Stdin = stdin
			process.stdin = stdin
			process.stdinW = stdinW
		}
	}

	if err := cmd.Start(); err != nil {
		process.closeStdin()
		return nil, fmt.Errorf("failed to start %s: %w", opts.racket, err)
	}

	go func() {
		process.waitErr = cmd.Wait()
		process.mu.Lock()
		process.state = cmd.ProcessState
		process.mu.Unlock()
		close(process.done)
		process.closeStdin()
	}()

	return process, nil
}

func (p *forgeProcess) closeStdin() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdinW != nil {
		_ = p.stdinW.Close()
	}
}

func (p *forgeProcess) stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *forgeProcess) processState() *os.ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *forgeProcess) wait() error {
	<-p.done
	return p.waitErr
}

func (p *forgeProcess) stop() {
	if p.stopped() {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
	}
}

func waitForProvider(child *forgeProcess, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if state := child.processState(); state != nil {
			return fmt.Errorf("Forge exited before Sterling came up (%s)", state)
		}
		if canConnect(host, port) {
			return nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %ds waiting for Forge on %s:%d", int(timeout.Seconds()), host, port)
}

func canConnect(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func freePort(host string) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func staticHandler(root string, verbose bool, alloyWS *atomic.Int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		name := path.Clean(r.URL.Path)
		if name == "." || name == "/" {
			name = "/index.html"
		}
		if strings.HasSuffix(name, "/") {
			name += "index.html"
		}

		filePath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
		rel, err := filepath.Rel(root, filePath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		info, err := os.Stat(filePath)
		if err == nil && info.IsDir() {
			filePath = filepath.Join(filePath, "index.html")
			info, err = os.Stat(filePath)
		}
		if err != nil || !info.Mode().IsRegular() {
			// Extensionless paths are treated as client-side routes.
			if filepath.Ext(name) == "" {
				filePath = filepath.Join(root, "index.html")
				info, err = os.Stat(filePath)
			}
		}
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		if verbose {
			if relative, err := filepath.Rel(root, filePath); err == nil {
				rel = relative
			}
			fmt.Fprintf(os.Stderr, "%s: %s %s -> %s\n", appName, r.Method, r.URL.Path, rel)
		}

		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		if alloyWS != nil && strings.HasSuffix(filePath, ".js") {
			data, err := os.ReadFile(filePath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if port := alloyWS.Load(); port > 0 {
				data = bytes.ReplaceAll(data,
					[]byte("ws://localhost:4000/alloy"),
					[]byte(fmt.Sprintf("ws://localhost:%d/alloy", port)))
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(data)
			return
		}

		file, err := os.Open(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		http.ServeContent(w, r, filepath.Base(filePath), info.ModTime(), file)
	})
}

func visualizerURL(opts options, staticPort int, providerPort *int) string {
	url := fmt.Sprintf("http://%s:%d/", opts.host, staticPort)
	if providerPort != nil {
		url += fmt.Sprintf("?%d", *providerPort)
	}
	return url
}

func emitReady(opts options, staticPort int, providerPort *int, staticDir string) error {
	url := visualizerURL(opts, staticPort, providerPort)

	payload := url
	if opts.json {
		info := struct {
			URL          string `json:"url"`
			Host         string `json:"host"`
			StaticPort   int    `json:"staticPort"`
			ProviderPort *int   `json:"providerPort"`
			Model        any    `json:"model"`
			StaticDir    string `json:"staticDir"`
		}{
			URL:          url,
			Host:         opts.host,
			StaticPort:   staticPort,
			ProviderPort: providerPort,
			StaticDir:    staticDir,
		}
		if opts.model != "" {
			info.Model = opts.model
		}
		data, err := json.Marshal(info)
		if err != nil {
			return err
		}
		payload = string(data)
	}

	fmt.Println(payload)

	if !opts.quiet {
		provider := ""
		if providerPort != nil {
			provider = fmt.Sprintf(" (provider port %d)", *providerPort)
		}
		fmt.Fprintf(os.Stderr, "%s: serving %s%s\n", appName, staticDir, provider)
		fmt.Fprintf(os.Stderr, "%s: open %s\n", appName, url)
	}

	if opts.readyFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(opts.readyFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(opts.readyFile, []byte(url+"\n"), 0o666)
}

// openBrowser starts the requested browser (or the platform default) without
// waiting for it. Browsers are a convenience, so failures are reported as
// warnings rather than terminating the server.
func openBrowser(opts options, url string) {
	if opts.noBrowser {
		return
	}

	program, args := browserCommand(opts.browser)
	if program == "" {
		logf(opts, "could not determine a browser; open %s manually", url)
		return
	}
	args = append(args, url)

	cmd := exec.Command(program, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logf(opts, "could not start browser %s: %v; open %s manually", program, err, url)
		return
	}
	// Reap the process in the background: a browser launcher such as xdg-open
	// may outlive us briefly, and we do not want its exit status to matter.
	go func() { _ = cmd.Wait() }()
}

func browserCommand(configured string) (string, []string) {
	if configured != "" {
		return configured, nil
	}

	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler"}
	}

	candidates := [][]string{
		{"xdg-open"},
		{"handlr", "open"},
		{"gio", "open"},
		{"sensible-browser"},
		{"x-www-browser"},
	}
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate[0], candidate[1:]
		}
	}
	return "", nil
}

func waitForSignal() {
	sigCh, stopSignals := notifyShutdownSignals()
	defer stopSignals()
	<-sigCh
}

func defaultStaticDir() string {
	if dir := envOr("STERLING_DIST", os.Getenv("STERLING_FORGE_DIST")); dir != "" {
		return dir
	}
	executable, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(executable)
	installed := filepath.Join(dir, "..", "share", appName)
	if exists(filepath.Join(installed, "index.html")) {
		return installed
	}
	source := filepath.Join(dir, "..", "dist")
	if exists(filepath.Join(source, "index.html")) {
		return source
	}
	return installed
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envModel() string {
	return envOr("STERLING_MODEL", os.Getenv("STERLING_FORGE_MODEL"))
}

func envPort(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 0 || port > 65535 {
		return 0, usagef("%s must be a port number between 0 and 65535", name)
	}
	return port, nil
}

func envPositiveInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, usagef("%s must be a positive number", name)
	}
	return number, nil
}

func validatePort(port int, name string) error {
	if port < 0 || port > 65535 {
		return usagef("%s must be a port number between 0 and 65535", name)
	}
	return nil
}

func defaultServeOnly() bool {
	return !allowModel || strings.EqualFold(os.Getenv("STERLING_SERVE_ONLY"), "1") ||
		strings.EqualFold(os.Getenv("STERLING_SERVE_ONLY"), "true")
}

func version() string {
	return envOr("STERLING_VERSION", envOr("STERLING_FORGE_VERSION", "unknown"))
}

func logf(opts options, format string, args ...any) {
	if opts.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", appName, fmt.Sprintf(format, args...))
}

func helpText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s [options]", appName)
	if allowModel {
		fmt.Fprintf(&b, " %s [TAG]", modelFileForFlavor())
	}
	fmt.Fprintf(&b, " [-- %s...]\n\n", extraArgsNameForFlavor())

	b.WriteString("Serve the locally-built Cope and Drag (Sterling) UI.\n")
	if allowModel {
		if flavor == "alloy" {
			b.WriteString("When a MODEL is supplied, start an Alloy Sterling provider and print the\n")
			b.WriteString("visualizer URL. Without a model, serve the UI for an external provider.\n")
		} else {
			b.WriteString("When a MODEL is supplied, start Forge in Sterling provider mode and print\n")
			b.WriteString("the visualizer URL.\n")
		}
	}
	b.WriteString("The URL is printed to stdout; diagnostics go to stderr.\n\n")

	b.WriteString("Options:\n")
	if allowModel {
		if flavor == "alloy" {
			b.WriteString("  -p, --provider-port PORT   Port for Alloy's Sterling provider (default: random)\n")
			b.WriteString("      --timeout SECONDS      How long to wait for Alloy commands (default: 120)\n")
			b.WriteString("      --alloy PATH           Alloy executable to use (default: alloy6)\n")
			b.WriteString("      --serve-only           Serve the UI without starting Alloy\n")
		} else {
			b.WriteString("  -p, --provider-port PORT   Port for Forge's Sterling provider (default: random)\n")
			b.WriteString("      --timeout SECONDS      How long to wait for Forge (default: 120)\n")
			b.WriteString("      --racket PATH          Racket executable to use (default: racket)\n")
			b.WriteString("      --serve-only           Serve the UI without starting Forge\n")
		}
		b.WriteString("      --model FILE           Model to run (alternative to a positional argument)\n")
	}
	b.WriteString("      --static-port PORT     Port for the static file server (default: random)\n")
	b.WriteString("      --host HOST            Address to bind (default: 127.0.0.1)\n")
	b.WriteString("      --static-dir DIR       Directory to serve (default: built-in dist)\n")
	b.WriteString("      --ready-file FILE      Write the visualizer URL to FILE when ready\n")
	b.WriteString("      --browser PROGRAM      Browser executable to open the UI with (default: system browser)\n")
	b.WriteString("      --no-browser           Do not open the UI in a browser\n")
	b.WriteString("      --json                 Print one JSON object on stdout instead of the URL\n")
	b.WriteString("  -q, --quiet                Suppress informational messages on stderr\n")
	b.WriteString("  -v, --verbose              Log HTTP requests\n")
	b.WriteString("  -h, --help                 Show this help\n")
	b.WriteString("  -V, --version              Show the version\n\n")

	b.WriteString("Environment:\n")
	b.WriteString("  STERLING_DIST              Same as --static-dir\n")
	if allowModel {
		b.WriteString("  STERLING_MODEL             Model to run when no positional argument is given\n")
		b.WriteString("  STERLING_PROVIDER_PORT     Same as --provider-port\n")
		b.WriteString("  STERLING_TIMEOUT           Same as --timeout\n")
		if flavor == "alloy" {
			b.WriteString("  STERLING_ALLOY             Same as --alloy\n")
		} else {
			b.WriteString("  STERLING_RACKET            Same as --racket\n")
		}
	}
	b.WriteString("  STERLING_STATIC_PORT       Same as --static-port\n")
	b.WriteString("  STERLING_HOST              Same as --host\n")
	b.WriteString("  STERLING_READY_FILE        Same as --ready-file\n")
	b.WriteString("  STERLING_BROWSER           Same as --browser\n")
	b.WriteString("  STERLING_APP_NAME          Name used in help and diagnostics\n")
	return b.String()
}

func defaultFlavor(name string) string {
	if strings.Contains(strings.ToLower(name), "alloy") {
		return "alloy"
	}
	return "forge"
}

func modelFileForFlavor() string {
	if flavor == "alloy" {
		return "MODEL.als"
	}
	return "MODEL.frg"
}

func extraArgsNameForFlavor() string {
	if flavor == "alloy" {
		return "ALLOY_ARGS"
	}
	return "RACKET_ARGS"
}
