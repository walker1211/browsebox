package app

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverControllerSocketPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are not used on Windows")
	}
	dir, err := os.MkdirTemp(".", "browsebox-socket-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	serviceSocket := filepath.Join(dir, "service.sock")
	tempSocket := filepath.Join(dir, "temp.sock")
	for _, path := range []string{serviceSocket, tempSocket} {
		listener, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
	}
	regularFile := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(regularFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missingSocket := filepath.Join(dir, "missing.sock")

	for _, test := range []struct {
		name       string
		candidates []string
		want       string
	}{
		{"service before temp", []string{serviceSocket, tempSocket}, serviceSocket},
		{"missing service falls back to temp", []string{missingSocket, tempSocket}, tempSocket},
		{"regular file is not a socket", []string{regularFile, tempSocket}, tempSocket},
		{"directory is not a socket", []string{dir, tempSocket}, tempSocket},
		{"no live socket keeps legacy default", []string{missingSocket, regularFile}, legacyControllerSocketPath},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := discoverControllerSocketPath(test.candidates...); got != test.want {
				t.Fatalf("discoverControllerSocketPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDefaultSourceConfigPathPrefersGenericMihomoConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
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
	t.Setenv("USERPROFILE", home)
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
	t.Setenv("USERPROFILE", home)
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
