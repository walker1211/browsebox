package app

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/walker1211/browsebox/internal/browser"
	"github.com/walker1211/browsebox/internal/mihomo"
)

// Options contains browsebox runtime settings parsed by the CLI.
type Options struct {
	ControllerSocket string
	ControllerURL    string
	ControllerPipe   string
	SourceConfigPath string
	RuntimeDir       string
	RuntimeCacheDir  string
	StateDir         string
	MihomoBinaryPath string
	ChromeBinaryPath string
	ChromeProfileDir string
	ChromeArgs       []string
	BrowserHeadless  bool
	Keep             bool
	Group            string
	DefaultNode      string
	ProxyPort        int
	ControllerPort   int
	DevToolsPort     int
	TargetURL        string
	HealthURLs       []string
	NodesConcurrency int
	DelayTimeoutMS   int
	SelectFastest    bool
}

// DefaultOptions returns safe macOS-oriented defaults for browsebox.
func DefaultOptions() Options {
	opts := Options{
		ControllerSocket: "/tmp/verge/verge-mihomo.sock",
		SourceConfigPath: defaultSourceConfigPath(),
		StateDir:         defaultStateDir(),
		MihomoBinaryPath: mihomo.DefaultBinaryPath(),
		ChromeBinaryPath: browser.DefaultChromePath(),
		ChromeArgs: []string{
			"no-first-run",
			"no-default-browser-check",
		},
		Group:            "All",
		DefaultNode:      "",
		ProxyPort:        17997,
		ControllerPort:   17998,
		DevToolsPort:     9223,
		TargetURL:        "https://x.com/OpenAI",
		NodesConcurrency: 16,
		DelayTimeoutMS:   5000,
		HealthURLs: []string{
			"https://x.com",
			"https://abs.twimg.com",
		},
	}
	if runtime.GOOS == "windows" {
		opts.ControllerSocket = ""
		opts.ControllerPipe = `\\.\pipe\verge-mihomo`
	}
	return opts
}

func defaultSourceConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "config.yaml"
	}

	genericPath := filepath.Join(home, ".config", "mihomo", "config.yaml")
	if fileExists(genericPath) {
		return genericPath
	}

	clashVergePath := filepath.Join(home, "Library", "Application Support", "io.github.clash-verge-rev.clash-verge-rev", "clash-verge.yaml")
	if fileExists(clashVergePath) {
		return clashVergePath
	}

	return genericPath
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func defaultStateDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".browsebox")
	}
	return ".browsebox"
}
