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
	URI      string `json:"uri,omitempty"`
	AssetID  string `json:"asset_id,omitempty"`
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

- 多模态附件约束：
  - `type=image` 时 `media.mime_type` 必填。
  - `media.uri` 与 `media.asset_id` 必须二选一，不能同时为空或同时提供。
  - `media.uri` 仅用于后端可读取的本地路径；Web 浏览器上传图片或文本应先通过 `POST /api/session-assets` 保存，再在 `gateway.run` 中使用 `media.asset_id` 引用。
  - `asset_id` 必须属于当前 `session_id`，不存在或跨 session 引用会在 runtime 输入准备阶段失败。
  - 文本附件（如 `.md`、`.json`、`.csv`）使用 `type=image` + 真实文本 mime 表达，runtime 端按 `session.TextAssetWhitelist` 自动判定并内联为 user message 的 text part；无需新增 `type` 字段。详见 issue #701。

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

## HTTP API: session assets

浏览器图片上传不应把本地伪路径传给 Runtime。Web 客户端需要在发送前先创建或确认 `session_id`，再通过受鉴权保护的 HTTP API 保存图片，最后在 `gateway.run.input_parts[].media.asset_id` 中引用。

### POST /api/session-assets

- Auth Required: Yes（`Authorization: Bearer <token>`）
- Headers:
  - `X-NeoCode-Workspace-Hash`: 当前工作区哈希。多工作区 Web 客户端必须发送；单工作区或旧客户端可省略并回落到默认工作区。
- Content-Type: `multipart/form-data`
- Fields:
  - `session_id`: 目标会话 ID，必填。
  - `file`: 图片或文本文件，必填。
- Server-side validation:
  - 接受 `image/png`、`image/jpeg`、`image/webp`（按文件头嗅探）。
  - 同时接受会话侧文本资产白名单内的扩展名（`.txt`、`.md`、`.json`、`.yaml`、`.yml`、`.csv`）与对应 MIME（`text/plain`、`text/markdown`、`application/json`、`text/yaml`、`application/x-yaml`、`text/csv`）。
  - 文本资产额外校验 UTF-8，非 UTF-8 内容返回 `415`。
  - MIME 以服务端文件头检测结果为准，不信任浏览器声明。
  - 空文件返回 `400`。
  - 超过 `MaxSessionAssetBytes`（图片）或 `MaxTextAssetBytes`（文本）返回 `413`。
  - 不在任一白名单内的类型返回 `415`。
  - 未认证返回 `401`，Origin/CORS 或 ACL 拒绝返回 `403`。
  - 工作区不存在返回 `404 workspace not found`；目标 session 不在该工作区返回 `404 session not found`。
- Response:

```json
{
  "session_id": "sess-1",
  "asset_id": "asset-1",
  "mime_type": "image/png",
  "size": 1024
}
```

文本附件上传成功后，runtime 会在 `PrepareUserInput` 阶段按 `session.TextAssetWhitelist` 命中后自动读取并内联为 user message 的 `text` content part（带文件名边界 + 可选截断提示），Provider 层不感知"文件"概念。配置项 `runtime.assets.text_asset_enabled`（默认 `true`）可关闭该行为，关闭后文本附件会作为普通附件原样提交。详见 `docs/runtime-provider-event-flow.md` 与 issue #701。

### GET /api/session-assets/{session_id}/{asset_id}

- Auth Required: Yes（`Authorization: Bearer <token>`）
- Headers:
  - `X-NeoCode-Workspace-Hash`: 当前工作区哈希。多工作区 Web 客户端必须发送；省略时回落到默认工作区。
- 返回图片二进制，`Content-Type` 为保存时确认的 MIME。
- 用于历史消息缩略图按需读取。
- 工作区不存在返回 `404 workspace not found`；不存在或不可见的 asset 返回 `404 asset not found`。

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

