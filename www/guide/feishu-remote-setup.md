---
title: 飞书接入配置指南
description: 通过 Feishu Adapter 将飞书消息接入本地 NeoCode Gateway，支持 SDK 长连接与 Webhook 两种模式。
---

# 飞书接入配置指南

配置完成后，你在飞书中发给机器人的消息会路由到本机 Gateway 执行，并把运行状态和最终结果回传到飞书。核心链路：

```text
飞书消息 -> Feishu Adapter -> 本机 Gateway -> Runtime/Tools -> 飞书卡片回传
```

更推荐在终端 TUI / Web UI 直接使用 NeoCode。如果你很少用到飞书接入，**不需要**按本文操作。

## 两种接入模式

| 模式 | 适用场景 | 是否需要公网地址 |
|------|----------|:---:|
| **SDK**（推荐） | 本机个人使用 | 否 |
| Webhook | 云端部署 / 公网联调 | 是 |

本文优先介绍 SDK 模式。Webhook 模式见[末尾章节](#webhook-模式可选)。

---

## 1. 前置准备

开始前请确认你已有：

1. **可用的飞书应用** — 在[飞书开放平台](https://open.feishu.cn)创建机器人应用，获取 `app_id`（`cli_xxx`）
2. **应用已发布** — 在飞书开放平台「版本管理与发布」中创建并发布当前版本
3. **订阅事件** — 「事件与回调」中**选择「使用长连接接收事件」**，并订阅 `im.message.receive_v1`
4. **本机能运行 NeoCode** — `go run ./cmd/neocode` 或已安装二进制
5. **有可用工作区** — 一个项目目录路径（如 `F:\qiniu\neo-code` 或 `/home/user/project`）

环境变量：

```bash
# macOS / Linux
export FEISHU_APP_SECRET="应用凭据页的 App Secret"

# Windows PowerShell
$env:FEISHU_APP_SECRET = "应用凭据页的 App Secret"
```

SDK 模式下不需要 `FEISHU_SIGNING_SECRET`，也不需要配置公网回调 URL。

---

## 2. 配置文件

将以下配置写入 `~/.neocode/config.yaml`（Windows：`C:\Users\<你的用户名>\.neocode\config.yaml`）：

```yaml
feishu:
  enabled: true
  ingress: "sdk"                 # SDK 长连接模式

  app_id: "cli_xxx"              # 飞书应用 App ID（必填）

  # 群聊 @ 机器人时需要；至少配一个，私聊可不填
  bot_user_id: "ou_xxx"          # 飞书开放平台「用户 ID」
  bot_open_id: "ou_xxx"          # 飞书开放平台「Open ID」

  # 以下仅 webhook 模式需要；SDK 可留空
  verify_token: ""
  insecure_skip_signature_verify: false
  adapter:
    listen: ""                   # SDK 模式无需
    event_path: ""
    card_path: ""

  # 超时与重连（均可保持默认）
  request_timeout_sec: 8         # 网关请求超时（秒）
  idempotency_ttl_sec: 600       # 事件去重窗口（秒）
  reconnect_backoff_min_ms: 500  # 网关重连最小退避（毫秒）
  reconnect_backoff_max_ms: 10000# 网关重连最大退避（毫秒）
  rebind_interval_sec: 15        # 重绑活跃会话间隔（秒）

  gateway:
    listen: ""                   # Gateway 的 IPC 地址，见下方说明
    token_file: ""               # 认证 token 文件路径，留空则用默认
```

### `gateway.listen` 填什么？

Gateway 和 Adapter 通过**同一个 listen 地址**通信。根据你的系统选择：

| 系统 | 推荐值 | 说明 |
|------|--------|------|
| Windows | `\\.\pipe\neocode-gateway` | 命名管道 |
| macOS / Linux | `127.0.0.1:8080` | TCP 回环地址 |

`token_file` 留空时，Gateway 和 Adapter 默认使用 `~/.neocode/auth.json`。

---

## 3. 启动步骤

**必须先启动 Gateway，再启动 Adapter**。建议开两个终端窗口。

### 3.1 启动 Gateway

Gateway 是 NeoCode 的后端服务进程。Adapter 通过它接入 Runtime 和工具。

```bash
# macOS / Linux
go run ./cmd/neocode-gateway \
  --listen "127.0.0.1:8080" \
  --http-listen "127.0.0.1:18181" \
  --workdir "/home/you/project"
```

```powershell
# Windows PowerShell
go run ./cmd/neocode-gateway `
  --listen "\\.\pipe\neocode-gateway" `
  --http-listen "127.0.0.1:18181" `
  --workdir "F:\qiniu\neo-code"
```

**Gateway 启动参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|:---:|--------|------|
| `--listen` | 是* | — | IPC 监听地址。Windows 用命名管道 `\\.\pipe\<name>`；Unix 用 TCP `127.0.0.1:8080` |
| `--workdir` | 是* | — | 工作区路径。没指定时会报 `workspace hash is empty` |
| `--http-listen` | 否 | `127.0.0.1:8400` | HTTP 网络通道监听地址 |
| `--token-file` | 否 | `~/.neocode/auth.json` | 认证 token 文件 |
| `--log-level` | 否 | `info` | 日志级别：`debug` / `info` / `warn` / `error` |

*`--listen` 和 `--workdir` 虽非 cobra 强制，但不提供会导致 Adapter 无法连接或 Agent 无法执行。

### 3.2 启动 Adapter

Adapter 负责桥接飞书长连接与本地 Gateway，把飞书消息翻译为 `gateway.run` 调用。

```bash
# macOS / Linux
go run ./cmd/neocode feishu-adapter \
  --ingress sdk \
  --gateway-listen "127.0.0.1:8080"
```

```powershell
# Windows PowerShell
go run ./cmd/neocode feishu-adapter `
  --ingress sdk `
  --gateway-listen "\\.\pipe\neocode-gateway"
```

**Adapter 启动参数说明：**

| 参数 | 必填 | 默认值 | 说明 |
|------|:---:|--------|------|
| `--ingress` | 否 | 从 config 读取 | 入站模式：`sdk`（推荐）/ `webhook` |
| `--gateway-listen` | 是* | — | Gateway 的 IPC 地址，**必须与 Gateway 的 `--listen` 一致** |
| `--app-id` | 否 | 从 config 读取 | 飞书应用 App ID，仅当 config 未配时需要 |
| `--gateway-token-file` | 否 | 从 config 读取 | 认证 token 文件路径，与 Gateway 共用同一个 |

### 3.3 如何判断启动成功？

**Gateway 日志中**应出现：

```
starting gateway (log-level=info)
gateway ipc listen address: \\.\pipe\neocode-gateway
gateway network listen address: 127.0.0.1:18181
```

**Adapter 日志中**应出现：

```
connected to wss://msg-frontier.feishu.cn
```

接着去飞书给机器人发一条私聊消息。预期：

1. 飞书聊天窗口出现一张 **"NeoCode 任务状态"** 卡片，显示初始状态为 `thinking`
2. 卡片实时更新（1.5 秒刷新）：`thinking` → `planning` → `running`
3. 任务完成后卡片结果更新为 `success`，摘要区显示最终回复内容
4. **不会再额外发一条文本消息** — 卡片本身就是完整的任务视图

---

## 4. 群聊 @ 触发

要让机器人在群聊中响应，需要：

1. 在配置中填写 `bot_user_id` 或 `bot_open_id`
2. 在群里显式 `@机器人` 后再发消息
3. @ 其他成员不会触发 NeoCode 运行

如何获取机器人的 User ID / Open ID：

- 飞书开放平台 → 应用详情 → 「添加应用能力」→ 确认机器人已添加
- 在飞书中搜索你的机器人，进入对话后查看「设置 → 机器人信息」

---

## 5. 审批功能

当 Agent 需要执行敏感操作时，飞书卡片会显示审批按钮：

- **允许一次**：放行本次操作
- **拒绝**：拒绝本次操作

如果飞书版本不支持卡片按钮回调，可使用文本降级（在聊天框直接发）：

- `允许 <request_id>` — 允许
- `拒绝 <request_id>` — 拒绝

审批结果会直接更新到任务状态卡片中。

---

## 6. 状态卡片说明

每个 run 会生成一张状态卡片（标题固定为「NeoCode 任务状态」），后续只更新不重发：

```
📋 <任务摘要>
💭 状态: thinking / planning / running
⏳ 审批: none / pending / approved / rejected
🎉 结果: pending / success / failure
⏱ <运行耗时>
---
摘要
<最终回复或错误信息>
```

---

## 7. Webhook 模式（可选）

如果你要部署到服务器或联调环境，使用 webhook 模式：

```yaml
feishu:
  enabled: true
  ingress: "webhook"
  app_id: "cli_xxx"
  verify_token: "你的 Verification Token"

  adapter:
    listen: "127.0.0.1:18080"
    event_path: "/feishu/events"
    card_path: "/feishu/cards"
```

启动 Gateway（同 SDK）：

```bash
go run ./cmd/neocode-gateway \
  --listen "127.0.0.1:8080" \
  --http-listen "127.0.0.1:18181" \
  --workdir "/path/to/project"
```

启动 Adapter：

```bash
# macOS / Linux
go run ./cmd/neocode feishu-adapter \
  --ingress webhook \
  --gateway-listen "127.0.0.1:8080" \
  --listen "127.0.0.1:18080"
```

然后用 ngrok / cloudflared 把 `18080` 暴露公网，在飞书后台配置：

- **事件回调地址**：`https://<your-domain>/feishu/events`
- **卡片回调地址**：`https://<your-domain>/feishu/cards`

---

## 8. 常见问题

### `workspace hash is empty and no default configured`

Gateway 启动时缺少 `--workdir`。解决：加上 `--workdir <项目路径>`。

### `请先设置环境变量 FEISHU_APP_SECRET`

Adapter 启动前强制检查 `FEISHU_APP_SECRET` 环境变量。解决：在当前终端设置后再启动。

### `请先设置环境变量 FEISHU_SIGNING_SECRET`

当前使用 webhook 模式但未设置签名密钥。解决：设置环境变量，或切换到 `sdk` 模式。

### 飞书收到"任务受理失败，请稍后重试"

Adapter 能收消息但调用 Gateway 失败。检查：
1. Gateway 是否在运行
2. Adapter 的 `--gateway-listen` 是否与 Gateway 的 `--listen` **完全一致**（包括管道名前缀 `\\.\pipe\`）
3. Gateway 日志中是否有 `authenticate` / `bindStream` / `run` 记录

### 飞书只看到一张 thinking 卡片，之后没更新

排查：
1. Gateway 日志中是否出现了 `run_done` / `run_error` 事件
2. 机器人是否配了 API Key（在 `config.yaml` 中配置 `selected_provider`）
3. 飞书应用是否已发布当前版本
4. 事件订阅是否包含 `im.message.receive_v1`

### Gateway 的 `--listen` 和 `--http-listen` 区别?

| 参数 | 用途 | 连接方 |
|------|------|--------|
| `--listen` | IPC 通道（Unix socket / Windows 命名管道） | Adapter 独占用 |
| `--http-listen` | HTTP + WebSocket 网络通道 | Web UI / 外部 HTTP 客户端 |
