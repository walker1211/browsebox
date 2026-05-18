# browsebox usage reference

## Repository discovery

Prefer this checkout when present:

```bash
cd "$HOME/Projects/browsebox"
```

The local browsebox configuration path is currently `configs/config.yaml` relative to the process working directory. Running an absolute binary from another project directory may skip this repository's local configuration.

## Commands

List groups:

```bash
./browsebox groups
```

Probe node reachability for a target URL:

```bash
./browsebox nodes --group "<group>" --url "<url>" --nodes-concurrency 12 --delay-timeout-ms 7000
```

Start a persistent session:

```bash
./browsebox start --group "<group>" --node "<node>" --url "<url>" --proxy-port 17997 --controller-port 17998 --devtools-port 9223
```

Show session status:

```bash
./browsebox status
```

Stop the session:

```bash
./browsebox stop
```

## X login profile

Use a dedicated persistent profile for X login state:

```bash
./browsebox start \
  --group "<group>" \
  --node "<node>" \
  --url "https://x.com/login" \
  --chrome-profile-dir "$HOME/.config/browsebox/x/profiles/default" \
  --proxy-port 17997 \
  --controller-port 17998 \
  --devtools-port 9223
```

After the user logs in once, later runs should reuse the same `--chrome-profile-dir`. Do not delete that profile unless the user wants to remove X login state.

## DevTools checks

```bash
curl --noproxy '*' http://127.0.0.1:9223/json/version
curl --noproxy '*' http://127.0.0.1:9223/json/list
```

## Node CDP visible-text extraction pattern

This pattern uses the browser already started by browsebox. Keep queries narrow and summarize sources before drawing conclusions.

```bash
node <<'NODE'
const devtools = 'http://127.0.0.1:9223';
const url = process.argv[2] || 'https://x.com/OpenAI';
async function createTarget() {
  const res = await fetch(`${devtools}/json/new?${encodeURIComponent('about:blank')}`, {method: 'PUT'});
  if (!res.ok) throw new Error(`create target failed: ${res.status}`);
  return res.json();
}
function connect(wsUrl) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(wsUrl);
    const pending = new Map();
    let nextID = 1;
    ws.addEventListener('open', () => resolve({
      call(method, params = {}) {
        const id = nextID++;
        ws.send(JSON.stringify({id, method, params}));
        return new Promise((res, rej) => pending.set(id, {res, rej, method}));
      },
      close() { ws.close(); },
    }));
    ws.addEventListener('message', event => {
      const msg = JSON.parse(event.data);
      if (!msg.id) return;
      const item = pending.get(msg.id);
      if (!item) return;
      pending.delete(msg.id);
      msg.error ? item.rej(new Error(`${item.method}: ${msg.error.message}`)) : item.res(msg.result);
    });
    ws.addEventListener('error', reject);
  });
}
async function evalValue(client, expression) {
  const result = await client.call('Runtime.evaluate', {
    expression,
    returnByValue: true,
    awaitPromise: true,
    timeout: 10000,
  });
  return result.result.value;
}
(async () => {
  const target = await createTarget();
  const client = await connect(target.webSocketDebuggerUrl);
  await client.call('Page.enable');
  await client.call('Runtime.enable');
  await client.call('Page.navigate', {url});
  let page;
  for (let i = 0; i < 20; i++) {
    page = await evalValue(client, `(() => ({
      href: location.href,
      title: document.title,
      text: document.body ? document.body.innerText : '',
      articleCount: document.querySelectorAll('article').length,
    }))()`);
    if (page.text && page.text.length > 500) break;
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  console.log(JSON.stringify({
    href: page.href,
    title: page.title,
    articleCount: page.articleCount,
    text: page.text.slice(0, 4000),
  }, null, 2));
  client.close();
})().catch(err => {
  console.error(err && err.stack ? err.stack : String(err));
  process.exit(1);
});
NODE
```

## Interpreting X pages

- `x.com/i/flow/login` means the profile is not logged in or the page requires login.
- Search result pages with `article` elements and no login wall can be used as leads.
- X engagement counts and social posts are not authoritative evidence by themselves.
- Use official OpenAI Help Center, release notes, developer community posts, or primary X posts for factual claims.
