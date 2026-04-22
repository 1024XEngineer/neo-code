# Gateway 第三方接入协作指南

本文面向接入 NeoCode Gateway 的第三方开发者，目标是让你在不阅读源码的前提下完成：

1. 建立连接并通过认证。
2. 发起一次完整运行并消费事件流。
3. 在常见错误下快速定位与恢复。

## 1. Getting Started

### 1.1 接入前提

- Gateway 已启动（本地 IPC 或 HTTP/WS/SSE 控制面）。
- 已获取有效认证 Token（默认来自 `~/.neocode/auth.json`）。
- 客户端具备 JSON-RPC 2.0 编解码能力。

### 1.2 传输通道选择

| 通道 | 适用场景 | 认证方式 | 备注 |
|---|---|---|---|
| IPC | 本地桌面/CLI | `gateway.authenticate` | 延迟低，推荐本机应用 |
| `POST /rpc` | 单次调用 | `Authorization: Bearer <token>` | 无长连接 |
| `GET /ws` | 双向长连接 | `gateway.authenticate`（可附带 token） | 适合事件+请求双向复用 |
| `GET /sse` | 单向事件流 | `?token=<token>` | 仅服务端推送 |

### 1.3 最小握手流程（推荐）

推荐顺序：

1. 连接 WS（或 IPC）。
2. 调用 `gateway.authenticate`。
3. 调用 `gateway.bindStream` 绑定 `session_id` 与可选 `run_id`。
4. 调用 `gateway.run`，收到 `ack`。
5. 持续消费 `gateway.event` 通知。
6. 必要时调用 `gateway.cancel`。

### 1.4 示例：`gateway.authenticate`

```json
{
  "jsonrpc": "2.0",
  "id": "req-auth-1",
  "method": "gateway.authenticate",
  "params": {
    "token": "<YOUR_TOKEN>"
  }
}
```

成功响应（示例）：

```json
{
  "jsonrpc": "2.0",
  "id": "req-auth-1",
  "result": {
    "type": "ack",
    "action": "authenticate",
    "request_id": "req-auth-1",
    "payload": {
      "message": "authenticated",
      "subject_id": "local_admin"
    }
  }
}
```

## 2. Message Protocol

### 2.1 JSON-RPC 请求/响应

请求：

```json
{
  "jsonrpc": "2.0",
  "id": "req-123",
  "method": "gateway.run",
  "params": {}
}
```

成功响应：

```json
{
  "jsonrpc": "2.0",
  "id": "req-123",
  "result": {
    "type": "ack",
    "action": "run",
    "request_id": "req-123",
    "session_id": "session-1",
    "run_id": "run-1",
    "payload": {
      "message": "run accepted"
    }
  }
}
```

错误响应：

```json
{
  "jsonrpc": "2.0",
  "id": "req-123",
  "error": {
    "code": -32602,
    "message": "missing required field: params.run_id",
    "data": {
      "gateway_code": "missing_required_field"
    }
  }
}
```

### 2.2 Notification（`gateway.event`）

网关会主动推送：

```json
{
  "jsonrpc": "2.0",
  "method": "gateway.event",
  "params": {
    "type": "event",
    "action": "run",
    "session_id": "session-1",
    "run_id": "run-1",
    "payload": {
      "event_type": "run_progress",
      "payload": {
        "runtime_event_type": "agent_chunk",
        "turn": 1,
        "phase": "reasoning",
        "timestamp": "2026-04-22T09:00:00Z",
        "payload_version": 2,
        "payload": "..."
      }
    }
  }
}
```

说明：

- `params` 是统一 `MessageFrame`。
- `payload.event_type` 为网关层三态：`run_progress`、`run_done`、`run_error`。
- 内层 `payload.payload` 为 runtime 事件 envelope，第三方可按需消费。

### 2.3 方法契约分层

稳定核心方法：

- `gateway.authenticate`
- `gateway.ping`
- `gateway.bindStream`
- `gateway.run`
- `gateway.compact`
- `gateway.cancel`
- `gateway.listSessions`
- `gateway.loadSession`
- `gateway.resolvePermission`
- `gateway.event`

实验扩展：

- `wake.openUrl`

### 2.4 参数约束（高频）

- `gateway.cancel`：`params.run_id` 必填。
- `gateway.bindStream`：`params.session_id` 必填，`channel` 允许 `all|ipc|ws|sse`。
- `gateway.run`：`input_text` 或 `input_parts` 至少一个非空。
- 参数解析为严格模式，未知字段会触发参数错误。

## 3. Status Codes

### 3.1 三层错误信号

