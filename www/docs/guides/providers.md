# 切换模型与 Provider

NeoCode 支持多个模型服务商（provider），可以随时切换。

## 内置 Provider

当前内置的 provider：

| Provider | 环境变量 |
|----------|----------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |

## 在 TUI 中切换

启动 NeoCode 后，在交互界面中可以直接切换 provider 和模型。切换结果会保存到 `~/.neocode/config.yaml` 的 `selected_provider` 和 `current_model` 字段。

## 添加自定义 Provider

对于兼容 OpenAI 接口的服务（企业网关、本地模型等），创建以下文件：

```text
~/.neocode/providers/<name>/provider.yaml
```

示例配置：

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

### 手动指定模型列表

如果 provider 不支持模型发现（`/models` 接口），使用 `model_source: manual`：

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

### 字段说明

| 字段 | 说明 |
|------|------|
| `name` | provider 标识，用于 `selected_provider` |
| `driver` | 驱动类型，目前支持 `openaicompat` |
| `api_key_env` | API Key 的环境变量名 |
| `model_source` | `discover`（自动发现）或 `manual`（手动列表） |
| `base_url` | 服务 base URL |
| `chat_api_mode` | `chat_completions` 或 `responses` |
| `chat_endpoint_path` | 聊天接口路径 |
| `discovery_endpoint_path` | 模型发现接口路径（`discover` 模式） |

## 相关文档

- [配置指南](./configuration)
