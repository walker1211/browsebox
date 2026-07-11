package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/walker1211/browsebox/internal/browser"
	"github.com/walker1211/browsebox/internal/mihomo"
	"github.com/walker1211/browsebox/internal/state"
)

func TestDefaultOptionsUseAutoGroupAndNoPrivateNode(t *testing.T) {
	opts := DefaultOptions()

	if opts.Group != "" {
		t.Fatalf("default group = %q, want empty for auto resolution", opts.Group)
	}
	if opts.DefaultNode != "" {
		t.Fatalf("default node = %q, want empty", opts.DefaultNode)
	}
}

func TestRunRequiresNodeBeforeReadingConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = filepath.Join(t.TempDir(), "missing.yaml")
	opts.DefaultNode = ""

	err := application.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run returned nil error, want node validation error")
	}
	if !strings.Contains(err.Error(), "--node is required") {
		t.Fatalf("Run error = %q, want clear --node validation", err.Error())
	}
}

func TestStartRequiresNodeBeforeReadingConfigOrState(t *testing.T) {
	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = filepath.Join(t.TempDir(), "state")
	opts.SourceConfigPath = filepath.Join(t.TempDir(), "missing.yaml")
	opts.DefaultNode = ""

	err := application.Start(context.Background(), opts)
	if err == nil {
		t.Fatal("Start returned nil error, want node validation error")
	}
	if !strings.Contains(err.Error(), "--node is required") {
		t.Fatalf("Start error = %q, want clear --node validation", err.Error())
	}
}

func TestProxyStartsMihomoWithoutChromeAndCleansUpOnCancel(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	runtimeBaseDir := filepath.Join(tempDir, "runtime")

	mihomoProc := &recordingProcess{pid: 1111}
	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return mihomoProc, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		t.Fatal("Proxy must not start Chrome")
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.RuntimeDir = runtimeBaseDir
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ProxyPort = 17997
	opts.ControllerPort = controllerPort
	opts.HealthURLs = nil

	done := make(chan error, 1)
	go func() { done <- application.Proxy(ctx, opts) }()

	deadline := time.After(2 * time.Second)
	for !strings.Contains(stdout.String(), "Proxy: http://127.0.0.1:17997") {
		select {
		case err := <-done:
			t.Fatalf("Proxy exited before ready: %v", err)
		case <-deadline:
			t.Fatalf("stdout missing proxy endpoint:\n%s", stdout.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Proxy returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Proxy did not exit after context cancellation")
	}
	if !mihomoProc.signaled && !mihomoProc.killed {
		t.Fatal("Proxy did not stop mihomo")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestValidateLocalControllerURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "localhost", rawURL: "http://localhost:9097"},
		{name: "ipv4 localhost", rawURL: "http://127.0.0.1:9097"},
		{name: "ipv6 localhost", rawURL: "http://[::1]:9097"},
		{name: "reject invalid localhost port", rawURL: "http://localhost:badport", wantErr: true},
		{name: "reject zero localhost port", rawURL: "http://localhost:0", wantErr: true},
		{name: "reject out of range localhost port", rawURL: "http://localhost:99999", wantErr: true},
		{name: "reject https", rawURL: "https://localhost:9097", wantErr: true},
		{name: "reject remote host", rawURL: "http://example.com:9097", wantErr: true},
		{name: "reject localhost suffix", rawURL: "http://localhost.example.com:9097", wantErr: true},
		{name: "reject missing host", rawURL: "http:///missing-host", wantErr: true},
		{name: "reject malformed scheme", rawURL: "://bad", wantErr: true},
		{name: "reject localhost userinfo to remote host", rawURL: "http://localhost@evil.example:9097", wantErr: true},
		{name: "reject userinfo on localhost", rawURL: "http://user:pass@localhost:9097", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLocalControllerURL(tt.rawURL)
			if tt.wantErr && err == nil {
				t.Fatalf("validateLocalControllerURL(%q) returned nil error, want error", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateLocalControllerURL(%q) returned error: %v", tt.rawURL, err)
			}
		})
	}
}

func TestRunWritesRuntimeConfigSelectsNodeLaunchesChromeAndCleansUp(t *testing.T) {
	disableLocalPortCheck(t)
	var selectedBody string
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read select body: %v", err)
			}
			selectedBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\nallow-lan: true\ntun:\n  enable: true\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "geosite.dat"), []byte("geosite"), 0o600); err != nil {
		t.Fatalf("write geosite data: %v", err)
	}
	runtimeBaseDir := filepath.Join(tempDir, "runtime")

	var rewritten string
	var runtimeDir string
	oldWriteRuntimeConfig := writeRuntimeConfig
	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		writeRuntimeConfig = oldWriteRuntimeConfig
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	writeRuntimeConfig = func(dir string, content []byte) (string, error) {
		rewritten = string(content)
		return mihomo.WriteRuntimeConfig(dir, content)
	}
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		if binaryPath != "/bin/mihomo" {
			t.Fatalf("mihomo binary = %q", binaryPath)
		}
		runtimeDir = dir
		if !strings.HasPrefix(runtimeDir, runtimeBaseDir+string(os.PathSeparator)) {
			t.Fatalf("runtime dir = %q, want child under %q", runtimeDir, runtimeBaseDir)
		}
		copiedData, err := os.ReadFile(filepath.Join(runtimeDir, "geosite.dat"))
		if err != nil {
			t.Fatalf("read copied geosite data: %v", err)
		}
		if string(copiedData) != "geosite" {
			t.Fatalf("copied geosite data = %q, want geosite", string(copiedData))
		}
		return nopProcess{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var chromeOpts browser.Options
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		if chromePath != "/bin/chrome" {
			t.Fatalf("chrome path = %q", chromePath)
		}
		chromeOpts = opts
		cancel()
		return nopProcess{}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.RuntimeDir = runtimeBaseDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.MihomoInterfaceName = "en0"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ProxyPort = 17997
	opts.ControllerPort = controllerPort
	opts.DevToolsPort = 9333
	opts.TargetURL = "https://example.com/start"
	opts.HealthURLs = nil

	if err := application.Run(ctx, opts); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"mixed-port: 17997",
		"allow-lan: false",
		"external-controller: 127.0.0.1:" + portText,
		"interface-name: en0",
		"tun:\n  enable: false",
	} {
		if !strings.Contains(rewritten, want) {
			t.Fatalf("rewritten config missing %q:\n%s", want, rewritten)
		}
	}
	if selectedBody != `{"name":"node-a"}`+"\n" {
		t.Fatalf("selected body = %q, want node selection payload", selectedBody)
	}
	if chromeOpts.ProxyPort != 17997 || chromeOpts.DevToolsPort != 9333 || chromeOpts.URL != "https://example.com/start" {
		t.Fatalf("chrome opts = %#v", chromeOpts)
	}
	if !strings.HasPrefix(chromeOpts.UserDataDir, runtimeDir) {
		t.Fatalf("chrome profile dir = %q, want under runtime dir %q", chromeOpts.UserDataDir, runtimeDir)
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir still exists or stat failed: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"127.0.0.1:17997", "127.0.0.1:" + portText, "127.0.0.1:9333", "All", "node-a", "https://example.com/start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestPrepareMihomoDataFilesRefreshesCacheAndCopiesIntoRuntime(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	sourceConfigPath := filepath.Join(sourceDir, "config.yaml")
	if err := os.WriteFile(sourceConfigPath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "geosite.dat"), []byte("geosite-v1"), 0o600); err != nil {
		t.Fatalf("write source data: %v", err)
	}

	cacheDir := filepath.Join(tempDir, "cache")
	runtimeDir := filepath.Join(tempDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}
	if err := prepareMihomoDataFiles(sourceConfigPath, cacheDir, runtimeDir); err != nil {
		t.Fatalf("prepareMihomoDataFiles returned error: %v", err)
	}
	assertFileContent(t, filepath.Join(cacheDir, "geosite.dat"), "geosite-v1")
	assertFileContent(t, filepath.Join(runtimeDir, "geosite.dat"), "geosite-v1")
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("cache dir mode = %o, want 700", got)
		}
	}

	if err := os.WriteFile(filepath.Join(sourceDir, "geosite.dat"), []byte("geosite-v2-new"), 0o600); err != nil {
		t.Fatalf("update source data: %v", err)
	}
	runtimeDir = filepath.Join(tempDir, "runtime-second")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create second runtime dir: %v", err)
	}
	if err := prepareMihomoDataFiles(sourceConfigPath, cacheDir, runtimeDir); err != nil {
		t.Fatalf("prepareMihomoDataFiles after update returned error: %v", err)
	}
	assertFileContent(t, filepath.Join(cacheDir, "geosite.dat"), "geosite-v2-new")
	assertFileContent(t, filepath.Join(runtimeDir, "geosite.dat"), "geosite-v2-new")
}

