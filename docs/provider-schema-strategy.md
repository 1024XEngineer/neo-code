# Provider Schema 抹平策略
## 为什么需要 Provider 层
不同模型 API 在消息结构、流式协议和工具调用格式上差异很大。NeoCode 将这些差异都封装在 `internal/provider` 内部，让 runtime 始终只面向一套干净的领域模型工作。

## 内部标准结构
- `Message`：统一消息格式，包含 `role`、`content`、可选工具调用和工具结果元信息
- `ToolCall`：统一工具调用结构，包含 `id`、`name` 和完整 JSON 参数字符串
- `ToolSpec`：Provider 可消费的统一工具 schema
- `ChatRequest` / `ChatResponse`：Provider 无关的请求与响应信封
- `StreamEvent`：Provider 在流式返回过程中发出的标准事件

## OpenAI 适配规则
- 将统一消息映射为 OpenAI 的 `messages` 格式
- 按照 SSE 逐行解析流式数据
- 根据 `tool_calls[index]` 拼接碎片化的 `arguments`
- 只有在参数拼接完整后，才向 runtime 返回结构化 `ToolCall`

## Runtime 契约
- runtime 绝不能直接操作厂商专属 JSON 结构
- tool role 的差异必须由 provider 适配器在内部抹平
- 所有 Provider HTTP 请求都必须遵守 `context.Context`
