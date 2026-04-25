# 配置指南

本文说明 NeoCode 当前真实生效的配置规则。

## 总原则

- `config.yaml` 只保存最小运行时状态
- provider 元数据来自代码内置定义或 custom provider 文件
- API Key 只从环境变量读取
- YAML 采用严格解析，未知字段直接报错

这意味着 NeoCode 当前不会：

- 自动清理旧版 `providers` / `provider_overrides`
- 自动兼容 `workdir`、`default_workdir` 等旧字段

## 配置文件位置

主配置文件路径：

```text
~/.neocode/config.yaml
```

custom provider 目录：

```text
~/.neocode/providers/<provider-name>/provider.yaml
```

## `config.yaml` 可写字段

当前支持的主配置示例：

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
  budget:
    prompt_budget: 0
    reserve_tokens: 13000
    fallback_prompt_budget: 100000
    max_reactive_compacts: 3
```

### 基础字段

| 字段 | 说明 |
|------|------|
| `selected_provider` | 当前选中的 provider 名称 |
| `current_model` | 当前选中的模型 ID |
| `shell` | 默认 shell，Windows 默认 `powershell`，其他平台默认 `bash` |
| `tool_timeout_sec` | 工具执行超时（秒） |

### `context` 字段

| 字段 | 说明 |
|------|------|
| `context.compact.manual_strategy` | `/compact` 手动压缩策略，支持 `keep_recent` / `full_replace` |
| `context.compact.manual_keep_recent_messages` | `keep_recent` 策略下保留的最近消息数 |
| `context.compact.micro_compact_retained_tool_spans` | 默认保留原始内容的最近可压缩工具块数量，默认 `6` |
| `context.compact.read_time_max_message_spans` | context 读时保留的 message span 上限 |
| `context.compact.max_summary_chars` | compact summary 最大字符数 |
| `context.compact.micro_compact_disabled` | 是否关闭默认启用的 micro compact |
| `context.budget.prompt_budget` | 显式输入预算；`> 0` 时直接使用，`0` 表示自动推导 |
| `context.budget.reserve_tokens` | 自动推导输入预算时，从模型窗口中预留给输出、tool call、system prompt 的缓冲 |
| `context.budget.fallback_prompt_budget` | 模型窗口不可用或推导失败时使用的保底输入预算 |
| `context.budget.max_reactive_compacts` | 单次 Run 内允许的 reactive compact 最大次数 |

### `runtime` 字段

| 字段 | 说明 |
|------|------|
| `runtime.max_no_progress_streak` | 连续"无进展"轮次提醒阈值，默认 `5` |
| `runtime.max_repeat_cycle_streak` | 连续"重复调用同一工具参数"提醒阈值，默认 `3` |
| `runtime.max_turns` | 单次 Run 的最大推理轮数上限，默认 `40` |
| `runtime.assets.max_session_asset_bytes` | 单个 session asset 最大字节数，默认 20 MiB |
| `runtime.assets.max_session_assets_total_bytes` | 单次请求可携带的 session asset 总字节上限，默认 20 MiB |

### `verification` 字段

| 字段 | 说明 |
|------|------|
| `verification.enabled` | 是否启用验证引擎，默认 `true` |
| `verification.final_intercept` | 是否在任务收尾前拦截并触发验证，默认 `true` |
| `verification.max_no_progress` | 验证无进展时的最大重试次数，默认 `3` |
| `verification.max_retries` | 验证失败后的最大重试次数，默认 `2` |
| `verification.verifiers.<name>.enabled` | 是否启用该验证器 |
| `verification.verifiers.<name>.required` | 该验证器是否为硬性要求 |
| `verification.verifiers.<name>.timeout_sec` | 该验证器的执行超时 |
| `verification.verifiers.<name>.fail_closed` | 验证器异常时是否按失败处理 |
| `verification.execution_policy.allowed_commands` | 验证器可执行的命令白名单 |
| `verification.execution_policy.denied_commands` | 验证器禁止执行的命令黑名单 |

### `tools` 字段

| 字段 | 说明 |
|------|------|
| `tools.webfetch.max_response_bytes` | WebFetch 最大响应字节数 |
| `tools.webfetch.supported_content_types` | WebFetch 允许的内容类型 |
| `tools.mcp.servers` | MCP server 列表，见 [MCP 配置](./mcp) |

## 不写入 `config.yaml` 的字段

以下内容不允许写入主配置文件：

- `providers`
- `provider_overrides`
- `workdir`
- `default_workdir`
- `base_url`
- `api_key_env`
- `models`

如果这些字段出现在 `config.yaml` 中，加载会直接失败。

## 环境变量

API Key 只从系统环境变量读取。

| Provider | 环境变量 |
|----------|----------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |
| `modelscope` | `MODELSCOPE_API_KEY` |

```bash
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
export MODELSCOPE_API_KEY="ms-..."
```

## CLI 运行参数覆盖

工作目录不写入 `config.yaml`，只通过启动参数覆盖：

```bash
neocode --workdir /path/to/workspace
```

## 常见错误

### 旧字段被拒绝

如果在 `config.yaml` 中包含 `workdir`、`providers` 等字段，当前版本会报未知字段错误。处理方式是手动删除这些字段。

### 旧 `context.auto_compact` 字段

如果配置中只存在 `context.auto_compact`，启动时 preflight 会自动迁移为 `context.budget`，并写入 `config.yaml.bak` 备份。如果 `context.auto_compact` 与 `context.budget` 同时存在，启动会直接报错，需要手动合并后再启动。

### API Key 未设置

```text
config: environment variable OPENAI_API_KEY is empty
```

在当前 shell 中设置对应环境变量后再启动 NeoCode。

## 相关文档

- [切换模型](./providers)
- [MCP 配置](./mcp)
- [更新升级](./update)