func TestPrepareMihomoDataFilesSkipsSymlinkSourceData(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	sourceConfigPath := filepath.Join(sourceDir, "config.yaml")
	if err := os.WriteFile(sourceConfigPath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	secretPath := filepath.Join(tempDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secretPath, filepath.Join(sourceDir, "geosite.dat")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatalf("create source symlink: %v", err)
	}
	runtimeDir := filepath.Join(tempDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}

	if err := prepareMihomoDataFiles(sourceConfigPath, "", runtimeDir); err != nil {
		t.Fatalf("prepareMihomoDataFiles returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "geosite.dat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink source geosite.dat was copied or stat failed: %v", err)
	}
}

func TestPrepareMihomoDataFilesRefusesSymlinkCacheEntry(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	sourceConfigPath := filepath.Join(sourceDir, "config.yaml")
	if err := os.WriteFile(sourceConfigPath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "geosite.dat"), []byte("geosite"), 0o600); err != nil {
		t.Fatalf("write source data: %v", err)
	}
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	secretPath := filepath.Join(tempDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secretPath, filepath.Join(cacheDir, "geosite.dat")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatalf("create cache symlink: %v", err)
	}
	runtimeDir := filepath.Join(tempDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}

	err := prepareMihomoDataFiles(sourceConfigPath, cacheDir, runtimeDir)
	if err == nil || !strings.Contains(err.Error(), "cache geosite.dat") {
		t.Fatalf("prepareMihomoDataFiles error = %v, want cache geosite.dat error", err)
	}
	assertFileContent(t, secretPath, "secret")
}

func TestPrepareMihomoDataFilesUsesExistingCacheWhenSourceDataMissing(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	sourceConfigPath := filepath.Join(sourceDir, "config.yaml")
	if err := os.WriteFile(sourceConfigPath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "geosite.dat"), []byte("cached-geosite"), 0o600); err != nil {
		t.Fatalf("write cached data: %v", err)
	}
	runtimeDir := filepath.Join(tempDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}

	if err := prepareMihomoDataFiles(sourceConfigPath, cacheDir, runtimeDir); err != nil {
		t.Fatalf("prepareMihomoDataFiles returned error: %v", err)
	}
	assertFileContent(t, filepath.Join(runtimeDir, "geosite.dat"), "cached-geosite")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, string(content), want)
	}
}

type nopProcess struct{}

func (nopProcess) PID() int               { return 0 }
func (nopProcess) Signal(os.Signal) error { return nil }
func (nopProcess) Kill() error            { return nil }
func (nopProcess) Wait() error            { return nil }

func disableLocalPortCheck(t *testing.T) {
	t.Helper()
	oldCheckLocalPorts := checkLocalPorts
	checkLocalPorts = func(...int) error { return nil }
	t.Cleanup(func() { checkLocalPorts = oldCheckLocalPorts })
}

