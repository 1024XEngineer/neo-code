# Runtime 与 Provider 事件流设计

## Runtime 事件类型

当前 runtime 对外暴露一组稳定事件：

- `user_message`
- `agent_chunk`
- `agent_done`
- `tool_call_thinking`
- `tool_start`
- `tool_chunk`
- `tool_result`
- `phase_changed`
- `progress_evaluated`
- `stop_reason_decided`
- `permission_requested`
- `permission_resolved`
- `budget_checked`
- `ledger_reconciled`
- `token_usage`
- `skill_activated`
- `skill_deactivated`
- `skill_missing`
- `compact_start`
- `compact_applied`
- `compact_error`

当前事件 envelope 的唯一有效 `payload_version` 为 `4`。

## ReAct 主循环

单次 run 的主链路为：

1. 加载或创建会话
2. 追加最新用户消息
3. 读取当前配置快照
4. 调用 `context.Builder` 构建 provider-facing request
5. 冻结当前 turn 的 `provider / model / tools / workdir / request / prompt_budget`
6. 调用 provider 的 `EstimateInputTokens`
7. 由 budget control plane 输出 `allow | compact | stop`
8. 若为 `compact`，先执行 `proactive` compact，再在同一 run 内重建 request
9. 若为 `allow`，调用 `Provider.Generate`
10. 若 provider 返回 `context_too_long`，触发 `reactive` compact，并重新进入预算闭环
11. 正常返回后，对 usage 做 reconcile
12. 追加 assistant 消息
13. 执行工具调用并写回 tool result
14. 如仍需继续推理，进入下一轮；否则结束

## Budget 控制面

runtime 不再消费旧的 builder 压缩建议，而是使用冻结快照上的显式预算决策。

### `budget_checked`

每次发送前预算判定都会发出 `budget_checked`：

- `attempt_seq`
- `request_hash`
- `action`
- `reason`
- `estimated_input_tokens`
- `prompt_budget`
- `estimate_source`
- `estimate_gate_policy`

语义：

- `allow`：本轮请求在预算内
- `compact`：首次超预算，需要先压缩
- `stop` + `reason=exceeds_budget_after_compact_stop`：压缩后仍超预算且估算可门禁（`gateable`），停止当前 run
- `allow` + `reason=exceeds_budget_after_compact_allow_advisory`：压缩后仍超预算但估算仅 advisory，继续放行

## Context Builder 职责

`runtime` 向 `context.Builder` 传入：

- 历史消息
- `workdir`
- `shell`
- 当前 `provider`
- 当前 `model`
- 会话累计输入 token
- 会话累计输出 token

`context.Builder` 只负责：

- 组装 `system prompt`
- 读取 `AGENTS.md`
- 注入 `Task State` / `Todo State` / `Skills` / `Memo`
- 执行 read-time trim 和 micro compact
- 输出最终 `SystemPrompt` 与消息列表

`context.Builder` 不再负责：

- token budget 判断
- proactive compact 触发
- 旧的 builder 压缩建议布尔值

## 流式桥接

- provider 发出 `StreamEvent`
- `internal/provider` 只处理协议差异
- `internal/runtime/streaming` 统一累积文本、tool call 增量和 `message_done`
- runtime 将结果映射成 `RuntimeEvent`
- TUI 通过 Bubble Tea `Cmd` 监听这些事件

## Usage 对账

provider 返回后，runtime 会执行显式的账本调和。

### `ledger_reconciled`

每轮 provider 调用完成后都会发出：

- `attempt_seq`
- `request_hash`
- `input_tokens`
- `input_source`
- `output_tokens`
- `output_source`
- `has_unknown_usage`

规则：

- provider 返回 usage 时，`input_source=observed`，`output_source=observed`
- provider usage 缺失时，输入侧回退到发送前 estimate，因此 `input_source=estimated`
- provider usage 缺失时，输出侧不伪装成观测值，因此 `output_source=unknown`
- 只要出现过未知 output usage，会话级 `HasUnknownUsage` 会被置为 `true`

### `token_usage`

`token_usage` 继续面向 TUI 提供单轮和会话累计数据，并新增来源标签：

- `input_tokens`
- `output_tokens`
- `input_source`
- `output_source`
- `has_unknown_usage`
- `session_input_tokens`
- `session_output_tokens`

## 持久化时机

- 用户消息提交后立即持久化
- assistant 完整回复后立即持久化
- 每个工具结果完成后立即持久化
- compact 成功后通过 `ReplaceTranscript` 原子重写 transcript

会话级 token totals 和 `HasUnknownUsage` 由 `runtime` 统一维护，并在持久化层落盘。
