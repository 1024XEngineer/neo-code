# Runtime 与 Provider 事件流设计

## Runtime 事件类型

当前 runtime 对外暴露一组小而稳定的事件：

- `user_message`
- `agent_chunk`
- `agent_done`
- `tool_start`
- `tool_chunk`
- `tool_result`
- `tool_call_thinking`
- `provider_retry`
- `compact_start`
- `compact_done`
- `compact_error`
- `micro_compact_applied`
- `run_canceled`
- `error`

## ReAct 主循环

1. 加载目标会话或创建新会话。
2. 追加最新的用户消息。
3. 读取最新配置快照。
4. 每轮请求前执行一次 `micro compact`（轻量压缩旧 tool result）。
5. 解析当前 provider 配置并构建 provider 实例。
6. 调用 `context.Builder` 生成本轮请求使用的 `system prompt` 和消息上下文。
7. 调用 `Provider.Chat`，并把流式事件桥接给 TUI。
8. 保存 assistant 完整回复。
9. 执行返回的工具调用，并保存每一个工具结果。
10. 如果仍需继续推理，则进入下一轮；否则结束。

## 手动 `/compact` 命令链路

1. TUI 识别 `/compact` 并调用 `runtime.Compact(...)`。
2. runtime 发出 `compact_start` 事件。
3. compact 服务先写 transcript（完整原始消息，JSONL）。
4. 按策略（`keep_recent` / `full_replace`）执行手动压缩并生成指标。
5. runtime 回写 session（仅在 `applied=true` 时写回）。
6. 成功时发出 `compact_done`；失败时发出 `compact_error`。

## Compact 结果与可观测字段

- `before_chars`
- `after_chars`
- `saved_ratio`
- `trigger_mode`（`micro` / `manual`）
- `transcript_id`
- `transcript_path`

### Context Builder 输入与职责

- `runtime` 只向 `context.Builder` 传递本轮所需元数据：
  - 历史消息
  - `workdir`
  - `shell`
  - 当前 `provider`
  - 当前 `model`
- `context.Builder` 负责统一组装：
  - 固定核心 system prompt sections
  - 从 `workdir` 向上发现的 `AGENTS.md`
  - 系统状态摘要（`workdir` / `shell` / `provider` / `model` / git branch / git dirty）
  - 裁剪后的历史消息
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
- runtime 将其转换成 `RuntimeEvent`
- TUI 使用 Bubble Tea `Cmd` 监听事件，并在处理完成后继续订阅

## 持久化时机

- 用户消息提交后保存
- assistant 完整回复后保存
- 每个工具结果完成后保存
- 每次 compact 执行前先保存 transcript（完整留痕）
- 避免在高频 UI 刷新路径中做磁盘 I/O
