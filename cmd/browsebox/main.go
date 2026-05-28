package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/walker1211/browsebox/internal/app"
)

const usageText = `browsebox launches isolated proxy-routed browser sessions.

Usage:
  browsebox [flags] <command> [flags]
  browsebox help
  browsebox --help

Commands:
  groups   List available proxy groups
  nodes    List available proxy nodes
  proxy    Launch a temporary isolated proxy without Chrome
  run      Launch a temporary isolated browser session
  start    Start a persistent isolated browser session
  status   Show browsebox session status
  stop     Stop a running browsebox session

Flags:
  --controller-socket path  Controller Unix socket path
  --controller-url url      Controller HTTP URL for localhost TCP access
  --controller-pipe path    Controller Windows named pipe path
  --config path             Source mihomo config path
  --runtime-dir path        Runtime directory for temporary files
  --runtime-cache-dir path  Cache directory for mihomo geodata files
  --state-dir path          Directory for persistent session state
  --mihomo path             Mihomo binary path
  --interface-name name     Network interface for mihomo outbound dials
  --chrome path             Chrome binary path
  --chrome-profile-dir path Chrome profile directory
  --headless                Launch Chrome in headless mode
  --keep                    Keep runtime files after exit
  --group name              Proxy group name; empty auto-resolves current group
  --node name               Default proxy node name
  --proxy-port port         Local proxy port
  --controller-port port    Local controller port
  --devtools-port port      Browser DevTools port
  --url url                 URL to open
  --target-url url          Legacy alias for --url
  --health-url url          Health check URL; repeat to set multiple URLs
  --nodes-concurrency n     Concurrent node delay checks
  --probe-rounds n          Delay check rounds per health URL when selecting/listing nodes
  --probe-interval-ms ms    Delay between probe rounds in milliseconds
  --delay-timeout-ms ms     Mihomo delay check timeout in milliseconds
  --show-unhealthy bool    Show unhealthy nodes in nodes output
  --highlight-current bool Highlight the current node in nodes output
  --select-fastest          Select the lowest-delay healthy node in the main controller after nodes checks
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts := app.DefaultOptions()
	if shouldLoadConfig(args) {
		if err := app.LoadConfigFile(app.DefaultConfigPath, &opts); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	}
	flags := newFlagSet("browsebox", &opts)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n\n", err)
		printUsage(stderr)
		return 2
	}

	remaining := flags.Args()
	if len(remaining) == 0 {
		printUsage(stdout)
		return 0
	}

	command := remaining[0]
	if command == "help" {
		printUsage(stdout)
		return 0
	}

	if !isKnownCommand(command) {
		fmt.Fprintf(stderr, "error: unknown command %q\n\n", command)
		printUsage(stderr)
		return 2
	}

	commandFlags := newFlagSet("browsebox "+command, &opts)
	if err := commandFlags.Parse(remaining[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n\n", err)
		printUsage(stderr)
		return 2
	}
	if commandFlags.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument %q for command %q\n\n", commandFlags.Arg(0), command)
		printUsage(stderr)
		return 2
	}
	applyCommandDefaults(command, &opts, commandLineOverrides(flags, commandFlags))

	application := app.New(stdout, stderr)
	if err := dispatch(context.Background(), application, command, opts); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

func dispatch(ctx context.Context, application *app.App, command string, opts app.Options) error {
	switch command {
	case "groups":
		return application.Groups(ctx, opts)
	case "nodes":
		return application.Nodes(ctx, opts)
	case "proxy":
		return application.Proxy(ctx, opts)
	case "run":
		return application.Run(ctx, opts)
	case "start":
		return application.Start(ctx, opts)
	case "status":
		return application.Status(ctx, opts)
	case "stop":
		return application.Stop(ctx, opts)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func shouldLoadConfig(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		if arg == "help" || arg == "--help" || arg == "-h" {
			return false
		}
	}
	return true
}

func isKnownCommand(command string) bool {
	switch command {
	case "groups", "nodes", "proxy", "run", "start", "status", "stop":
		return true
	default:
		return false
	}
}

type commandOverrides struct {
	healthURLs    bool
	selectFastest bool
}

func commandLineOverrides(flagSets ...*flag.FlagSet) commandOverrides {
	overrides := commandOverrides{}
	for _, flags := range flagSets {
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "health-url":
				overrides.healthURLs = true
			case "select-fastest":
				overrides.selectFastest = true
			}
		})
	}
	return overrides
}

func applyCommandDefaults(command string, opts *app.Options, overrides commandOverrides) {
	switch command {
	case "nodes":
		if !overrides.healthURLs && opts.NodesHealthURLs != nil {
			opts.HealthURLs = cloneStrings(opts.NodesHealthURLs)
		}
		if !overrides.selectFastest {
			opts.SelectFastest = opts.NodesSelectFastest
		}
	case "proxy", "run", "start":
		if !overrides.healthURLs && opts.SessionHealthURLs != nil {
			opts.HealthURLs = cloneStrings(opts.SessionHealthURLs)
		}
		if !overrides.selectFastest {
			opts.SelectFastest = opts.SessionSelectFastest
		}
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
}

func newFlagSet(name string, opts *app.Options) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	flags.StringVar(&opts.ControllerSocket, "controller-socket", opts.ControllerSocket, "controller Unix socket path")
	flags.StringVar(&opts.ControllerURL, "controller-url", opts.ControllerURL, "controller HTTP URL for localhost TCP access")
	flags.StringVar(&opts.ControllerPipe, "controller-pipe", opts.ControllerPipe, "controller Windows named pipe path")
	flags.StringVar(&opts.SourceConfigPath, "config", opts.SourceConfigPath, "source mihomo config path")
	flags.StringVar(&opts.RuntimeDir, "runtime-dir", opts.RuntimeDir, "runtime directory for temporary files")
	flags.StringVar(&opts.RuntimeCacheDir, "runtime-cache-dir", opts.RuntimeCacheDir, "cache directory for mihomo geodata files")
	flags.StringVar(&opts.StateDir, "state-dir", opts.StateDir, "directory for persistent session state")
	flags.StringVar(&opts.MihomoBinaryPath, "mihomo", opts.MihomoBinaryPath, "mihomo binary path")
	flags.StringVar(&opts.MihomoInterfaceName, "interface-name", opts.MihomoInterfaceName, "network interface for mihomo outbound dials")
	flags.StringVar(&opts.ChromeBinaryPath, "chrome", opts.ChromeBinaryPath, "chrome binary path")
	flags.StringVar(&opts.ChromeProfileDir, "chrome-profile-dir", opts.ChromeProfileDir, "chrome profile directory")
	flags.BoolVar(&opts.BrowserHeadless, "headless", opts.BrowserHeadless, "launch Chrome in headless mode")
	flags.BoolVar(&opts.Keep, "keep", opts.Keep, "keep runtime files after exit")
	flags.StringVar(&opts.Group, "group", opts.Group, "proxy group name")
	flags.StringVar(&opts.DefaultNode, "node", opts.DefaultNode, "default proxy node name")
	flags.IntVar(&opts.ProxyPort, "proxy-port", opts.ProxyPort, "local proxy port")
	flags.IntVar(&opts.ControllerPort, "controller-port", opts.ControllerPort, "local controller port")
	flags.IntVar(&opts.DevToolsPort, "devtools-port", opts.DevToolsPort, "browser DevTools port")
	flags.StringVar(&opts.TargetURL, "url", opts.TargetURL, "URL to open")
	flags.StringVar(&opts.TargetURL, "target-url", opts.TargetURL, "legacy alias for --url")
	flags.Var(&healthURLFlag{values: &opts.HealthURLs}, "health-url", "health check URL; repeat to set multiple URLs")
	flags.IntVar(&opts.NodesConcurrency, "nodes-concurrency", opts.NodesConcurrency, "concurrent node delay checks")
	flags.IntVar(&opts.NodeProbeRounds, "probe-rounds", opts.NodeProbeRounds, "delay check rounds per health URL when selecting/listing nodes")
	flags.IntVar(&opts.NodeProbeIntervalMS, "probe-interval-ms", opts.NodeProbeIntervalMS, "delay between probe rounds in milliseconds")
	flags.IntVar(&opts.DelayTimeoutMS, "delay-timeout-ms", opts.DelayTimeoutMS, "mihomo delay check timeout in milliseconds")
	flags.BoolVar(&opts.ShowUnhealthyNodes, "show-unhealthy", opts.ShowUnhealthyNodes, "show unhealthy nodes in nodes output")
	flags.BoolVar(&opts.HighlightCurrentNode, "highlight-current", opts.HighlightCurrentNode, "highlight the current node in nodes output")
	flags.BoolVar(&opts.SelectFastest, "select-fastest", opts.SelectFastest, "select the lowest-delay healthy node in the main controller after nodes checks")

	return flags
}

type healthURLFlag struct {
	values *[]string
	seen   bool
}

func (f *healthURLFlag) String() string {
	if f.values == nil {
		return ""
	}
	return strings.Join(*f.values, ",")
}

func (f *healthURLFlag) Set(value string) error {
	if !f.seen {
		*f.values = nil
		f.seen = true
	}
	*f.values = append(*f.values, value)
	return nil
}
