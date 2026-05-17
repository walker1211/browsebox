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
go build -o browsebox ./cmd/browsebox
```

You can also run it without creating a binary:

```bash
go run ./cmd/browsebox --help
```

## Quick Start

Minimal golden path: list nodes, then launch an isolated browser session with one selected node.

```bash
./browsebox nodes --group "<group>"
./browsebox run --group "<group>" --node "<node>"
```

Show help:

```bash
./browsebox --help
```

## Command examples

List nodes and health status:

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
- `--config <path>`: source mihomo config, defaulting to `~/.config/mihomo/config.yaml`.
- `--runtime-dir <path>`: parent directory for temporary runtime directories; if omitted, the system temp directory is used.
- `--state-dir <path>`: persistent session state directory, defaulting to `~/.browsebox`.
- `--mihomo <path>`: mihomo executable path.
- `--chrome <path>`: Google Chrome executable path.
- `--proxy-port <port>`, `--controller-port <port>`, `--devtools-port <port>`: localhost session ports.
- `--health-url <url>`: node delay check URL; repeat the flag to set multiple URLs.

## Safety notes

- `nodes` only reads proxy groups and node delay from the main controller; it does not switch the main controller selector.
- `run` / `start` copy and rewrite the source config, then select `<node>` only inside the temporary mihomo controller.
- Proxy, temporary controller, and DevTools endpoints are bound to `127.0.0.1` only.
- Do not commit runtime configs, state, logs, local config, or files containing credentials.
- Do not paste real node names, tokens, private service URLs, or full local absolute paths into public docs, issues, or logs.
