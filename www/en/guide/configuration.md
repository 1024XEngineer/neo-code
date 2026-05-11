---
title: Configuration
description: Configure API keys, models, shell, workspace, custom providers, and MCP tools.
---

# Configuration

NeoCode keeps configuration small: save your normal choices in config, keep secrets in environment variables. For first use, configure a provider, model, and shell; enable other features only when needed.

Config file:

```text
~/.neocode/config.yaml
```

## Minimal config

```yaml
selected_provider: openai
current_model: gpt-5.4
shell: bash
```

Windows users usually want:

```yaml
shell: powershell
```

## API keys

NeoCode reads API keys from environment variables only. It does not write plaintext keys into config files.

| Provider | Environment variable |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Gemini | `GEMINI_API_KEY` |
| OpenLL | `AI_API_KEY` |
| Qiniu | `QINIU_API_KEY` |
| ModelScope | `MODELSCOPE_API_KEY` |

macOS / Linux:

```bash
export OPENAI_API_KEY="your_key_here"
```

Windows PowerShell:

```powershell
$env:OPENAI_API_KEY = "your_key_here"
```

If you want the variable to persist, use your operating system or shell's normal environment variable setup. Do not put real keys in `config.yaml`.

### ModelScope API Key Setup

ModelScope uses a semi-automated TUI guide flow to configure the API key:

1. Run `/provider` in the TUI and select `modelscope`
2. Press Enter to step through: guide page → login page, complete ModelScope community login
3. On the Token page (<https://www.modelscope.cn/my/access/token>), create and copy your API Key
4. Back in the TUI, paste the token in the guide panel and press Enter to validate
5. **Bind Alibaba Cloud account**: Visit [Account Settings](https://www.modelscope.cn/my/settings/account) to link your Alibaba Cloud account. This step is required — API calls will fail without it

If token validation detects an auth or permission error, the TUI will automatically open the account binding page.

## Switch provider and model

The recommended path is the NeoCode UI; selections are saved automatically:

```text
/provider
/model
```

You can also edit config directly:

```yaml
selected_provider: gemini
current_model: gemini-2.5-pro
```

If the model list is empty, first check that the active provider's API key is set in the same terminal that launched NeoCode.

## Workspace

Use the launch argument for workspaces:

```bash
neocode -w /path/to/project
```

Inside NeoCode:

```text
/cwd
/cwd /path/to/project
```

## Shell and tool timeout

The shell controls the environment used when the agent runs commands:

```yaml
shell: powershell    # Windows
shell: bash          # macOS / Linux
```

If tests or builds in your project often take longer, raise the tool timeout:

```yaml
tool_timeout_sec: 30
```

## Custom providers

If your model service is not built in, add an OpenAI-compatible provider.

The easiest path is interactive setup:

```text
/provider add
```

Or create:

```text
~/.neocode/providers/company/provider.yaml
```

Example:

```yaml
name: company
driver: openaicompat
api_key_env: COMPANY_API_KEY
model_source: discover
base_url: https://llm.example.com/v1
chat_api_mode: chat_completions
chat_endpoint_path: /chat/completions
discovery_endpoint_path: /models
```

If your service cannot list models automatically, use a manual list:

```yaml
name: company
driver: openaicompat
api_key_env: COMPANY_API_KEY
model_source: manual
base_url: https://llm.example.com/v1
chat_endpoint_path: /chat/completions
models:
  - id: company-coder
    name: Company Coder
    context_window: 128000
```

Custom providers also store only the environment variable name. Put the real key in `COMPANY_API_KEY`.

## MCP tools

Use MCP when you want the agent to call external tools, such as documentation search, issue lookup, or internal automation.

Minimal example:

```yaml
tools:
  mcp:
    servers:
      - id: docs
        enabled: true
        source: stdio
        stdio:
          command: node
          args:
            - ./mcp-server.js
          workdir: ./mcp
        env:
          - name: MCP_TOKEN
            value_env: MCP_TOKEN
```

After configuring it, start NeoCode and ask:

```text
List the tools currently available to you.
```

For fuller MCP setup, exposure controls, and troubleshooting, see [MCP Tools](./mcp).

## Common issues

### API key not set

If you see an error like:

```text
environment variable OPENAI_API_KEY is empty
```

the active provider's environment variable is not available in the current terminal session. Set it, then restart NeoCode.

### Unknown config field

NeoCode validates `config.yaml` strictly. If you copied config from old docs, keep the common fields first: `selected_provider`, `current_model`, `shell`, `tool_timeout_sec`, and `tools`.

Remove fields you are unsure about, then reconfigure with `/provider`, `/model`, or `/provider add`.

## Next steps

- Daily commands: [Daily Use](./daily-use)
- Approval behavior: [Tools & Permissions](./tools-permissions)
- External tools: [MCP Tools](./mcp)
- Something wrong: [Troubleshooting](./troubleshooting)
