# 记忆提取器

NeoCode 现在通过 `memory.extractor` 支持多种记忆提取策略。

## 模式说明

- `rule`：仅使用确定性的规则提取。这是默认模式，不会增加额外模型调用。
- `llm`：使用聊天模型将对话内容转换为结构化记忆 JSON。
- `auto`：优先尝试 LLM 提取；当模型调用失败或 JSON 解析失败时，回退到规则提取。

## 配置项

```yaml
memory:
  extractor: "rule"
  extractor_model: ""
  extractor_timeout_seconds: 20
```

- `memory.extractor_model`：可选项。为空时默认复用 `ai.model`。
- `memory.extractor_timeout_seconds`：单次 LLM 提取请求的等待上限，仅在 `llm` 或 `auto` 模式下生效。

## 验证要求

- `rule` 模式应保持当前行为，不增加网络请求成本。
- `llm` 模式要求底层聊天提供方可用，且 API Key 配置正确。
- `auto` 模式在 LLM 提取失败时，仍应通过规则提取完成记忆写入。
