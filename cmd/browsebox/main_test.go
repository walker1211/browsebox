package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpPrintsUsageAndCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"browsebox launches isolated proxy-routed browser sessions.",
		"groups",
		"nodes",
		"run",
		"start",
		"status",
		"stop",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestUnknownCommandPrintsErrorAndUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"nope"}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("run returned 0, want non-zero")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	errOut := stderr.String()
	for _, want := range []string{"unknown command \"nope\"", "Usage:", "groups", "nodes"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

func TestRunCommandParsesMilestone4Flags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{
		"run",
		"--config", "/tmp/source.yaml",
		"--runtime-dir", "/tmp/runtime",
		"--mihomo", "/bin/mihomo",
		"--chrome", "/bin/chrome",
		"--keep",
		"--url", "https://example.com/url-alias",
	}, &stdout, &stderr)

	if code == 2 {
		t.Fatalf("run returned parse error; stderr = %q", stderr.String())
	}
}

func TestTargetURLFlagStillWorks(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"run", "--target-url", "https://example.com/legacy"}, &stdout, &stderr)

	if code == 2 {
		t.Fatalf("run returned parse error; stderr = %q", stderr.String())
	}
}

func TestStatusCommandParsesStateDirFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"status", "--state-dir", t.TempDir()}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run returned %d; stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "No browsebox session") {
		t.Fatalf("stdout = %q, want no-session message", stdout.String())
	}
}
