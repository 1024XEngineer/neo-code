# Feishu Adapter 接入指南（#554 + #557）

本文档说明 NeoCode Feishu Adapter 的两种入站模式：

- `webhook`：飞书回调到开发者服务（通常需要公网 HTTPS）。
- `sdk`：本机 Adapter 通过飞书 SDK 长连接接收事件（无需公网回调）。

## 1. 架构边界

- Adapter 不直接调用 runtime 私有接口，只复用 Gateway 已有协议：
  - `gateway.authenticate`
  - `gateway.bindStream`
  - `gateway.run`
  - `gateway.resolvePermission`
  - `gateway.event`
- 会话与运行 ID 保持实现一致：
  - `session_id = "feishu_" + stableHash(chat_id)`
  - `run_id = "feishu_" + stableHash(message_id)`
- #557 只新增 SDK 入站，不包含 #555 Local Runner 主动长连。

## 2. 事件执行顺序

每条消息都按固定顺序执行：

1. `authenticate`
2. `bindStream(session_id, run_id)`
3. `run(session_id, run_id, input_text)`
4. 持续消费 `gateway.event` 并回传飞书

## 3. 配置示例

完整示例见 [../examples/feishu.yaml](../examples/feishu.yaml)。

关键字段：

- `feishu.ingress`：`webhook` 或 `sdk`，默认 `webhook`
- `feishu.app_id`
- `FEISHU_APP_SECRET`（固定环境变量，启动时必检）
- `feishu.bot_user_id` / `feishu.bot_open_id`（群聊 @ 命中建议至少配置一个）
- `feishu.verify_token`
- `FEISHU_SIGNING_SECRET`（仅 `webhook` 强制，固定环境变量）

## 4. 启动方式

### 4.1 Webhook 模式（#554）

```bash
neocode feishu-adapter --ingress webhook
```

通常还会覆盖地址参数：

```bash
neocode feishu-adapter \
  --ingress webhook \
  --listen 127.0.0.1:18080 \
  --event-path /feishu/events \
  --card-path /feishu/cards
```

### 4.2 SDK 模式（#557，本地无公网）

```bash
export FEISHU_APP_SECRET="cli_secret_xxx"
neocode feishu-adapter --ingress sdk
```

SDK 模式下不要求公网回调地址，不要求 `adapter.listen/event_path/card_path`。
如果缺少 `FEISHU_APP_SECRET`，启动会直接失败，避免把明文 secret 落到 `config.yaml`。

## 5. 群聊触发规则

- 私聊：默认处理。
- 群聊：必须 `@` 当前 bot 才会触发 run，建议配置 `bot_user_id` 或 `bot_open_id` 作为命中目标。
- 任意 mention（@其他用户）不会触发 NeoCode。

## 6. 幂等与重试

- 消息去重键：`event_id + message_id`
- 卡片去重键：`request_id + decision`
- 仅当 `gateway.run` 成功受理后才标记成功；
- 若 `run` 失败会释放去重状态，Webhook 返回 `HTTP 500`，SDK 长连接回调返回失败 ACK，允许飞书重试恢复。

## 6.1 轻量级状态卡片

- 每个 run 会创建一个独立状态卡片，并复用同一个 `card_id` 更新：
  - `任务`
  - `状态`：`thinking / planning / running`
  - `审批`：`none / pending / approved / rejected`
  - `结果`：`pending / success / failure`
- `permission_requested`、`hook_notification(async_rewake)`、`run_done`、`run_error` 都会更新同一张卡片，不额外刷多条进度文本。
- 最终完成/失败仍会回传一条用户可读文本，卡片则保留结构化状态摘要，便于下一轮继续追踪。

## 7. 审批能力边界

- Webhook 模式：支持卡片回调 -> `resolvePermission`。
- SDK 模式：优先支持 SDK 卡片动作事件；若租户侧不可用，支持文本审批降级：
  - `允许 <request_id>`
  - `拒绝 <request_id>`

以上两种路径都复用 `gateway.resolvePermission`，不新增 Gateway action。

## 8. 安全要求

- 默认启用签名校验（Webhook）；
- 日志不会输出 `app_secret`、签名密钥、gateway token、Authorization 等敏感信息；
- 用户侧只回关键状态（受理、权限请求、完成、失败），不暴露内部堆栈和控制面细节。
