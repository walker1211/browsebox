package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfigFileAppliesRuntimeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`mihomo:
  controller_socket: /tmp/browsebox.sock
  controller_url: http://127.0.0.1:9097
  controller_pipe: \\.\pipe\verge-mihomo
  config_path: ~/mihomo/config.yaml
  binary_path: ~/bin/mihomo
browser:
  chrome_path: /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
  profile_dir: ~/.config/browsebox/chrome-profile
  headless: true
  chrome_args:
    - no-first-run
    - no-default-browser-check
    - disable-background-networking
    - disable-component-update
runtime:
  dir: /tmp/browsebox-runtime
  cache_dir: ~/.cache/browsebox/mihomo
  state_dir: ~/.browsebox-state
  keep: true
ports:
  proxy: 19001
  controller: 19002
  devtools: 9333
session:
  group: XFLTD
  node: "node-a"
  url: https://example.com/start
  health_urls:
    - https://example.com/health
    - https://static.example.com/health
nodes:
  concurrency: 24
  delay_timeout_ms: 2500
  show_unhealthy: true
  highlight_current: false
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir returned error: %v", err)
	}
	opts := DefaultOptions()

	if err := LoadConfigFile(path, &opts); err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}

	checks := map[string]bool{
		"ControllerSocket":     opts.ControllerSocket == "/tmp/browsebox.sock",
		"ControllerURL":        opts.ControllerURL == "http://127.0.0.1:9097",
		"ControllerPipe":       opts.ControllerPipe == `\\.\pipe\verge-mihomo`,
		"SourceConfigPath":     opts.SourceConfigPath == filepath.Join(home, "mihomo", "config.yaml"),
		"MihomoBinaryPath":     opts.MihomoBinaryPath == filepath.Join(home, "bin", "mihomo"),
		"ChromeBinaryPath":     opts.ChromeBinaryPath == "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"ChromeProfileDir":     opts.ChromeProfileDir == filepath.Join(home, ".config", "browsebox", "chrome-profile"),
		"ChromeArgs":           len(opts.ChromeArgs) == 4 && opts.ChromeArgs[0] == "no-first-run" && opts.ChromeArgs[3] == "disable-component-update",
		"BrowserHeadless":      opts.BrowserHeadless,
		"RuntimeDir":           opts.RuntimeDir == "/tmp/browsebox-runtime",
		"RuntimeCacheDir":      opts.RuntimeCacheDir == filepath.Join(home, ".cache", "browsebox", "mihomo"),
		"StateDir":             opts.StateDir == filepath.Join(home, ".browsebox-state"),
		"Keep":                 opts.Keep,
		"ProxyPort":            opts.ProxyPort == 19001,
		"ControllerPort":       opts.ControllerPort == 19002,
		"DevToolsPort":         opts.DevToolsPort == 9333,
		"Group":                opts.Group == "XFLTD",
		"DefaultNode":          opts.DefaultNode == "node-a",
		"TargetURL":            opts.TargetURL == "https://example.com/start",
		"NodesConcurrency":     opts.NodesConcurrency == 24,
		"DelayTimeoutMS":       opts.DelayTimeoutMS == 2500,
		"ShowUnhealthyNodes":   opts.ShowUnhealthyNodes,
		"HighlightCurrentNode": !opts.HighlightCurrentNode,
		"HealthURLs":           len(opts.HealthURLs) == 2 && opts.HealthURLs[0] == "https://example.com/health" && opts.HealthURLs[1] == "https://static.example.com/health",
	}
	for name, ok := range checks {
		if !ok {
			t.Fatalf("%s not applied correctly: %#v", name, opts)
		}
	}
}

func TestLoadConfigFileAppendsChromeArgsToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("browser:\n  chrome_args:\n    - disable-background-networking\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := DefaultOptions()

	if err := LoadConfigFile(path, &opts); err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	want := []string{"no-first-run", "no-default-browser-check", "disable-background-networking"}
	if !reflect.DeepEqual(opts.ChromeArgs, want) {
		t.Fatalf("ChromeArgs = %#v, want %#v", opts.ChromeArgs, want)
	}
}

func TestLoadConfigFileKeepsExplicitEmptyChromeArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("browser:\n  chrome_args: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := DefaultOptions()

	if err := LoadConfigFile(path, &opts); err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if opts.ChromeArgs == nil || len(opts.ChromeArgs) != 0 {
		t.Fatalf("ChromeArgs = %#v, want explicit empty slice", opts.ChromeArgs)
	}
}

func TestLoadConfigFileMissingSucceeds(t *testing.T) {
	opts := DefaultOptions()
	if err := LoadConfigFile(filepath.Join(t.TempDir(), "missing.yaml"), &opts); err != nil {
		t.Fatalf("LoadConfigFile missing returned error: %v", err)
	}
}

func TestLoadConfigFileRejectsInvalidNodesTuning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("nodes:\n  concurrency: 0\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := DefaultOptions()

	if err := LoadConfigFile(path, &opts); err == nil {
		t.Fatal("LoadConfigFile returned nil error, want invalid config error")
	}
}

func TestLoadConfigFileRejectsInvalidNodesBooleans(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "show unhealthy",
			content: "nodes:\n  show_unhealthy: maybe\n",
			want:    "show_unhealthy must be true or false",
		},
		{
			name:    "highlight current",
			content: "nodes:\n  highlight_current: maybe\n",
			want:    "highlight_current must be true or false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			opts := DefaultOptions()

			err := LoadConfigFile(path, &opts)
			if err == nil {
				t.Fatal("LoadConfigFile returned nil error, want invalid config error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
