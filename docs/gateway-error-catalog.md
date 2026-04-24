# Gateway 错误字典（HTTP / JSON-RPC / gateway_code 对照）

> 处理建议：客户端 SHOULD 以 `gateway_code` 作为主分支条件，HTTP 与 JSON-RPC 作为传输层辅助信息。

| gateway_code | HTTP 状态（`/rpc`） | JSON-RPC `error.code` | Reasoning（触发逻辑） | 客户端建议 |
|---|---:|---:|---|---|
| `invalid_frame` | 200 | -32602 或 -32700 | 请求体不是合法 JSON、JSON-RPC 结构非法、`params` 解码失败、字段类型错误。 | 直接失败，修正请求结构后重试。 |
| `invalid_action` | 200 | -32602 | 方法参数语义非法（如 `bindStream.channel` 非法）、运行被取消映射为动作无效。 | 直接失败，修正参数或状态机。 |
| `invalid_multimodal_payload` | 200 | -32602 | `gateway.run` 的多模态片段结构不符合约束（类型/字段不合法）。 | 直接失败，修正 payload。 |
| `missing_required_field` | 200 | -32602 | 缺失必填字段（如 `params.session_id`、`params.request_id`、`payload.run_id`）。 | 直接失败，补齐字段。 |
| `unsupported_action` | 200 | -32601 | 方法不存在或当前版本未实现。 | 降级到兼容方法，或提示版本不支持。 |
| `internal_error` | 200 | -32603 | 网关内部异常、运行时不可用、不可归类的执行失败。 | 可短暂重试；持续失败需告警。 |
| `timeout` | 200 | -32603 | Gateway 调用 runtime 超过操作超时窗口。 | 可重试并增加客户端超时预算；必要时调用 `gateway.cancel`。 |
| `unauthorized` | 401 | -32602 | 未提供有效 token 或连接未完成认证。 | 刷新凭据并重新认证，不建议盲重试。 |
| `access_denied` | 403 | -32602 | 已认证但 ACL/主体权限不允许当前动作或资源访问。 | 直接失败，提示权限不足。 |
| `resource_not_found` | 200 | -32602 | 目标资源在业务层不存在或不可见（典型为会话/运行目标查无记录）；不是“格式错误”。 | 可提示用户检查 `session_id/run_id` 是否真实存在。 |

## 说明

1. HTTP 状态映射在 `/rpc` 路径中仅对 `unauthorized` 与 `access_denied` 使用 401/403，其余错误通常仍返回 200 + JSON-RPC 错误体。
2. `resource_not_found` 的判断来自 runtime 语义错误映射（查不到目标），而非参数格式校验；格式问题通常进入 `invalid_frame` 或 `missing_required_field`。
