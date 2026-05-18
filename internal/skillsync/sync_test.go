package skillsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyThenCheck(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	paths := DefaultPaths(repoRoot, home)

	apply, err := paths.Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(apply.Applied) != 1 || apply.Applied[0] != paths.ClaudeInstallDir {
		t.Fatalf("applied = %#v, want %q", apply.Applied, paths.ClaudeInstallDir)
	}
	content, err := os.ReadFile(filepath.Join(paths.ClaudeInstallDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if !strings.Contains(string(content), "name: browsebox") {
		t.Fatalf("installed skill content = %q", string(content))
	}

	check, err := paths.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(check.Drift) != 0 {
		t.Fatalf("drift after apply = %v", check.Drift)
	}
}

func TestCheckReportsDrift(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	paths := DefaultPaths(repoRoot, home)
	if err := os.MkdirAll(paths.ClaudeInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.ClaudeInstallDir, "SKILL.md"), []byte("different"), 0o644); err != nil {
		t.Fatalf("write install: %v", err)
	}

	result, err := paths.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	joined := strings.Join(result.Drift, "\n")
	if !strings.Contains(joined, "claude install file differs: SKILL.md") {
		t.Fatalf("drift = %v", result.Drift)
	}
	if !strings.Contains(joined, "claude install file missing: references/usage.md") {
		t.Fatalf("drift = %v", result.Drift)
	}
}

func TestFindRepositoryRoot(t *testing.T) {
	repoRoot := writeTestRepo(t)
	nested := filepath.Join(repoRoot, "internal", "skillsync")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	found, err := FindRepositoryRoot(nested)
	if err != nil {
		t.Fatalf("find root: %v", err)
	}
	if found != repoRoot {
		t.Fatalf("root = %q, want %q", found, repoRoot)
	}
}

func TestValidateSourceRejectsMarkers(t *testing.T) {
	repoRoot := writeTestRepo(t)
	path := filepath.Join(repoRoot, ".claude", "skills", skillName, "SKILL.md")
	if err := os.WriteFile(path, []byte("TODO remove me"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	_, err := DefaultPaths(repoRoot, t.TempDir()).Check()
	if err == nil || !strings.Contains(err.Error(), "unresolved marker") {
		t.Fatalf("err = %v, want unresolved marker", err)
	}
}

func writeTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/browsebox\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	skillDir := filepath.Join(repoRoot, ".claude", "skills", skillName)
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: browsebox\ndescription: test\n---\n\n# browsebox\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "usage.md"), []byte("# usage\n"), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	return repoRoot
}