func TestRunUsesConfiguredChromeProfileDir(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	configuredProfileDir := filepath.Join(tempDir, "chrome-profile")

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return nopProcess{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var chromeOpts browser.Options
	var chromeProcessCtx context.Context
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		chromeProcessCtx = ctx
		chromeOpts = opts
		cancel()
		return nopProcess{}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ControllerPort = controllerPort
	opts.HealthURLs = nil
	opts.ChromeProfileDir = configuredProfileDir
	opts.BrowserHeadless = true
	opts.ChromeArgs = []string{"disable-background-networking"}

	if err := application.Run(ctx, opts); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if chromeOpts.UserDataDir != configuredProfileDir {
		t.Fatalf("Chrome profile dir = %q, want %q", chromeOpts.UserDataDir, configuredProfileDir)
	}
	if !chromeOpts.Headless {
		t.Fatal("Chrome headless option was not passed to browser launch")
	}
	if len(chromeOpts.ChromeArgs) != 1 || chromeOpts.ChromeArgs[0] != "disable-background-networking" {
		t.Fatalf("ChromeArgs = %#v, want configured arg", chromeOpts.ChromeArgs)
	}
	if err := chromeProcessCtx.Err(); err != nil {
		t.Fatalf("Chrome process context was canceled before graceful cleanup: %v", err)
	}
}

func TestStartSavesStateAndPrintsEndpointsWithoutStoppingProcesses(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	stateDir := filepath.Join(tempDir, "state")
	runtimeBaseDir := filepath.Join(tempDir, "runtime")

	mihomoProc := &recordingProcess{pid: 1111}
	chromeProc := &recordingProcess{pid: 2222}
	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return mihomoProc, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		return chromeProc, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.StateDir = stateDir
	opts.RuntimeDir = runtimeBaseDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ProxyPort = 17997
	opts.ControllerPort = controllerPort
	opts.DevToolsPort = 9333
	opts.TargetURL = "https://example.com/start"
	opts.HealthURLs = nil

	if err := application.Start(context.Background(), opts); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if mihomoProc.signaled || chromeProc.signaled || mihomoProc.killed || chromeProc.killed {
		t.Fatal("Start stopped child processes before returning")
	}

	session, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("load saved state: %v", err)
	}
	if session.ManagedBy != "browsebox" || session.MihomoPID != 1111 || session.ChromePID != 2222 {
		t.Fatalf("saved process metadata = %#v", session)
	}
	if session.ProxyPort != 17997 || session.ControllerPort != controllerPort || session.DevToolsPort != 9333 {
		t.Fatalf("saved ports = %#v", session)
	}
	if session.Group != "All" || session.Node != "node-a" || session.URL != "https://example.com/start" {
		t.Fatalf("saved selection metadata = %#v", session)
	}
	if !strings.HasPrefix(session.RuntimeDir, runtimeBaseDir+string(os.PathSeparator)) {
		t.Fatalf("runtime dir = %q, want child under %q", session.RuntimeDir, runtimeBaseDir)
	}
	if session.ChromeDir != filepath.Join(session.RuntimeDir, "chrome-profile") {
		t.Fatalf("chrome dir = %q, want profile under runtime dir", session.ChromeDir)
	}

	out := stdout.String()
	for _, want := range []string{"127.0.0.1:17997", "127.0.0.1:" + portText, "127.0.0.1:9333", "All", "node-a", "https://example.com/start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestStartSelectsFastestHealthyNodeWhenNodeOmitted(t *testing.T) {
	disableLocalPortCheck(t)
	requests := make(chan string, 1)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/XFLTD":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node","dead-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/slow-node/delay":
			if got := r.URL.Query().Get("url"); got != "https://x.com" {
				t.Fatalf("delay url query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":90}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/fast-node/delay":
			if got := r.URL.Query().Get("url"); got != "https://x.com" {
				t.Fatalf("delay url query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/XFLTD":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	stateDir := filepath.Join(tempDir, "state")

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return &recordingProcess{pid: 1111}, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		return &recordingProcess{pid: 2222}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.StateDir = stateDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "XFLTD"
	opts.DefaultNode = ""
	opts.SelectFastest = true
	opts.ControllerPort = controllerPort
	opts.TargetURL = "https://x.com"
	opts.HealthURLs = nil

	if err := application.Start(context.Background(), opts); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("selected node payload = %q, want fast-node", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start did not select the fastest healthy node")
	}

	session, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("load saved state: %v", err)
	}
	if session.Group != "XFLTD" || session.Node != "fast-node" {
		t.Fatalf("saved selection metadata = %#v", session)
	}
	if !strings.Contains(stdout.String(), "fast-node") {
		t.Fatalf("stdout missing selected node:\n%s", stdout.String())
	}
}

func TestStartAutoResolvesCurrentProxyGroupWhenGroupEmpty(t *testing.T) {
	disableLocalPortCheck(t)
	requests := make(chan string, 1)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"All":{"name":"All","type":"Selector","all":["DIRECT","XFLTD"],"now":"XFLTD"},"XFLTD":{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/XFLTD":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			if got := r.URL.Query().Get("url"); got != "https://x.com" {
				t.Fatalf("delay url query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":90}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			if got := r.URL.Query().Get("url"); got != "https://x.com" {
				t.Fatalf("delay url query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/XFLTD":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	stateDir := filepath.Join(tempDir, "state")

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return &recordingProcess{pid: 1111}, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		return &recordingProcess{pid: 2222}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerURL = controller.URL
	opts.SourceConfigPath = sourcePath
	opts.StateDir = stateDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = ""
	opts.DefaultNode = ""
	opts.SelectFastest = true
	opts.ControllerPort = controllerPort
	opts.TargetURL = "https://x.com"
	opts.HealthURLs = nil

	if err := application.Start(context.Background(), opts); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("selected node payload = %q, want fast-node", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start did not select the fastest healthy node in the auto-resolved group")
	}

	session, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("load saved state: %v", err)
	}
	if session.Group != "XFLTD" || session.Node != "fast-node" {
		t.Fatalf("saved selection metadata = %#v", session)
	}
	if !strings.Contains(stdout.String(), "XFLTD") || !strings.Contains(stdout.String(), "fast-node") {
		t.Fatalf("stdout missing selected group or node:\n%s", stdout.String())
	}
}

func TestStartRefusesExistingSessionState(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	if err := state.Save(stateDir, state.Session{ManagedBy: "browsebox", MihomoPID: 1}); err != nil {
		t.Fatalf("save existing state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir
	opts.DefaultNode = "node-a"

	err := application.Start(context.Background(), opts)
	if err == nil {
		t.Fatal("Start returned nil error, want existing-session error")
	}
	for _, want := range []string{"session already exists", "browsebox status", "browsebox stop"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Start error = %q, want %q", err.Error(), want)
		}
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q, want no output", stdout.String(), stderr.String())
	}
}

func TestStatusReportsMissingAndExistingSession(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		application := New(&stdout, &stderr)
		opts := DefaultOptions()
		opts.StateDir = filepath.Join(t.TempDir(), "state")

		if err := application.Status(context.Background(), opts); err != nil {
			t.Fatalf("Status returned error: %v", err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
		if !strings.Contains(stdout.String(), "No browsebox session") {
			t.Fatalf("stdout = %q, want no-session message", stdout.String())
		}
	})

	t.Run("existing", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "state")
		want := state.Session{
			ManagedBy:      "browsebox",
			MihomoPID:      1111,
			ChromePID:      2222,
			ProxyPort:      17997,
			ControllerPort: 17998,
			DevToolsPort:   9333,
			RuntimeDir:     "/tmp/browsebox-runtime",
			ChromeDir:      "/tmp/browsebox-runtime/chrome-profile",
			Group:          "All",
			Node:           "node-a",
			URL:            "https://example.com/start",
			StartedAt:      "2026-05-17T10:11:12Z",
		}
		if err := state.Save(stateDir, want); err != nil {
			t.Fatalf("save state: %v", err)
		}

		var stdout, stderr bytes.Buffer
		application := New(&stdout, &stderr)
		opts := DefaultOptions()
		opts.StateDir = stateDir

		if err := application.Status(context.Background(), opts); err != nil {
			t.Fatalf("Status returned error: %v", err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
		out := stdout.String()
		for _, want := range []string{"1111", "2222", "127.0.0.1:17997", "127.0.0.1:17998", "127.0.0.1:9333", "All", "node-a", "https://example.com/start", "/tmp/browsebox-runtime"} {
			if !strings.Contains(out, want) {
				t.Fatalf("status output missing %q:\n%s", want, out)
			}
		}
	})
}

func TestStatusReportsRecordedProcessLiveness(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := state.Save(stateDir, state.Session{ManagedBy: "browsebox", MihomoPID: 1111, ChromePID: 2222}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	oldProcessAlive := processAlive
	processAlive = func(pid int) (bool, error) {
		switch pid {
		case 1111:
			return true, nil
		case 2222:
			return false, nil
		default:
			t.Fatalf("unexpected liveness check for pid %d", pid)
			return false, nil
		}
	}
	t.Cleanup(func() { processAlive = oldProcessAlive })

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	if err := application.Status(context.Background(), opts); err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Mihomo PID: 1111 (alive)", "Chrome PID: 2222 (not running)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestRunChecksHealthURLsBeforeLaunchingChrome(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/node-a/delay":
			if got := r.URL.Query().Get("url"); got != "https://health.example/ping" {
				t.Fatalf("health url query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":123}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		return nopProcess{}, nil
	}
	chromeStarted := false
	ctx, cancel := context.WithCancel(context.Background())
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		chromeStarted = true
		cancel()
		return nopProcess{}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ControllerPort = controllerPort
	opts.HealthURLs = []string{"https://health.example/ping"}

	if err := application.Run(ctx, opts); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !chromeStarted {
		t.Fatal("Run did not launch Chrome after passing health check")
	}
}

func TestRunStopsBeforeLaunchingChromeWhenHealthCheckFails(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/node-a/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	runtimeBaseDir := filepath.Join(tempDir, "runtime")

	mihomoProc := &recordingProcess{pid: 1111}
	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	var runtimeDir string
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		runtimeDir = dir
		return mihomoProc, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		t.Fatal("Chrome should not start when health check fails")
		return nil, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.RuntimeDir = runtimeBaseDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ControllerPort = controllerPort
	opts.HealthURLs = []string{"https://health.example/ping"}

	err = application.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run returned nil error, want health-check error")
	}
	if !strings.Contains(err.Error(), "health check") || !strings.Contains(err.Error(), "https://health.example/ping") {
		t.Fatalf("Run error = %q, want health-check context", err.Error())
	}
	if !mihomoProc.signaled {
		t.Fatal("temporary mihomo process was not stopped after health-check failure")
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Fatalf("runtime dir still exists after health-check failure or stat failed: %v", err)
	}
}

func TestRunReportsUnavailableLocalPortBeforeReadingConfig(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on busy port: %v", err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split busy port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse busy port: %v", err)
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = filepath.Join(t.TempDir(), "missing.yaml")
	opts.DefaultNode = "node-a"
	opts.ProxyPort = port

	err = application.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run returned nil error, want unavailable-port error")
	}
	if !strings.Contains(err.Error(), "check local ports") || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("Run error = %q, want unavailable-port context", err.Error())
	}
	if strings.Contains(err.Error(), "read source config") {
		t.Fatalf("Run error = %q, port check should happen before reading config", err.Error())
	}
}

func TestStopMissingSessionPrintsNoSession(t *testing.T) {
	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = filepath.Join(t.TempDir(), "state")

	if err := application.Stop(context.Background(), opts); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "No browsebox session") {
		t.Fatalf("stdout = %q, want no-session message", stdout.String())
	}
}

func TestStopRefusesStateNotManagedByBrowsebox(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := state.Save(stateDir, state.Session{ManagedBy: "other-tool", MihomoPID: 1111, ChromePID: 2222}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	oldFindProcess := findProcess
	t.Cleanup(func() { findProcess = oldFindProcess })
	findProcess = func(pid int) (process, error) {
		t.Fatalf("findProcess should not be called for non-browsebox state; got PID %d", pid)
		return nil, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	err := application.Stop(context.Background(), opts)
	if err == nil {
		t.Fatal("Stop returned nil error, want managed-by validation error")
	}
	if !strings.Contains(err.Error(), "not managed by browsebox") {
		t.Fatalf("Stop error = %q, want managed-by validation", err.Error())
	}
}

func TestStopTreatsMissingRecordedPIDsAsStaleWithoutSignaling(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	runtimeChild := filepath.Join(tempDir, "runtime", "browsebox-stale")
	chromeDir := filepath.Join(runtimeChild, "chrome-profile")
	if err := os.MkdirAll(chromeDir, 0o700); err != nil {
		t.Fatalf("create chrome dir: %v", err)
	}
	if err := state.Save(stateDir, state.Session{
		ManagedBy:        "browsebox",
		MihomoPID:        1111,
		ChromePID:        2222,
		RuntimeDir:       runtimeChild,
		ChromeDir:        chromeDir,
		MihomoBinaryPath: "/bin/mihomo",
		ChromeBinaryPath: "/bin/chrome",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
	})
	inspectProcess = func(pid int) (processInfo, error) {
		return processInfo{}, os.ErrNotExist
	}
	signalProcess = func(pid int, sig os.Signal) error {
		t.Fatalf("signalProcess should not be called for stale missing PID %d", pid)
		return nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	if err := application.Stop(context.Background(), opts); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(state.Path(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("state file still exists or stat failed: %v", err)
	}
}

func TestStopRefusesIdentityMismatchWithoutSignaling(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	runtimeChild := filepath.Join(tempDir, "runtime", "browsebox-owned")
	chromeDir := filepath.Join(runtimeChild, "chrome-profile")
	if err := os.MkdirAll(chromeDir, 0o700); err != nil {
		t.Fatalf("create chrome dir: %v", err)
	}
	if err := state.Save(stateDir, state.Session{
		ManagedBy:        "browsebox",
		MihomoPID:        1111,
		ChromePID:        2222,
		RuntimeDir:       runtimeChild,
		ChromeDir:        chromeDir,
		MihomoBinaryPath: "/bin/mihomo",
		ChromeBinaryPath: "/bin/chrome",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
	})
	inspectProcess = func(pid int) (processInfo, error) {
		return processInfo{PID: pid, Owner: strconv.Itoa(os.Geteuid()), Command: "/usr/bin/unrelated --flag"}, nil
	}
	signalProcess = func(pid int, sig os.Signal) error {
		t.Fatalf("signalProcess should not be called for mismatched PID %d", pid)
		return nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	err := application.Stop(context.Background(), opts)
	if err == nil {
		t.Fatal("Stop returned nil error, want identity validation error")
	}
	if !strings.Contains(err.Error(), "does not match browsebox session") {
		t.Fatalf("Stop error = %q, want identity mismatch", err.Error())
	}
	if _, err := os.Stat(state.Path(stateDir)); err != nil {
		t.Fatalf("state file should remain after identity mismatch: %v", err)
	}
}

func TestStopRevalidatesIdentityBeforeEscalatingToKill(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "browsebox-owned")
	chromeDir := filepath.Join(runtimeDir, "chrome-profile")
	session := state.Session{
		ManagedBy:        "browsebox",
		ChromePID:        2222,
		RuntimeDir:       runtimeDir,
		ChromeDir:        chromeDir,
		ChromeBinaryPath: "/bin/chrome",
	}

	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	oldProcessAlive := processAlive
	oldCurrentProcessOwner := currentProcessOwner
	oldChromeStopTimeout := chromeStopTimeout
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
		processAlive = oldProcessAlive
		currentProcessOwner = oldCurrentProcessOwner
		chromeStopTimeout = oldChromeStopTimeout
	})
	chromeStopTimeout = 25 * time.Millisecond
	currentProcessOwner = func() (string, error) { return "501", nil }
	inspectCalls := 0
	inspectProcess = func(pid int) (processInfo, error) {
		inspectCalls++
		if inspectCalls == 1 {
			return processInfo{PID: pid, Owner: "501", Command: "/bin/chrome --user-data-dir=" + chromeDir}, nil
		}
		return processInfo{PID: pid, Owner: "501", Command: "/usr/bin/unrelated --flag"}, nil
	}
	signals := []os.Signal{}
	signalProcess = func(pid int, sig os.Signal) error {
		signals = append(signals, sig)
		return nil
	}
	processAlive = func(pid int) (bool, error) { return true, nil }

	err := stopManagedProcess(session, managedProcessTarget{name: "chrome", pid: 2222})
	if err == nil {
		t.Fatal("stopManagedProcess returned nil error, want revalidation mismatch")
	}
	if !strings.Contains(err.Error(), "does not match browsebox session") {
		t.Fatalf("error = %q, want identity mismatch", err.Error())
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %#v, want only SIGTERM and no SIGKILL", signals)
	}
	if inspectCalls < 2 {
		t.Fatalf("inspect calls = %d, want reinspection before SIGKILL", inspectCalls)
	}
}

func TestStopRefusesOwnerMismatchWithoutSignaling(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "browsebox-owned")
	chromeDir := filepath.Join(runtimeDir, "chrome-profile")
	session := state.Session{
		ManagedBy:        "browsebox",
		ChromePID:        2222,
		RuntimeDir:       runtimeDir,
		ChromeDir:        chromeDir,
		ChromeBinaryPath: "/bin/chrome",
	}

	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	oldCurrentProcessOwner := currentProcessOwner
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
		currentProcessOwner = oldCurrentProcessOwner
	})
	currentProcessOwner = func() (string, error) { return "501", nil }
	inspectProcess = func(pid int) (processInfo, error) {
		return processInfo{PID: pid, Owner: "0", Command: "/bin/chrome --user-data-dir=" + chromeDir}, nil
	}
	signalProcess = func(pid int, sig os.Signal) error {
		t.Fatalf("signalProcess should not be called for owner mismatch; pid=%d sig=%v", pid, sig)
		return nil
	}

	err := stopManagedProcess(session, managedProcessTarget{name: "chrome", pid: 2222})
	if err == nil {
		t.Fatal("stopManagedProcess returned nil error, want owner mismatch")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Fatalf("error = %q, want owner mismatch", err.Error())
	}
}

func TestStopRefusesUnsafeRuntimeDirAndPreservesState(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	unsafeDir := filepath.Join(tempDir, "user-data")
	if err := os.MkdirAll(unsafeDir, 0o700); err != nil {
		t.Fatalf("create unsafe dir: %v", err)
	}
	sentinelPath := filepath.Join(unsafeDir, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("do not delete"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if err := state.Save(stateDir, state.Session{ManagedBy: "browsebox", RuntimeDir: unsafeDir}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	err := application.Stop(context.Background(), opts)
	if err == nil {
		t.Fatal("Stop returned nil error, want unsafe runtime dir error")
	}
	if !strings.Contains(err.Error(), "unsafe runtime dir") {
		t.Fatalf("Stop error = %q, want unsafe runtime dir", err.Error())
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("unsafe dir sentinel should survive: %v", err)
	}
	if _, err := os.Stat(state.Path(stateDir)); err != nil {
		t.Fatalf("state file should remain after unsafe runtime dir: %v", err)
	}
}

func TestStopAllowsChromeProfileOutsideRuntimeDir(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	runtimeChild := filepath.Join(tempDir, "runtime-base", "browsebox-child")
	chromeDir := filepath.Join(tempDir, "profiles", "walker")
	if err := os.MkdirAll(runtimeChild, 0o700); err != nil {
		t.Fatalf("create runtime child: %v", err)
	}
	if err := os.MkdirAll(chromeDir, 0o700); err != nil {
		t.Fatalf("create chrome dir: %v", err)
	}
	profileSentinel := filepath.Join(chromeDir, "sentinel.txt")
	if err := os.WriteFile(profileSentinel, []byte("keep profile"), 0o600); err != nil {
		t.Fatalf("write profile sentinel: %v", err)
	}
	if err := state.Save(stateDir, state.Session{
		ManagedBy:        "browsebox",
		MihomoPID:        1111,
		ChromePID:        2222,
		RuntimeDir:       runtimeChild,
		ChromeDir:        chromeDir,
		MihomoBinaryPath: "/bin/mihomo",
		ChromeBinaryPath: "/bin/chrome",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	oldProcessAlive := processAlive
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
		processAlive = oldProcessAlive
	})
	inspectProcess = func(pid int) (processInfo, error) {
		switch pid {
		case 1111:
			return processInfo{PID: pid, Owner: strconv.Itoa(os.Geteuid()), Command: "/bin/mihomo -d " + runtimeChild}, nil
		case 2222:
			return processInfo{PID: pid, Owner: strconv.Itoa(os.Geteuid()), Command: "/bin/chrome --user-data-dir=" + chromeDir}, nil
		default:
			t.Fatalf("unexpected PID inspection: %d", pid)
			return processInfo{}, os.ErrNotExist
		}
	}
	signalProcess = func(pid int, sig os.Signal) error { return nil }
	processAlive = func(pid int) (bool, error) { return false, nil }

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	if err := application.Stop(context.Background(), opts); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(runtimeChild); !os.IsNotExist(err) {
		t.Fatalf("runtime dir still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(profileSentinel); err != nil {
		t.Fatalf("external chrome profile should survive stop: %v", err)
	}
}

func TestStopSignalsRecordedPIDsRemovesStateAndRuntimeChild(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	runtimeBase := filepath.Join(tempDir, "runtime-base")
	runtimeChild := filepath.Join(runtimeBase, "browsebox-child")
	if err := os.MkdirAll(runtimeChild, 0o700); err != nil {
		t.Fatalf("create runtime child: %v", err)
	}
	sentinelPath := filepath.Join(runtimeBase, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep parent"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	chromeDir := filepath.Join(runtimeChild, "chrome-profile")
	if err := os.MkdirAll(chromeDir, 0o700); err != nil {
		t.Fatalf("create chrome dir: %v", err)
	}
	if err := state.Save(stateDir, state.Session{
		ManagedBy:        "browsebox",
		MihomoPID:        1111,
		ChromePID:        2222,
		RuntimeDir:       runtimeChild,
		ChromeDir:        chromeDir,
		MihomoBinaryPath: "/bin/mihomo",
		ChromeBinaryPath: "/bin/chrome",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	signaled := map[int]os.Signal{}
	oldInspectProcess := inspectProcess
	oldSignalProcess := signalProcess
	oldProcessAlive := processAlive
	t.Cleanup(func() {
		inspectProcess = oldInspectProcess
		signalProcess = oldSignalProcess
		processAlive = oldProcessAlive
	})
	inspectProcess = func(pid int) (processInfo, error) {
		switch pid {
		case 1111:
			return processInfo{PID: pid, Owner: strconv.Itoa(os.Geteuid()), Command: "/bin/mihomo -d " + runtimeChild + " -f " + filepath.Join(runtimeChild, "config.yaml")}, nil
		case 2222:
			return processInfo{PID: pid, Owner: strconv.Itoa(os.Geteuid()), Command: "/bin/chrome --user-data-dir=" + chromeDir}, nil
		default:
			t.Fatalf("unexpected PID inspection: %d", pid)
			return processInfo{}, os.ErrNotExist
		}
	}
	signalProcess = func(pid int, sig os.Signal) error {
		signaled[pid] = sig
		return nil
	}
	processAlive = func(pid int) (bool, error) { return false, nil }

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir

	if err := application.Stop(context.Background(), opts); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if signaled[1111] != syscall.SIGTERM || signaled[2222] != syscall.SIGTERM {
		t.Fatalf("recorded processes were not sent SIGTERM: %#v", signaled)
	}
	if _, ok := signaled[3333]; ok {
		t.Fatal("unrecorded process was signaled")
	}
	if _, err := os.Stat(state.Path(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("state file still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(runtimeChild); !os.IsNotExist(err) {
		t.Fatalf("runtime child still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("parent sentinel should survive stop: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Browsebox session stopped") {
		t.Fatalf("stdout = %q, want stopped message", stdout.String())
	}
}

func TestStopKeepPreservesRuntimeDir(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	runtimeChild := filepath.Join(tempDir, "runtime", "browsebox-child")
	if err := os.MkdirAll(runtimeChild, 0o700); err != nil {
		t.Fatalf("create runtime child: %v", err)
	}
	if err := state.Save(stateDir, state.Session{ManagedBy: "browsebox", RuntimeDir: runtimeChild}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.StateDir = stateDir
	opts.Keep = true

	if err := application.Stop(context.Background(), opts); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(runtimeChild); err != nil {
		t.Fatalf("runtime child should survive keep=true: %v", err)
	}
	if _, err := os.Stat(state.Path(stateDir)); !os.IsNotExist(err) {
		t.Fatalf("state file still exists or stat failed: %v", err)
	}
}

type recordingProcess struct {
	pid      int
	signaled bool
	killed   bool
}

func (p *recordingProcess) PID() int               { return p.pid }
func (p *recordingProcess) Signal(os.Signal) error { p.signaled = true; return nil }
func (p *recordingProcess) Kill() error            { p.killed = true; return nil }
func (p *recordingProcess) Wait() error            { return nil }

func TestRunRemovesOnlyCreatedRuntimeChildWhenRuntimeDirBaseProvided(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	baseDir := filepath.Join(tempDir, "user-supplied-base")
	if err := os.Mkdir(baseDir, 0o700); err != nil {
		t.Fatalf("create base dir: %v", err)
	}
	sentinelPath := filepath.Join(baseDir, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("do not delete"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	var childRuntimeDir string
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		childRuntimeDir = dir
		if childRuntimeDir == baseDir {
			t.Fatalf("runtime dir = base dir %q, want private child", baseDir)
		}
		if !strings.HasPrefix(childRuntimeDir, baseDir+string(os.PathSeparator)) {
			t.Fatalf("runtime dir = %q, want child under base %q", childRuntimeDir, baseDir)
		}
		return nopProcess{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		cancel()
		return nopProcess{}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.RuntimeDir = baseDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ControllerPort = controllerPort
	opts.HealthURLs = nil

	if err := application.Run(ctx, opts); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("sentinel should survive cleanup: %v", err)
	}
	if _, err := os.Stat(childRuntimeDir); !os.IsNotExist(err) {
		t.Fatalf("created runtime child still exists or stat failed: %v", err)
	}
}

func TestRunKeepsRuntimeDirWhenKeepIsTrue(t *testing.T) {
	disableLocalPortCheck(t)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["node-a"],"now":"node-a"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(controller.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(controller.URL, "http://"))
	if err != nil {
		t.Fatalf("split controller URL: %v", err)
	}
	controllerPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse controller port: %v", err)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.yaml")
	if err := os.WriteFile(sourcePath, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	runtimeDir := filepath.Join(tempDir, "runtime")

	oldStartProcess := startMihomoProcess
	oldStartChrome := startChrome
	t.Cleanup(func() {
		startMihomoProcess = oldStartProcess
		startChrome = oldStartChrome
	})
	var createdRuntimeDir string
	startMihomoProcess = func(ctx context.Context, binaryPath, dir, configPath string) (process, error) {
		createdRuntimeDir = dir
		return nopProcess{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		cancel()
		return nopProcess{}, nil
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.SourceConfigPath = sourcePath
	opts.RuntimeDir = runtimeDir
	opts.MihomoBinaryPath = "/bin/mihomo"
	opts.ChromeBinaryPath = "/bin/chrome"
	opts.Keep = true
	opts.Group = "All"
	opts.DefaultNode = "node-a"
	opts.ControllerPort = controllerPort
	opts.HealthURLs = nil

	if err := application.Run(ctx, opts); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("runtime base dir stat after Run with keep=true: %v", err)
	}
	if _, err := os.Stat(createdRuntimeDir); err != nil {
		t.Fatalf("created runtime dir stat after Run with keep=true: %v", err)
	}
}

func TestNodesUsesOnlyGETAndPrintsCandidateDelays(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["fast-node","slow-node"]}`))
		case "/proxies/fast-node/delay":
			if r.URL.Query().Get("url") != "https://health.example/ping" {
				t.Fatalf("fast-node health url = %q", r.URL.Query().Get("url"))
			}
			if r.URL.Query().Get("timeout") != "5000" {
				t.Fatalf("fast-node timeout = %q", r.URL.Query().Get("timeout"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":42}`))
		case "/proxies/slow-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.TargetURL = "https://target.example"
	opts.ShowUnhealthyNodes = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"NODE", "STATUS", "DELAY", "fast-node", "ok", "42ms", "slow-node", "unhealthy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nodes output missing %q:\n%s", want, out)
		}
	}
}

func TestNodesHidesUnhealthyByDefaultAndPrintsSummary(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["slow-node","dead-node","fast-node"]}`))
		case "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":90}`))
		case "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 3

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"nodes: total 3, ok 2, shown 2", "NODE", "STATUS", "DELAY", "fast-node", "ok", "10ms", "slow-node", "90ms"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nodes output missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"dead-node", "unhealthy"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("nodes output contains hidden unhealthy value %q:\n%s", unwanted, out)
		}
	}
}

func TestNodesHighlightsVisibleCurrentNode(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","now":"current-node","all":["other-node","current-node"]}`))
		case "/proxies/other-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":30}`))
		case "/proxies/current-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "\x1b[1;36mcurrent-node") {
		t.Fatalf("nodes output did not color current node:\n%q", out)
	}
	if !strings.Contains(out, "-\x1b[0m") {
		t.Fatalf("nodes output did not reset color after current row:\n%q", out)
	}
	if strings.Contains(out, "CURRENT") {
		t.Fatalf("nodes output added unexpected CURRENT column:\n%s", out)
	}
}

func TestNodesCanDisableCurrentNodeHighlight(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","now":"current-node","all":["current-node"]}`))
		case "/proxies/current-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.HighlightCurrentNode = false

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("nodes output unexpectedly contains ANSI styling:\n%q", out)
	}
	if !strings.Contains(out, "current-node") {
		t.Fatalf("nodes output missing current node:\n%s", out)
	}
}

func TestNodesDoesNotShowHiddenUnhealthyCurrentNode(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","now":"dead-node","all":["fast-node","dead-node"]}`))
		case "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "nodes: total 2, ok 1, shown 1") {
		t.Fatalf("nodes output missing summary:\n%s", out)
	}
	if strings.Contains(out, "dead-node") {
		t.Fatalf("nodes output contains hidden unhealthy current node:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("nodes output unexpectedly contains ANSI styling:\n%q", out)
	}
}

func TestNodesShowUnhealthyOptionPreservesUnhealthyRows(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["fast-node","dead-node"]}`))
		case "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.ShowUnhealthyNodes = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"nodes: total 2, ok 1, shown 2", "fast-node", "ok", "10ms", "dead-node", "unhealthy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nodes output missing %q:\n%s", want, out)
		}
	}
}

func TestNodesAlignsStatusColumnForWideNodeNames(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["plain","香港节点","dead"]}`))
		case "/proxies/plain/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":12}`))
		case "/proxies/香港节点/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":7}`))
		case "/proxies/dead/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 3
	opts.ShowUnhealthyNodes = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	want := "nodes: total 3, ok 2, shown 3\n" +
		"NODE      STATUS     DELAY  CHECKS\n" +
		"香港节点  ok         7ms    -\n" +
		"plain     ok         12ms   -\n" +
		"dead      unhealthy  -      health:unavailable\n"
	if got := stdout.String(); got != want {
		t.Fatalf("nodes output mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestNodesSortsHealthyDelaysAscendingAndUnhealthyLast(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.Path {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["slow-node","dead-node","fast-node"]}`))
		case "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":90}`))
		case "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 3
	opts.ShowUnhealthyNodes = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	fastIndex := strings.Index(out, "fast-node")
	slowIndex := strings.Index(out, "slow-node")
	deadIndex := strings.Index(out, "dead-node")
	if fastIndex == -1 || slowIndex == -1 || deadIndex == -1 {
		t.Fatalf("nodes output missing expected rows:\n%s", out)
	}
	if !(fastIndex < slowIndex && slowIndex < deadIndex) {
		t.Fatalf("nodes output not sorted by healthy delay with unhealthy last:\n%s", out)
	}
}

func TestNodesCapabilityChecksClassifyAndSortRows(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["fast-failed","unhealthy","fast-ok"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-failed/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":8}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-ok/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/unhealthy/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	var selectedMu sync.Mutex
	selectedName := ""
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/All" {
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode select payload: %v", err)
		}
		selectedMu.Lock()
		selectedName = payload.Name
		selectedMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer controller.Close()

	oldStartCapabilityProxy := startCapabilityProxy
	oldNewCapabilityHTTPClient := newCapabilityHTTPClient
	t.Cleanup(func() {
		startCapabilityProxy = oldStartCapabilityProxy
		newCapabilityHTTPClient = oldNewCapabilityHTTPClient
	})
	startCapabilityProxy = func(processCtx, controlCtx context.Context, opts Options) (startedProxy, error) {
		return startedProxy{
			session:       state.Session{RuntimeDir: t.TempDir()},
			mihomoProcess: &recordingProcess{pid: 4000},
			controllerURL: controller.URL,
		}, nil
	}
	newCapabilityHTTPClient = func(proxyPort, timeoutMS int) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			selectedMu.Lock()
			node := selectedName
			selectedMu.Unlock()
			status := http.StatusUnauthorized
			body := `{"error":{"message":"Missing bearer authentication"}}`
			if node == "fast-failed" {
				status = http.StatusForbidden
				body = `{"error":{"code":"unsupported_country_region_territory"}}`
			}
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		})}
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 1
	opts.NodesCapabilityConcurrency = 1
	opts.ShowUnhealthyNodes = true
	opts.NodesCapabilityChecks = []CapabilityCheck{{
		Name:             "openai",
		URL:              "https://api.openai.example",
		PassStatus:       []int{http.StatusUnauthorized},
		FailStatus:       []int{403},
		FailBodyContains: []string{"unsupported_country_region_territory"},
	}}

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"NODE", "STATUS", "DELAY", "CHECKS", "fast-ok", "ok", "10ms", "fast-failed", "failed", "8ms", "openai:unsupported_country_region_territory", "unhealthy", "health:unavailable"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nodes output missing %q:\n%s", want, out)
		}
	}
	okIndex := strings.Index(out, "fast-ok")
	failedIndex := strings.Index(out, "fast-failed")
	unhealthyIndex := strings.Index(out, "unhealthy")
	if okIndex == -1 || failedIndex == -1 || unhealthyIndex == -1 {
		t.Fatalf("nodes output missing expected rows:\n%s", out)
	}
	if !(okIndex < failedIndex && failedIndex < unhealthyIndex) {
		t.Fatalf("nodes output not sorted by ok, failed, unhealthy groups:\n%s", out)
	}
}

func TestNodesStartsCapabilityChecksAsSoonAsNodeIsHealthy(t *testing.T) {
	var slowDelayStartedOnce sync.Once
	slowDelayStarted := make(chan struct{})
	allowSlowDelay := make(chan struct{})
	capabilityStarted := make(chan struct{})
	var capabilityStartedOnce sync.Once

	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["fast-node","slow-node"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			slowDelayStartedOnce.Do(func() { close(slowDelayStarted) })
			<-allowSlowDelay
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":500}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/All" {
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer controller.Close()

	oldStartCapabilityProxy := startCapabilityProxy
	oldNewCapabilityHTTPClient := newCapabilityHTTPClient
	t.Cleanup(func() {
		startCapabilityProxy = oldStartCapabilityProxy
		newCapabilityHTTPClient = oldNewCapabilityHTTPClient
	})
	startCapabilityProxy = func(processCtx, controlCtx context.Context, opts Options) (startedProxy, error) {
		return startedProxy{
			session:       state.Session{RuntimeDir: t.TempDir()},
			mihomoProcess: &recordingProcess{pid: 3000},
			controllerURL: controller.URL,
		}, nil
	}
	newCapabilityHTTPClient = func(proxyPort, timeoutMS int) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			capabilityStartedOnce.Do(func() { close(capabilityStarted) })
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     http.StatusText(http.StatusUnauthorized),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Missing bearer authentication"}}`)),
				Request:    r,
			}, nil
		})}
	}

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.NodesCapabilityConcurrency = 1
	opts.ShowUnhealthyNodes = true
	opts.NodesCapabilityChecks = []CapabilityCheck{{
		Name:       "openai",
		URL:        "https://api.openai.example/v1/models",
		PassStatus: []int{http.StatusUnauthorized},
	}}

	done := make(chan error, 1)
	go func() { done <- application.Nodes(context.Background(), opts) }()

	select {
	case <-slowDelayStarted:
	case err := <-done:
		t.Fatalf("Nodes returned before slow health check started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("slow health check did not start")
	}

	select {
	case <-capabilityStarted:
		close(allowSlowDelay)
	case <-time.After(200 * time.Millisecond):
		close(allowSlowDelay)
		if err := <-done; err != nil {
			t.Fatalf("Nodes returned error after releasing slow health check: %v", err)
		}
		t.Fatal("capability check did not start while another health check was still running")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Nodes returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Nodes did not finish after slow health check was released")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestProbeNodeCapabilitiesFromSkipsBootstrapSelectError(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/All" {
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer controller.Close()

	oldStartCapabilityProxy := startCapabilityProxy
	oldNewCapabilityHTTPClient := newCapabilityHTTPClient
	t.Cleanup(func() {
		startCapabilityProxy = oldStartCapabilityProxy
		newCapabilityHTTPClient = oldNewCapabilityHTTPClient
	})
	startCapabilityProxy = func(processCtx, controlCtx context.Context, opts Options) (startedProxy, error) {
		if opts.DefaultNode == "missing-node" {
			return startedProxy{}, errors.New(`select temp node "missing-node" in group "All": proxy not exist`)
		}
		return startedProxy{
			session:       state.Session{RuntimeDir: t.TempDir()},
			mihomoProcess: &recordingProcess{pid: 5000},
			controllerURL: controller.URL,
		}, nil
	}
	newCapabilityHTTPClient = func(proxyPort, timeoutMS int) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     http.StatusText(http.StatusUnauthorized),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Missing bearer authentication"}}`)),
				Request:    r,
			}, nil
		})}
	}

	results := []nodeDelayResult{
		{index: 0, name: "missing-node", delay: 10, healthy: true, capabilityOK: true, checks: "-"},
		{index: 1, name: "working-node", delay: 20, healthy: true, capabilityOK: true, checks: "-"},
	}
	healthyJobs := make(chan int, len(results))
	healthyJobs <- 0
	healthyJobs <- 1
	close(healthyJobs)
	opts := DefaultOptions()
	opts.Group = "All"
	opts.NodesCapabilityConcurrency = 1
	opts.NodesCapabilityChecks = []CapabilityCheck{{
		Name:       "openai",
		URL:        "https://api.openai.example/v1/models",
		PassStatus: []int{http.StatusUnauthorized},
	}}

	if err := probeNodeCapabilitiesFrom(context.Background(), opts, results, healthyJobs); err != nil {
		t.Fatalf("probeNodeCapabilitiesFrom returned error: %v", err)
	}
	if results[0].capabilityOK || results[0].checks != "proxy:select_error" {
		t.Fatalf("missing-node = capabilityOK %v checks %q, want select error", results[0].capabilityOK, results[0].checks)
	}
	if !results[1].capabilityOK || results[1].checks != "-" {
		t.Fatalf("working-node = capabilityOK %v checks %q, want ok", results[1].capabilityOK, results[1].checks)
	}
}

