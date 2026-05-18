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

安装到 `PREFIX/bin`（默认 `/usr/local/bin`）：

```bash
./build.sh install
```

也可以手动构建：

```bash
go build -o browsebox ./cmd/browsebox
```

也可以不生成二进制，直接使用：

```bash
go run ./cmd/browsebox --help
```

## 快速开始

最小路径：先查看代理组，再查看节点，最后选择一个节点启动隔离浏览器会话。

```bash
./browsebox groups
./browsebox nodes --group "<group>"
./browsebox run --group "<group>" --node "<node>"
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

并发检测节点延迟；健康节点按延迟从低到高排序，失败节点排在最后：

```bash
./browsebox nodes --group "<group>"
```

启动一次性隔离会话，前台运行，收到中断信号后退出并清理运行时文件：

```bash
./browsebox run --group "<group>" --node "<node>" --url "https://example.com"
```

启动持久隔离会话：

```bash
./browsebox start --group "<group>" --node "<node>"
```

查看持久会话状态：

```bash
./browsebox status
```

停止持久会话并清理状态：

```bash
./browsebox stop
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
  --group "<group>" \
  --node "<node>"
```

常见配置项：

- `--controller-socket <path>`：主 Clash Verge Rev/mihomo 控制器 Unix socket。
- `--config <path>`：源 mihomo 配置。默认优先使用 `~/.config/mihomo/config.yaml`；不存在时会探测 Clash Verge Rev 的常见 macOS 配置位置。
- `--runtime-dir <path>`：临时运行目录的父目录；未设置时使用系统临时目录。
- `--runtime-cache-dir <path>`：mihomo geodata 缓存目录，用于复用 `geosite` / `geoip` 等数据文件。
- `--state-dir <path>`：持久会话状态目录，默认位于 `~/.browsebox`。
- `--mihomo <path>`：mihomo 可执行文件路径。
- `--chrome <path>`：Google Chrome 可执行文件路径。
- `--chrome-profile-dir <path>`：Chrome profile 目录；留空时每次会话自动创建隔离临时 profile。
- `browser.chrome_args`：额外 Chrome 启动参数配置；使用 block list 或 `[]`，每项可以带或不带开头的 `--`，会保留顺序并按参数名去重。`user-data-dir`、`proxy-server`、`remote-debugging-port` 由 browsebox 管理，配置中同名参数会被忽略。
- `--headless`：以无头模式启动 Chrome，适合 browser-mcp / CDP 自动化；默认可视化启动。
- `--proxy-port <port>`、`--controller-port <port>`、`--devtools-port <port>`：本机会话端口。
- `--nodes-concurrency <n>`：`nodes` 并发测速数量，默认 16。
- `--delay-timeout-ms <ms>`：mihomo 延迟检查超时，默认 5000ms，也用于 `run` / `start` 的启动健康检查。
- `--health-url <url>`：启动 `run` / `start` 前通过临时 mihomo 检查所选节点的 URL，可重复传入；任一检查失败会停止启动并清理临时资源。

## 安全说明

- `nodes` 只读取主控制器的代理组和节点延迟，不切换主控制器选择器。
- `run` / `start` 会复制并改写源配置，只在临时 mihomo 控制器中选择 `<node>`。
- 代理、临时控制器和 DevTools 端点仅绑定到 `127.0.0.1`。
- 不要提交运行时配置、状态、日志、本地配置或包含凭据的文件。
- 不要在公开文档、issue 或日志中粘贴真实节点名、令牌、私有服务地址或完整本地绝对路径。
