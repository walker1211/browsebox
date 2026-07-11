package skillsync

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const skillName = "browsebox"

var removeBackupDir = os.RemoveAll

type Paths struct {
	RepoRoot           string
	RepoCodexMirrorDir string
	ClaudeInstallDir   string
	CodexInstallDir    string

	repoCodexMirrorParent string
	claudeInstallParent   string
	codexInstallParent    string
}

type syncTarget struct {
	Label  string
	Dir    string
	Parent string
}

type preparedTarget struct {
	target      syncTarget
	stagedDir   string
	backupDir   string
	hadOriginal bool
	promoted    bool
}

type Result struct {
	Drift    []string
	Applied  []string
	Warnings []string
}

func DefaultPaths(repoRoot string, home string) Paths {
	repoRoot = absolutePathOrClean(repoRoot)
	home = absolutePathOrClean(home)
	repoCodexMirrorParent := filepath.Join(repoRoot, ".agents", "skills")
	claudeInstallParent := filepath.Join(home, ".claude", "skills")
	codexInstallParent := filepath.Join(home, ".agents", "skills")
	return Paths{
		RepoRoot:              repoRoot,
		RepoCodexMirrorDir:    filepath.Join(repoCodexMirrorParent, skillName),
		ClaudeInstallDir:      filepath.Join(claudeInstallParent, skillName),
		CodexInstallDir:       filepath.Join(codexInstallParent, skillName),
		repoCodexMirrorParent: repoCodexMirrorParent,
		claudeInstallParent:   claudeInstallParent,
		codexInstallParent:    codexInstallParent,
	}
}

func (p Paths) sourceDir() string {
	return filepath.Join(p.RepoRoot, ".claude", "skills", skillName)
}

func (p Paths) targets() []syncTarget {
	return []syncTarget{
		{
			Label:  "repository codex mirror",
			Dir:    p.RepoCodexMirrorDir,
			Parent: p.repoCodexMirrorParent,
		},
		{
			Label:  "claude install",
			Dir:    p.ClaudeInstallDir,
			Parent: p.claudeInstallParent,
		},
		{
			Label:  "codex install",
			Dir:    p.CodexInstallDir,
			Parent: p.codexInstallParent,
		},
	}
}

func (p Paths) Check() (Result, error) {
	sourceDir := p.sourceDir()
	if err := validateSource(sourceDir); err != nil {
		return Result{}, fmt.Errorf("source invalid: %w", err)
	}
	var result Result
	for _, target := range p.targets() {
		drift, err := compareSkillTrees(sourceDir, target.Dir, target.Label, target.Label)
		if err != nil {
			return Result{}, err
		}
		result.Drift = append(result.Drift, drift...)
	}
	return result, nil
}

func (p Paths) Apply() (Result, error) {
	sourceDir := p.sourceDir()
	if err := validateSource(sourceDir); err != nil {
		return Result{}, fmt.Errorf("source invalid: %w", err)
	}
	targets := p.targets()
	if err := validateTargets(sourceDir, targets); err != nil {
		return Result{}, err
	}
	prepared, err := prepareTargets(sourceDir, targets)
	if err != nil {
		return Result{}, err
	}
	cleanupWarnings, err := promoteTargets(prepared)
	if err != nil {
		return Result{}, err
	}
	applied := make([]string, 0, len(targets))
	for _, target := range targets {
		applied = append(applied, target.Dir)
	}
	warnings := make([]string, 0, len(cleanupWarnings))
	for _, warning := range cleanupWarnings {
		warnings = append(warnings, warning.Error())
	}
	return Result{Applied: applied, Warnings: warnings}, nil
}

func FindRepositoryRoot(cwd string) (string, error) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && dirExists(filepath.Join(dir, ".claude", "skills", skillName)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find browsebox repository root from %s", cwd)
		}
		dir = parent
	}
}

func compareSkillTrees(sourceDir string, destinationDir string, label string, missingLabel string) ([]string, error) {
	sourceFiles, err := listFiles(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("read source skill: %w", err)
	}
	destinationFiles, err := listFilesWithSymlinks(destinationDir, true)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{fmt.Sprintf("%s missing: %s", missingLabel, destinationDir)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}

	var drift []string
	for rel, sourcePath := range sourceFiles {
		destinationPath, ok := destinationFiles[rel]
		if !ok {
			drift = append(drift, fmt.Sprintf("%s file missing: %s", label, rel))
			continue
		}
		isLink, err := isSymlink(destinationPath)
		if err != nil {
			return nil, err
		}
		if isLink {
			drift = append(drift, fmt.Sprintf("%s file is symlink: %s", label, rel))
			continue
		}
		same, err := sameFile(sourcePath, destinationPath)
		if err != nil {
			return nil, err
		}
		if !same {
			drift = append(drift, fmt.Sprintf("%s file differs: %s", label, rel))
		}
	}
	for rel := range destinationFiles {
		if _, ok := sourceFiles[rel]; !ok {
			drift = append(drift, fmt.Sprintf("%s extra file: %s", label, rel))
		}
	}
	sort.Strings(drift)
	return drift, nil
}

