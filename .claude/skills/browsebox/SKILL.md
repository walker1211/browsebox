---
name: browsebox
description: Use this skill when diagnosing web access through isolated browsebox Chrome sessions, x.com searches, OpenAI web pages, proxy node selection, mihomo/Clash Verge routing checks, or when the user wants browser access without changing the main proxy selector.
---

# browsebox

Use this skill to operate the `browsebox` project safely for isolated proxy-routed browser access.

## First decide the scenario

Classify the user's request into one of these scenarios:

1. **Node probe** — the user needs to know which proxy nodes can reach a target URL.
2. **Isolated browser session** — the user needs a temporary Chrome routed through a selected node.
3. **Login-required X workflow** — the user wants X search, timelines, or other pages that require login state.
4. **Page extraction** — the user wants visible text, title, URL, or simple DOM data from a browser page.
5. **Troubleshooting** — browsebox fails to start, ports conflict, health checks fail, or Chrome/DevTools is unreachable.
6. **Configuration review** — the user asks whether `configs/config.yaml` is correct.

Read `references/usage.md` for concrete commands and CDP snippets.

## Ground rules

- Run browsebox commands from the repository root so `configs/config.yaml` is loaded. Prefer `cd "$HOME/Projects/browsebox" && ./browsebox ...` when that checkout exists.
- Do not mutate the main Clash Verge/mihomo selector. Use `nodes` for probing and let `run`/`start` select nodes only in the temporary controller.
- Bind and query only localhost endpoints: proxy, temporary controller, and DevTools.
- Do not print or commit `configs/config.yaml`, `.env`, runtime configs, state files, logs, Chrome profiles, cookies, or credential-bearing output.
- Treat persistent `chrome-profile-dir` values as sensitive because they can contain logged-in browser state.
- For webpage-resource tasks, browser/CDP success is more relevant than `curl` success through the proxy.
- X search usually needs a logged-in persistent isolated profile. Public X profile pages may work with a temporary profile.
- X search results are leads, not proof. Cross-check factual claims against official pages, help center articles, release notes, primary posts, or reliable media.

## Minimal workflows

### Probe target reachability

```bash
cd "$HOME/Projects/browsebox"
./browsebox groups
./browsebox nodes --group "<group>" --url "https://x.com" --nodes-concurrency 12 --delay-timeout-ms 7000
./browsebox nodes --group "<group>" --url "https://abs.twimg.com" --nodes-concurrency 12 --delay-timeout-ms 7000
```

Choose a node that is healthy for all domains needed by the page.

### Start a temporary isolated browser

```bash
cd "$HOME/Projects/browsebox"
./browsebox start --group "<group>" --node "<node>" --url "https://x.com/OpenAI"
./browsebox status
```

Stop it when finished:

```bash
cd "$HOME/Projects/browsebox"
./browsebox stop
```

### Start a persistent isolated profile for X login

Use a dedicated browsebox profile, not the user's main Chrome profile:

```bash
cd "$HOME/Projects/browsebox"
./browsebox start \
  --group "<group>" \
  --node "<node>" \
  --url "https://x.com/login" \
  --chrome-profile-dir "$HOME/.config/browsebox/x/profiles/default" \
  --proxy-port 17997 \
  --controller-port 17998 \
  --devtools-port 9223
```

Do not use `--headless` for first login. Reuse the same `--chrome-profile-dir` for later X searches.

### Use DevTools after start

```bash
curl --noproxy '*' http://127.0.0.1:9223/json/version
curl --noproxy '*' http://127.0.0.1:9223/json/list
```

Use CDP/browser automation to navigate and extract visible text. Keep extracted output focused on the user's question.

## Configuration review checklist

When reviewing `configs/config.yaml`, check:

- `browser.chrome_path` is used, not `browser_path`.
- `browser.profile_dir` is set only when persistent login state is intended.
- `browser.headless` is `false` for manual login or challenge handling.
- `session.health_urls` include every critical page domain, such as `https://x.com` and `https://abs.twimg.com` for X.
- Port values do not conflict with an active browsebox session.
- The user runs from the browsebox repo root if relying on local `configs/config.yaml`.

## Before saying it worked

Verify with fresh evidence:

- For config loading: run `cd "$HOME/Projects/browsebox" && ./browsebox status`.
- For node reachability: run the exact `nodes --url ...` probe for the target domain.
- For browser availability: read `http://127.0.0.1:<devtools-port>/json/version`.
- For page extraction: confirm the extracted page URL, title, and whether login wall or challenge text is present.
