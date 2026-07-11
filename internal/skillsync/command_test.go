package skillsync

import (
	"bytes"
	"errors"
	"os"
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
	if !strings.Contains(stdout.String(), filepath.Join(home, ".agents", "skills", skillName)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(repoRoot, ".agents", "skills", skillName)) {
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
	if !strings.Contains(stdout.String(), "skill mirrors and installs are up to date") {
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
			if !strings.Contains(stdout.String(), "Claude and Codex") {
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

func TestRunDefaultsToCheckingAllTargets(t *testing.T) {
	repoRoot := writeTestRepo(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run([]string{"--repo-root", repoRoot}, CommandContext{
		Stdout:      &stdout,
		Stderr:      &stderr,
		UserHomeDir: func() (string, error) { return t.TempDir(), nil },
	})
	if exitCode != 1 {
		t.Fatalf("exit = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{"repository codex mirror missing", "claude install missing", "codex install missing"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
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

func TestRunAcceptsRelativeRepositoryRoot(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("current directory: %v", err)
	}
	relativeRoot, err := filepath.Rel(cwd, repoRoot)
	if err != nil {
		t.Fatalf("relative repository root: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"--apply", "--repo-root", relativeRoot}, CommandContext{
		Stdout:      &stdout,
		Stderr:      &stderr,
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if exitCode != 0 {
		t.Fatalf("exit = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), filepath.Join(repoRoot, ".agents", "skills", skillName)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunReportsPostCommitCleanupWarningsWithoutFailing(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	context := CommandContext{
		UserHomeDir: func() (string, error) { return home, nil },
	}
	if exitCode := Run([]string{"--apply", "--repo-root", repoRoot}, context); exitCode != 0 {
		t.Fatalf("initial apply exit = %d", exitCode)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".claude", "skills", skillName, "SKILL.md"), []byte("updated"), 0o644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	oldRemoveBackupDir := removeBackupDir
	t.Cleanup(func() { removeBackupDir = oldRemoveBackupDir })
	removeBackupDir = func(string) error { return errors.New("directory busy") }
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	context.Stdout = &stdout
	context.Stderr = &stderr

	exitCode := Run([]string{"--apply", "--repo-root", repoRoot}, context)
	if exitCode != 0 {
		t.Fatalf("apply exit = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "copied ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "warning: skill sync succeeded but cleanup was incomplete") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
