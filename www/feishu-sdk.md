---
title: Feishu SDK 模式
description: 使用飞书 SDK 长连接把群聊或私聊消息接入本机 NeoCode Gateway。
---

# Feishu SDK 模式

`sdk` 模式适合本地个人使用：不需要公网回调地址，也不需要 ngrok / cloudflared。Adapter 通过飞书官方长连接接收 `im.message.receive_v1`，私聊消息和群聊 `@bot` 消息都会走统一的 `bindStream -> run` 链路。

## 配置

`config.yaml` 里只保留非敏感字段：

```yaml
feishu:
  enabled: true
  ingress: "sdk"
  app_id: "cli_xxx"
  bot_user_id: "ou_xxx"
  bot_open_id: "ou_xxx"
  request_timeout_sec: 8
  idempotency_ttl_sec: 600
  reconnect_backoff_min_ms: 500
  reconnect_backoff_max_ms: 10000
  rebind_interval_sec: 15
  gateway:
    listen: "127.0.0.1:8080"
    token_file: "~/.neocode/auth.json"
```

启动前必须设置环境变量：

```bash
export FEISHU_APP_SECRET="cli_secret_xxx"
```

Webhook 模式才需要额外设置：

```bash
export FEISHU_SIGNING_SECRET="signing_secret_xxx"
```

## 启动

```bash
neocode feishu-adapter --ingress sdk
```

如果缺少 `FEISHU_APP_SECRET`，启动会直接失败，防止用户把明文 secret 写进配置文件。

## 用户价值

- 保证群聊消息能触发 run 并回传状态：SDK 订阅直接处理 `im.message.receive_v1`，群聊 `@bot` 与私聊共用一套消息映射。
- 防止 SDK 忽略失败导致不可重试：`gateway.Run` 等瞬时错误会返回失败 ACK，飞书可以继续重试。
- 用户无需明文存储秘密：`config.yaml` 不再保存 `app_secret` / `signing_secret`。
- 卡片集中显示任务状态方便用户观察：每个 run 只维护一张状态卡片，不再用多条进度文本刷屏。

## 轻量级卡片示例

```text
任务：修复 SDK 群聊消息
状态：planning
审批：pending
结果：pending
回灌：async_rewake
```

状态变化时 Adapter 会调用 `UpdateCard(cardID, payload)` 更新同一张卡片；完成或失败后，卡片会写入 `success/failure`，同时再回传一条最终文本摘要。
