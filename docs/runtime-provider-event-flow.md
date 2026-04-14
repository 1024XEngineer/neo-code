# Runtime 与 Provider 事件流设计

## Runtime 事件类型

当前 runtime 对外暴露一组小而稳定的事件（1A 硬切后不再保留旧事件镜像）：

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
- `provider_retry`
- `permission_requested`
- `permission_resolved`
- `token_usage`
- `compact_start`
- `compact_applied`
- `compact_error`

这三类 compact 事件同时用于 `manual`、`auto` 与 `reactive` 三种来源，调用方可通过 payload 中的 `trigger_mode` 区分。

## ReAct 主循环

1. 加载目标会话或创建新会话。
2. 追加最新的用户消息。
3. 读取最新配置快照。
4. 调用 `context.Builder` 生成本轮请求使用的 `system prompt` 和消息上下文。
5. 如命中 token 阈值自动压缩建议，则先执行一次 compact，再在同一轮内重建请求。
6. 冻结当前 turn 的 `provider / model / tools / workdir / request` 快照。
7. 调用 `Provider.Generate`，并把流式事件桥接给 TUI。
8. 如 provider 返回“上下文过长”错误，则触发 `reactive` compact，并在同一 run 内最多做 3 次逐步降级的恢复尝试。
9. 保存 assistant 完整回复。
10. 执行返回的工具调用，并保存每一个工具结果。
11. 如果最终 assistant 回复后没有后续工具调用，则在 runtime 收口处安排一次后台 memo 自动提取。
12. 如果仍需继续推理，则进入下一轮；否则结束。

补充说明：
- runtime 不再设置内部 `max_loops` 停止条件；单次 run 仅在拿到最终 assistant 回复、遇到错误或收到外部取消时结束。
- 由于 session 锁覆盖整个 run 生命周期，同一会话如果持续陷入工具调用循环，会一直占用该会话直到模型自行收口、报错或被取消。

### Memo 自动提取调度

- 自动提取只在最终 assistant 回复完成且当前轮没有后续工具调用时调度。
- 如果本次 `Run` 已成功调用 `memo_remember`，则不再安排自动提取，避免与显式写入重复。
- runtime 只负责在结束点调度，不直接执行提取逻辑；实际 debounce、尾随执行与持久化去重由 `internal/memo` 内部处理。
- 调度时会绑定当次 provider/model 快照，后台任务不会重新读取全局当前配置，避免把历史会话消息发送到后续切换后的 provider。
- 自动提取失败只记日志，不额外发出 TUI 事件，也不影响主链路完成。
- `memo` 的最近消息窗口会复用 `internal/context` 的只读投影规则，只保留 provider-safe 的消息序列。
- assistant 含 `tool_calls` 时，只有在窗口内能同时保留对应 `tool` 响应时才会注入；缺响应、空内容或已被 micro compact 清空的 assistant/tool 片段会整组丢弃，保留项会先投影为模型可消费的结构化文本。
- recent window 的总消息数有硬预算：`min(limit*2, 24)`；超过预算的整段 tool span 会被跳过，避免窗口体积失控。
- 进入 `memo` 提取前，tool 文本会二次收敛为 `content_excerpt`，并按 `600` rune 上限截断。

补充约束：
- 同一 turn 内的 provider retry 只重放冻结后的 turn 快照，不会重新读取配置。
- `auto compact` 与 `reactive compact` 都不额外消耗 reasoning turn。
- 权限审批等待由 `internal/runtime/approval` 负责 request 生命周期，runtime 自己负责事件发射与 tool 重试编排。

### Context Builder 输入与职责

- `runtime` 只向 `context.Builder` 传递本轮所需元数据：
  - 历史消息
  - `workdir`
  - `shell`
  - 当前 `provider`
  - 当前 `model`
  - 会话累计输入 token 数（`SessionInputTokens`）
  - 会话累计输出 token 数（`SessionOutputTokens`）
  - 自动压缩阈值（`AutoCompactThreshold`）
- `context.Builder` 负责统一组装：
  - 固定核心 system prompt sections
  - 从 `workdir` 向上发现的 `AGENTS.md`
  - 系统状态摘要（`workdir` / `shell` / `provider` / `model` / git branch / git dirty）
  - 裁剪后的历史消息
  - 自动压缩决策（`BuildResult.AutoCompactSuggested`）
- `runtime` 不直接读取规则文件，也不直接查询 git 状态。
- `provider` 只消费最终生成的 `SystemPrompt`、消息列表和工具 schema，不感知上下文来源。

### System Prompt 注入顺序

当前 `system prompt` 按以下顺序拼装：

1. 固定核心 sections
2. `Project Rules` section
3. `System State` section

其中：

- 规则文件只支持大写文件名 `AGENTS.md`
- 多份命中结果按“从全局到局部”的顺序注入
- git 只注入摘要，不注入完整 `git status`
- 各 section 统一由 `internal/context` 内部的 `renderPromptSection` 和 `composeSystemPrompt` 渲染，`runtime` 仍只消费最终字符串

## 流式桥接

- Provider 发出 `StreamEvent`
- `internal/provider/streaming` 统一累积文本、tool call 增量和 `message_done`
- runtime 将累积过程映射成 `RuntimeEvent`
- TUI 使用 Bubble Tea `Cmd` 监听事件，并在处理完成后继续订阅
- `provider.GenerateText` 只在上游 `Generate` 成功返回时，才把缺失 `message_done` 视为流式中断。
- 如果 provider 在真正开始流式输出前直接返回 HTTP/ProviderError，则优先保留原始错误，不再额外包装成 `message_done` 缺失。

同一套流式累积逻辑同时复用于：
- 普通 `Run()` 的 assistant 回复收敛
- compact summary 生成阶段的 provider 输出消费

## Token 计量

runtime 在转发 provider 流式事件时，从 `MessageDone` 事件中提取 `Usage`（`InputTokens`、`OutputTokens`），累积到会话级计数器，并发出 `token_usage` 事件供 TUI 消费。

`token_usage` payload 包含：

- `input_tokens`：本次调用输入 token
- `output_tokens`：本次调用输出 token
- `session_input_tokens`：会话累计输入 token
- `session_output_tokens`：会话累计输出 token

## 持久化时机

- 用户消息提交后保存
- assistant 完整回复后保存
- 每个工具结果完成后保存
- 避免在高频 UI 刷新路径中做磁盘 I/O

会话 JSON 结构、工作区分桶以及 token 计数持久化约束统一见 [Session 持久化设计](./session-persistence-design.md)。
