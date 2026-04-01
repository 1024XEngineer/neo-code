# Runtime 与 Provider 事件流设计

## Runtime 事件类型

当前 runtime 对外暴露一组小而稳定的事件：

- `agent_chunk`
- `agent_done`
- `tool_start`
- `tool_result`
- `error`

## ReAct 主循环

1. 加载目标会话或创建新会话。
2. 追加最新的用户消息。
3. 读取最新配置快照。
4. 解析当前 provider 配置并构建 provider 实例。
5. 调用 `context.Builder` 生成本轮请求使用的 `system prompt` 和消息上下文。
6. 调用 `Provider.Chat`，并把流式事件桥接给 TUI。
7. 保存 assistant 完整回复。
8. 执行返回的工具调用，并保存每一个工具结果。
9. 如果仍需继续推理，则进入下一轮；否则结束。

## 流式桥接

- Provider 发出 `StreamEvent`
- runtime 将其转换成 `RuntimeEvent`
- TUI 使用 Bubble Tea `Cmd` 监听事件，并在处理完成后继续订阅

## 持久化时机

- 用户消息提交后保存
- assistant 完整回复后保存
- 每个工具结果完成后保存
- 避免在高频 UI 刷新路径中做磁盘 I/O
