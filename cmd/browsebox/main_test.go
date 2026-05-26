package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
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

	if err := flags.Parse([]string{"--nodes-concurrency", "32", "--probe-rounds", "5", "--probe-interval-ms", "250", "--delay-timeout-ms", "2500", "--select-fastest", "--show-unhealthy=true", "--highlight-current=false", "--runtime-cache-dir", "/tmp/cache", "--chrome-profile-dir", "/tmp/profile", "--interface-name", "en0", "--headless"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if opts.NodesConcurrency != 32 {
		t.Fatalf("NodesConcurrency = %d, want 32", opts.NodesConcurrency)
	}
	if opts.NodeProbeRounds != 5 {
		t.Fatalf("NodeProbeRounds = %d, want 5", opts.NodeProbeRounds)
	}
	if opts.NodeProbeIntervalMS != 250 {
		t.Fatalf("NodeProbeIntervalMS = %d, want 250", opts.NodeProbeIntervalMS)
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

func TestCommandFlagsOverrideConfigNodeProbeTuning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("nodes:\n  concurrency: 8\n  probe_rounds: 5\n  probe_interval_ms: 250\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	opts := app.DefaultOptions()
	if err := app.LoadConfigFile(path, &opts); err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}

	flags := newFlagSet("browsebox nodes", &opts)
	if err := flags.Parse([]string{"--nodes-concurrency", "12", "--probe-rounds", "2", "--probe-interval-ms", "50"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if opts.NodesConcurrency != 12 {
		t.Fatalf("NodesConcurrency = %d, want CLI override 12", opts.NodesConcurrency)
	}
	if opts.NodeProbeRounds != 2 {
		t.Fatalf("NodeProbeRounds = %d, want CLI override 2", opts.NodeProbeRounds)
	}
	if opts.NodeProbeIntervalMS != 50 {
		t.Fatalf("NodeProbeIntervalMS = %d, want CLI override 50", opts.NodeProbeIntervalMS)
	}
}

func TestCommandDefaultsUseNodesConfigForNodes(t *testing.T) {
	opts := app.DefaultOptions()
	opts.HealthURLs = []string{"https://x.example"}
	opts.SessionHealthURLs = []string{"https://x.example"}
	opts.NodesHealthURLs = []string{"https://chatgpt.com"}
	opts.SessionSelectFastest = false
	opts.NodesSelectFastest = true

	applyCommandDefaults("nodes", &opts, commandOverrides{})

	wantHealthURLs := []string{"https://chatgpt.com"}
	if !reflect.DeepEqual(opts.HealthURLs, wantHealthURLs) {
		t.Fatalf("HealthURLs = %#v, want %#v", opts.HealthURLs, wantHealthURLs)
	}
	if !opts.SelectFastest {
		t.Fatal("SelectFastest = false, want nodes default true")
	}
}

func TestCommandDefaultsUseSessionConfigForStart(t *testing.T) {
	opts := app.DefaultOptions()
	opts.HealthURLs = []string{"https://chatgpt.com"}
	opts.SessionHealthURLs = []string{"https://x.com", "https://abs.twimg.com"}
	opts.NodesHealthURLs = []string{"https://chatgpt.com"}
	opts.SessionSelectFastest = true
	opts.NodesSelectFastest = false

	applyCommandDefaults("start", &opts, commandOverrides{})

	wantHealthURLs := []string{"https://x.com", "https://abs.twimg.com"}
	if !reflect.DeepEqual(opts.HealthURLs, wantHealthURLs) {
		t.Fatalf("HealthURLs = %#v, want %#v", opts.HealthURLs, wantHealthURLs)
	}
	if !opts.SelectFastest {
		t.Fatal("SelectFastest = false, want session default true")
	}
}

func TestCommandDefaultsKeepExplicitFlags(t *testing.T) {
	opts := app.DefaultOptions()
	opts.HealthURLs = []string{"https://cli.example"}
	opts.NodesHealthURLs = []string{"https://chatgpt.com"}
	opts.SelectFastest = false
	opts.NodesSelectFastest = true

	applyCommandDefaults("nodes", &opts, commandOverrides{healthURLs: true, selectFastest: true})

	wantHealthURLs := []string{"https://cli.example"}
	if !reflect.DeepEqual(opts.HealthURLs, wantHealthURLs) {
		t.Fatalf("HealthURLs = %#v, want %#v", opts.HealthURLs, wantHealthURLs)
	}
	if opts.SelectFastest {
		t.Fatal("SelectFastest = true, want explicit flag value to be preserved")
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
