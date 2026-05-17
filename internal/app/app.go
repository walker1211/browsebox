package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/walker1211/browsebox/internal/browser"
	"github.com/walker1211/browsebox/internal/mihomo"
	"github.com/walker1211/browsebox/internal/state"
)

const (
	managedByBrowsebox      = "browsebox"
	controllerLookupTimeout = 200 * time.Millisecond
	controllerDelayTimeout  = 6 * time.Second
	controllerReadyTimeout  = 5 * time.Second
	controllerReadyInterval = 25 * time.Millisecond
	selectNodeTimeout       = 2 * time.Second
	processStopTimeout      = 2 * time.Second
)

type process interface {
	PID() int
	Signal(os.Signal) error
	Kill() error
	Wait() error
}

type cmdProcess struct {
	cmd *exec.Cmd
}

type osProcess struct {
	process *os.Process
}

type processInfo struct {
	PID     int
	Owner   string
	Command string
}

func (p cmdProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p cmdProcess) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

func (p cmdProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p cmdProcess) Wait() error {
	return p.cmd.Wait()
}

func (p osProcess) PID() int {
	if p.process == nil {
		return 0
	}
	return p.process.Pid
}

func (p osProcess) Signal(sig os.Signal) error {
	if p.process == nil {
		return nil
	}
	return p.process.Signal(sig)
}

func (p osProcess) Kill() error {
	if p.process == nil {
		return nil
	}
	return p.process.Kill()
}

func (p osProcess) Wait() error {
	if p.process == nil {
		return nil
	}
	_, err := p.process.Wait()
	return err
}

var (
	readFile           = os.ReadFile
	writeRuntimeConfig = mihomo.WriteRuntimeConfig
	startMihomoProcess = func(ctx context.Context, binaryPath, runtimeDir, configPath string) (process, error) {
		cmd, err := mihomo.StartProcess(ctx, binaryPath, runtimeDir, configPath)
		if err != nil {
			return nil, err
		}
		return cmdProcess{cmd: cmd}, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		cmd, err := browser.StartChrome(ctx, chromePath, opts)
		if err != nil {
			return nil, err
		}
		return cmdProcess{cmd: cmd}, nil
	}
	findProcess = func(pid int) (process, error) {
		p, err := os.FindProcess(pid)
		if err != nil {
			return nil, err
		}
		return osProcess{process: p}, nil
	}
	inspectProcess = func(pid int) (processInfo, error) {
		cmd := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "uid=", "-o", "command=")
		out, err := cmd.Output()
		line := strings.TrimSpace(string(out))
		if err != nil || line == "" {
			return processInfo{}, os.ErrNotExist
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return processInfo{}, fmt.Errorf("process %d inspection missing owner or command", pid)
		}
		return processInfo{PID: pid, Owner: fields[0], Command: strings.TrimSpace(strings.TrimPrefix(line, fields[0]))}, nil
	}
	signalProcess = func(pid int, sig os.Signal) error {
		sysSig, ok := sig.(syscall.Signal)
		if !ok {
			return fmt.Errorf("unsupported signal %v", sig)
		}
		return syscall.Kill(pid, sysSig)
	}
	processAlive = func(pid int) (bool, error) {
		err := syscall.Kill(pid, 0)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	checkLocalPorts     = ensureLocalPortsAvailable
	currentProcessOwner = func() (string, error) {
		return strconv.Itoa(os.Geteuid()), nil
	}
)

// App executes browsebox commands without owning CLI parsing concerns.
type App struct {
	stdout io.Writer
	stderr io.Writer
}

// New creates an App that writes user-facing output to stdout and stderr.
func New(stdout, stderr io.Writer) *App {
	return &App{
		stdout: stdout,
		stderr: stderr,
	}
}

