# 配置指南

本文档说明 NeoCode 的配置策略和配置文件结构。

## Provider 策略

NeoCode 采用"内置 Provider 优先"的策略：

### 核心原则

✅ **配置集中管理**
- 所有内置 provider 定义集中在 `internal/config/builtin_providers.go`
- 配置随代码版本发布，自动更新

✅ **最小持久化**
- `config.yaml` 不再持久化完整 `providers` 列表
- 只保存当前选择状态和通用运行配置
- 运行时的 `providers` 完全来自代码内置定义
- 用户不能通过 YAML 注入新的 provider

✅ **安全第一**
- API Key 只从环境变量读取，永不写入 YAML
- 不硬编码在源码中

### 当前内置 Provider

| Provider | Driver | 说明 |
|----------|--------|------|
| `openai` | `openai` | OpenAI 官方 API |
| `gemini` | `openai` | Google Gemini (OpenAI-compatible API) |
| `openll` | `openai` | OpenLL 服务 (OpenAI-compatible API) |
| `qiniu` | `openai` | 七牛云推理服务 (OpenAI-compatible API) |

所有内置 provider 都复用 `openai` 驱动，支持流式输出和 Tool Call。

### 设计优势

这种方式意味着：

- ✅ 新用户启动后自动获得当前版本最新的内置 provider
- ✅ 未来代码新增 provider 时，用户无需修改 YAML
- ✅ 老配置文件中的 `providers` / `provider_overrides` 会在加载时被清理
- ✅ 配置文件始终保持简洁，只包含必要的运行时状态
- ✅ 如需新增 provider，只能通过代码扩展内建列表完成

## 配置文件

### 默认路径

```
~/.neocode/config.yaml
```

### 完整配置示例

```yaml
selected_provider: openai
current_model: gpt-4.1
workdir: /Users/username/projects/myproject
shell: bash
max_loops: 8

tools:
  webfetch:
    max_response_bytes: 1048576
    supported_content_types:
      - text/html
      - text/plain
      - application/json
```

### 字段说明

#### 基础配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `selected_provider` | string | `openai` | 当前选择的 provider 名称 |
| `current_model` | string | 取决于 provider | 当前选择的模型名称 |
| `workdir` | string | `.` (当前目录) | 工作目录的绝对路径 |
| `shell` | string | `bash` (Linux/Mac)<br>`powershell` (Windows) | Shell 类型 |
| `max_loops` | int | `8` | Agent 推理循环最大轮数 |

#### 工具配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `tools.webfetch.max_response_bytes` | int | `1048576` (1MB) | WebFetch 工具最大响应字节数 |
| `tools.webfetch.supported_content_types` | []string | `[text/html, text/plain, application/json]` | 支持的内容类型 |

### 配置文件特点

**自动管理**：
- 首次启动时自动创建默认配置
- `workdir` 自动转换为绝对路径
- 无效配置会在启动时报错

**最小持久化**：
- 不保存 `providers` 列表（由代码内置提供）
- 不保存 `base_url`、`models` 等 provider 元数据
- 只保存用户的选择状态和自定义设置

## 环境变量

每个 provider 对应的 API Key 环境变量：

