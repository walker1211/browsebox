package skillsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPathsIncludesRepositoryClaudeAndCodexTargets(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	paths := DefaultPaths(repoRoot, home)

	want := map[string]string{
		"repository codex mirror": filepath.Join(repoRoot, ".agents", "skills", skillName),
		"claude install":          filepath.Join(home, ".claude", "skills", skillName),
		"codex install":           filepath.Join(home, ".agents", "skills", skillName),
	}
	got := map[string]string{
		"repository codex mirror": paths.RepoCodexMirrorDir,
		"claude install":          paths.ClaudeInstallDir,
		"codex install":           paths.CodexInstallDir,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func TestApplyThenCheckAllTargets(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	paths := DefaultPaths(repoRoot, home)

	apply, err := paths.Apply()
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	wantApplied := []string{
		paths.RepoCodexMirrorDir,
		paths.ClaudeInstallDir,
		paths.CodexInstallDir,
	}
	if !reflect.DeepEqual(apply.Applied, wantApplied) {
		t.Fatalf("applied = %#v, want %#v", apply.Applied, wantApplied)
	}
	if len(apply.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", apply.Warnings)
	}
	for _, target := range wantApplied {
		content, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
		if err != nil {
			t.Fatalf("read installed skill at %s: %v", target, err)
		}
		if !strings.Contains(string(content), "name: browsebox") {
			t.Fatalf("installed skill content at %s = %q", target, string(content))
		}
	}

	check, err := paths.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(check.Drift) != 0 {
		t.Fatalf("drift after apply = %v", check.Drift)
	}
}

func TestCheckReportsDriftAcrossAllTargets(t *testing.T) {
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	paths := DefaultPaths(repoRoot, home)
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.RepoCodexMirrorDir, "SKILL.md"), []byte("different"), 0o644); err != nil {
		t.Fatalf("write repository mirror: %v", err)
	}
	if err := os.Remove(filepath.Join(paths.ClaudeInstallDir, "references", "usage.md")); err != nil {
		t.Fatalf("remove Claude reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.CodexInstallDir, "extra.md"), []byte("extra"), 0o644); err != nil {
		t.Fatalf("write Codex extra file: %v", err)
	}

	result, err := paths.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	joined := strings.Join(result.Drift, "\n")
	for _, want := range []string{
		"repository codex mirror file differs: SKILL.md",
		"claude install file missing: references/usage.md",
		"codex install extra file: extra.md",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("drift = %v, want %q", result.Drift, want)
		}
	}
}

func TestApplyRemovesStaleFilesAcrossAllTargets(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	for _, target := range paths.targets() {
		if err := os.WriteFile(filepath.Join(target.Dir, "stale.md"), []byte("stale"), 0o644); err != nil {
			t.Fatalf("write stale file for %s: %v", target.Label, err)
		}
	}

	if _, err := paths.Apply(); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	for _, target := range paths.targets() {
		if _, err := os.Stat(filepath.Join(target.Dir, "stale.md")); !os.IsNotExist(err) {
			t.Fatalf("stale file for %s still exists or stat failed: %v", target.Label, err)
		}
	}
}

func TestApplyStagesEveryTargetBeforeReplacingExistingTrees(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	repositoryBefore := mustReadFile(t, filepath.Join(paths.RepoCodexMirrorDir, "SKILL.md"))
	claudeBefore := mustReadFile(t, filepath.Join(paths.ClaudeInstallDir, "SKILL.md"))
	if err := os.WriteFile(filepath.Join(paths.sourceDir(), "SKILL.md"), []byte("new source"), 0o644); err != nil {
		t.Fatalf("change source: %v", err)
	}

	badParent := filepath.Join(t.TempDir(), "skills")
	if err := os.WriteFile(badParent, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write bad parent: %v", err)
	}
	paths.CodexInstallDir = filepath.Join(badParent, skillName)
	paths.codexInstallParent = badParent

	if _, err := paths.Apply(); err == nil {
		t.Fatal("apply succeeded with an unusable Codex parent")
	}
	assertFileEquals(t, filepath.Join(paths.RepoCodexMirrorDir, "SKILL.md"), repositoryBefore)
	assertFileEquals(t, filepath.Join(paths.ClaudeInstallDir, "SKILL.md"), claudeBefore)
	assertNoTemporarySkillDirs(t, paths.repoCodexMirrorParent)
	assertNoTemporarySkillDirs(t, paths.claudeInstallParent)
}

