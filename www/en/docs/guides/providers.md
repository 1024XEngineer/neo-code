# Switching Models and Providers

NeoCode supports multiple model providers and lets you switch between them at any time.

## Built-in providers

| Provider | Environment variable |
|----------|---------------------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |

## Switching in the TUI

Select a different provider or model from the interactive menu inside NeoCode. The selection is saved to `~/.neocode/config.yaml`.

## Adding a custom provider

For any OpenAI-compatible API (enterprise gateways, local models), create:

```text
~/.neocode/providers/<name>/provider.yaml
```

Example:

```yaml
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: discover
base_url: https://llm.example.com/v1
chat_api_mode: chat_completions
chat_endpoint_path: /chat/completions
discovery_endpoint_path: /models
```

### Manual model list

If the provider does not support model discovery, use `model_source: manual`:

```yaml
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: manual
base_url: https://llm.example.com/v1
chat_endpoint_path: /chat/completions
models:
  - id: gpt-4o-mini
    name: GPT-4o Mini
    context_window: 128000
```

### Field reference

| Field | Description |
|-------|-------------|
| `name` | Provider identifier, used in `selected_provider` |
| `driver` | Driver type. Currently supports `openaicompat` |
| `api_key_env` | Environment variable name for the API key |
| `model_source` | `discover` (auto) or `manual` (explicit list) |
| `base_url` | Service base URL |
| `chat_api_mode` | `chat_completions` or `responses` |
| `chat_endpoint_path` | Chat endpoint path |
| `discovery_endpoint_path` | Model discovery path (`discover` mode only) |

## Related

- [Configuration guide](./configuration)