func TestDefaultProbeNodeCapabilitiesUsesIndependentWorkers(t *testing.T) {
	var inFlight int32
	var maxInFlight int32
	capabilityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			observed := atomic.LoadInt32(&maxInFlight)
			if current <= observed || atomic.CompareAndSwapInt32(&maxInFlight, observed, current) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Missing bearer authentication"}}`))
	}))
	defer capabilityServer.Close()

	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/All" {
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer controller.Close()

	oldStartCapabilityProxy := startCapabilityProxy
	oldNewCapabilityHTTPClient := newCapabilityHTTPClient
	t.Cleanup(func() {
		startCapabilityProxy = oldStartCapabilityProxy
		newCapabilityHTTPClient = oldNewCapabilityHTTPClient
	})

	var proxyPorts []int
	var controllerPorts []int
	startCapabilityProxy = func(processCtx, controlCtx context.Context, opts Options) (startedProxy, error) {
		proxyPorts = append(proxyPorts, opts.ProxyPort)
		controllerPorts = append(controllerPorts, opts.ControllerPort)
		return startedProxy{
			session:       state.Session{RuntimeDir: t.TempDir()},
			mihomoProcess: &recordingProcess{pid: 1000 + len(proxyPorts)},
			controllerURL: controller.URL,
		}, nil
	}
	newCapabilityHTTPClient = func(proxyPort, timeoutMS int) *http.Client {
		return capabilityServer.Client()
	}

	results := []nodeDelayResult{
		{index: 0, name: "node-a", delay: 10, healthy: true, capabilityOK: true, checks: "-"},
		{index: 1, name: "node-b", delay: 20, healthy: true, capabilityOK: true, checks: "-"},
		{index: 2, name: "node-c", delay: 30, healthy: true, capabilityOK: true, checks: "-"},
		{index: 3, name: "node-d", delay: 40, healthy: true, capabilityOK: true, checks: "-"},
	}
	opts := DefaultOptions()
	opts.Group = "All"
	opts.ProxyPort = 19001
	opts.ControllerPort = 19002
	opts.NodesCapabilityConcurrency = 2
	opts.NodesCapabilityChecks = []CapabilityCheck{{
		Name:       "openai",
		URL:        capabilityServer.URL + "/v1/models",
		PassStatus: []int{http.StatusUnauthorized},
	}}

	got, err := defaultProbeNodeCapabilities(context.Background(), opts, results)
	if err != nil {
		t.Fatalf("defaultProbeNodeCapabilities returned error: %v", err)
	}
	if !reflect.DeepEqual(proxyPorts, []int{19001, 19003}) {
		t.Fatalf("proxyPorts = %#v, want independent worker ports", proxyPorts)
	}
	if !reflect.DeepEqual(controllerPorts, []int{19002, 19004}) {
		t.Fatalf("controllerPorts = %#v, want independent worker ports", controllerPorts)
	}
	if atomic.LoadInt32(&maxInFlight) < 2 {
		t.Fatalf("max concurrent capability requests = %d, want at least 2", maxInFlight)
	}
	for _, result := range got {
		if !result.capabilityOK || result.checks != "-" {
			t.Fatalf("result for %s = capabilityOK %v checks %q, want ok", result.name, result.capabilityOK, result.checks)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDefaultProbeNodeCapabilitiesUsesFreshHTTPClientPerNode(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/All" {
			t.Fatalf("unexpected controller request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer controller.Close()

	oldStartCapabilityProxy := startCapabilityProxy
	oldNewCapabilityHTTPClient := newCapabilityHTTPClient
	t.Cleanup(func() {
		startCapabilityProxy = oldStartCapabilityProxy
		newCapabilityHTTPClient = oldNewCapabilityHTTPClient
	})

	startCapabilityProxy = func(processCtx, controlCtx context.Context, opts Options) (startedProxy, error) {
		return startedProxy{
			session:       state.Session{RuntimeDir: t.TempDir()},
			mihomoProcess: &recordingProcess{pid: 2000},
			controllerURL: controller.URL,
		}, nil
	}
	var clientCreations int32
	newCapabilityHTTPClient = func(proxyPort, timeoutMS int) *http.Client {
		clientID := atomic.AddInt32(&clientCreations, 1)
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			status := http.StatusUnauthorized
			body := `{"error":{"message":"Missing bearer authentication"}}`
			if clientID == 2 {
				status = http.StatusForbidden
				body = `{"error":{"code":"unsupported_country_region_territory"}}`
			}
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		})}
	}

	results := []nodeDelayResult{
		{index: 0, name: "node-a", delay: 10, healthy: true, capabilityOK: true, checks: "-"},
		{index: 1, name: "node-b", delay: 20, healthy: true, capabilityOK: true, checks: "-"},
	}
	opts := DefaultOptions()
	opts.Group = "All"
	opts.NodesCapabilityConcurrency = 1
	opts.NodesCapabilityChecks = []CapabilityCheck{{
		Name:             "openai",
		URL:              "https://api.openai.example/v1/responses",
		PassStatus:       []int{http.StatusUnauthorized},
		FailBodyContains: []string{"unsupported_country_region_territory"},
	}}

	got, err := defaultProbeNodeCapabilities(context.Background(), opts, results)
	if err != nil {
		t.Fatalf("defaultProbeNodeCapabilities returned error: %v", err)
	}
	if atomic.LoadInt32(&clientCreations) != 2 {
		t.Fatalf("HTTP clients created = %d, want one per node", clientCreations)
	}
	if !got[0].capabilityOK || got[0].checks != "-" {
		t.Fatalf("node-a = capabilityOK %v checks %q, want ok", got[0].capabilityOK, got[0].checks)
	}
	if got[1].capabilityOK || got[1].checks != "openai:unsupported_country_region_territory" {
		t.Fatalf("node-b = capabilityOK %v checks %q, want unsupported region failure", got[1].capabilityOK, got[1].checks)
	}
}

