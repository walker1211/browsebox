# Contributing

Thanks for helping improve browsebox.

## Local setup

Requirements:

- macOS
- Go 1.22+
- Clash Verge Rev / mihomo for live smoke testing
- Google Chrome for browser-session smoke testing

Build and test:

```bash
./build.sh
```

Install locally:

```bash
./build.sh install
```

Use a temporary install prefix when testing install behavior:

```bash
PREFIX="$(mktemp -d)" ./build.sh install
```

## Development checks

Run these before opening a pull request:

```bash
gofmt -w cmd internal
go test ./...
./build.sh
```

For behavior that touches live proxy or browser sessions, also smoke test the affected command with a non-sensitive node and URL.

## Safety rules

- Do not commit generated runtime configs, state files, logs, local config, or binaries.
- Do not paste subscription URLs, proxy credentials, private node names, tokens, or local absolute paths into public issues, docs, tests, or logs.
- `nodes` and `groups` must stay read-only against the main Clash Verge/mihomo controller.
- Node selection must only happen through the temporary mihomo controller created by browsebox.
- Temporary proxy, controller, and DevTools endpoints must bind to localhost only.

## Pull requests

Keep pull requests focused and describe:

- What changed
- Why it changed
- How it was tested
- Any live smoke testing performed or intentionally skipped

Use Conventional Commits for commit messages when practical.
