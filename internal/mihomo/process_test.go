package mihomo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBinaryPathUsesClashVergeMihomo(t *testing.T) {
	want := "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"
	if got := DefaultBinaryPath(); got != want {
		t.Fatalf("DefaultBinaryPath() = %q, want %q", got, want)
	}
}

func TestWriteRuntimeConfigCreatesPrivateRuntimeConfig(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	configPath, err := WriteRuntimeConfig(runtimeDir, []byte("mixed-port: 17997\n"))
	if err != nil {
		t.Fatalf("WriteRuntimeConfig returned error: %v", err)
	}
	if configPath != filepath.Join(runtimeDir, "config.yaml") {
		t.Fatalf("configPath = %q, want runtime config path", configPath)
	}

	dirInfo, err := os.Stat(runtimeDir)
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("runtime dir mode = %o, want 0700", got)
	}

	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 0600", got)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(content) != "mixed-port: 17997\n" {
		t.Fatalf("config content = %q", string(content))
	}
}

func TestNewCommandConstructsMihomoRuntimeCommandWithoutStartingIt(t *testing.T) {
	cmd := newCommand("/bin/mihomo", "/tmp/browsebox-runtime", "/tmp/browsebox-runtime/config.yaml")
	if cmd.Path != "/bin/mihomo" {
		t.Fatalf("cmd.Path = %q, want /bin/mihomo", cmd.Path)
	}
	wantArgs := []string{"/bin/mihomo", "-d", "/tmp/browsebox-runtime", "-f", "/tmp/browsebox-runtime/config.yaml"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}
	for i := range wantArgs {
		if cmd.Args[i] != wantArgs[i] {
			t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
		}
	}
	if cmd.Process != nil {
		t.Fatalf("cmd.Process is set; command should not have been started")
	}
}
