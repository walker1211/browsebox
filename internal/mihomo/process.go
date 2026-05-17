package mihomo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// DefaultBinaryPath returns the macOS Clash Verge mihomo binary path.
func DefaultBinaryPath() string {
	return "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"
}

// WriteRuntimeConfig creates runtimeDir privately and writes config.yaml privately.
func WriteRuntimeConfig(runtimeDir string, content []byte) (string, error) {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		return "", err
	}
	configPath := filepath.Join(runtimeDir, "config.yaml")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return "", err
	}
	return configPath, nil
}

func newCommand(binaryPath, runtimeDir, configPath string) *exec.Cmd {
	return exec.Command(binaryPath, "-d", runtimeDir, "-f", configPath)
}

// StartProcess starts mihomo with the given runtime directory and config path.
func StartProcess(ctx context.Context, binaryPath, runtimeDir, configPath string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "-d", runtimeDir, "-f", configPath)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