func validateSource(root string) error {
	files, err := listFiles(root)
	if err != nil {
		return err
	}
	if _, ok := files["SKILL.md"]; !ok {
		return fmt.Errorf("SKILL.md missing")
	}
	markers := []string{"T" + "BD", "TO" + "DO", "PLACE" + "HOLDER"}
	secretAssignments := []string{
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"X_AUTH_TOKEN=",
		"TWITTER_AUTH_TOKEN=",
		"auth_token=",
	}
	for rel, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				return fmt.Errorf("%s contains unresolved marker %q", rel, marker)
			}
		}
		for _, assignment := range secretAssignments {
			if strings.Contains(text, assignment) {
				return fmt.Errorf("%s contains secret assignment %q", rel, assignment)
			}
		}
	}
	return nil
}

func validateDestination(path string, installParent string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("destination must be absolute: %s", path)
	}
	if !filepath.IsAbs(installParent) {
		return fmt.Errorf("expected install parent must be absolute: %s", installParent)
	}
	clean := filepath.Clean(path)
	cleanParent := filepath.Clean(installParent)
	if filepath.Base(clean) != skillName {
		return fmt.Errorf("destination must end with %s: %s", skillName, path)
	}
	if filepath.Base(filepath.Dir(clean)) != "skills" {
		return fmt.Errorf("destination parent must be skills: %s", path)
	}
	rel, err := filepath.Rel(cleanParent, clean)
	if err != nil {
		return err
	}
	if rel != skillName {
		return fmt.Errorf("destination outside expected install parent %s: %s", cleanParent, path)
	}
	return nil
}

func validateTargets(sourceDir string, targets []syncTarget) error {
	sourceDir = filepath.Clean(sourceDir)
	seen := make(map[string]string, len(targets))
	for _, target := range targets {
		if err := validateDestination(target.Dir, target.Parent); err != nil {
			return fmt.Errorf("%s destination invalid: %w", target.Label, err)
		}
		clean := filepath.Clean(target.Dir)
		if clean == sourceDir {
			return fmt.Errorf("%s destination must not replace the canonical source: %s", target.Label, target.Dir)
		}
		if previous, ok := seen[clean]; ok {
			return fmt.Errorf("%s destination duplicates %s: %s", target.Label, previous, target.Dir)
		}
		seen[clean] = target.Label
	}
	for _, target := range targets {
		if err := os.MkdirAll(target.Parent, 0o755); err != nil {
			return fmt.Errorf("create %s parent: %w", target.Label, err)
		}
	}
	if err := validatePhysicalTargetAliases(sourceDir, targets); err != nil {
		return err
	}
	return nil
}

func validatePhysicalTargetAliases(sourceDir string, targets []syncTarget) error {
	sourceInfo, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("inspect canonical source: %w", err)
	}
	sourceParentInfo, err := os.Stat(filepath.Dir(sourceDir))
	if err != nil {
		return fmt.Errorf("inspect canonical source parent: %w", err)
	}

	type physicalTarget struct {
		label      string
		parentInfo fs.FileInfo
		targetInfo fs.FileInfo
	}
	var physicalTargets []physicalTarget
	for _, target := range targets {
		parentInfo, err := os.Stat(target.Parent)
		if err != nil {
			return fmt.Errorf("inspect %s parent: %w", target.Label, err)
		}
		if os.SameFile(sourceParentInfo, parentInfo) {
			return fmt.Errorf("%s destination aliases the canonical source through its parent: %s", target.Label, target.Dir)
		}

		var targetInfo fs.FileInfo
		if info, err := os.Stat(target.Dir); err == nil {
			targetInfo = info
			if os.SameFile(sourceInfo, info) {
				return fmt.Errorf("%s destination aliases the canonical source: %s", target.Label, target.Dir)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect %s destination: %w", target.Label, err)
		}

		for _, previous := range physicalTargets {
			if os.SameFile(previous.parentInfo, parentInfo) {
				return fmt.Errorf("%s destination aliases %s through its parent: %s", target.Label, previous.label, target.Dir)
			}
			if targetInfo != nil && previous.targetInfo != nil && os.SameFile(previous.targetInfo, targetInfo) {
				return fmt.Errorf("%s destination aliases %s: %s", target.Label, previous.label, target.Dir)
			}
		}
		physicalTargets = append(physicalTargets, physicalTarget{
			label:      target.Label,
			parentInfo: parentInfo,
			targetInfo: targetInfo,
		})
	}
	return nil
}

func absolutePathOrClean(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return absolute
}

