[English](./README.md)

# browsebox

browsebox 是一个 Go 标准库 CLI，用于通过隔离的临时 mihomo 控制器启动独立的 Google Chrome 代理浏览会话。它可以从现有 Clash Verge Rev/mihomo 控制器读取节点列表，并支持一次性会话与持久会话。

## 要求

- macOS
- Clash Verge Rev / mihomo（提供本机控制器）
- Google Chrome
- Go 1.22+

## 安装 / 构建

在仓库根目录执行：

```bash
./build.sh
```

脚本会运行测试并生成两个本地二进制：`./browsebox` 和 `./skill-sync`。成功时会输出类似：

```text
Building...
Done. Binaries: ./browsebox ./skill-sync
```

安装到 `PREFIX/bin`（默认 `/usr/local/bin`）：

```bash
./build.sh install
```

安装后的命令名是 `browsebox` 和 `browsebox-skill-sync`。可以这样验证：

```bash
browsebox --help
browsebox-skill-sync --help
```

也可以手动构建：

```bash
go build -o browsebox ./cmd/browsebox
go build -o skill-sync ./cmd/skill-sync
```

也可以不生成二进制，直接使用：

```bash
go run ./cmd/browsebox --help
```

## 快速开始

最小路径：先查看代理组，再查看节点，最后选择一个节点启动隔离浏览器会话。未传 `--group` 时会尝试自动匹配当前 Clash/mihomo 选择的代理组。

```bash
./browsebox groups
./browsebox nodes
./browsebox run --node "<node>"
```

查看帮助：

```bash
./browsebox --help
```

## 命令示例

列出代理组：

```bash
./browsebox groups
```

并发检测节点延迟；默认隐藏 unhealthy 节点，健康节点按延迟从低到高排序；使用 `--show-unhealthy=true` 时，unhealthy 节点会显示在健康节点之后：

```bash
./browsebox nodes
```

如果无法唯一匹配当前代理组，显式指定组名：

```bash
./browsebox nodes --group "<group>"
```

显式切换主 Clash/mihomo 选择器到当前测速中延迟最低的健康节点：

```bash
./browsebox nodes --url "https://chatgpt.com" --select-fastest
```

启动一次性隔离浏览器会话，前台运行，收到中断信号后退出并清理运行时文件：

```bash
./browsebox run --node "<node>" --url "https://example.com"
```

启动只提供本地代理端口、不启动 Chrome 的一次性隔离代理：

```bash
./browsebox proxy --select-fastest --health-url "https://example.com"
```

启动持久隔离会话：

```bash
./browsebox start --node "<node>"
```

查看持久会话状态：

```bash
./browsebox status
```

停止持久会话并清理状态：

```bash
./browsebox stop
```

同步仓库内置的 browsebox Claude skill 到用户级 skill 安装目录：

```bash
./skill-sync --check
./skill-sync --apply
```

如果已经通过 `./build.sh install` 安装，则使用：

```bash
browsebox-skill-sync --check
browsebox-skill-sync --apply
```

## 配置与默认位置

本地结构化配置会从 `configs/config.yaml` 自动读取；可从非敏感模板复制后按需调整，命令行 flag 会覆盖本地配置：

```bash
cp configs/config.example.yaml configs/config.yaml
```

常用参数可以在任意命令后添加：

```bash
./browsebox run \
  --config ~/.config/mihomo/config.yaml \
  --state-dir ~/.browsebox \
  --node "<node>"
```

常见配置项：

- `--controller-socket <path>`：主 Clash Verge Rev/mihomo 控制器 Unix socket。
- `--config <path>`：源 mihomo 配置。默认优先使用 `~/.config/mihomo/config.yaml`；不存在时会探测 Clash Verge Rev 的常见 macOS 配置位置。
- `--runtime-dir <path>`：临时运行目录的父目录；未设置时使用系统临时目录。
- `--runtime-cache-dir <path>`：mihomo geodata 缓存目录，用于复用 `geosite` / `geoip` 等数据文件。
- `--state-dir <path>`：持久会话状态目录，默认位于 `~/.browsebox`。
- `--mihomo <path>`：mihomo 可执行文件路径。
- `--interface-name <name>` / `mihomo.interface_name`：强制临时 mihomo 出站走指定网卡，例如 `en0`，用于避免被主 Clash Verge/TUN 干扰。
- `--chrome <path>`：Google Chrome 可执行文件路径。
- `--chrome-profile-dir <path>`：Chrome profile 目录；留空时每次会话自动创建隔离临时 profile。
- `browser.chrome_args`：额外 Chrome 启动参数配置；使用 block list 或 `[]`，每项可以带或不带开头的 `--`，会保留顺序并按参数名去重。`user-data-dir`、`proxy-server`、`remote-debugging-port` 由 browsebox 管理，配置中同名参数会被忽略。
- `--headless`：以无头模式启动 Chrome，适合 browser-mcp / CDP 自动化；默认可视化启动。
- `--proxy-port <port>`、`--controller-port <port>`、`--devtools-port <port>`：本机会话端口。
- `--nodes-concurrency <n>`：`nodes` 并发测速数量，默认 16。
- `--delay-timeout-ms <ms>`：mihomo 延迟检查超时，默认 5000ms，也用于 `run` / `start` 的启动健康检查。
- `--show-unhealthy=true|false`：`nodes` 是否展示 unhealthy 节点，默认 `false`，只展示可用节点。
- `--highlight-current=true|false`：`nodes` 是否用颜色标记当前节点，默认 `true`；如果当前节点被过滤则不会显示。
- `--group <group>` / `session.group`：代理组名；留空时自动匹配当前 Clash/mihomo 选择的代理组，无法唯一匹配时需要显式指定。
- `--select-fastest`：仅用于显式 opt-in；`nodes` 测速后把主控制器中选定或自动匹配到的代理组切换到延迟最低的健康节点。
- `--health-url <url>`：启动 `run` / `start` 前通过临时 mihomo 检查所选节点的 URL，可重复传入；任一检查失败会停止启动并清理临时资源。

## 本地验证与发布

本地完整验证：

```bash
scripts/ci-local.sh clean
```

安装 pre-push hook 后，每次 push 前会运行 clean CI：

```bash
scripts/install-hooks.sh
```

发布使用 `v*` tag 触发 GitHub Release workflow：

```bash
scripts/tag-release.sh v0.1.0
git push origin v0.1.0
```

Release workflow 会做 history secret scan、多平台构建、生成校验和，并创建或更新 GitHub Release。发布归档只应包含可执行文件、README、LICENSE 和非敏感配置模板。

## 安全说明

- 默认情况下，`nodes` 只读取主控制器的代理组和节点延迟，不切换主控制器选择器；只有传入 `--select-fastest` 时才会修改主控制器中选定或自动匹配到的代理组。
- `run` / `start` 会复制并改写源配置，只在临时 mihomo 控制器中选择 `<node>`。
- 代理、临时控制器和 DevTools 端点仅绑定到 `127.0.0.1`。
- 不要提交运行时配置、状态、日志、本地配置或包含凭据的文件。
- 不要在公开文档、issue 或日志中粘贴真实节点名、令牌、私有服务地址或完整本地绝对路径。
