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

Explicitly switch the main Clash/mihomo selector to the lowest-delay healthy node from the current check:

```bash
./browsebox nodes --group "<group>" --url "https://chatgpt.com" --select-fastest
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

Sync the repository-provided browsebox Claude skill into the user-level skill install:

```bash
./skill-sync --check
./skill-sync --apply
```

If installed through `./build.sh install`, use:

```bash
browsebox-skill-sync --check
browsebox-skill-sync --apply
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
- `--runtime-cache-dir <path>`: mihomo geodata cache directory for reusing files such as `geosite` and `geoip`.
- `--state-dir <path>`: persistent session state directory, defaulting to `~/.browsebox`.
- `--mihomo <path>`: mihomo executable path.
- `--chrome <path>`: Google Chrome executable path.
- `--chrome-profile-dir <path>`: Chrome profile directory; if empty, each session gets an isolated temporary profile.
- `browser.chrome_args`: extra Chrome launch arguments in config; use block-list syntax or `[]`. Entries may include or omit the leading `--`, and duplicate names are ignored after the first occurrence. `user-data-dir`, `proxy-server`, and `remote-debugging-port` are managed by browsebox and ignored if configured.
- `--headless`: launch Chrome in headless mode for browser-mcp / CDP automation; visible Chrome remains the default.
- `--proxy-port <port>`, `--controller-port <port>`, `--devtools-port <port>`: localhost session ports.
- `--nodes-concurrency <n>`: concurrent delay checks for `nodes`, defaulting to 16.
- `--delay-timeout-ms <ms>`: mihomo delay-check timeout, defaulting to 5000ms; also used by `run` / `start` startup health checks.
- `--select-fastest`: explicit opt-in for `nodes`; after delay checks, switch `<group>` in the main controller to the lowest-delay healthy node.
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

- By default, `nodes` only reads proxy groups and node delay from the main controller; it switches the main controller selector only when `--select-fastest` is passed.
- `run` / `start` copy and rewrite the source config, then select `<node>` only inside the temporary mihomo controller.
- Proxy, temporary controller, and DevTools endpoints are bound to `127.0.0.1` only.
- Do not commit runtime configs, state, logs, local config, or files containing credentials.
- Do not paste real node names, tokens, private service URLs, or full local absolute paths into public docs, issues, or logs.