func TestEvaluateCapabilityChecksTreatsModelsUnauthorizedAsReachableAndUnsupportedRegionAsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Missing bearer authentication"}}`))
		case "/blocked":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"unsupported_country_region_territory"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := server.Client()
	passes := evaluateCapabilityChecks(context.Background(), client, []CapabilityCheck{{
		Name:       "openai",
		URL:        server.URL + "/v1/models",
		PassStatus: []int{http.StatusUnauthorized},
	}})
	if len(passes) != 0 {
		t.Fatalf("401 capability check failures = %#v, want none", passes)
	}

	fails := evaluateCapabilityChecks(context.Background(), client, []CapabilityCheck{{
		Name:             "openai",
		URL:              server.URL + "/blocked",
		FailStatus:       []int{http.StatusForbidden},
		FailBodyContains: []string{"unsupported_country_region_territory"},
	}})
	want := []string{"openai:unsupported_country_region_territory"}
	if !reflect.DeepEqual(fails, want) {
		t.Fatalf("403 capability check failures = %#v, want %#v", fails, want)
	}
}

func TestNodesSelectFastestPutsFastestHealthyNode(t *testing.T) {
	requests := make(chan struct {
		path string
		body string
	}, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["slow-node","fast-node","dead-node"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":80}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- struct {
				path string
				body string
			}{r.URL.Path, string(body)}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 3
	opts.ShowUnhealthyNodes = true
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var got struct {
		path string
		body string
	}
	select {
	case got = <-requests:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node")
	}
	if got.path != "/proxies/All" {
		t.Fatalf("PUT path = %q, want /proxies/All", got.path)
	}
	if got.body != `{"name":"fast-node"}`+"\n" {
		t.Fatalf("PUT body = %q, want fast-node JSON payload", got.body)
	}

	out := stdout.String()
	fastIndex := strings.Index(out, "fast-node")
	slowIndex := strings.Index(out, "slow-node")
	deadIndex := strings.Index(out, "dead-node")
	if fastIndex == -1 || slowIndex == -1 || deadIndex == -1 {
		t.Fatalf("nodes output missing expected rows:\n%s", out)
	}
	if !(fastIndex < slowIndex && slowIndex < deadIndex) {
		t.Fatalf("nodes output not sorted by healthy delay with unhealthy last:\n%s", out)
	}
	if !strings.Contains(out, "selected fast-node (10ms) for group All") {
		t.Fatalf("nodes output missing selection success line:\n%s", out)
	}
}

func TestNodesAveragesMultipleHealthURLsAcrossProbeRounds(t *testing.T) {
	requests := make(chan struct {
		path string
		body string
	}, 1)
	counts := map[string]int{}
	var countsMu sync.Mutex
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["single-fast-node","balanced-node","dead-node"]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/delay"):
			key := r.URL.Path + "|" + r.URL.Query().Get("url")
			countsMu.Lock()
			counts[key]++
			countsMu.Unlock()
			switch {
			case r.URL.Path == "/proxies/single-fast-node/delay" && r.URL.Query().Get("url") == "https://x.example":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"delay":10}`))
			case r.URL.Path == "/proxies/single-fast-node/delay" && r.URL.Query().Get("url") == "https://static.example":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"delay":100}`))
			case r.URL.Path == "/proxies/balanced-node/delay":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"delay":40}`))
			case r.URL.Path == "/proxies/dead-node/delay":
				http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
			default:
				t.Fatalf("unexpected delay request: %s", r.URL.String())
			}
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- struct {
				path string
				body string
			}{r.URL.Path, string(body)}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://x.example", "https://static.example"}
	opts.NodeProbeRounds = 2
	opts.NodesConcurrency = 3
	opts.ShowUnhealthyNodes = true
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got.path != "/proxies/All" {
			t.Fatalf("PUT path = %q, want /proxies/All", got.path)
		}
		if got.body != `{"name":"balanced-node"}`+"\n" {
			t.Fatalf("PUT body = %q, want balanced-node JSON payload", got.body)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the lowest average node")
	}

	countsMu.Lock()
	defer countsMu.Unlock()
	for _, node := range []string{"single-fast-node", "balanced-node"} {
		for _, healthURL := range opts.HealthURLs {
			key := "/proxies/" + node + "/delay|" + healthURL
			if counts[key] != opts.NodeProbeRounds {
				t.Fatalf("probe count for %s = %d, want %d", key, counts[key], opts.NodeProbeRounds)
			}
		}
	}

	out := stdout.String()
	balancedIndex := strings.Index(out, "balanced-node")
	singleFastIndex := strings.Index(out, "single-fast-node")
	deadIndex := strings.Index(out, "dead-node")
	if balancedIndex == -1 || singleFastIndex == -1 || deadIndex == -1 {
		t.Fatalf("nodes output missing expected rows:\n%s", out)
	}
	if !(balancedIndex < singleFastIndex && singleFastIndex < deadIndex) {
		t.Fatalf("nodes output was not sorted by average delay with unhealthy last:\n%s", out)
	}
	if !strings.Contains(out, "balanced-node") || !strings.Contains(out, "40ms") || !strings.Contains(out, "single-fast-node") || !strings.Contains(out, "55ms") {
		t.Fatalf("nodes output missing averaged delays:\n%s", out)
	}
}

func TestNodesAutoResolveSkipsGlobalDirectLeafInRuleMode(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"name":"GLOBAL","type":"Selector","all":["global-node","other-node"],"now":"global-node"},"All":{"name":"All","type":"Selector","all":["DIRECT","XFLTD"],"now":"XFLTD"},"XFLTD":{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/XFLTD":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":80}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/global-node/delay":
			t.Fatal("nodes should not probe GLOBAL's direct leaf in rule mode")
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/XFLTD":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = ""
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("PUT body = %q, want fast-node JSON payload", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node in the rule-mode group")
	}
	if out := stdout.String(); !strings.Contains(out, "selected fast-node (10ms) for group XFLTD") {
		t.Fatalf("nodes output missing rule-mode selection line:\n%s", out)
	}
}

func TestNodesAutoResolvePrefersManualSelectorWhenRuleModeHasAutoGroups(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"name":"GLOBAL","type":"Selector","all":["global-node"],"now":"global-node"},"All":{"name":"All","type":"Selector","all":["DIRECT","XFLTD"],"now":"XFLTD"},"AI":{"name":"AI","type":"Selector","all":["自动选择"],"now":"自动选择"},"FallbackPolicy":{"name":"FallbackPolicy","type":"Selector","all":["故障转移"],"now":"故障转移"},"XFLTD":{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"},"故障转移":{"name":"故障转移","type":"Fallback","all":["fallback-node"],"now":"fallback-node"},"自动选择":{"name":"自动选择","type":"URLTest","all":["auto-node"],"now":"auto-node"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/XFLTD":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":80}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/XFLTD":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = ""
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("PUT body = %q, want fast-node JSON payload", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node in the manual selector group")
	}
	if out := stdout.String(); !strings.Contains(out, "selected fast-node (10ms) for group XFLTD") {
		t.Fatalf("nodes output missing manual selector selection line:\n%s", out)
	}
}

func TestNodesAutoResolvePrefersChineseAutoGroupWhenAutoIsAmbiguous(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"ChatGPT":{"name":"ChatGPT","type":"Selector","all":["chat-node"],"now":"chat-node"},"Twitter":{"name":"Twitter","type":"Selector","all":["twitter-node"],"now":"twitter-node"},"自动选择":{"name":"自动选择","type":"URLTest","all":["slow-node","fast-node"],"now":"slow-node"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/自动选择":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"自动选择","type":"URLTest","all":["slow-node","fast-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":80}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodGet && (r.URL.Path == "/proxies/chat-node/delay" || r.URL.Path == "/proxies/twitter-node/delay"):
			t.Fatalf("nodes should probe 自动选择, not ambiguous leaf selector %s", r.URL.Path)
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/自动选择":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = ""
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("PUT body = %q, want fast-node JSON payload", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node in 自动选择")
	}
	if out := stdout.String(); !strings.Contains(out, "selected fast-node (10ms) for group 自动选择") {
		t.Fatalf("nodes output missing 自动选择 selection line:\n%s", out)
	}
}

func TestNodesAutoResolvesCurrentProxyGroupWhenGroupEmpty(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"name":"GLOBAL","type":"Selector","all":["All"],"now":"All"},"All":{"name":"All","type":"Selector","all":["DIRECT","XFLTD","Other"],"now":"XFLTD"},"XFLTD":{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"},"Other":{"name":"Other","type":"Selector","all":["other-node"],"now":"other-node"},"OpenAI":{"name":"OpenAI","type":"Selector","all":["OpenAIAuto"],"now":"OpenAIAuto"},"OpenAIAuto":{"name":"OpenAIAuto","type":"Selector","all":["oa-node"],"now":"oa-node"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/XFLTD":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"XFLTD","type":"Selector","all":["slow-node","fast-node"],"now":"slow-node"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/slow-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":80}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/XFLTD":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read PUT body: %v", err)
			}
			requests <- string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = ""
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != `{"name":"fast-node"}`+"\n" {
			t.Fatalf("PUT body = %q, want fast-node JSON payload", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node in the auto-resolved group")
	}
	if out := stdout.String(); !strings.Contains(out, "selected fast-node (10ms) for group XFLTD") {
		t.Fatalf("nodes output missing auto-resolved selection line:\n%s", out)
	}
}

func TestNodesSelectFastestUsesLookupGroupWhenResponseOmitsName(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"Selector","all":["fast-node"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodPut:
			requests <- r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != "/proxies/All" {
			t.Fatalf("PUT path = %q, want /proxies/All", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select the fastest node")
	}
}

func TestNodesSelectFastestReturnsErrorWhenNoHealthyNodes(t *testing.T) {
	var putCount int32
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["dead-node"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/dead-node/delay":
			http.Error(w, `{"message":"timeout"}`, http.StatusGatewayTimeout)
		case r.Method == http.MethodPut:
			atomic.AddInt32(&putCount, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.SelectFastest = true

	err := application.Nodes(context.Background(), opts)
	if err == nil {
		t.Fatal("Nodes returned nil error, want no healthy nodes error")
	}
	if !strings.Contains(err.Error(), `select fastest node in group "All": no healthy nodes`) {
		t.Fatalf("error = %v, want no healthy nodes context", err)
	}
	if got := atomic.LoadInt32(&putCount); got != 0 {
		t.Fatalf("PUT count = %d, want 0", got)
	}

	out := stdout.String()
	if !strings.Contains(out, "nodes: total 1, ok 0, shown 0") || !strings.Contains(out, "NODE") {
		t.Fatalf("filtered table summary was not printed before error:\n%s", out)
	}
	if strings.Contains(out, "dead-node") || strings.Contains(out, "unhealthy") {
		t.Fatalf("default nodes output should hide unhealthy rows before error:\n%s", out)
	}
}

func TestNodesSelectFastestReturnsErrorWhenControllerRejectsSelection(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"All","type":"Selector","all":["fast-node"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/fast-node/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":10}`))
		case r.Method == http.MethodPut && r.URL.Path == "/proxies/All":
			http.Error(w, `{"message":"selection rejected"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.SelectFastest = true

	err := application.Nodes(context.Background(), opts)
	if err == nil {
		t.Fatal("Nodes returned nil error, want selection rejection error")
	}
	if !strings.Contains(err.Error(), `select fastest node "fast-node" in group "All"`) {
		t.Fatalf("error = %v, want selection context", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "fast-node") || !strings.Contains(out, "ok") || !strings.Contains(out, "10ms") {
		t.Fatalf("delay table was not printed before error:\n%s", out)
	}
	if strings.Contains(out, "selected fast-node") {
		t.Fatalf("nodes output contains unexpected success line:\n%s", out)
	}
}

func TestNodesRespectsConcurrencyLimitAndDelayTimeout(t *testing.T) {
	nodes := []string{"node-1", "node-2", "node-3", "node-4"}
	delayRequests := make(chan struct{}, len(nodes))
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(closeRelease)

	var active int32
	var maxActive int32
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		if r.URL.Path == "/proxies/All" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"name": "All",
				"type": "Selector",
				"all":  nodes,
			}); err != nil {
				t.Fatalf("encode group response: %v", err)
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/proxies/node-") || !strings.HasSuffix(r.URL.Path, "/delay") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("timeout") != "1234" {
			t.Fatalf("delay timeout = %q, want 1234", r.URL.Query().Get("timeout"))
		}

		current := atomic.AddInt32(&active, 1)
		for {
			maximum := atomic.LoadInt32(&maxActive)
			if current <= maximum || atomic.CompareAndSwapInt32(&maxActive, maximum, current) {
				break
			}
		}
		delayRequests <- struct{}{}
		<-release
		atomic.AddInt32(&active, -1)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":1}`))
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.NodesConcurrency = 2
	opts.DelayTimeoutMS = 1234

	done := make(chan error, 1)
	go func() {
		done <- application.Nodes(context.Background(), opts)
	}()

	for range 2 {
		select {
		case <-delayRequests:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Nodes did not start delay checks up to the configured concurrency")
		}
	}
	select {
	case <-delayRequests:
		t.Fatal("Nodes started more delay checks than the configured concurrency before workers were released")
	case <-time.After(100 * time.Millisecond):
	}

	closeRelease()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Nodes returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Nodes did not return after releasing delay checks")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got := atomic.LoadInt32(&maxActive); got > 2 {
		t.Fatalf("max active delay checks = %d, want at most 2", got)
	}
}