1. HTTP 状态码（仅网络入口可见）。
2. JSON-RPC `error.code`（标准码）。
3. `error.data.gateway_code`（网关稳定语义码）。

### 3.2 HTTP 映射

| 场景 | HTTP 状态 |
|---|---|
| `unauthorized` | `401` |
| `access_denied` | `403` |
| 其他业务错误 | `200`（错误在 JSON-RPC `error` 中） |

### 3.3 稳定 `gateway_code`

| gateway_code | 含义 | 建议动作 |
|---|---|---|
| `invalid_frame` | 协议帧非法/JSON 解析失败 | 修正请求格式 |
| `invalid_action` | 方法参数非法或动作无效 | 修正参数值 |
| `invalid_multimodal_payload` | 多模态输入格式不合法 | 修正 `input_parts` |
| `missing_required_field` | 缺失必填字段 | 补齐字段 |
| `unsupported_action` | 方法未实现或不支持 | 升级客户端或降级调用 |
| `internal_error` | 网关内部错误 | 重试并记录 request_id |
| `timeout` | 网关下游调用超时 | 指数退避重试 |
| `unauthorized` | 认证失败 | 刷新 token 并重新认证 |
| `access_denied` | ACL 或资源权限拒绝 | 检查方法权限 |
| `resource_not_found` | 目标会话或运行不存在 | 校验 `session_id/run_id` |

## 4. Client Best Practices

### 4.1 连接与重连

- 客户端 `SHOULD` 使用指数退避重连（如 200ms 起步，逐步扩展到 3s 上限）。
- 每次重连成功后 `MUST` 重新 `authenticate`，并按需重新 `bindStream`。
- 对于长连接，`SHOULD` 周期性调用 `gateway.ping` 维持活性并刷新绑定。

### 4.2 请求幂等与关联

- `id` `MUST` 全局唯一（至少在进程生命周期内）。
- `run_id` `SHOULD` 由客户端生成并持久化，便于取消与追踪。
- 日志中 `SHOULD` 记录 `request_id/session_id/run_id` 三元组。

### 4.3 超时与重试

- `gateway.run` 为异步受理，不应等待“最终模型输出”才算成功。
- 对 `ping/auth/bindStream/list/load` 这类短调用，`SHOULD` 采用较短超时并允许小次数重试。
- 对 `timeout` 或网络传输错误，`MAY` 重试；对 `invalid_*` 或 `missing_required_field`，不应盲目重试。

### 4.4 事件消费

- 客户端 `MUST` 区分响应与通知，不能把 `gateway.event` 当作请求响应。
- 事件消费协程 `SHOULD` 与请求协程解耦，避免背压阻塞导致断连。

### 4.5 安全建议

- Token 只放内存与受控安全存储，不应写入日志。
- 浏览器场景需遵循 Origin 白名单策略，避免跨站调用失败。

## 5. Failure Playbook

### 5.1 连接失败（dial refused / no such pipe）

排查步骤：

1. 确认 Gateway 进程是否在运行。
2. 确认地址配置是否与 Gateway 监听地址一致。
3. 若客户端启用自动拉起，检查自动拉起日志与探测窗口是否超时。

恢复动作：

- 执行指数退避重连；重连后重新认证与绑定。

### 5.2 认证失败（`unauthorized`）

排查步骤：

1. 检查 Token 来源是否正确（是否读取了期望的 token 文件）。
2. 校验 Header 或 query 参数是否丢失或污染空格。

恢复动作：

- 刷新 token，重新执行 `gateway.authenticate`。

### 5.3 参数错误（`missing_required_field` / `invalid_action`）

排查步骤：

1. 对照方法契约检查必填字段。
2. 检查是否发送了未定义字段（严格解码会拒绝）。

恢复动作：

- 修正请求体，不要直接重试同一无效请求。

### 5.4 超时（`timeout`）

排查步骤：

1. 区分是网络超时还是 gateway 下游调用超时。
2. 检查当前并发、请求体大小与运行时负载。

恢复动作：

- 降低并发、启用退避重试、增加调用级超时预算。

### 5.5 连接重置后的恢复

恢复顺序（必须按序）：

1. 重建连接。
2. 重新认证。
3. 重新绑定流（`gateway.bindStream`）。
4. 根据业务状态决定是否补发请求或仅恢复事件订阅。

---

协作约定：

- 当你接入的是“稳定核心方法 + 稳定错误码”，网关小版本升级不应破坏基础兼容性。
- 当你使用实验扩展（如 `wake.openUrl`），应预留版本探测与降级处理逻辑。
