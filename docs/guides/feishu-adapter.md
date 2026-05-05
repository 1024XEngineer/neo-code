# Feishu Adapter 接入指南（Phase 1）

本文档描述 #554 Phase 1 的最小可用接入方式：通过 `neocode feishu-adapter` 把飞书消息桥接到现有 Gateway。

## 1. 边界说明

- 本阶段只实现云端控制面桥接（Adapter）。
- 不新增任何 Gateway RPC action。
- 仅复用：
  - `gateway.authenticate`
  - `gateway.bindStream`
  - `gateway.run`
  - `gateway.resolvePermission`
  - `gateway.event`（通知）
- 不依赖 `gateway.createSession`。
- 真正“云端飞书 -> 用户本机执行”的主动长连通道属于 #555，不在本阶段内。

## 2. 会话与运行 ID 规则

- `session_id = "feishu_" + stableHash(chat_id)`
- `run_id = "feishu_" + stableHash(message_id)`

这两个 ID 都是确定性生成，便于幂等和追踪。

## 3. 事件处理顺序

飞书消息进入后，适配器固定执行：

1. `authenticate`
2. `bindStream(session_id, run_id)`
3. `run(session_id, run_id, input_text)`
4. 持续消费 `gateway.event`

说明：必须先 `bindStream` 再 `run`，避免丢失 run 早期事件。

## 4. 幂等与重放防护

- 消息事件去重键：`event_id + message_id`（TTL 内只执行一次 run）。
- 卡片回调去重键：`request_id + decision`（TTL 内只提交一次审批）。
- 去重在 `run` 被网关受理后才标记成功；若受理失败会释放去重键，允许飞书重试恢复。

## 5. 审批闭环

Phase 1 仅支持最小动作：

- `allow_once`
- `reject`

当收到 `permission_requested` 事件时，适配器发送最小审批卡片；用户点击后回调 `gateway.resolvePermission`。

## 6. 安全要求

适配器日志不会打印以下敏感信息：

- `app_secret`
- `verify_token`
- `signing_secret`
- `gateway token`
- `Authorization` 头

默认要求签名校验与回调 token 校验都开启：

- `verify_token` 必填
- `signing_secret` 必填

仅在联调场景可显式设置 `insecure_skip_signature_verify=true` 跳过签名校验（不建议生产使用）。

群聊消息默认仅在检测到 `@` 机器人时受理；私聊消息默认受理。

用户可见错误仅返回摘要，不回传内部堆栈。

## 7. 启动方式

```bash
neocode feishu-adapter
```

也可以通过命令行参数覆盖配置：

```bash
neocode feishu-adapter \
  --listen 127.0.0.1:18080 \
  --event-path /feishu/events \
  --card-path /feishu/cards \
  --app-id xxx \
  --app-secret yyy \
  --verify-token zzz \
  --signing-secret sss
```

## 8. 配置示例

参考 [../examples/feishu.yaml](../examples/feishu.yaml)。
