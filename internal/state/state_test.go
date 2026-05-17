package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTripCreatesPrivateFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	want := Session{
		ManagedBy:        "browsebox",
		MihomoPID:        1234,
		ChromePID:        5678,
		ProxyPort:        17997,
		ControllerPort:   17998,
		DevToolsPort:     9223,
		RuntimeDir:       "/tmp/browsebox-runtime",
		ChromeDir:        "/tmp/browsebox-runtime/chrome-profile",
		Group:            "All",
		Node:             "node-a",
		URL:              "https://example.com/start",
		StartedAt:        "2026-05-17T10:11:12Z",
		MihomoBinaryPath: "/bin/mihomo",
		ChromeBinaryPath: "/bin/chrome",
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state dir mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %o, want 600", got)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestSaveTightensExistingLoosePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("create loose state dir: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod loose state dir: %v", err)
	}
	if err := os.WriteFile(Path(dir), []byte(`{"managed_by":"old"}`), 0o666); err != nil {
		t.Fatalf("write loose state file: %v", err)
	}
	if err := os.Chmod(Path(dir), 0o666); err != nil {
		t.Fatalf("chmod loose state file: %v", err)
	}

	want := Session{ManagedBy: "browsebox", MihomoPID: 1234}
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state dir mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %o, want 600", got)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestLoadMissingReturnsNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load missing error = %v, want os.ErrNotExist", err)
	}
}

func TestRemoveMissingSucceeds(t *testing.T) {
	if err := Remove(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("Remove missing returned error: %v", err)
	}
}
