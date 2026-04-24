# Gateway RPC API（XGO 风格）

本文描述 Gateway 控制面的 JSON-RPC 合约。  
关键行为使用 RFC 术语：`MUST` / `SHOULD` / `MAY`。

## 自动示例生成

为避免“文实不符”，仓库提供了基于 Go 结构体的自动示例生成：

1. 生成命令：`go generate ./internal/gateway/protocol`
2. 产出文件：`docs/generated/gateway-rpc-examples.json`

## 通用约束

1. 协议版本 MUST 为 `jsonrpc: "2.0"`。
2. 客户端 MUST 提供可关联的 `id`。
3. 建议优先以 `error.data.gateway_code` 作为错误分支主键。
4. 除实验能力外，本文方法默认稳定（Stable）。

---

## Method: gateway.authenticate

- Stability: Stable
- Auth Required: No（本方法用于建立认证态）
- Request Schema (Go Struct):

```go
type AuthenticateParams struct {
	Token string `json:"token"`
}
```

- Response Schema:
  - Success:

```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
    "result": {
      "type": "ack",
      "action": "authenticate",
      "request_id": "auth-1"
    }
  }
```

  - Failure（示例）:

```json
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "error": {
    "code": -32602,
    "message": "unauthorized",
    "data": { "gateway_code": "unauthorized" }
  }
}
```

- Observation:
  - Prometheus: `gateway_requests_total{method="gateway.authenticate",...}`
  - 日志：结构化请求日志字段 `request_id/method/source/status/gateway_code`

---

## Method: gateway.ping

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
// params 可为空对象 {}
```

- Response Schema:
  - Success 返回 `ack`，action=`ping`
  - Failure 返回标准 `error`（`unauthorized` / `access_denied` 等）
- Observation:
  - Prometheus: `gateway_requests_total{method="gateway.ping",...}`
  - 日志：请求级结构化日志

---

## Method: gateway.bindStream

- Stability: Stable
- Auth Required: Yes
- Request Schema (Go Struct):

```go
type BindStreamParams struct {
	SessionID string `json:"session_id"`           // MUST
	RunID     string `json:"run_id,omitempty"`     // MAY
	Channel   string `json:"channel,omitempty"`    // all|ipc|ws|sse, default all
}
```

- Response Schema:
  - Success:

```json
{
  "jsonrpc": "2.0",
  "id": "bind-1",
  "result": {
    "type": "ack",
    "action": "bind_stream",
    "request_id": "bind-1",
    "session_id": "sess-1",
    "run_id": "run-1",
    "payload": {
      "message": "stream binding updated",
      "channel": "ws"
    }
  }
}
```

  - Failure（示例）:

```json
{
  "jsonrpc": "2.0",
  "id": "bind-1",
  "error": {
    "code": -32602,
    "message": "missing required field: params.session_id",
    "data": { "gateway_code": "missing_required_field" }
  }
}
```

- 双向交互细节（重点）:
  1. 客户端在 WS/SSE 建立后 SHOULD 先调用 `gateway.bindStream`。
  2. 绑定成功后，网关将该连接注册为 `session_id`（可选 `run_id`）的事件订阅者。
  3. 后续 `gateway.event` 通知将按绑定关系定向推送，而不是广播给所有连接。
  4. 重连后 MUST 重新绑定；绑定关系不保证跨连接自动继承。

- Observation:
  - Prometheus: `gateway_requests_total{method="gateway.bindStream",...}`
  - 连接指标：`gateway_connections_active{channel="ws|sse"}`
  - 日志：`request_id/method/source/status/gateway_code`

---

## Method: gateway.run

- Stability: Stable
- Auth Required: Yes
- Request Schema (Go Struct):

```go
type RunInputMedia struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type"`
	FileName string `json:"file_name,omitempty"`
}

