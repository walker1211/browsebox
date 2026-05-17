package app

import (
	"os"
	"path/filepath"

	"github.com/walker1211/browsebox/internal/browser"
	"github.com/walker1211/browsebox/internal/mihomo"
)

// Options contains browsebox runtime settings parsed by the CLI.
type Options struct {
	ControllerSocket string
	SourceConfigPath string
	RuntimeDir       string
	StateDir         string
	MihomoBinaryPath string
	ChromeBinaryPath string
	Keep             bool
	Group            string
	DefaultNode      string
	ProxyPort        int
	ControllerPort   int
	DevToolsPort     int
	TargetURL        string
	HealthURLs       []string
}

// DefaultOptions returns safe macOS-oriented defaults for browsebox.
func DefaultOptions() Options {
	return Options{
		ControllerSocket: "/tmp/verge/verge-mihomo.sock",
		SourceConfigPath: defaultSourceConfigPath(),
		StateDir:         defaultStateDir(),
		MihomoBinaryPath: mihomo.DefaultBinaryPath(),
		ChromeBinaryPath: browser.DefaultChromePath(),
		Group:            "All",
		DefaultNode:      "",
		ProxyPort:        17997,
		ControllerPort:   17998,
		DevToolsPort:     9223,
		TargetURL:        "https://x.com/OpenAI",
		HealthURLs: []string{
			"https://x.com",
			"https://abs.twimg.com",
		},
	}
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