// Groups lists available proxy groups.
func (a *App) Groups(ctx context.Context, opts Options) error {
	client := mihomo.NewClient(opts.ControllerSocket)
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	groups, err := client.ProxyGroups(groupCtx)
	cancelGroup()
	if err != nil {
		return fmt.Errorf("list proxy groups: %w", err)
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "GROUP\tTYPE\tNODES"); err != nil {
		return err
	}
	for _, group := range groups {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\n", sanitizeDisplayName(group.Name), sanitizeDisplayName(group.Type), len(group.All)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// Nodes lists available proxy nodes.
func (a *App) Nodes(ctx context.Context, opts Options) error {
	client := mihomo.NewClient(opts.ControllerSocket)
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	group, err := client.ProxyGroup(groupCtx, opts.Group)
	cancelGroup()
	if err != nil {
		return fmt.Errorf("lookup proxy group %q: %w", opts.Group, err)
	}

	targetURL := opts.TargetURL
	if len(opts.HealthURLs) > 0 {
		targetURL = opts.HealthURLs[0]
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "NODE\tSTATUS\tDELAY"); err != nil {
		return err
	}
	for _, node := range group.All {
		displayName := sanitizeDisplayName(node)
		delayCtx, cancelDelay := context.WithTimeout(ctx, controllerDelayTimeout)
		delay, err := client.Delay(delayCtx, node, targetURL, 5000)
		cancelDelay()
		if err != nil {
			if _, writeErr := fmt.Fprintf(writer, "%s\tunhealthy\t-\n", displayName); writeErr != nil {
				return writeErr
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "%s\tok\t%dms\n", displayName, delay.Delay); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	return nil
}

// Run launches a temporary isolated browser session.
func (a *App) Run(ctx context.Context, opts Options) error {
	if err := requireNode(opts); err != nil {
		return err
	}

	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	childCtx, cancelChildren := context.WithCancel(signalCtx)
	defer cancelChildren()

	started, err := startSession(childCtx, signalCtx, opts)
	if err != nil {
		return err
	}
	defer stopProcess(started.chromeProcess)
	defer stopProcess(started.mihomoProcess)
	if !opts.Keep {
		defer os.RemoveAll(started.session.RuntimeDir)
	}

	if err := a.printRunEndpoints(opts, started.controllerURL, started.session.RuntimeDir); err != nil {
		return err
	}

	<-signalCtx.Done()
	return nil
}

// Start starts a persistent isolated browser session.
func (a *App) Start(ctx context.Context, opts Options) error {
	if err := requireNode(opts); err != nil {
		return err
	}

	if _, err := state.Load(opts.StateDir); err == nil {
		return fmt.Errorf("session already exists at %s; run `browsebox status` or `browsebox stop` before starting another session", state.Path(opts.StateDir))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load session state: %w", err)
	}

	started, err := startSession(ctx, ctx, opts)
	if err != nil {
		return err
	}
	if err := state.Save(opts.StateDir, started.session); err != nil {
		stopProcess(started.chromeProcess)
		stopProcess(started.mihomoProcess)
		if !opts.Keep {
			_ = os.RemoveAll(started.session.RuntimeDir)
		}
		return fmt.Errorf("save session state: %w", err)
	}
	return a.printRunEndpoints(opts, started.controllerURL, started.session.RuntimeDir)
}

// Status reports the current browsebox session status.
func (a *App) Status(ctx context.Context, opts Options) error {
	_ = ctx
	session, err := state.Load(opts.StateDir)
	if errors.Is(err, os.ErrNotExist) {
		_, writeErr := fmt.Fprintln(a.stdout, "No browsebox session is recorded.")
		return writeErr
	}
	if err != nil {
		return fmt.Errorf("load session state: %w", err)
	}
	return a.printSessionStatus(session)
}

// Stop stops a running browsebox session.
func (a *App) Stop(ctx context.Context, opts Options) error {
	_ = ctx
	session, err := state.Load(opts.StateDir)
	if errors.Is(err, os.ErrNotExist) {
		_, writeErr := fmt.Fprintln(a.stdout, "No browsebox session is recorded.")
		return writeErr
	}
	if err != nil {
		return fmt.Errorf("load session state: %w", err)
	}
	if session.ManagedBy != managedByBrowsebox {
		return fmt.Errorf("session state at %s is not managed by browsebox", state.Path(opts.StateDir))
	}
	if !opts.Keep && session.RuntimeDir != "" {
		if err := validateRuntimeDirForRemoval(session, opts.StateDir); err != nil {
			return err
		}
	}
	for _, target := range []managedProcessTarget{
		{name: "chrome", pid: session.ChromePID},
		{name: "mihomo", pid: session.MihomoPID},
	} {
		if target.pid <= 0 {
			continue
		}
		if err := stopManagedProcess(session, target); err != nil {
			return err
		}
	}
	if !opts.Keep && session.RuntimeDir != "" {
		if err := os.RemoveAll(session.RuntimeDir); err != nil {
			return fmt.Errorf("remove runtime dir: %w", err)
		}
	}
	if err := state.Remove(opts.StateDir); err != nil {
		return fmt.Errorf("remove session state: %w", err)
	}
	_, writeErr := fmt.Fprintln(a.stdout, "Browsebox session stopped")
	return writeErr
}

type startedSession struct {
	session       state.Session
	mihomoProcess process
	chromeProcess process
	controllerURL string
}

func startSession(processCtx, controlCtx context.Context, opts Options) (startedSession, error) {
	if err := checkLocalPorts(opts.ProxyPort, opts.ControllerPort, opts.DevToolsPort); err != nil {
		return startedSession{}, fmt.Errorf("check local ports: %w", err)
	}

	sourceConfig, err := readFile(opts.SourceConfigPath)
	if err != nil {
		return startedSession{}, fmt.Errorf("read source config: %w", err)
	}

	runtimeDir, err := createRuntimeDir(opts.RuntimeDir)
	if err != nil {
		return startedSession{}, fmt.Errorf("create runtime dir: %w", err)
	}
	cleanupRuntime := true
	defer func() {
		if cleanupRuntime && !opts.Keep {
			_ = os.RemoveAll(runtimeDir)
		}
	}()

	rewritten := mihomo.RewriteConfig(string(sourceConfig), mihomo.RuntimeConfigOptions{
		ProxyPort:      opts.ProxyPort,
		ControllerPort: opts.ControllerPort,
		Group:          opts.Group,
		Node:           opts.DefaultNode,
	})
	configPath, err := writeRuntimeConfig(runtimeDir, []byte(rewritten))
	if err != nil {
		return startedSession{}, fmt.Errorf("write runtime config: %w", err)
	}

	mihomoProcess, err := startMihomoProcess(processCtx, opts.MihomoBinaryPath, runtimeDir, configPath)
	if err != nil {
		return startedSession{}, fmt.Errorf("start mihomo: %w", err)
	}
	cleanupMihomo := true
	defer func() {
		if cleanupMihomo {
			stopProcess(mihomoProcess)
		}
	}()

	controllerURL := fmt.Sprintf("http://127.0.0.1:%d", opts.ControllerPort)
	tempClient := mihomo.NewTCPClient(controllerURL)
	if err := waitControllerReady(controlCtx, tempClient, opts.Group); err != nil {
		return startedSession{}, fmt.Errorf("wait for temp controller: %w", err)
	}
	selectCtx, cancelSelect := context.WithTimeout(controlCtx, selectNodeTimeout)
	selectErr := tempClient.SelectNode(selectCtx, opts.Group, opts.DefaultNode)
	cancelSelect()
	if selectErr != nil {
		return startedSession{}, fmt.Errorf("select temp node %q in group %q: %w", opts.DefaultNode, opts.Group, selectErr)
	}

	profileDir := filepath.Join(runtimeDir, "chrome-profile")
	chromeProcess, err := startChrome(processCtx, opts.ChromeBinaryPath, browser.Options{
		UserDataDir:  profileDir,
		ProxyPort:    opts.ProxyPort,
		DevToolsPort: opts.DevToolsPort,
		URL:          opts.TargetURL,
	})
	if err != nil {
		return startedSession{}, fmt.Errorf("start chrome: %w", err)
	}

	cleanupRuntime = false
	cleanupMihomo = false
	return startedSession{
		session: state.Session{
			ManagedBy:        managedByBrowsebox,
			MihomoPID:        mihomoProcess.PID(),
			ChromePID:        chromeProcess.PID(),
			ProxyPort:        opts.ProxyPort,
			ControllerPort:   opts.ControllerPort,
			DevToolsPort:     opts.DevToolsPort,
			RuntimeDir:       runtimeDir,
			ChromeDir:        profileDir,
			Group:            opts.Group,
			Node:             opts.DefaultNode,
			URL:              opts.TargetURL,
			StartedAt:        time.Now().UTC().Format(time.RFC3339),
			MihomoBinaryPath: opts.MihomoBinaryPath,
			ChromeBinaryPath: opts.ChromeBinaryPath,
		},
		mihomoProcess: mihomoProcess,
		chromeProcess: chromeProcess,
		controllerURL: controllerURL,
	}, nil
}

func requireNode(opts Options) error {
	if strings.TrimSpace(opts.DefaultNode) == "" {
		return errors.New("--node is required for run and start")
	}
	return nil
}

func ensureLocalPortsAvailable(ports ...int) error {
	seen := make(map[int]bool, len(ports))
	for _, port := range ports {
		if port <= 0 {
			return fmt.Errorf("invalid port %d", port)
		}
		if seen[port] {
			continue
		}
		seen[port] = true
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("127.0.0.1:%d is unavailable: %w", port, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("release 127.0.0.1:%d probe: %w", port, err)
		}
	}
	return nil
}

func createRuntimeDir(baseDir string) (string, error) {
	if baseDir == "" {
		return os.MkdirTemp("", fmt.Sprintf("browsebox-%d-", os.Getpid()))
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(baseDir, "browsebox-*")
}

func waitControllerReady(ctx context.Context, client *mihomo.Client, group string) error {
	ctx, cancel := context.WithTimeout(ctx, controllerReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(controllerReadyInterval)
	defer ticker.Stop()

	for {
		lookupCtx, cancelLookup := context.WithTimeout(ctx, controllerLookupTimeout)
		_, err := client.ProxyGroup(lookupCtx, group)
		cancelLookup()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
		}
	}
}

func (a *App) printRunEndpoints(opts Options, controllerURL, runtimeDir string) error {
	lines := []string{
		fmt.Sprintf("Proxy: http://127.0.0.1:%d", opts.ProxyPort),
		fmt.Sprintf("Controller: %s", controllerURL),
		fmt.Sprintf("DevTools: http://127.0.0.1:%d", opts.DevToolsPort),
		fmt.Sprintf("Selected: %s / %s", sanitizeDisplayName(opts.Group), sanitizeDisplayName(opts.DefaultNode)),
		fmt.Sprintf("Opened: %s", opts.TargetURL),
	}
	if opts.Keep {
		lines = append(lines, fmt.Sprintf("Cleanup: kept runtime dir %s", runtimeDir))
	} else {
		lines = append(lines, "Cleanup: runtime files will be removed on exit; use --keep to preserve them.")
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func processStatusText(pid int) string {
	if pid <= 0 {
		return "not recorded"
	}
	alive, err := processAlive(pid)
	if err != nil {
		return "unknown"
	}
	if alive {
		return "alive"
	}
	return "not running"
}

func (a *App) printSessionStatus(session state.Session) error {
	lines := []string{
		"Browsebox session recorded:",
		fmt.Sprintf("Mihomo PID: %d (%s)", session.MihomoPID, processStatusText(session.MihomoPID)),
		fmt.Sprintf("Chrome PID: %d (%s)", session.ChromePID, processStatusText(session.ChromePID)),
		fmt.Sprintf("Proxy: http://127.0.0.1:%d", session.ProxyPort),
		fmt.Sprintf("Controller: http://127.0.0.1:%d", session.ControllerPort),
		fmt.Sprintf("DevTools: http://127.0.0.1:%d", session.DevToolsPort),
		fmt.Sprintf("Selected: %s / %s", sanitizeDisplayName(session.Group), sanitizeDisplayName(session.Node)),
		fmt.Sprintf("Opened: %s", session.URL),
		fmt.Sprintf("Runtime: %s", session.RuntimeDir),
	}
	if session.ChromeDir != "" {
		lines = append(lines, fmt.Sprintf("Chrome profile: %s", session.ChromeDir))
	}
	if session.StartedAt != "" {
		lines = append(lines, fmt.Sprintf("Started: %s", session.StartedAt))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}

type managedProcessTarget struct {
	name string
	pid  int
}

func stopManagedProcess(session state.Session, target managedProcessTarget) error {
	info, err := inspectProcess(target.pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s process %d: %w", target.name, target.pid, err)
	}
	if err := validateProcessOwnership(info, target); err != nil {
		return err
	}
	if !processMatchesSession(info, session, target.name) {
		return fmt.Errorf("%s process %d does not match browsebox session", target.name, target.pid)
	}
	if err := signalProcess(target.pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal %s process %d: %w", target.name, target.pid, err)
	}
	deadline := time.Now().Add(processStopTimeout)
	for time.Now().Before(deadline) {
		alive, err := processAlive(target.pid)
		if err != nil {
			return fmt.Errorf("check %s process %d: %w", target.name, target.pid, err)
		}
		if !alive {
			return nil
		}
		time.Sleep(controllerReadyInterval)
	}
	info, err = inspectProcess(target.pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reinspect %s process %d before kill: %w", target.name, target.pid, err)
	}
	if err := validateProcessOwnership(info, target); err != nil {
		return err
	}
	if !processMatchesSession(info, session, target.name) {
		return fmt.Errorf("%s process %d does not match browsebox session before kill", target.name, target.pid)
	}
	if err := signalProcess(target.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill %s process %d: %w", target.name, target.pid, err)
	}
	return nil
}

func validateProcessOwnership(info processInfo, target managedProcessTarget) error {
	if info.Owner == "" {
		return fmt.Errorf("%s process %d owner cannot be verified", target.name, target.pid)
	}
	owner, err := currentProcessOwner()
	if err != nil || owner == "" {
		if err == nil {
			err = errors.New("empty current owner")
		}
		return fmt.Errorf("current process owner cannot be verified: %w", err)
	}
	if info.Owner != owner {
		return fmt.Errorf("%s process %d owner %q does not match current owner %q", target.name, target.pid, info.Owner, owner)
	}
	return nil
}

func processMatchesSession(info processInfo, session state.Session, name string) bool {
	command := info.Command
	switch name {
	case "chrome":
		return commandContainsPath(command, session.ChromeBinaryPath) && session.ChromeDir != "" && strings.Contains(command, session.ChromeDir)
	case "mihomo":
		return commandContainsPath(command, session.MihomoBinaryPath) && session.RuntimeDir != "" && strings.Contains(command, session.RuntimeDir)
	default:
		return false
	}
}

func commandContainsPath(command, path string) bool {
	if path == "" {
		return true
	}
	if strings.Contains(command, path) {
		return true
	}
	base := filepath.Base(path)
	return base != "." && base != string(filepath.Separator) && strings.Contains(command, base)
}

func validateRuntimeDirForRemoval(session state.Session, stateDir string) error {
	runtimeDir := filepath.Clean(session.RuntimeDir)
	if session.RuntimeDir != runtimeDir || !filepath.IsAbs(runtimeDir) {
		return fmt.Errorf("unsafe runtime dir %q: must be a clean absolute path", session.RuntimeDir)
	}
	matched, err := filepath.Match("browsebox-*", filepath.Base(runtimeDir))
	if err != nil || !matched {
		return fmt.Errorf("unsafe runtime dir %q: basename must match browsebox-*", session.RuntimeDir)
	}
	if runtimeDir == string(filepath.Separator) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove root", session.RuntimeDir)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && runtimeDir == filepath.Clean(home) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove home dir", session.RuntimeDir)
	}
	if stateDir != "" && runtimeDir == filepath.Clean(stateDir) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove state dir", session.RuntimeDir)
	}
	if session.ChromeDir != "" {
		chromeDir := filepath.Clean(session.ChromeDir)
		if session.ChromeDir != chromeDir || !filepath.IsAbs(chromeDir) {
			return fmt.Errorf("unsafe runtime dir %q: chrome dir must be a clean absolute path", session.RuntimeDir)
		}
		rel, err := filepath.Rel(runtimeDir, chromeDir)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return fmt.Errorf("unsafe runtime dir %q: chrome dir is outside runtime dir", session.RuntimeDir)
		}
	}
	return nil
}

func stopProcess(p process) {
	if p == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()
	_ = p.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(processStopTimeout):
		_ = p.Kill()
		<-done
	}
}

func sanitizeDisplayName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case 0x1b:
			b.WriteString(`\x1b`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
