package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Options controls an isolated Chrome launch.
type Options struct {
	UserDataDir  string
	ProxyPort    int
	DevToolsPort int
	Headless     bool
	ChromeArgs   []string
	URL          string
}

// DefaultChromePath returns the macOS Google Chrome binary path.
func DefaultChromePath() string {
	return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}

// ChromeArgs builds Chrome arguments for an isolated localhost-proxied session.
func ChromeArgs(opts Options) []string {
	args := normalizeChromeArgs(opts.ChromeArgs)
	args = append(args,
		"--user-data-dir="+opts.UserDataDir,
		fmt.Sprintf("--remote-debugging-port=%d", opts.DevToolsPort),
		fmt.Sprintf("--proxy-server=http://127.0.0.1:%d", opts.ProxyPort),
	)
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	return append(args, opts.URL)
}

func normalizeChromeArgs(values []string) []string {
	args := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		trimmed = strings.TrimLeft(trimmed, "-")
		if trimmed == "" {
			continue
		}
		name, _, _ := strings.Cut(trimmed, "=")
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || isManagedChromeArg(name) {
			continue
		}
		seen[name] = true
		args = append(args, "--"+trimmed)
	}
	return args
}

func isManagedChromeArg(name string) bool {
	switch name {
	case "user-data-dir", "remote-debugging-port", "proxy-server":
		return true
	default:
		return false
	}
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