## Method: gateway.executeSystemTool

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ExecuteSystemToolParams struct {
	SessionID string          `json:"session_id,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	ToolName  string          `json:"tool_name"` // MUST
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
```

- Response Schema:
  - Success: `ack` + tool result payload
  - Failure: 标准 `error`（`missing_required_field` / `invalid_action` 等）
- Runtime Restriction:
  - 网关层对 `tool_name` 实施白名单校验。
  - 当前允许 `memo_list`、`memo_remember`、`memo_recall`、`memo_remove`、`diagnose`。

---

## Method: gateway.activateSessionSkill

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ActivateSessionSkillParams struct {
	SessionID string `json:"session_id"` // MUST
	SkillID   string `json:"skill_id"`   // MUST
}
```

- Response Schema:
  - Success: `ack`，`payload` 返回 `session_id`、`skill_id` 与状态提示
  - Failure: 标准 `error`（`missing_required_field` / `invalid_action` / `access_denied` 等）
- Observation:
  - `gateway_requests_total{method="gateway.activateSessionSkill",...}`

---

## Method: gateway.deactivateSessionSkill

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type DeactivateSessionSkillParams struct {
	SessionID string `json:"session_id"` // MUST
	SkillID   string `json:"skill_id"`   // MUST
}
```

- Response Schema:
  - Success: `ack`，`payload` 返回 `session_id`、`skill_id` 与状态提示
  - Failure: 标准 `error`（`missing_required_field` / `invalid_action` / `access_denied` 等）
- Observation:
  - `gateway_requests_total{method="gateway.deactivateSessionSkill",...}`

---

## Method: gateway.listSessionSkills

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ListSessionSkillsParams struct {
	SessionID string `json:"session_id"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload.skills`（会话内激活技能状态数组）
  - Failure: 标准 `error`（`missing_required_field` / `access_denied` 等）
- Observation:
  - `gateway_requests_total{method="gateway.listSessionSkills",...}`

---

## Method: gateway.listAvailableSkills

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ListAvailableSkillsParams struct {
	SessionID string `json:"session_id,omitempty"` // OPTIONAL
}
```

- Response Schema:
  - Success: `ack` + `payload.skills`（可见技能状态数组，含 `active` 标记）
  - `payload.skills[*].descriptor.source.layer`（可选）：`project|global`，用于区分技能来源层级
  - Failure: 标准 `error`（`invalid_action` / `access_denied` 等）
- Observation:
  - `gateway_requests_total{method="gateway.listAvailableSkills",...}`

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

## Method: gateway.approvePlan

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type ApprovePlanParams struct {
	SessionID string `json:"session_id"` // MUST
	PlanID    string `json:"plan_id"`    // MUST
	Revision  int    `json:"revision"`   // MUST > 0
}
```

- Response Schema:

```json
{
  "type": "ack",
  "action": "approve_plan",
  "session_id": "session-1",
  "payload": {
    "plan_id": "plan-1",
    "revision": 2,
    "status": "approved"
  }
}
```

- Semantics:
  - 仅批准当前会话中匹配 `plan_id + revision` 的 `draft` 计划。
  - 成功后客户端可再调用 `gateway.run({ "mode": "build" })` 执行已批准计划。

---

## Method: gateway.userQuestionAnswer

- Stability: Beta
- Auth Required: Yes
- Request Schema:

```go
type UserQuestionAnswerParams struct {
	RequestID string   `json:"request_id"`        // MUST
	Status    string   `json:"status,omitempty"`  // answered|skipped，默认 answered
	Values    []string `json:"values,omitempty"`  // 可选：选择值
	Message   string   `json:"message,omitempty"` // 可选：文本回答
}
```

- Response Schema: `ack`（提交成功）或标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.userQuestionAnswer",...}`

---

## Method: gateway.listProviders

- Stability: Stable
- Auth Required: Yes
- Request Schema: 空对象 `{}` 或省略 `params`
- Response Schema: `ack` + `payload.providers`（ProviderOption 数组）
- Observation:
  - `gateway_requests_total{method="gateway.listProviders",...}`

---

## Method: gateway.createCustomProvider

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type CreateCustomProviderParams struct {
	Name                  string                    `json:"name"`     // MUST
	Driver                string                    `json:"driver"`   // MUST
	BaseURL               string                    `json:"base_url,omitempty"`
	ChatAPIMode           string                    `json:"chat_api_mode,omitempty"`
	ChatEndpointPath      string                    `json:"chat_endpoint_path,omitempty"`
	APIKeyEnv             string                    `json:"api_key_env"` // MUST
	APIKey                string                    `json:"api_key,omitempty"`
	ModelSource           string                    `json:"model_source,omitempty"`
	DiscoveryEndpointPath string                    `json:"discovery_endpoint_path,omitempty"`
	Models                []ProviderModelDescriptor `json:"models,omitempty"`
}
```

- Response Schema:
  - Success: `ack` + `payload` 包含 `provider_id`、`model_id`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.createCustomProvider",...}`

---

## Method: gateway.deleteCustomProvider

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type DeleteCustomProviderParams struct {
	ProviderID string `json:"provider_id"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload` 包含 `deleted`、`provider_id`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.deleteCustomProvider",...}`

---

## Method: gateway.selectProviderModel

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type SelectProviderModelParams struct {
	ProviderID string `json:"provider_id"`        // MUST
	ModelID    string `json:"model_id,omitempty"` // 省略表示仅切换 provider
}
```

- Response Schema:
  - Success: `ack` + `payload` 包含 `provider_id`、`model_id`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.selectProviderModel",...}`

---

## Method: gateway.listMCPServers

- Stability: Stable
- Auth Required: Yes
- Request Schema: 空对象 `{}` 或省略 `params`
- Response Schema: `ack` + `payload.servers`（MCPServerEntry 数组）
- Observation:
  - `gateway_requests_total{method="gateway.listMCPServers",...}`

---

## Method: gateway.upsertMCPServer

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type UpsertMCPServerParams struct {
	Server MCPServerParams `json:"server"` // MUST
}

type MCPServerParams struct {
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled,omitempty"`
	Source  string            `json:"source,omitempty"`
	Version string            `json:"version,omitempty"`
	Stdio   MCPStdioParams    `json:"stdio,omitempty"`
	Env     []MCPEnvVarParams `json:"env,omitempty"`
}

type MCPStdioParams struct {
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	Workdir           string   `json:"workdir,omitempty"`
	StartTimeoutSec   int      `json:"start_timeout_sec,omitempty"`
	CallTimeoutSec    int      `json:"call_timeout_sec,omitempty"`
	RestartBackoffSec int      `json:"restart_backoff_sec,omitempty"`
}

type MCPEnvVarParams struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	ValueEnv string `json:"value_env,omitempty"`
}
```

- Response Schema:
  - Success: `ack` + `payload.server`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.upsertMCPServer",...}`

---

## Method: gateway.setMCPServerEnabled

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type SetMCPServerEnabledParams struct {
	ID      string `json:"id"`      // MUST
	Enabled bool   `json:"enabled"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload` 包含 `id`、`enabled`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.setMCPServerEnabled",...}`

---

## Method: gateway.deleteMCPServer

- Stability: Stable
- Auth Required: Yes
- Request Schema:

```go
type DeleteMCPServerParams struct {
	ID string `json:"id"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload` 包含 `deleted`、`id`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="gateway.deleteMCPServer",...}`

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
  - 与 daemon dispatcher 自动拉起链路联动

---

## Method: session.todos.list

- Stability: Beta
- Auth Required: Yes
- Request Schema:

```go
type ListSessionTodosParams struct {
	SessionID string `json:"session_id"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload.todos`（todo 列表）和 `payload.summary`（聚合统计）
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="session.todos.list",...}`

---

## Method: runtime.snapshot.get

- Stability: Beta
- Auth Required: Yes
- Request Schema:

```go
type GetRuntimeSnapshotParams struct {
	SessionID string `json:"session_id"` // MUST
}
```

- Response Schema:
  - Success: `ack` + `payload.snapshot`（runtime facts、decision、todo snapshot）
  - `payload.snapshot.pending_user_question`（可选）：
    - `request_id`
    - `question_id`
    - `title` / `description`
    - `kind`（`text|single_choice|multi_choice`）
    - `options`
    - `required` / `allow_skip`
    - `max_choices` / `timeout_sec`
  - Failure: 标准 `error`
- Observation:
  - `gateway_requests_total{method="runtime.snapshot.get",...}`
