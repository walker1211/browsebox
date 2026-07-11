[中文](./README.zh-CN.md)

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

The script runs tests and creates two local binaries: `./browsebox` and `./skill-sync`. On success it prints output like:

```text
Building...
Done. Binaries: ./browsebox ./skill-sync
```

Install to `PREFIX/bin` (`/usr/local/bin` by default):

```bash
./build.sh install
```

The installed command names are `browsebox` and `browsebox-skill-sync`. Verify them with:

```bash
browsebox --help
browsebox-skill-sync --help
```

You can also build manually:

```bash
go build -o browsebox ./cmd/browsebox
go build -o skill-sync ./cmd/skill-sync
```

You can also run it without creating a binary:

```bash
go run ./cmd/browsebox --help
```

## Quick Start

Minimal golden path: list proxy groups, list nodes, then launch an isolated browser session with one selected node. When `--group` is omitted, browsebox tries to auto-match the proxy group currently selected in Clash/mihomo.

```bash
./browsebox groups
./browsebox nodes
./browsebox run --node "<node>"
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

Check node delays concurrently. `nodes.health.urls` drives average delay, and optional `nodes.capability.checks` run real HTTP checks; default output shows only `ok` nodes sorted by ascending delay. With `--show-unhealthy=true`, `failed` capability rows and `unhealthy` delay rows are shown after `ok` rows:

```bash
./browsebox nodes
```

If browsebox cannot uniquely match the current proxy group, specify it explicitly:

```bash
./browsebox nodes --group "<group>"
```

Explicitly switch the main Clash/mihomo selector to the lowest-delay `ok` node from the current check:

```bash
./browsebox nodes --url "https://chatgpt.com" --select-fastest
```

Launch a one-shot isolated browser session in the foreground. It exits on interrupt and cleans runtime files by default:

```bash
./browsebox run --node "<node>" --url "https://example.com"
```

Launch a one-shot isolated proxy without Chrome:

```bash
./browsebox proxy --select-fastest --health-url "https://example.com"
```

Start a persistent isolated session:

```bash
./browsebox start --node "<node>"
```

Check persistent session status:

```bash
./browsebox status
```

Stop the persistent session and clean state:

```bash
./browsebox stop
```

Keep the repository-provided browsebox skill synchronized for Claude and Codex-compatible agents:

```bash
./skill-sync --check
./skill-sync --apply
```

The canonical source is `.claude/skills/browsebox`. The generated repository mirror `.agents/skills/browsebox` is tracked for Codex-compatible project discovery; do not edit it directly. `--check` validates that mirror plus the user installs at `~/.claude/skills/browsebox` and `~/.agents/skills/browsebox`. `--apply` stages and updates all three targets.

If installed through `./build.sh install`, use:

```bash
browsebox-skill-sync --check
browsebox-skill-sync --apply
```

The skill files are not embedded in the binary. Run the installed command inside a browsebox source checkout, or pass `--repo-root <checkout>`.

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
  --node "<node>"
```

Common configuration options:

- `--controller-socket <path>`: Unix socket for the main Clash Verge Rev/mihomo controller.
- `--config <path>`: source mihomo config. The default prefers `~/.config/mihomo/config.yaml`; if missing, browsebox probes the common macOS Clash Verge Rev config location.
- `--runtime-dir <path>`: parent directory for temporary runtime directories; if omitted, the system temp directory is used.
- `--runtime-cache-dir <path>`: mihomo geodata cache directory for reusing files such as `geosite` and `geoip`.
- `--state-dir <path>`: persistent session state directory, defaulting to `~/.browsebox`.
- `--mihomo <path>`: mihomo executable path.
- `--interface-name <name>` / `mihomo.interface_name`: force the temporary mihomo outbound dials through a network interface such as `en0`, useful when the main Clash Verge/TUN interferes.
- `--chrome <path>`: Google Chrome executable path.
- `--chrome-profile-dir <path>`: Chrome profile directory; if empty, each session gets an isolated temporary profile.
- `browser.chrome_args`: extra Chrome launch arguments in config; use block-list syntax or `[]`. Entries may include or omit the leading `--`, and duplicate names are ignored after the first occurrence. `user-data-dir`, `proxy-server`, and `remote-debugging-port` are managed by browsebox and ignored if configured.
- `--headless`: launch Chrome in headless mode for browser-mcp / CDP automation; visible Chrome remains the default.
- `--proxy-port <port>`, `--controller-port <port>`, `--devtools-port <port>`: localhost session ports.
- `--nodes-concurrency <n>` / `nodes.health.concurrency`: concurrent delay-check workers for `nodes`, defaulting to 16. Each worker takes another node when its current node finishes.
- `nodes.health.urls`: URLs used by `./browsebox nodes` to calculate average delay. A node that fails health is marked `unhealthy` and does not run capability checks.
- `nodes.health.probe_rounds`: delay-check rounds per node and health URL.
- `nodes.health.probe_interval_ms`: delay between delay-check rounds in milliseconds.
- `--delay-timeout-ms <ms>` / `nodes.delay_timeout_ms`: common timeout for node delay checks and capability HTTP checks, defaulting to 5000ms; also used by `run` / `start` startup health checks.
- `--capability-concurrency <n>` / `nodes.capability.concurrency`: concurrent capability HTTP check workers. Capability workers start as soon as individual nodes pass health, and each worker uses an independent temporary mihomo instance to avoid selector races.
- `nodes.capability.checks`: optional array of real HTTP checks for `./browsebox nodes`. `STATUS=ok` means health passed and every capability rule passed; `STATUS=failed` means health passed but at least one capability rule failed. `CHECKS` is `-` for ok rows and compact failure reasons such as `openai:unsupported_country_region_territory` for failed rows.
- `--show-unhealthy=true|false` / `nodes.show_unhealthy`: whether `nodes` shows non-ok nodes, defaulting to `false` so only available nodes are shown.
- `--highlight-current=true|false` / `nodes.highlight_current`: whether `nodes` color-highlights the current node, defaulting to `true`; hidden current nodes are not shown separately.
- `--group <group>` / `session.group`: proxy group name. Leave it empty to auto-match the proxy group currently selected in Clash/mihomo; specify it explicitly if the match is ambiguous.
- `--select-fastest`: explicit opt-in for `nodes`; after delay and capability checks, switch the selected or auto-matched group in the main controller to the lowest-delay `ok` node.
- `--health-url <url>`: URL checked through the selected node before `run` / `start` launches Chrome; repeat the flag to set multiple URLs. Any failed check stops startup and cleans temporary resources.

## Local verification and release

Run the full local verification flow:

```bash
scripts/ci-local.sh clean
```

Install the pre-push hook to run clean CI before each push:

```bash
scripts/install-hooks.sh
```

Releases are created from `v*` tags by the GitHub Release workflow:

```bash
scripts/tag-release.sh v0.1.0
git push origin v0.1.0
```

The release workflow runs history secret scanning, multi-platform builds, checksum generation, and GitHub Release creation or update. Release archives should contain only binaries, READMEs, LICENSE, and non-sensitive configuration templates.

## Safety notes

- By default, `nodes` only reads proxy groups and node delay from the main controller; it switches the selected or auto-matched group only when `--select-fastest` is passed.
- `run` / `start` copy and rewrite the source config, then select `<node>` only inside the temporary mihomo controller.
- Proxy, temporary controller, and DevTools endpoints are bound to `127.0.0.1` only.
- Do not commit runtime configs, state, logs, local config, or files containing credentials.
- Do not paste real node names, tokens, private service URLs, or full local absolute paths into public docs, issues, or logs.
