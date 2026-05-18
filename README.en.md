# browsebox

browsebox is a Go standard-library CLI for launching isolated, proxy-routed Google Chrome sessions through a temporary mihomo controller. It can read node information from an existing Clash Verge Rev/mihomo controller and supports both one-shot and persistent browser sessions.

## Requirements

- macOS
- Clash Verge Rev / mihomo with a local controller
- Google Chrome
- Go 1.22+

## Install / build

From the repository root:

```bash
./build.sh
```

Install to `PREFIX/bin` (`/usr/local/bin` by default):

```bash
./build.sh install
```

You can also build manually:

```bash
go build -o browsebox ./cmd/browsebox
```

You can also run it without creating a binary:

```bash
go run ./cmd/browsebox --help
```

## Quick Start

Minimal golden path: list proxy groups, list nodes, then launch an isolated browser session with one selected node.

```bash
./browsebox groups
./browsebox nodes --group "<group>"
./browsebox run --group "<group>" --node "<node>"
```

Show help:

```bash
./browsebox --help
```

## Command examples

List proxy groups:

```bash
./browsebox groups
```

Check node delays concurrently; healthy nodes are sorted by ascending delay and failed nodes are listed last:

```bash
./browsebox nodes --group "<group>"
```

Launch a one-shot isolated session in the foreground. It exits on interrupt and cleans runtime files by default:

```bash
./browsebox run --group "<group>" --node "<node>" --url "https://example.com"
```

Start a persistent isolated session:

```bash
./browsebox start --group "<group>" --node "<node>"
```

Check persistent session status:

```bash
./browsebox status
```

Stop the persistent session and clean state:

```bash
./browsebox stop
```

## Configuration and default locations

Local structured configuration is loaded automatically from `configs/config.yaml`. Copy the non-sensitive template and adjust it as needed; command-line flags override local configuration:

```bash
cp configs/config.example.yaml configs/config.yaml
```

Common flags can be passed after any command:

```bash
./browsebox run \
  --config ~/.config/mihomo/config.yaml \
  --state-dir ~/.browsebox \
  --group "<group>" \
  --node "<node>"
```

Common configuration options:

- `--controller-socket <path>`: Unix socket for the main Clash Verge Rev/mihomo controller.
- `--config <path>`: source mihomo config. The default prefers `~/.config/mihomo/config.yaml`; if missing, browsebox probes the common macOS Clash Verge Rev config location.
- `--runtime-dir <path>`: parent directory for temporary runtime directories; if omitted, the system temp directory is used.
- `--state-dir <path>`: persistent session state directory, defaulting to `~/.browsebox`.
- `--mihomo <path>`: mihomo executable path.
- `--chrome <path>`: Google Chrome executable path.
- `--chrome-profile-dir <path>`: Chrome profile directory; if empty, each session gets an isolated temporary profile.
- `--headless`: launch Chrome in headless mode for browser-mcp / CDP automation; visible Chrome remains the default.
- `--proxy-port <port>`, `--controller-port <port>`, `--devtools-port <port>`: localhost session ports.
- `--nodes-concurrency <n>`: concurrent delay checks for `nodes`, defaulting to 16.
- `--delay-timeout-ms <ms>`: mihomo delay-check timeout, defaulting to 5000ms; also used by `run` / `start` startup health checks.
- `--health-url <url>`: URL checked through the selected node before `run` / `start` launches Chrome; repeat the flag to set multiple URLs. Any failed check stops startup and cleans temporary resources.

## Safety notes

- `nodes` only reads proxy groups and node delay from the main controller; it does not switch the main controller selector.
- `run` / `start` copy and rewrite the source config, then select `<node>` only inside the temporary mihomo controller.
- Proxy, temporary controller, and DevTools endpoints are bound to `127.0.0.1` only.
- Do not commit runtime configs, state, logs, local config, or files containing credentials.
- Do not paste real node names, tokens, private service URLs, or full local absolute paths into public docs, issues, or logs.