type RunInputPart struct {
	Type  string         `json:"type"`          // text|image
	Text  string         `json:"text,omitempty"`
	Media *RunInputMedia `json:"media,omitempty"`
}

type RunParams struct {
	SessionID  string         `json:"session_id,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	InputText  string         `json:"input_text,omitempty"`
	InputParts []RunInputPart `json:"input_parts,omitempty"`
	Workdir    string         `json:"workdir,omitempty"`
}
```

- Response Schema:
  - Success（受理即返回）:

```json
{
  "jsonrpc": "2.0",
  "id": "run-req-1",
  "result": {
    "type": "ack",
    "action": "run",
    "request_id": "run-req-1",
    "session_id": "sess-1",
    "run_id": "run-1",
    "payload": {
      "message": "run accepted"
    }
  }
}
```

  - Failure（示例）:

```json
{
  "jsonrpc": "2.0",
  "id": "run-req-1",
  "error": {
    "code": -32602,
    "message": "missing required field: ...",
    "data": { "gateway_code": "missing_required_field" }
  }
}
```

- 双向交互细节（重点）:
  1. `gateway.run` 是异步模型：网关在 runtime 真正完成前先返回 `ack`。
  2. 客户端 MUST 使用 `session_id + run_id` 追踪后续 `gateway.event` 通知。
  3. 若请求未提供 `run_id`，网关会按规则归一化（优先请求显式值，其次回退 `request_id`，再生成内部 ID）。
  4. 运行中的进度/完成/错误通过 `gateway.event` 推送；客户端 SHOULD 处理乱序与重连重订阅。
  5. 取消流程使用 `gateway.cancel`，且 `run_id` 为必填关联键。

- Observation:
  - Prometheus: `gateway_requests_total{method="gateway.run",...}`
  - 异步失败日志：`gateway run async failed: request_id=... session_id=... run_id=... code=...`
  - 请求日志：`request_id/method/source/status/gateway_code`

---

## Method: gateway.compact

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type CompactParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
}
```

- Response Schema:
  - Success: `ack` + compact 结果
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.compact",...}`

---

## Method: gateway.cancel

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type CancelParams struct {
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"` // MUST（业务语义必填）
}
```

- Response Schema:
  - Success: `ack`，payload 包含取消结果
  - Failure: `missing_required_field` / `resource_not_found` / `access_denied` 等
- Observation:
  - `gateway_requests_total{method="gateway.cancel",...}`

---

## Method: gateway.listSessions

- Stability: Stable
- Auth Required: Yes
- Request Schema: 空对象 `{}` 或省略 `params`
- Response Schema: `ack` + sessions 摘要列表
- Observation:
  - `gateway_requests_total{method="gateway.listSessions",...}`

---

## Method: gateway.loadSession

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type LoadSessionParams struct {
	SessionID string `json:"session_id"`
}
```

- Response Schema: `ack` + session 详情
- Observation:
  - `gateway_requests_total{method="gateway.loadSession",...}`

---

## Method: gateway.resolvePermission

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ResolvePermissionParams struct {
	RequestID string `json:"request_id"` // MUST
	Decision  string `json:"decision"`   // allow_once|allow_session|reject
}
```

- Response Schema: `ack`（提交成功）或标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.resolvePermission",...}`

---

## Method: gateway.event

- Stability: Stable
- Auth Required: Yes（由连接态决定）
- Request Schema: N/A（通知方法，由网关下推）
- Response Schema: N/A
- Observation:
  - 通过 WS/SSE/IPC 连接投递
  - 与 `gateway.bindStream` 绑定关系联动

---

## Method: wake.openUrl

- Stability: Experimental
- Auth Required: Yes（同连接鉴权策略）
- Request Schema: `WakeIntent`（action/session/workdir/params）
- Response Schema: `ack` 或标准 `error`
- Observation:
  - 统计进入 `gateway_requests_total{method="wake.openUrl",...}`
  - 与 url-dispatch 自动拉起链路联动
