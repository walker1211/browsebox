package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/walker1211/browsebox/internal/app"
)

func TestHelpPrintsUsageAndCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"run", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(args, &stdout, &stderr)

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
		})
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

func TestNodesRejectsUnexpectedPositionalArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"nodes", "https://example.com"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run returned %d, want 2; stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unexpected argument \"https://example.com\" for command \"nodes\"") {
		t.Fatalf("stderr = %q, want unexpected argument message", stderr.String())
	}
}

func TestNodeTuningFlagsParse(t *testing.T) {
	opts := app.DefaultOptions()
	flags := newFlagSet("browsebox nodes", &opts)

	if err := flags.Parse([]string{"--nodes-concurrency", "32", "--delay-timeout-ms", "2500", "--select-fastest", "--show-unhealthy=true", "--highlight-current=false", "--runtime-cache-dir", "/tmp/cache", "--chrome-profile-dir", "/tmp/profile", "--interface-name", "en0", "--headless"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if opts.NodesConcurrency != 32 {
		t.Fatalf("NodesConcurrency = %d, want 32", opts.NodesConcurrency)
	}
	if opts.DelayTimeoutMS != 2500 {
		t.Fatalf("DelayTimeoutMS = %d, want 2500", opts.DelayTimeoutMS)
	}
	if !opts.SelectFastest {
		t.Fatal("SelectFastest = false, want true")
	}
	if !opts.ShowUnhealthyNodes {
		t.Fatal("ShowUnhealthyNodes = false, want true")
	}
	if opts.HighlightCurrentNode {
		t.Fatal("HighlightCurrentNode = true, want false")
	}
	if opts.RuntimeCacheDir != "/tmp/cache" {
		t.Fatalf("RuntimeCacheDir = %q, want /tmp/cache", opts.RuntimeCacheDir)
	}
	if opts.ChromeProfileDir != "/tmp/profile" {
		t.Fatalf("ChromeProfileDir = %q, want /tmp/profile", opts.ChromeProfileDir)
	}
	if opts.MihomoInterfaceName != "en0" {
		t.Fatalf("MihomoInterfaceName = %q, want en0", opts.MihomoInterfaceName)
	}
	if !opts.BrowserHeadless {
		t.Fatal("BrowserHeadless = false, want true")
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
