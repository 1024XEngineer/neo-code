# 记忆提取器

本文档聚焦 `memory.extractor` 的配置语义和运行行为。关于整体分层和三类记忆的职责，请优先参考 `docs/memory-architecture.md`。

## 作用范围

`memory.extractor` 只负责一件事：把当前这一轮 `userInput + assistantReply` 转成结构化记忆条目。

它不负责：

- 加载 `AGENTS.md` 或其他显式配置的兼容约定文件
- 构建工作会话记忆快照
- 直接决定记忆召回排序

## 统一契约

所有提取模式共享同一接口：

- 输入：一轮对话的 `userInput` 与 `assistantReply`
- 输出：`[]MemoryItem`
- 触发时机：聊天回复完成后，由 `memorySvc.Save(...)` 调用

提取器会直接跳过以下情况：

- 用户输入为空
- 助手回复为空
- 助手回复看起来是工具调用协议载荷，而不是自然语言答复

## 模式说明

### `rule`

- 含义：仅使用确定性规则提取。
- 方式：通过关键字、路径锚点和语义线索匹配生成结构化条目。
- 优点：稳定、可预测、无额外模型调用。
- 限制：对隐含语义、复杂修复经验的抽取能力弱于 `llm`。

### `llm`

- 含义：使用聊天模型将对话内容转换为结构化记忆 JSON。
- 方式：额外发起一次模型调用，请求模型返回限定 schema 的 JSON。
- 优点：语义抽取能力更强。
- 限制：依赖模型提供方、API Key、模型稳定性以及 JSON 可解析性。

### `auto`

- 含义：优先尝试 LLM 提取；失败时回退到规则提取。
- 方式：先执行 `llm`，若 provider 缺失、超时、模型调用失败或 JSON 解析失败，则自动改用 `rule`。
- 优点：在保证可用性的前提下尽量获得更好的抽取质量。
- 建议：作为推荐模式使用。

## 输出条目类型

提取器当前可产出的条目类型为：

- `user_preference`
- `project_rule`
- `code_fact`
- `fix_recipe`
- `session_memory`

其中：

- `session_memory` 写入会话存储，仅服务当前会话。
- 其余类型写入长期存储，但仍会受 `memory.persist_types` 配置约束。

## 配置项

```yaml
memory:
  extractor: "rule"
  extractor_model: ""
  extractor_timeout_seconds: 20
```

- `memory.extractor`：提取策略，可选 `rule`、`llm`、`auto`。
- `memory.extractor_model`：可选项。为空时默认复用 `ai.model`。
- `memory.extractor_timeout_seconds`：单次 LLM 提取请求的等待上限，仅在 `llm` 或 `auto` 模式下生效。

## 验证要求

- `rule` 模式应保持当前行为，不增加网络请求成本。
- `llm` 模式要求底层聊天提供方可用，且 API Key 配置正确。
- `auto` 模式在 LLM 提取失败时，仍应通过规则提取完成记忆写入。
- 提取器失败不应影响主聊天回复已经产生的结果，只影响本轮记忆沉淀质量。
