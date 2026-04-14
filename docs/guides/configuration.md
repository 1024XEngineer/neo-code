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
- 自动加载 `.env`

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
    read_time_max_message_spans: 24
    max_summary_chars: 1200
    micro_compact_disabled: false
  auto_compact:
    enabled: false
    input_token_threshold: 100000
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
| `context.compact.read_time_max_message_spans` | context 读时保留的 message span 上限，用于降低“继续”时较早文件读取结果被过早裁掉的风险 |
| `context.compact.max_summary_chars` | compact summary 最大字符数 |
| `context.compact.micro_compact_disabled` | 是否关闭默认启用的 micro compact |
| `context.auto_compact.enabled` | 是否启用自动压缩 |
| `context.auto_compact.input_token_threshold` | 自动压缩输入 token 阈值 |

### `tools` 字段

| 字段 | 说明 |
|------|------|
| `tools.webfetch.max_response_bytes` | WebFetch 最大响应字节数 |
| `tools.webfetch.supported_content_types` | WebFetch 允许的内容类型 |
| `tools.mcp.servers` | MCP server 列表 |

## 不写入 `config.yaml` 的字段

以下内容不允许写入主配置文件：

- `providers`
- `provider_overrides`
- `workdir`
- `default_workdir`
- `base_url`
- `api_key_env`
- `models`

如果这些字段出现在 `config.yaml` 中，加载会直接失败，而不是被“自动迁移”或“悄悄清理”。

## provider 策略

NeoCode 采用“builtin provider + custom provider”双来源模型。

### builtin provider

builtin provider 由代码内置，集中定义在：

```text
internal/config/builtin_providers.go
```

当前内置 provider：

- `openai`
- `gemini`
- `openll`
- `qiniu`

### custom provider

custom provider 通过单独文件声明，而不是写进 `config.yaml`：

```yaml
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: chat_completions
```

文件路径：

```text
~/.neocode/providers/company-gateway/provider.yaml
```

## 环境变量

API Key 只从系统环境变量读取。

常见映射：

| Provider | 环境变量 |
|----------|----------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |

示例：

```bash
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
```

Windows PowerShell：

```powershell
$env:OPENAI_API_KEY = "sk-..."
$env:GEMINI_API_KEY = "AI..."
```

## 启动时的选择修正

`config.yaml` 里的 `selected_provider/current_model` 表达的是“用户上次保存的选择状态”。

启动时系统还会进行选择校验与必要修正；若 driver 不受支持会报错并中止。因此需要区分两件事：

- 配置快照结构合法
- 当前选择已经可直接运行

前者由 `config.ValidateSnapshot()` 保证，后者由 `internal/config/state.Service.EnsureSelection()` 保证。

不要把这两层职责混在一起理解。

## CLI Workdir 覆盖

工作目录不写入 `config.yaml`，只通过启动参数覆盖：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

说明：

- `--workdir` 只影响本次进程
- 不会回写到 `config.yaml`
- 工具根目录与 session 隔离都会使用该工作区

## 常见错误

### 旧字段被拒绝

如果在 `config.yaml` 中看到如下字段：

- `workdir`
- `default_workdir`
- `providers`
- `provider_overrides`

当前版本会直接报未知字段错误。处理方式是手动删除这些字段，而不是等待程序自动迁移。

### API Key 未设置

报错示例：

```text
config: environment variable OPENAI_API_KEY is empty
```

处理方式：先在当前 shell 中设置对应环境变量，再启动 NeoCode。

## 相关文档

- [添加 Provider](./adding-providers.md)
- [配置管理详细设计](../config-management-detail-design.md)
- [Context Compact](../context-compact.md)
