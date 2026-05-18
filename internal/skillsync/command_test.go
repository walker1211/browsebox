package skillsync

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunApplyAndCheck(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--apply", "--repo-root", repoRoot}, CommandContext{
		Stdout:      &stdout,
		Stderr:      &stderr,
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if exitCode != 0 {
		t.Fatalf("apply exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(home, ".claude", "skills", skillName)) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = Run([]string{"--check", "--repo-root", repoRoot}, CommandContext{
		Stdout:      &stdout,
		Stderr:      &stderr,
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if exitCode != 0 {
		t.Fatalf("check exit = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "skill install is up to date") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunHelpDoesNotNeedContext(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := Run([]string{arg}, CommandContext{Stdout: &stdout, Stderr: &stderr})
			if exitCode != 0 {
				t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsApplyAndCheck(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := Run([]string{"--apply", "--check"}, CommandContext{Stderr: &stderr})
	if exitCode != 2 {
		t.Fatalf("exit = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "choose either --check or --apply") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunFindsRepositoryRoot(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	cwd := filepath.Join(repoRoot, "cmd", "skill-sync")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run([]string{"--apply"}, CommandContext{
		Stdout:      &stdout,
		Stderr:      &stderr,
		Getwd:       func() (string, error) { return cwd, nil },
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if exitCode != 0 {
		t.Fatalf("exit = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}
