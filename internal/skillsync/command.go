package skillsync

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

const usageText = `browsebox-skill-sync keeps the browsebox Claude and Codex skills in sync with this repository.

Usage:
  browsebox-skill-sync [--check] [--repo-root path]
  browsebox-skill-sync --apply [--repo-root path]
  browsebox-skill-sync help
  browsebox-skill-sync --help

Flags:
  --apply           Sync the repository Codex mirror and local Claude/Codex installs
  --check           Check all mirrors and installs against the repository source
  --repo-root path  Repository root; defaults to nearest browsebox root
`

type CommandContext struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Getwd       func() (string, error)
	UserHomeDir func() (string, error)
}

func Run(args []string, ctx CommandContext) int {
	stdout := ctx.Stdout
	stderr := ctx.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if wantsHelp(args) {
		return printUsage(stdout)
	}

	fs := flag.NewFlagSet("skill-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "sync the repository Codex mirror and local Claude/Codex installs")
	check := fs.Bool("check", false, "check all mirrors and installs against the repository source")
	repoRootFlag := fs.String("repo-root", "", "repository root; defaults to nearest browsebox root")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printUsage(stdout)
		}
		return printLine(stderr, 2, err.Error())
	}
	if fs.NArg() != 0 {
		return printLine(stderr, 2, "unexpected positional arguments")
	}
	if *apply && *check {
		return printLine(stderr, 2, "choose either --check or --apply")
	}

	repoRoot := *repoRootFlag
	if repoRoot == "" {
		if ctx.Getwd == nil {
			fmt.Fprintln(stderr, "current working directory lookup is not configured")
			return 2
		}
		cwd, err := ctx.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		found, err := FindRepositoryRoot(cwd)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		repoRoot = found
	}
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return printLine(stderr, 1, err.Error())
	}

	if ctx.UserHomeDir == nil {
		return printLine(stderr, 2, "home directory lookup is not configured")
	}
	home, err := ctx.UserHomeDir()
	if err != nil {
		return printLine(stderr, 1, err.Error())
	}
	if home == "" {
		return printLine(stderr, 1, "home directory is empty")
	}
	paths := DefaultPaths(repoRoot, home)

	if *apply {
		result, err := paths.Apply()
		if err != nil {
			return printLine(stderr, 1, err.Error())
		}
		for _, path := range result.Applied {
			if err := writef(stdout, "copied %s\n", path); err != nil {
				return 1
			}
		}
		for _, warning := range result.Warnings {
			if err := writef(stderr, "warning: skill sync succeeded but cleanup was incomplete: %s\n", warning); err != nil {
				return 1
			}
		}
		return 0
	}

	result, err := paths.Check()
	if err != nil {
		return printLine(stderr, 1, err.Error())
	}
	if len(result.Drift) > 0 {
		if err := writeLine(stdout, "skill mirrors or installs are out of sync:"); err != nil {
			return 1
		}
		for _, line := range result.Drift {
			if err := writef(stdout, "- %s\n", line); err != nil {
				return 1
			}
		}
		return 1
	}
	return printLine(stdout, 0, "skill mirrors and installs are up to date")
}

func wantsHelp(args []string) bool {
	return len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func printUsage(w io.Writer) int {
	if err := writef(w, "%s", usageText); err != nil {
		return 1
	}
	return 0
}

func printLine(w io.Writer, exitCode int, text string) int {
	if err := writeLine(w, text); err != nil {
		return 1
	}
	return exitCode
}

func writeLine(w io.Writer, text string) error {
	_, err := fmt.Fprintln(w, text)
	return err
}

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
