# 配置指南

本文档说明 NeoCode 的配置策略和配置文件结构。

## Provider 策略

NeoCode 采用"内建 Provider 优先"的策略：

- 内建 provider 定义随代码版本发布
- `config.yaml` 不再持久化完整 `providers` 列表
- `config.yaml` 只保存当前选择状态和通用运行配置
- 运行时的 `providers` 完全来自代码内建定义
- API Key 只从环境变量读取，不写入 YAML
- 当前内建 provider 包括 `openai` 和 `gemini`
- `gemini` 复用 OpenAI-compatible driver，请求地址指向 Gemini 的兼容接口
- provider 实例自己定义 `base_url`、默认模型、可选模型列表和 `api_key_env`
- `base_url` 不在 TUI 中展示给用户
- driver 只负责协议构造与响应解析，不决定 `models`、`base_url` 或 `api_key_env`

### 设计理念

这意味着：

- 新用户启动后会自动拿到当前版本最新的内建 provider
- 未来代码新增 provider 时，新用户不需要修改 YAML
- 老配置文件中的 `providers` / `provider_overrides` 会在加载时被清理为新的最小状态格式

## 配置文件

### 默认路径

```
~/.neocode/config.yaml
```

### 配置结构示例

```yaml
selected_provider: openai
current_model: gpt-5.4
workdir: .
shell: powershell
max_loops: 8
tool_timeout_sec: 20
```

### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `selected_provider` | string | 当前选择的 provider 名称 |
| `current_model` | string | 当前选择的模型名称 |
| `workdir` | string | 工作目录路径 |
| `shell` | string | Shell 类型（如 `powershell`、`bash`） |
| `max_loops` | int | Agent 推理循环最大轮数 |
| `tool_timeout_sec` | int | 工具执行超时时间（秒） |

### 环境变量

每个 provider 对应的环境变量：

| Provider | 环境变量 | 说明 |
|----------|----------|------|
| `openai` | `OPENAI_API_KEY` | OpenAI API Key |
| `gemini` | `GEMINI_API_KEY` | Google Gemini API Key |

**安全提示**：
- API Key 仅从环境变量读取，永不写入配置文件
- 不要将 API Key 硬编码在代码中
- 建议使用 `.env` 文件或系统环境变量管理密钥

## Slash Commands

NeoCode 提供以下 slash 命令用于快速切换配置：

- `/provider` — 打开 provider 选择器
- `/model` — 打开当前 provider 的模型选择器

## 配置管理

配置管理由 `internal/config` 模块负责：

- YAML 加载与保存
- `.env` 文件集成
- 默认值管理
- 并发安全访问
- 配置校验

详细设计参见 [`config-management-detail-design.md`](../config-management-detail-design.md)。
