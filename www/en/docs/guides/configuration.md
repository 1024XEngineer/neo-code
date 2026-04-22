# Configuration Guide

This page covers the configuration rules that NeoCode enforces.

## Core rules

- `config.yaml` stores only minimal runtime state
- Provider metadata comes from built-in definitions or custom provider files
- API keys are read from environment variables only
- YAML is parsed strictly — unknown fields cause a startup error

NeoCode does not:

- Auto-migrate old `providers` / `provider_overrides` fields
- Silently ignore `workdir`, `default_workdir`, or other legacy fields

## Config file location

Main config:

```text
~/.neocode/config.yaml
```

Custom provider directory:

```text
~/.neocode/providers/<provider-name>/provider.yaml
```

## Writable fields in `config.yaml`

```yaml
selected_provider: openai
current_model: gpt-5.4
shell: bash
tool_timeout_sec: 20
runtime:
  max_no_progress_streak: 3
  max_repeat_cycle_streak: 3
  assets:
    max_session_asset_bytes: 20971520
    max_session_assets_total_bytes: 20971520

tools:
  webfetch:
    max_response_bytes: 262144
    supported_content_types:
      - text/html
      - text/plain
      - application/json

context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 10
    micro_compact_retained_tool_spans: 6
    read_time_max_message_spans: 24
    max_summary_chars: 1200
    micro_compact_disabled: false
  auto_compact:
    enabled: false
    input_token_threshold: 0
    reserve_tokens: 13000
    fallback_input_token_threshold: 100000
```

### Basic fields

| Field | Description |
|-------|-------------|
| `selected_provider` | Active provider name |
| `current_model` | Active model ID |
| `shell` | Default shell (`powershell` on Windows, `bash` elsewhere) |
| `tool_timeout_sec` | Tool execution timeout in seconds |

### `runtime` fields

| Field | Description |
|-------|-------------|
| `runtime.max_no_progress_streak` | Consecutive no-progress rounds before circuit break, default `3` |
| `runtime.max_repeat_cycle_streak` | Consecutive repeated tool calls before circuit break, default `3` |
| `runtime.assets.max_session_asset_bytes` | Max bytes per session asset, default 20 MiB |
| `runtime.assets.max_session_assets_total_bytes` | Max total session asset bytes per request, default 20 MiB |

### `tools` fields

| Field | Description |
|-------|-------------|
| `tools.webfetch.max_response_bytes` | Max response bytes for WebFetch |
| `tools.webfetch.supported_content_types` | Allowed content types for WebFetch |
| `tools.mcp.servers` | MCP server list, see [MCP Configuration](./mcp) |

## Fields that must NOT be in `config.yaml`

These fields will cause a startup error if present:

- `providers`
- `provider_overrides`
- `workdir`
- `default_workdir`
- `base_url`
- `api_key_env`
- `models`

Remove them manually — NeoCode does not auto-migrate them.

## Environment variables

| Provider | Environment variable |
|----------|---------------------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |

```bash
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
```

## CLI overrides

Working directory is passed at startup, not saved:

```bash
neocode --workdir /path/to/workspace
```

## Common errors

### Unknown field error

If `config.yaml` contains `workdir`, `providers`, or similar legacy fields, remove them manually.

### API key not set

```text
config: environment variable OPENAI_API_KEY is empty
```

Set the environment variable in your shell before starting NeoCode.

## Related

- [Switch models](./providers)
- [MCP configuration](./mcp)
- [Updating](./update)