func TestNodesResolvesSingleSuffixProxyGroup(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"Provider/All":{"name":"Provider/All","type":"Selector","all":["node-a"]},"Other":{"name":"Other","type":"Selector","all":["node-b"]}}}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/proxies/Provider%2FAll":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"Provider/All","type":"Selector","all":["node-a"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/node-a/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":11}`))
		case r.Method == http.MethodPut && r.URL.EscapedPath() == "/proxies/Provider%2FAll":
			requests <- r.URL.EscapedPath()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}
	opts.SelectFastest = true

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	select {
	case got := <-requests:
		if got != "/proxies/Provider%2FAll" {
			t.Fatalf("PUT path = %q, want resolved Provider/All group", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not select fastest node in resolved group")
	}

	out := stdout.String()
	for _, want := range []string{"node-a", "11ms", "selected node-a (11ms) for group Provider/All"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nodes output missing %q:\n%s", want, out)
		}
	}
}

func TestNodesReturnsAmbiguousSuffixProxyGroupError(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/All":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{"A/All":{"name":"A/All","type":"Selector","all":["node-a"]},"B/All":{"name":"B/All","type":"Selector","all":["node-b"]}}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"

	err := application.Nodes(context.Background(), opts)
	if err == nil {
		t.Fatal("Nodes returned nil error, want ambiguous proxy group error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"ambiguous", "A/All", "B/All"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	}
}

func TestNodesReturnsClearErrorWhenGroupLookupFails(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		http.NotFound(w, r)
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"

	err := application.Nodes(context.Background(), opts)
	if err == nil {
		t.Fatal("Nodes returned nil error, want group lookup error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(err.Error(), `lookup proxy group "All"`) {
		t.Fatalf("error = %q, want clear group lookup context", err.Error())
	}
}

func TestNodesReturnsWhenControllerStalls(t *testing.T) {
	requestStarted := make(chan struct{})
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		close(requestStarted)
		select {}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- application.Nodes(ctx, opts)
	}()

	select {
	case <-requestStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stalled test server did not receive request")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Nodes returned nil error, want timeout error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Nodes did not return when controller stalled")
	}
}

func TestGroupsUsesControllerURLBeforeSocketOrPipe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q, want /proxies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"proxies":{"node-a":{"name":"node-a","type":"Shadowsocks"},"url-group":{"name":"url-group","type":"Selector","all":["node-a"]}}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerURL = server.URL
	opts.ControllerSocket = filepath.Join(t.TempDir(), "bogus-controller.sock")
	opts.ControllerPipe = filepath.Join(t.TempDir(), "bogus-pipe")

	if err := application.Groups(context.Background(), opts); err != nil {
		t.Fatalf("Groups returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "url-group") {
		t.Fatalf("groups output missing test group:\n%s", out)
	}
}

func TestGroupsUsesControllerSocketBeforeControllerPipe(t *testing.T) {
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q, want /proxies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"proxies":{"node-a":{"name":"node-a","type":"Shadowsocks"},"socket-group":{"name":"socket-group","type":"Selector","all":["node-a"]}}}`))
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = filepath.Join(t.TempDir(), "bogus-pipe")

	if err := application.Groups(context.Background(), opts); err != nil {
		t.Fatalf("Groups returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "socket-group") {
		t.Fatalf("groups output missing test group:\n%s", out)
	}
}

