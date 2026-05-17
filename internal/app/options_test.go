package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSourceConfigPathPrefersGenericMihomoConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	genericPath := filepath.Join(home, ".config", "mihomo", "config.yaml")
	clashVergePath := filepath.Join(home, "Library", "Application Support", "io.github.clash-verge-rev.clash-verge-rev", "clash-verge.yaml")
	writeFile(t, genericPath)
	writeFile(t, clashVergePath)

	got := defaultSourceConfigPath()
	if got != genericPath {
		t.Fatalf("defaultSourceConfigPath() = %q, want %q", got, genericPath)
	}
}

func TestDefaultSourceConfigPathFallsBackToClashVergeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clashVergePath := filepath.Join(home, "Library", "Application Support", "io.github.clash-verge-rev.clash-verge-rev", "clash-verge.yaml")
	writeFile(t, clashVergePath)

	got := defaultSourceConfigPath()
	if got != clashVergePath {
		t.Fatalf("defaultSourceConfigPath() = %q, want %q", got, clashVergePath)
	}
}

func TestDefaultSourceConfigPathReturnsGenericPathWhenNoCandidateExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "mihomo", "config.yaml")

	got := defaultSourceConfigPath()
	if got != want {
		t.Fatalf("defaultSourceConfigPath() = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("mixed-port: 7890\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}
