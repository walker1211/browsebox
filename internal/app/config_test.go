package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFileAppliesNodesTuning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("nodes:\n  concurrency: 24\n  delay_timeout_ms: 2500\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := DefaultOptions()

	if err := LoadConfigFile(path, &opts); err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if opts.NodesConcurrency != 24 {
		t.Fatalf("NodesConcurrency = %d, want 24", opts.NodesConcurrency)
	}
	if opts.DelayTimeoutMS != 2500 {
		t.Fatalf("DelayTimeoutMS = %d, want 2500", opts.DelayTimeoutMS)
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
