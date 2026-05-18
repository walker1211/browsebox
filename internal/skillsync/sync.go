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

type Paths struct {
	RepoRoot         string
	ClaudeInstallDir string

	claudeInstallParent string
}

type Result struct {
	Drift   []string
	Applied []string
}

func DefaultPaths(repoRoot string, home string) Paths {
	claudeInstallParent := filepath.Join(home, ".claude", "skills")
	return Paths{
		RepoRoot:            repoRoot,
		ClaudeInstallDir:    filepath.Join(claudeInstallParent, skillName),
		claudeInstallParent: claudeInstallParent,
	}
}

func (p Paths) sourceDir() string {
	return filepath.Join(p.RepoRoot, ".claude", "skills", skillName)
}

func (p Paths) Check() (Result, error) {
	sourceDir := p.sourceDir()
	if err := validateSource(sourceDir); err != nil {
		return Result{}, fmt.Errorf("source invalid: %w", err)
	}
	drift, err := compareSkillTrees(sourceDir, p.ClaudeInstallDir, "claude install", "claude install")
	if err != nil {
		return Result{}, err
	}
	return Result{Drift: drift}, nil
}

func (p Paths) Apply() (Result, error) {
	sourceDir := p.sourceDir()
	if err := validateSource(sourceDir); err != nil {
		return Result{}, fmt.Errorf("source invalid: %w", err)
	}
	if err := validateDestination(p.ClaudeInstallDir, p.claudeInstallParent); err != nil {
		return Result{}, fmt.Errorf("claude destination invalid: %w", err)
	}
	if err := os.RemoveAll(p.ClaudeInstallDir); err != nil {
		return Result{}, fmt.Errorf("remove claude install: %w", err)
	}
	if err := copyDir(sourceDir, p.ClaudeInstallDir); err != nil {
		return Result{}, fmt.Errorf("copy claude skill: %w", err)
	}
	return Result{Applied: []string{p.ClaudeInstallDir}}, nil
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
