package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const sessionFileName = "session.json"

// Session records browsebox-owned persistent session metadata.
type Session struct {
	ManagedBy        string `json:"managed_by"`
	MihomoPID        int    `json:"mihomo_pid"`
	ChromePID        int    `json:"chrome_pid"`
	ProxyPort        int    `json:"proxy_port"`
	ControllerPort   int    `json:"controller_port"`
	DevToolsPort     int    `json:"devtools_port"`
	RuntimeDir       string `json:"runtime_dir"`
	ChromeDir        string `json:"chrome_dir"`
	Group            string `json:"group"`
	Node             string `json:"node"`
	URL              string `json:"url"`
	StartedAt        string `json:"started_at"`
	MihomoBinaryPath string `json:"mihomo_binary_path,omitempty"`
	ChromeBinaryPath string `json:"chrome_binary_path,omitempty"`
}

// Path returns the full path to the browsebox session state file.
func Path(dir string) string {
	return filepath.Join(dir, sessionFileName)
}

// Save writes session state with private directory and file permissions.
func Save(dir string, session Session) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	tempFile, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, Path(dir)); err != nil {
		return err
	}
	cleanupTemp = false
	return os.Chmod(Path(dir), 0o600)
}

// Load reads session state. Missing state returns an os.IsNotExist-compatible error.
func Load(dir string) (Session, error) {
	content, err := os.ReadFile(Path(dir))
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(content, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// Remove deletes session state. Missing state is treated as success.
func Remove(dir string) error {
	err := os.Remove(Path(dir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
