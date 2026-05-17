package browser

import (
	"os"
	"path/filepath"
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
		URL:          "https://example.com/start",
	}

	args := ChromeArgs(opts)
	want := []string{
		"--user-data-dir=/tmp/browsebox/profile",
		"--remote-debugging-port=9223",
		"--proxy-server=http://127.0.0.1:17997",
		"--no-first-run",
		"--no-default-browser-check",
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

func TestEnsureUserDataDirCreatesPrivateDirectory(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "profile")
	if err := ensureUserDataDir(profileDir); err != nil {
		t.Fatalf("ensureUserDataDir returned error: %v", err)
	}
	info, err := os.Stat(profileDir)
	if err != nil {
		t.Fatalf("stat profile dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("profile dir mode = %o, want 0700", got)
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
