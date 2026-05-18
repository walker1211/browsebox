package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFileAppliesRuntimeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`mihomo:
  controller_socket: /tmp/browsebox.sock
  config_path: ~/mihomo/config.yaml
  binary_path: ~/bin/mihomo
browser:
  chrome_path: /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
  profile_dir: ~/.config/browsebox/chrome-profile
  headless: true
runtime:
  dir: /tmp/browsebox-runtime
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
		"ControllerSocket": opts.ControllerSocket == "/tmp/browsebox.sock",
		"SourceConfigPath": opts.SourceConfigPath == filepath.Join(home, "mihomo", "config.yaml"),
		"MihomoBinaryPath": opts.MihomoBinaryPath == filepath.Join(home, "bin", "mihomo"),
		"ChromeBinaryPath": opts.ChromeBinaryPath == "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"ChromeProfileDir": opts.ChromeProfileDir == filepath.Join(home, ".config", "browsebox", "chrome-profile"),
		"BrowserHeadless":  opts.BrowserHeadless,
		"RuntimeDir":       opts.RuntimeDir == "/tmp/browsebox-runtime",
		"StateDir":         opts.StateDir == filepath.Join(home, ".browsebox-state"),
		"Keep":             opts.Keep,
		"ProxyPort":        opts.ProxyPort == 19001,
		"ControllerPort":   opts.ControllerPort == 19002,
		"DevToolsPort":     opts.DevToolsPort == 9333,
		"Group":            opts.Group == "XFLTD",
		"DefaultNode":      opts.DefaultNode == "node-a",
		"TargetURL":        opts.TargetURL == "https://example.com/start",
		"NodesConcurrency": opts.NodesConcurrency == 24,
		"DelayTimeoutMS":   opts.DelayTimeoutMS == 2500,
		"HealthURLs":       len(opts.HealthURLs) == 2 && opts.HealthURLs[0] == "https://example.com/health" && opts.HealthURLs[1] == "https://static.example.com/health",
	}
	for name, ok := range checks {
		if !ok {
			t.Fatalf("%s not applied correctly: %#v", name, opts)
		}
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