func TestApplyValidatesEveryDestinationBeforeReplacingTargets(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	repositoryBefore := mustReadFile(t, filepath.Join(paths.RepoCodexMirrorDir, "SKILL.md"))
	claudeBefore := mustReadFile(t, filepath.Join(paths.ClaudeInstallDir, "SKILL.md"))
	if err := os.WriteFile(filepath.Join(paths.sourceDir(), "SKILL.md"), []byte("new source"), 0o644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	paths.CodexInstallDir = filepath.Join(t.TempDir(), "skills", skillName)

	_, err := paths.Apply()
	if err == nil || !strings.Contains(err.Error(), "outside expected install parent") {
		t.Fatalf("err = %v, want destination boundary error", err)
	}
	assertFileEquals(t, filepath.Join(paths.RepoCodexMirrorDir, "SKILL.md"), repositoryBefore)
	assertFileEquals(t, filepath.Join(paths.ClaudeInstallDir, "SKILL.md"), claudeBefore)
}

func TestPromoteTargetsRollsBackEveryChangedTarget(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	before := make(map[string][]byte)
	for _, target := range paths.targets() {
		before[target.Dir] = mustReadFile(t, filepath.Join(target.Dir, "SKILL.md"))
	}
	if err := os.WriteFile(filepath.Join(paths.sourceDir(), "SKILL.md"), []byte("new source"), 0o644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	prepared, err := prepareTargets(paths.sourceDir(), paths.targets())
	if err != nil {
		t.Fatalf("prepare targets: %v", err)
	}
	if err := os.RemoveAll(prepared[1].stagedDir); err != nil {
		t.Fatalf("remove staged Claude target: %v", err)
	}
	if _, err := promoteTargets(prepared); err == nil {
		t.Fatal("promote succeeded after a staged target was removed")
	}
	for _, target := range paths.targets() {
		assertFileEquals(t, filepath.Join(target.Dir, "SKILL.md"), before[target.Dir])
		assertNoTemporarySkillDirs(t, target.Parent)
	}
}

func TestApplyRejectsTargetThatWouldReplaceCanonicalSource(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	paths.ClaudeInstallDir = paths.sourceDir()
	paths.claudeInstallParent = filepath.Dir(paths.sourceDir())

	_, err := paths.Apply()
	if err == nil || !strings.Contains(err.Error(), "must not replace the canonical source") {
		t.Fatalf("err = %v, want canonical source protection", err)
	}
}

func TestApplyRejectsParentSymlinkThatAliasesCanonicalSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires extra privileges on Windows")
	}
	repoRoot := writeTestRepo(t)
	sourcePath := filepath.Join(repoRoot, ".claude", "skills", skillName, "SKILL.md")
	sourceBefore := mustReadFile(t, sourcePath)
	if err := os.Symlink(filepath.Join(repoRoot, ".claude"), filepath.Join(repoRoot, ".agents")); err != nil {
		t.Fatalf("create repository .agents alias: %v", err)
	}

	_, err := DefaultPaths(repoRoot, t.TempDir()).Apply()
	if err == nil || !strings.Contains(err.Error(), "aliases the canonical source through its parent") {
		t.Fatalf("err = %v, want canonical source alias rejection", err)
	}
	assertFileEquals(t, sourcePath, sourceBefore)
}

func TestApplyRejectsParentSymlinkThatAliasesAnotherTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires extra privileges on Windows")
	}
	repoRoot := writeTestRepo(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("create Claude parent: %v", err)
	}
	if err := os.Symlink(filepath.Join(home, ".claude"), filepath.Join(home, ".agents")); err != nil {
		t.Fatalf("create user .agents alias: %v", err)
	}

	_, err := DefaultPaths(repoRoot, home).Apply()
	if err == nil || !strings.Contains(err.Error(), "codex install destination aliases claude install through its parent") {
		t.Fatalf("err = %v, want duplicate physical target rejection", err)
	}
}

func TestApplyReportsBackupCleanupFailureAsWarningAfterCommit(t *testing.T) {
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	updated := []byte("---\nname: browsebox\ndescription: updated\n---\n\n# browsebox\n")
	if err := os.WriteFile(filepath.Join(paths.sourceDir(), "SKILL.md"), updated, 0o644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	oldRemoveBackupDir := removeBackupDir
	t.Cleanup(func() { removeBackupDir = oldRemoveBackupDir })
	removeBackupDir = func(string) error { return errors.New("directory busy") }

	result, err := paths.Apply()
	if err != nil {
		t.Fatalf("apply returned an error after commit: %v", err)
	}
	if len(result.Applied) != 3 {
		t.Fatalf("applied = %v", result.Applied)
	}
	if len(result.Warnings) != 3 {
		t.Fatalf("warnings = %v, want one per old target", result.Warnings)
	}
	for _, warning := range result.Warnings {
		if !strings.Contains(warning, "directory busy") {
			t.Fatalf("warning = %q", warning)
		}
	}
	for _, target := range paths.targets() {
		assertFileEquals(t, filepath.Join(target.Dir, "SKILL.md"), updated)
	}
}

func TestCheckReportsDestinationSymlinkAsDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires extra privileges on Windows")
	}
	repoRoot := writeTestRepo(t)
	paths := DefaultPaths(repoRoot, t.TempDir())
	if _, err := paths.Apply(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := os.Remove(filepath.Join(paths.CodexInstallDir, "SKILL.md")); err != nil {
		t.Fatalf("remove Codex SKILL.md: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(paths.CodexInstallDir, "SKILL.md")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	result, err := paths.Check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(strings.Join(result.Drift, "\n"), "codex install file is symlink: SKILL.md") {
		t.Fatalf("drift = %v", result.Drift)
	}
}

func TestRepositoryCodexMirrorMatchesCanonicalSource(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot, err := FindRepositoryRoot(cwd)
	if err != nil {
		t.Fatalf("find repository root: %v", err)
	}
	paths := DefaultPaths(repoRoot, t.TempDir())
	drift, err := compareSkillTrees(paths.sourceDir(), paths.RepoCodexMirrorDir, "repository codex mirror", "repository codex mirror")
	if err != nil {
		t.Fatalf("compare repository mirror: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("repository Codex mirror drift = %v; run ./skill-sync --apply", drift)
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

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got := mustReadFile(t, path)
	if !bytes.Equal(got, want) {
		t.Fatalf("file %s = %q, want %q", path, string(got), string(want))
	}
}

func assertNoTemporarySkillDirs(t *testing.T, parent string) {
	t.Helper()
	for _, pattern := range []string{".browsebox-skill-stage-*", ".browsebox-skill-backup-*"} {
		matches, err := filepath.Glob(filepath.Join(parent, pattern))
		if err != nil {
			t.Fatalf("glob temporary skill directories: %v", err)
		}
		if len(matches) > 0 {
			t.Fatalf("temporary skill directories remain under %s: %v", parent, matches)
		}
	}
}
