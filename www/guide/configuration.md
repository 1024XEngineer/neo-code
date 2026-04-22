---
title: 配置入口
description: NeoCode 当前真实生效的配置文件、字段范围、Provider 入口与环境变量约束。
---

# 配置入口

## 配置文件位置

主配置文件：

```text
~/.neocode/config.yaml
```

自定义 Provider：

```text
~/.neocode/providers/<provider-name>/provider.yaml
```

## 配置原则

NeoCode 当前的配置规则很明确：

- `config.yaml` 只保存最小运行时状态
- Provider 元数据来自代码内置定义或 custom provider 文件
- API Key 只从环境变量读取
- YAML 采用严格解析，未知字段直接报错

这意味着 NeoCode 不会把明文密钥写进主配置，也不会悄悄兼容一批旧字段。

## `config.yaml` 里当前常见字段

下面这个示例直接对应当前配置指南中的可写字段：

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

## 用户最常关心的字段

### 基础字段

- `selected_provider`：当前选中的 Provider
- `current_model`：当前选中的模型 ID
- `shell`：默认 Shell，Windows 通常是 `powershell`
- `tool_timeout_sec`：工具执行超时秒数

### `context` 相关

- `context.compact.manual_strategy`：`/compact` 的手动压缩策略
- `context.compact.manual_keep_recent_messages`：保留的最近消息数
- `context.auto_compact.enabled`：是否启用自动压缩
- `context.auto_compact.reserve_tokens`：自动阈值推导时的预留 token

### `runtime` 相关

- `runtime.max_no_progress_streak`：连续无进展轮次的熔断阈值
- `runtime.max_repeat_cycle_streak`：重复调用同一工具参数时的熔断阈值
- `runtime.assets.*`：单个与总 `session_asset` 大小限制

## custom provider 示例

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

如果你使用 `manual` 模式，需要显式提供 `models` 列表；如果没配，加载会直接报错。

## 不会写进主配置的内容

以下内容不允许写入 `config.yaml`：

- `providers`
- `provider_overrides`
- `workdir`
- `default_workdir`
- `base_url`
- `api_key_env`
- `models`

如果这些字段出现在主配置里，加载会失败，而不是静默迁移。

## 环境变量

常见映射如下：

| Provider | 环境变量 |
| --- | --- |
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |

## 继续阅读

- 会话工作区与 `/cwd`：看 [工作区与会话](./workspace-session)
- Gateway 配置和安全限制：看 [Gateway 与 URL Dispatch](./gateway)
- 更完整的设计说明：看 [深入阅读](/reference/)