func prepareTargets(sourceDir string, targets []syncTarget) (result []preparedTarget, resultErr error) {
	var prepared []preparedTarget
	sourceInfo, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("inspect source skill: %w", err)
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, cleanupPreparedTargets(prepared))
		}
	}()

	for _, target := range targets {
		if err := os.MkdirAll(target.Parent, 0o755); err != nil {
			return nil, fmt.Errorf("create %s parent: %w", target.Label, err)
		}
		stagedDir, err := os.MkdirTemp(target.Parent, ".browsebox-skill-stage-")
		if err != nil {
			return nil, fmt.Errorf("stage %s: %w", target.Label, err)
		}
		prepared = append(prepared, preparedTarget{target: target, stagedDir: stagedDir})
		if err := copyDir(sourceDir, stagedDir); err != nil {
			return nil, fmt.Errorf("copy staged %s: %w", target.Label, err)
		}
		if err := os.Chmod(stagedDir, sourceInfo.Mode().Perm()); err != nil {
			return nil, fmt.Errorf("set staged %s permissions: %w", target.Label, err)
		}
	}

	for _, item := range prepared {
		drift, err := compareSkillTrees(sourceDir, item.stagedDir, item.target.Label+" stage", item.target.Label+" stage")
		if err != nil {
			return nil, fmt.Errorf("verify staged %s: %w", item.target.Label, err)
		}
		if len(drift) > 0 {
			return nil, fmt.Errorf("verify staged %s: %s", item.target.Label, strings.Join(drift, "; "))
		}
	}
	return prepared, nil
}

func promoteTargets(prepared []preparedTarget) ([]error, error) {
	for index := range prepared {
		if err := promoteTarget(&prepared[index]); err != nil {
			rollbackErr := rollbackTargets(prepared[:index+1])
			return nil, errors.Join(
				err,
				rollbackErr,
				cleanupStagedTargets(prepared),
			)
		}
	}

	var cleanupErrors []error
	for index := range prepared {
		if prepared[index].backupDir == "" {
			continue
		}
		if err := removeBackupDir(prepared[index].backupDir); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove old %s backup %s: %w", prepared[index].target.Label, prepared[index].backupDir, err))
			continue
		}
		prepared[index].backupDir = ""
	}
	return cleanupErrors, nil
}

func promoteTarget(item *preparedTarget) error {
	_, err := os.Lstat(item.target.Dir)
	if err == nil {
		backupDir, reserveErr := reserveSiblingPath(item.target.Parent)
		if reserveErr != nil {
			return fmt.Errorf("reserve old %s: %w", item.target.Label, reserveErr)
		}
		item.backupDir = backupDir
		if err := os.Rename(item.target.Dir, backupDir); err != nil {
			return fmt.Errorf("backup old %s: %w", item.target.Label, err)
		}
		item.hadOriginal = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect old %s: %w", item.target.Label, err)
	}

	if err := os.Rename(item.stagedDir, item.target.Dir); err != nil {
		return fmt.Errorf("promote %s: %w", item.target.Label, err)
	}
	item.stagedDir = ""
	item.promoted = true
	return nil
}

func rollbackTargets(prepared []preparedTarget) error {
	var rollbackErrors []error
	for index := len(prepared) - 1; index >= 0; index-- {
		item := &prepared[index]
		if item.promoted {
			if err := os.RemoveAll(item.target.Dir); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove promoted %s during rollback: %w", item.target.Label, err))
				continue
			}
			item.promoted = false
		}
		if item.hadOriginal && item.backupDir != "" {
			if err := os.Rename(item.backupDir, item.target.Dir); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore old %s: %w", item.target.Label, err))
				continue
			}
			item.backupDir = ""
			item.hadOriginal = false
		}
	}
	return errors.Join(rollbackErrors...)
}

func cleanupPreparedTargets(prepared []preparedTarget) error {
	var cleanupErrors []error
	for _, item := range prepared {
		for _, path := range []string{item.stagedDir, item.backupDir} {
			if path == "" {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

func cleanupStagedTargets(prepared []preparedTarget) error {
	var cleanupErrors []error
	for _, item := range prepared {
		if item.stagedDir == "" {
			continue
		}
		if err := os.RemoveAll(item.stagedDir); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func reserveSiblingPath(parent string) (string, error) {
	path, err := os.MkdirTemp(parent, ".browsebox-skill-backup-")
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func listFiles(root string) (map[string]string, error) {
	return listFilesWithSymlinks(root, false)
}

func listFilesWithSymlinks(root string, allowSymlinks bool) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 && !allowSymlinks {
			return fmt.Errorf("source contains symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func copyDir(source string, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("source contains symlink: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode().Perm())
	})
}

func isSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&fs.ModeSymlink != 0, nil
}

func sameFile(left string, right string) (bool, error) {
	leftContent, err := os.ReadFile(left)
	if err != nil {
		return false, err
	}
	rightContent, err := os.ReadFile(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftContent, rightContent), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
