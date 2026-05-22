package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestDefaultChromePathUsesMacOSGoogleChrome(t *testing.T) {
	want := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if got := DefaultChromePath(); got != want {
		t.Fatalf("DefaultChromePath() = %q, want %q", got, want)
	}
}

func TestChromeArgsBuildsIsolatedProxySession(t *testing.T) {
	opts := Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		ChromeArgs:   []string{"no-first-run", "no-default-browser-check"},
		URL:          "https://example.com/start",
	}

	args := ChromeArgs(opts)
	want := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=/tmp/browsebox/profile",
		"--remote-debugging-port=9223",
		"--proxy-server=http://127.0.0.1:17997",
		"https://example.com/start",
	}
	if len(args) != len(want) {
		t.Fatalf("ChromeArgs len = %d (%#v), want %d (%#v)", len(args), args, len(want), want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("ChromeArgs() = %#v, want %#v", args, want)
		}
	}
}

func TestChromeArgsNormalizesConfiguredChromeArgs(t *testing.T) {
	opts := Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		ChromeArgs:   []string{" --no-first-run ", "disable-component-update=true", "no-first-run", "", "--disable-background-networking"},
		URL:          "https://example.com/start",
	}

	args := ChromeArgs(opts)
	wantPrefix := []string{"--no-first-run", "--disable-component-update=true", "--disable-background-networking"}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("ChromeArgs() = %#v, want prefix %#v", args, wantPrefix)
		}
	}
}

func TestChromeArgsIgnoresManagedChromeArgs(t *testing.T) {
	opts := Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		ChromeArgs: []string{
			"user-data-dir=/tmp/unsafe-profile",
			"remote-debugging-port=1",
			"proxy-server=http://127.0.0.1:8080",
			"no-first-run",
		},
		URL: "https://example.com/start",
	}

	args := ChromeArgs(opts)
	for _, unwanted := range []string{"--user-data-dir=/tmp/unsafe-profile", "--remote-debugging-port=1", "--proxy-server=http://127.0.0.1:8080"} {
		if slices.Contains(args, unwanted) {
			t.Fatalf("ChromeArgs() = %#v, should ignore managed arg %q", args, unwanted)
		}
	}
	for _, want := range []string{"--user-data-dir=/tmp/browsebox/profile", "--remote-debugging-port=9223", "--proxy-server=http://127.0.0.1:17997"} {
		if !slices.Contains(args, want) {
			t.Fatalf("ChromeArgs() = %#v, want managed arg %q", args, want)
		}
	}
}

func TestChromeArgsAddsHeadlessWhenConfigured(t *testing.T) {
	opts := Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		Headless:     true,
		URL:          "https://example.com/start",
	}

	args := ChromeArgs(opts)
	if !slices.Contains(args, "--headless=new") {
		t.Fatalf("ChromeArgs() = %#v, want --headless=new", args)
	}
}

func TestEnsureUserDataDirCreatesPrivateDirectory(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "profile")
	if err := ensureUserDataDir(profileDir); err != nil {
		t.Fatalf("ensureUserDataDir returned error: %v", err)
	}
	info, err := os.Stat(profileDir)
	if err != nil {
		t.Fatalf("stat profile dir: %v", err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("profile dir mode = %o, want 0700", got)
		}
	}
}

func TestNewCommandBuildsChromeCommandWithoutStartingIt(t *testing.T) {
	cmd := newCommand("/bin/chrome", Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		URL:          "https://example.com/start",
	})
	if cmd.Path != "/bin/chrome" {
		t.Fatalf("cmd.Path = %q, want /bin/chrome", cmd.Path)
	}
	wantArgs := append([]string{"/bin/chrome"}, ChromeArgs(Options{
		UserDataDir:  "/tmp/browsebox/profile",
		ProxyPort:    17997,
		DevToolsPort: 9223,
		URL:          "https://example.com/start",
	})...)
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}
	for i := range wantArgs {
		if cmd.Args[i] != wantArgs[i] {
			t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
		}
	}
	if cmd.Process != nil {
		t.Fatalf("cmd.Process is set; command should not have been started")
	}
}