| Provider | 环境变量 | 获取方式 |
|----------|----------|----------|
| `openai` | `OPENAI_API_KEY` | [OpenAI Platform](https://platform.openai.com/api-keys) |
| `gemini` | `GEMINI_API_KEY` | [Google AI Studio](https://aistudio.google.com/apikey) |
| `openll` | `AI_API_KEY` | OpenLL 服务提供商 |
| `qiniu` | `QINIU_API_KEY` | 七牛云推理服务 |

### 环境变量管理

#### 方式：系统环境变量

**Linux/macOS**：
```bash
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
export AI_API_KEY="your-key"
```

**Windows PowerShell**：
```powershell
$env:OPENAI_API_KEY="sk-..."
$env:GEMINI_API_KEY="AI..."
$env:AI_API_KEY="your-key"
```

### 安全最佳实践

⚠️ **重要安全提示**：

1**使用环境变量**
   - API Key 仅从环境变量读取
   - 永不写入配置文件
   - 不硬编码在代码中

2**密钥轮换**
   - 定期更换 API Key
   - 不要在多个环境使用同一个 Key

## Slash Commands

NeoCode 提供以下 slash 命令用于快速切换配置：

### /provider - Provider 选择器

```
/provider
```

打开 provider 选择器，列出所有可用的内置 provider。

**界面示例**：
```
? Select a provider:
  ❯ openai
    gemini
    openll
    qiniu
```

### /model - 模型选择器

```
/model
```

打开当前 provider 的模型选择器。

模型列表会合并当前 provider 的默认模型、服务端动态发现结果和本地缓存结果。

**界面示例**（选择 openai provider 后）：
```
? Select a model:
  ❯ gpt-4.1
    gpt-4o
    gpt-5.4
    gpt-5.3-codex
```

## 配置管理

配置管理由 `internal/config` 模块负责：

### 核心功能

- ✅ YAML 加载与保存
- ✅ 默认值管理
- ✅ 并发安全访问
- ✅ 配置校验

### 配置流程

```
启动
  ↓
加载 ~/.neocode/config.yaml
  ↓
应用内置 defaults (来自 builtin_providers.go)
  ↓
验证配置完整性
  ↓
运行时使用
```

### 配置更新

当用户通过 TUI 切换 provider 或 model 时：

1. 更新内存中的配置
2. 立即持久化到 `config.yaml`
3. 下次启动自动恢复选择状态

## 扩展内建 Provider

如需添加新的内建 provider（包括企业内部服务），请参考：

👉 [adding-providers.md](./adding-providers.md)

### 快速步骤

**OpenAI 兼容服务**（推荐）：
1. 在 `internal/config/builtin_providers.go` 添加配置函数
2. 在 `DefaultProviders()` 中注册
3. 设置对应的环境变量

**自定义协议**：
1. 在 `internal/provider/yourprovider/` 实现驱动
2. 在 `internal/provider/builtin/builtin.go` 注册驱动
3. 在 `internal/config/builtin_providers.go` 添加配置

## 配置示例场景

### 场景一：使用 Gemini

```bash
# 1. 设置环境变量
export GEMINI_API_KEY="your-gemini-api-key"

# 2. 启动 NeoCode
go run ./cmd/neocode

# 3. 在 TUI 中切换
/provider  # 选择 gemini
/model     # 选择 gemini-2.5-flash
```

### 场景二：使用 OpenLL

```bash
# 1. 设置环境变量
export AI_API_KEY="your-openll-api-key"

# 2. 启动并切换
go run ./cmd/neocode
# 在 TUI 中: /provider → openll
```

### 场景三：自定义工作目录

```yaml
# ~/.neocode/config.yaml
selected_provider: openai
current_model: gpt-4.1
workdir: /Users/username/projects/myproject
shell: bash
max_loops: 10
```

## 故障排查

### 配置加载失败

**错误**：`config validation failed: providers is empty`

**解决**：
- 确保使用最新版本的代码
- 删除 `~/.neocode/config.yaml` 让系统重新生成

### API Key 未找到

**错误**：`environment variable OPENAI_API_KEY is empty`

**解决**：
```bash
# 检查环境变量
echo $OPENAI_API_KEY
```

### Provider 不存在

**错误**：`provider not found: xxx`

**解决**：
- 检查 provider 名称拼写
- 使用 `/provider` 命令查看所有可用 provider

## 相关文档

- [添加新 Provider](./adding-providers.md)

## Context Compact

以下配置用于控制手动上下文压缩行为。

### 配置示例

```yaml
context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 10
    max_summary_chars: 1200
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `context.compact.manual_strategy` | string | `keep_recent` | 手动 `/compact` 策略，可选 `keep_recent` / `full_replace` |
| `context.compact.manual_keep_recent_messages` | int | `10` | `keep_recent` 模式下保留最近 N 条消息；会按 tool call 与 tool result 的原子块整体保留 |
| `context.compact.max_summary_chars` | int | `1200` | compact summary 最大字符数 |

更多行为说明见 [context-compact.md](../context-compact.md)。
