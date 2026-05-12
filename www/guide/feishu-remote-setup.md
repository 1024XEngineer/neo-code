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

### 配置清单（先看这个）

| 项 | SDK | Webhook |
|---|:---:|:---:|
| `feishu.app_id` | 必填 | 必填 |
| `FEISHU_APP_SECRET` 环境变量 | 必填 | 必填 |
| `feishu.verify_token` | 不需要 | 必填 |
| `FEISHU_SIGNING_SECRET` 环境变量 | 不需要 | 默认必填（除非 `insecure_skip_signature_verify=true`） |
| `adapter.listen/event_path/card_path` | 不需要 | 需有有效值（可用默认） |
| 飞书后台回调 URL | 不需要 | 必填（事件 + 卡片） |

快速记忆：SDK 模式准备 `app_id + FEISHU_APP_SECRET` 即可；`FEISHU_SIGNING_SECRET` 和公网回调地址只在 Webhook 模式需要。

---

## 1. 前置准备

开始前请确认你已有：

1. **可用的飞书应用** — 在[飞书开放平台](https://open.feishu.cn)创建机器人应用，获取 `app_id`（`cli_xxx`）
2. **应用已发布** — 在飞书开放平台「版本管理与发布」中创建并发布当前版本
3. **订阅事件（SDK 模式）** — 「事件与回调」中选择「使用长连接接收事件」，至少订阅：
   - `im.message.receive_v1`（接收用户消息）
   - `card.action.trigger`（接收卡片按钮动作，用于审批与 ask_user）
4. **开启应用权限（必须这 3 项）** — 「开发配置 → 权限管理 → 应用身份权限」中确保已开通：
   - `im:message.group_at_msg:readonly`（获取群组中用户 @ 机器人消息）
   - `im:message.p2p_msg:readonly`（读取用户发给机器人的单聊消息）
   - `im:message:send_as_bot`（以应用身份发消息）
5. **本机能运行 NeoCode** — `go run ./cmd/neocode` 或已安装二进制
6. **有可用工作区** — 一个项目目录路径（如 `F:\qiniu\neo-code` 或 `/home/user/project`）

如果上面 3 个权限缺失，常见现象是：

- 缺 `im:message.group_at_msg:readonly`：群聊 @ 机器人不触发
- 缺 `im:message.p2p_msg:readonly`：私聊机器人不触发
- 缺 `im:message:send_as_bot`：机器人能收到消息但无法回消息/更新状态卡片

环境变量（建议持久化到用户环境）：

#### macOS / Linux

```bash
# bash
echo 'export FEISHU_APP_SECRET="应用凭据页的 App Secret"' >> ~/.bashrc
source ~/.bashrc

# zsh
echo 'export FEISHU_APP_SECRET="应用凭据页的 App Secret"' >> ~/.zshrc
source ~/.zshrc
```

#### Windows

```powershell
# PowerShell（写入当前用户环境变量，重开终端生效）
[Environment]::SetEnvironmentVariable("FEISHU_APP_SECRET", "应用凭据页的 App Secret", "User")
```

SDK 模式下仍然需要 `FEISHU_APP_SECRET`，但不需要 `FEISHU_SIGNING_SECRET`，也不需要配置公网回调 URL。

---

## 2. 配置文件

将以下配置写入 `~/.neocode/config.yaml`（Windows：`C:\Users\<你的用户名>\.neocode\config.yaml`）。

### 2.1 SDK 模式最小可用示例（推荐）

```yaml
feishu:
  enabled: true
  ingress: "sdk"
  app_id: "cli_xxx"

  # 群聊 @ 机器人时建议至少配置一个；私聊可不填
  bot_user_id: "ou_xxx"
  bot_open_id: "ou_xxx"

  request_timeout_sec: 8
  idempotency_ttl_sec: 600
  reconnect_backoff_min_ms: 500
  reconnect_backoff_max_ms: 10000
  rebind_interval_sec: 15

  gateway:
    listen: ""      # Gateway 的 IPC 地址，见下方说明
    token_file: ""  # 认证 token 文件路径，留空则用默认
```

### `gateway.listen` 填什么？

Gateway 和 Adapter 通过**同一个 listen 地址**通信。根据你的系统选择：

| 系统 | 推荐值 | 说明 |
|------|--------|------|
| Windows | `\\.\pipe\neocode-gateway` | 命名管道 |
| macOS / Linux | `127.0.0.1:8080` | TCP 回环地址 |

`token_file` 留空时，Gateway 和 Adapter 默认使用 `~/.neocode/auth.json`。

### 2.2 Webhook 模式额外字段

如果你切到 `ingress: "webhook"`，还需要补齐：

- `feishu.verify_token`（必填）
- `FEISHU_SIGNING_SECRET` 环境变量（默认必填）
- `feishu.adapter.listen`、`feishu.adapter.event_path`、`feishu.adapter.card_path`（需有有效值，可使用默认值）

---

## 3. 启动步骤

**必须先启动 Gateway，再启动 Adapter**。建议开两个终端窗口。

### 3.1 启动 Gateway

Gateway 是 NeoCode 的后端服务进程。Adapter 通过它接入 Runtime 和工具。

#### macOS / Linux

```bash
# 开发模式 (go run)
go run ./cmd/neocode-gateway --listen "127.0.0.1:8080" --http-listen "127.0.0.1:18181" --workdir "/home/you/project"

# 安装模式 (neocode)
neocode gateway --listen "127.0.0.1:8080" --http-listen "127.0.0.1:18181" --workdir "/home/you/project"
```

#### Windows

```powershell
# 开发模式 (go run)
go run ./cmd/neocode-gateway --listen "\\.\pipe\neocode-gateway" --http-listen "127.0.0.1:18181" --workdir "F:\qiniu\neo-code"

# 安装模式 (neocode)
neocode gateway --listen "\\.\pipe\neocode-gateway" --http-listen "127.0.0.1:18181" --workdir "F:\qiniu\neo-code"
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

#### macOS / Linux

```bash
# 开发模式 (go run)
go run ./cmd/neocode adapter feishu --ingress sdk --gateway-listen "127.0.0.1:8080"

# 安装模式 (neocode)
neocode adapter feishu --ingress sdk --gateway-listen "127.0.0.1:8080"
```

#### Windows

```powershell
# 开发模式 (go run)
go run ./cmd/neocode adapter feishu --ingress sdk --gateway-listen "\\.\pipe\neocode-gateway"

# 安装模式 (neocode)
neocode adapter feishu --ingress sdk --gateway-listen "\\.\pipe\neocode-gateway"
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

并先持久化环境变量（建议设置为用户级，重开终端后生效）：

#### macOS / Linux

```bash
# bash
echo 'export FEISHU_APP_SECRET="应用凭据页的 App Secret"' >> ~/.bashrc
echo 'export FEISHU_SIGNING_SECRET="事件与回调页的 Signing Secret（签名密钥）"' >> ~/.bashrc
source ~/.bashrc

# zsh
echo 'export FEISHU_APP_SECRET="应用凭据页的 App Secret"' >> ~/.zshrc
echo 'export FEISHU_SIGNING_SECRET="事件与回调页的 Signing Secret（签名密钥）"' >> ~/.zshrc
source ~/.zshrc
```

#### Windows

```powershell
# PowerShell（写入当前用户环境变量，重开终端生效）
[Environment]::SetEnvironmentVariable("FEISHU_APP_SECRET", "应用凭据页的 App Secret", "User")
[Environment]::SetEnvironmentVariable("FEISHU_SIGNING_SECRET", "事件与回调页的 Signing Secret（签名密钥）", "User")
```

启动 Gateway（同 SDK）：

```bash
# 开发模式 (go run)
go run ./cmd/neocode-gateway --listen "127.0.0.1:8080" --http-listen "127.0.0.1:18181" --workdir "/path/to/project"

# 安装模式 (neocode)
neocode gateway --listen "127.0.0.1:8080" --http-listen "127.0.0.1:18181" --workdir "/path/to/project"
```

启动 Adapter：

```bash
# 开发模式 (go run)
go run ./cmd/neocode adapter feishu --ingress webhook --gateway-listen "127.0.0.1:8080" --listen "127.0.0.1:18080"

# 安装模式 (neocode)
neocode adapter feishu --ingress webhook --gateway-listen "127.0.0.1:8080" --listen "127.0.0.1:18080"
```

然后用 ngrok / cloudflared 把 `18080` 暴露公网，在飞书后台配置：

- **事件回调地址**：`https://<your-domain>/feishu/events`
- **卡片回调地址**：`https://<your-domain>/feishu/cards`
- **Verification Token**：与 `config.yaml` 里的 `feishu.verify_token` 保持一致

---

## 8. 飞书后台配置示例（长连接 / 回调）

### 8.1 SDK 长连接（无公网）

在飞书开放平台「事件与回调」：

1. 选择「使用长连接接收事件」
2. 勾选 `im.message.receive_v1`
3. 勾选 `card.action.trigger`
4. 在「开发配置 → 权限管理 → 应用身份权限」中再确认这 3 项已开通：`im:message.group_at_msg:readonly`、`im:message.p2p_msg:readonly`、`im:message:send_as_bot`
5. 发布应用版本后再联调

该模式下你**不需要**填写事件回调 URL 和卡片回调 URL。

### 8.2 Webhook 回调（公网）

如果使用 `webhook` 入站，按 `adapter` 配置拼接完整 URL，例如：

- `adapter.listen = "127.0.0.1:18080"`
- `adapter.event_path = "/feishu/events"`
- `adapter.card_path = "/feishu/cards"`
- 公网域名（ngrok / cloudflared）= `https://demo.example.com`

对应飞书后台配置：

- 事件回调 URL：`https://demo.example.com/feishu/events`
- 卡片回调 URL：`https://demo.example.com/feishu/cards`

### 8.3 卡片回调 payload 示例

`/feishu/cards` 会接收类似结构（示例仅展示关键字段）：

```json
{
  "token": "verify_token_xxx",
  "header": {
    "event_id": "8d3b9f2c",
    "token": "verify_token_xxx"
  },
  "action": {
    "value": {
      "action_type": "permission",
      "request_id": "req_123",
      "decision": "allow_once"
    }
  }
}
```

ask_user 场景会使用：

```json
{
  "action": {
    "value": {
      "action_type": "user_question",
      "request_id": "req_456",
      "status": "answered",
      "value": "选项A",
      "message": "补充说明"
    }
  }
}
```

> 说明：`permission` 仅接受 `allow_once/reject`；`user_question` 仅接受 `answered/skipped`。其余值会被安全忽略。

---

## 9. Local Runner（本机工具执行）

如果你的 NeoCode Gateway 部署在云端，但希望工具（文件读写、命令执行等）在你的**本机电脑**上运行，就需要启动 Local Runner。

Runner 会主动通过 WebSocket 连接云端 Gateway，接收工具执行请求并在本机完成，无需开放入站端口。

```
飞书消息 -> Adapter (云端) -> Gateway (云端) -> WebSocket -> Local Runner (你的电脑)
                                                        ↑ 主动出站连接
```

### 9.1 启动 Runner

```bash
# 开发模式 (go run)
go run ./cmd/neocode runner --gateway-address "your-gateway.com:8080" --token-file ~/.neocode/auth.json --runner-name "我的 MacBook" --workdir /path/to/project

# 安装模式 (neocode)
neocode runner --gateway-address "your-gateway.com:8080" --token-file ~/.neocode/auth.json --runner-name "我的 MacBook" --workdir /path/to/project
```

```powershell
# 开发模式 (go run)
go run ./cmd/neocode runner --gateway-address "your-gateway.com:8080" --token-file "$env:USERPROFILE\.neocode\auth.json" --runner-name "我的 PC" --workdir "F:\qiniu\neo-code"

# 安装模式 (neocode)
neocode runner --gateway-address "your-gateway.com:8080" --token-file "$env:USERPROFILE\.neocode\auth.json" --runner-name "我的 PC" --workdir "F:\qiniu\neo-code"
```

Runner 启动后会打印连接状态：

```
runner my-macbook connecting to your-gateway.com:8080...
connected to gateway at ws://your-gateway.com:8080/ws
runner registered: my-macbook
```

### 9.2 参数说明

| 参数 | 必填 | 默认值 | 说明 |
|------|:---:|--------|------|
| `--gateway-address` | 否 | `127.0.0.1:8080` | Gateway WebSocket 地址 |
| `--token-file` | 否 | — | Gateway 认证 token 文件，需与 Gateway 共用同一个 |
| `--runner-id` | 否 | 本机 hostname | Runner 唯一标识，同台机器重复启动会冲突 |
| `--runner-name` | 否 | — | 人类可读名称，便于在日志中区分多台 Runner |
| `--workdir` | 否 | 当前目录 | Runner 工作目录，工具在此目录下执行 |

### 9.3 断线重连

Runner 断连后会自动重连，采用指数退避 + 随机抖动策略：

- 初始退避：500ms
- 最大退避：10s
- 每次失败后退避时间翻倍，并加入随机抖动避免惊群

### 9.4 安全边界

当前已实现：

- Runner 验证 Gateway 签发的 CapabilityToken（HMAC-SHA256 签名校验、TTL 过期检查、工具白名单）
- Token 有过期时间（5 分钟 TTL），过期请求会被拒绝
- 支持配置工作区路径白名单（`WorkdirAllowlist`），拒绝越界路径访问
- 所有工具在 Runner 本机执行

传输安全注意事项：

- Runner 与 Gateway 之间当前使用明文 WebSocket（`ws://`），建议仅在受信任的本地网络中使用，或通过 SSH 隧道 / VPN 加固传输层
- TLS 加密传输（`wss://`）计划在后续版本支持

### 9.5 错误提示

当 Runner 不可用时，飞书卡片会显示友好的中文提示：

- **Runner 离线**：`本机 Runner 未连接，请在电脑上启动 neocode runner`
- **权限不足**：`权限不足：当前能力令牌不允许此操作`
- **执行失败**：`工具执行失败：<具体错误>`

## 10. 常见问题

### `workspace hash is empty and no default configured`

Gateway 启动时缺少 `--workdir`。解决：加上 `--workdir <项目路径>`。

### `请先设置环境变量 FEISHU_APP_SECRET`

Adapter 启动前强制检查 `FEISHU_APP_SECRET` 环境变量。解决：先写入用户环境变量并重开终端，再启动 Adapter。

### `请先设置环境变量 FEISHU_SIGNING_SECRET`

当前使用 webhook 模式但未设置签名密钥。解决：先写入 `FEISHU_SIGNING_SECRET` 用户环境变量并重开终端，或切换到 `sdk` 模式。

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
4. 事件订阅是否包含 `im.message.receive_v1`（若审批/问答按钮无响应，再检查 `card.action.trigger`）

### Gateway 的 `--listen` 和 `--http-listen` 区别?

| 参数 | 用途 | 连接方 |
|------|------|--------|
| `--listen` | IPC 通道（Unix socket / Windows 命名管道） | Adapter 独占用 |
| `--http-listen` | HTTP + WebSocket 网络通道 | Web UI / 外部 HTTP 客户端 |
