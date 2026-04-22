# Gateway Error Catalog（错误字典）

本文档用于第三方客户端实现统一异常处理策略，覆盖 Gateway 稳定错误码集合：

`invalid_frame`、`invalid_action`、`invalid_multimodal_payload`、`missing_required_field`、`unsupported_action`、`internal_error`、`timeout`、`unauthorized`、`access_denied`、`resource_not_found`。

## 1. 错误码对照表

| gateway_code | HTTP 状态码（/rpc） | JSON-RPC code | Reasoning（触发逻辑） | 典型触发场景 | 客户端处理建议 |
| --- | --- | --- | --- | --- | --- |
| `invalid_frame` | `200` | `-32700` / `-32600` / `-32602` | 请求帧结构或编码不合法。包括 JSON 解析失败、请求体包含多余 JSON 值、`id/jsonrpc` 非法、`params` 严格解码失败。 | 非法 JSON；`id` 为 `null`；`params` 含未知字段。 | 不要直接重试，先修复请求构造器。 |
| `invalid_action` | `200` | `-32602` | 动作参数值非法，但方法本身存在。 | `params.channel` 不在 `all/ipc/ws/sse`；`params.decision` 非 `allow_once/allow_session/reject`。 | 视为调用方输入错误，修正参数后再发。 |
| `invalid_multimodal_payload` | `200` | `-32602` | `gateway.run` 的 `input_parts` 结构或字段不满足契约。 | `image` 分片缺少 `media.uri` 或 `media.mime_type`；`text` 分片文本为空。 | 校验输入分片后重试，不做盲重试。 |
| `missing_required_field` | `200` | `-32600` / `-32602` | 缺失必填字段。请求层字段缺失多映射为 `-32600`，方法参数层字段缺失多映射为 `-32602`。 | 缺失 `id`；缺失 `params`；`cancel` 缺失 `run_id`。 | 调整参数补齐必填项再重试。 |
| `unsupported_action` | `200` | `-32601` | 方法未注册或不被网关识别。 | 调用不存在的方法名。 | 客户端按能力探测降级，或升级服务端版本。 |
| `internal_error` | `200` | `-32603` | 网关内部异常或未分类下游异常。 | 结果编码失败；runtime port 不可用；未知运行时错误。 | 采用指数退避重试；持续失败时告警。 |
| `timeout` | `200` | `-32603` | 网关调用 runtime 超时（`context.DeadlineExceeded`）。 | `run/compact/cancel/loadSession/resolvePermission` 下游调用超时。 | 可重试且建议带幂等键（如固定 `run_id`）。 |
| `unauthorized` | `401`（仅 /rpc） | `-32602` | 请求未通过认证。 | 未携带 token；token 非法；连接未先 `authenticate`。 | 先刷新凭证并重新认证，认证成功后再发业务请求。 |
| `access_denied` | `403`（仅 /rpc） | `-32602` | 已认证但不具备该方法或资源权限。 | ACL 拒绝当前来源调用该方法；runtime 返回 access denied。 | 终止当前请求并提示授权不足，不要盲重试。 |
| `resource_not_found` | `200` | `-32602` | 目标资源不存在或不可见，由 runtime 明确返回 `ErrRuntimeResourceNotFound`。 | `gateway.cancel` 指定的 `run_id` 不存在或已结束。 | 可视为业务态终态，通常无需重试。 |

## 2. resource_not_found 判定边界（重点）

1. `resource_not_found` 不是“参数格式错误”。  
2. `session_id/run_id` 的格式问题通常归入 `invalid_frame` 或 `missing_required_field`。  
3. 当前实现中，`resource_not_found` 主要来自 runtime 明确返回“目标不存在”（例如取消不存在的 `run_id`）。  
4. `gateway.loadSession` 在默认桥接实现下可能自动创建会话，因此“会话不存在”不一定返回 `resource_not_found`。  

## 3. HTTP 与 JSON-RPC 组合规则

1. `/rpc` 默认返回 `HTTP 200`，并在 JSON-RPC `error` 中给出 `gateway_code`。  
2. 仅当 `gateway_code=unauthorized` 时返回 `HTTP 401`。  
3. 仅当 `gateway_code=access_denied` 时返回 `HTTP 403`。  

## 4. 客户端 try-catch 推荐顺序

1. 先解析 `error.data.gateway_code`（稳定分支键）。  
2. 再解析 `error.code`（JSON-RPC 互操作）。  
3. 最后使用 `error.message` 做日志与人类可读提示。  
