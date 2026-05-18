package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Options controls an isolated Chrome launch.
type Options struct {
	UserDataDir  string
	ProxyPort    int
	DevToolsPort int
	Headless     bool
	URL          string
}

// DefaultChromePath returns the macOS Google Chrome binary path.
func DefaultChromePath() string {
	return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}

// ChromeArgs builds Chrome arguments for an isolated localhost-proxied session.
func ChromeArgs(opts Options) []string {
	args := []string{
		"--user-data-dir=" + opts.UserDataDir,
		fmt.Sprintf("--remote-debugging-port=%d", opts.DevToolsPort),
		fmt.Sprintf("--proxy-server=http://127.0.0.1:%d", opts.ProxyPort),
		"--no-first-run",
		"--no-default-browser-check",
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	return append(args, opts.URL)
}

func ensureUserDataDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func newCommand(chromePath string, opts Options) *exec.Cmd {
	return exec.Command(chromePath, ChromeArgs(opts)...)
}

// StartChrome creates the isolated user data directory and starts Chrome.
func StartChrome(ctx context.Context, chromePath string, opts Options) (*exec.Cmd, error) {
	if err := ensureUserDataDir(opts.UserDataDir); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, chromePath, ChromeArgs(opts)...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
