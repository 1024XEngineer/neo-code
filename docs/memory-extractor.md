# Memory Extractor

NeoCode now supports multiple memory extraction strategies through `memory.extractor`.

## Modes

- `rule`: deterministic rule-based extraction only. This is the default and adds no extra model call.
- `llm`: uses a chat model to convert a conversation turn into structured memory JSON.
- `auto`: tries the LLM extractor first and falls back to the rule extractor when the model call or JSON parsing fails.

## Config

```yaml
memory:
  extractor: "rule"
  extractor_model: ""
  extractor_timeout_seconds: 20
```

- `memory.extractor_model`: optional. When empty, NeoCode reuses `ai.model`.
- `memory.extractor_timeout_seconds`: timeout for a single LLM extraction request.

## Validation

- `rule` should preserve current behavior and add no network cost.
- `llm` requires a working chat provider and valid API key.
- `auto` should still write memory when the LLM extractor fails, by falling back to the rule extractor.