func TestGroupsListsProxyGroupsWithoutMutatingMainController(t *testing.T) {
	requests := make(chan string, 1)
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Path != "/proxies" {
			t.Fatalf("path = %q, want /proxies", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"proxies":{"node-a":{"name":"node-a","type":"Shadowsocks"},"group-b":{"name":"group-b","type":"Selector","all":["node-a"]},"group-a":{"type":"URLTest","all":["node-b","node-c"]}}}`))
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""

	if err := application.Groups(context.Background(), opts); err != nil {
		t.Fatalf("Groups returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"GROUP", "TYPE", "NODES", "group-a", "URLTest", "2", "group-b", "Selector", "1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("groups output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "node-a") {
		t.Fatalf("groups output should not include individual node entries:\n%s", out)
	}
	if got := <-requests; got != "GET /proxies" {
		t.Fatalf("request = %q, want GET /proxies", got)
	}
}

func TestGroupsSanitizesControlCharactersInGroupNames(t *testing.T) {
	unsafeGroup := "evil\ngroup\t\x1b[31mred\rend"
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"proxies": map[string]any{
				unsafeGroup: map[string]any{
					"type": "Selector",
					"all":  []string{"node-a"},
				},
			},
		}); err != nil {
			t.Fatalf("encode groups response: %v", err)
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""

	if err := application.Groups(context.Background(), opts); err != nil {
		t.Fatalf("Groups returned error: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, unsafeGroup) {
		t.Fatalf("groups output contains unsafe raw group name: %q", out)
	}
	if !strings.Contains(out, `evil\ngroup\t\x1b[31mred\rend`) {
		t.Fatalf("groups output missing sanitized group name: %q", out)
	}
}

func TestNodesSanitizesControlCharactersInNodeNames(t *testing.T) {
	unsafeNode := "evil\nnode\t\x1b[31mred\rend"
	socketPath := startAppUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected mutating request: %s %s", r.Method, r.URL.String())
		}

		switch r.URL.EscapedPath() {
		case "/proxies/All":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"name": "All",
				"type": "Selector",
				"all":  []string{unsafeNode},
			}); err != nil {
				t.Fatalf("encode group response: %v", err)
			}
		case "/proxies/evil%0Anode%09%1B%5B31mred%0Dend/delay":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":7}`))
		default:
			t.Fatalf("unexpected request path: %s", r.URL.EscapedPath())
		}
	}))

	var stdout, stderr bytes.Buffer
	application := New(&stdout, &stderr)
	opts := DefaultOptions()
	opts.ControllerSocket = socketPath
	opts.ControllerPipe = ""
	opts.Group = "All"
	opts.HealthURLs = []string{"https://health.example/ping"}

	if err := application.Nodes(context.Background(), opts); err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, unsafeNode) {
		t.Fatalf("nodes output contains unsafe raw node name: %q", out)
	}
	if strings.Count(out, "\n") != 3 {
		t.Fatalf("nodes output should contain summary, header, and one data row, got %d newlines in %q", strings.Count(out, "\n"), out)
	}
	for _, unsafe := range []string{"\t", "\r", "\x1b"} {
		if strings.Contains(out, unsafe) {
			t.Fatalf("nodes output contains unsafe control %q in %q", unsafe, out)
		}
	}
	if !strings.Contains(out, `evil\nnode\t\x1b[31mred\rend`) {
		t.Fatalf("nodes output missing sanitized node name: %q", out)
	}
}

func TestDisplayWidthHandlesWideAndEscapedText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ascii", text: "plain", want: 5},
		{name: "han", text: "香港节点", want: 8},
		{name: "regional flag pair", text: "🇭🇰", want: 2},
		{name: "emoji with variation selector", text: "✈️", want: 2},
		{name: "wide emoji presentation symbol", text: "❕", want: 2},
		{name: "zwj family sequence", text: "👨‍👩‍👧‍👦", want: 2},
		{name: "skin tone emoji", text: "👍🏽", want: 2},
		{name: "escaped tab", text: `node\t1`, want: 7},
		{name: "combining mark", text: "é", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayWidth(tt.text); got != tt.want {
				t.Fatalf("displayWidth(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func startAppUnixHTTPServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "browsebox-app-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "m.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server.Serve returned error: %v", err)
		}
	}()

	t.Cleanup(func() {
		_ = server.Close()
		<-done
		_ = os.Remove(socketPath)
	})

	return socketPath
}
