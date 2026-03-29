# NeoCode

NeoCode 是一个基于 Go 和 Bubble Tea 的本地编码 Agent。它在终端中运行 ReAct 闭环，能够对话、调用工具、持久化会话，并以流式方式展示模型输出。

## 当前 Provider 策略

- 内建 provider 定义随代码版本发布。
- `config.yaml` 不再持久化完整 `providers` 列表。
- `config.yaml` 只保存当前选择状态和通用运行配置。
- 运行时的 `providers` 完全来自代码内建定义。
- API Key 只从环境变量读取，不写入 YAML。
- 当前内建 provider 包括 `openai` 和 `gemini`。
- `gemini` 复用 OpenAI-compatible driver，请求地址指向 Gemini 的兼容接口。
- provider 实例自己定义 `base_url`、默认模型、可选模型列表和 `api_key_env`。
- `base_url` 不在 TUI 中展示给用户。
- driver 只负责协议构造与响应解析，不决定 `models`、`base_url` 或 `api_key_env`。

这意味着：

- 新用户启动后会自动拿到当前版本最新的内建 provider。
- 未来代码新增 provider 时，新用户不需要修改 YAML。
- 老配置文件中的 `providers` / `provider_overrides` 会在加载时被清理为新的最小状态格式。

## 配置文件

默认路径：
[`~/.neocode/config.yaml`](~/.neocode/config.yaml)

当前落盘结构示例：

```yaml
selected_provider: openai
current_model: gpt-5.4
workdir: .
shell: powershell
max_loops: 8
tool_timeout_sec: 20
```

其中：

- `selected_provider` 和 `current_model` 是用户当前选择。
- provider 的 `base_url`、`models`、`api_key_env` 和 `driver` 都由开发者在代码中预设。
- `openai` 默认读取 `OPENAI_API_KEY`，`gemini` 默认读取 `GEMINI_API_KEY`。
- 完整 provider 列表不落盘，用户不需要在 YAML 中维护供应商元数据。

## Slash Commands

- `/provider`：打开 provider 选择器。
- `/model`：打开当前 provider 的模型选择器。

## 运行

```bash
go run ./cmd/neocode
```

## 开发

```bash
gofmt -w ./cmd ./internal
go test ./...
```

## 架构概览
- `internal/config`：负责 YAML 加载、`.env` 集成、默认值管理和并发安全更新。
- `internal/provider`：将厂商特定的请求和流式响应抹平成统一领域模型。
- `internal/runtime`：负责事件总线、ReAct loop、Provider 动态构建和会话持久化。
- `internal/tools`：提供工具注册表以及各类具体工具实现。
- `internal/tui`：负责终端交互体验，以及 runtime 事件到 Bubble Tea 消息的桥接。

## 目录结构
```text
.
|-- cmd/neocode
|-- docs
|-- internal/app
|-- internal/config
|-- internal/provider
|-- internal/runtime
|-- internal/tools
`-- internal/tui
```

## 如何增加 Provider

NeoCode 的 provider 分为两层：**配置层**（`ProviderConfig`）和 **驱动层**（`DriverDefinition`）。驱动负责实际的 API 协议差异，配置层提供 `base_url`、模型列表、API Key 环境变量等元数据。一个驱动可被多个 provider 复用（如 `gemini` 和 `openll` 都复用 `openai` 驱动）。

### 方式一：复用已有驱动（推荐，适用于 OpenAI 兼容接口）

如果新 provider 的 API 与 OpenAI Chat Completions 协议兼容，只需添加一个纯配置文件，无需编写新的驱动代码。

以添加 `deepseek` 为例：

**1. 创建 `internal/provider/deepseek/deepseek.go`：**

```go
package deepseek

import (
    "neo-code/internal/config"
)

const (
    Name             = "deepseek"
    DriverName       = "openai"                                // 复用 openai 驱动
    DefaultBaseURL   = "https://api.deepseek.com/v1"           // DeepSeek 的 OpenAI 兼容端点
    DefaultModel     = "deepseek-chat"
    DefaultAPIKeyEnv = "DEEPSEEK_API_KEY"                      //对应的环境变量名
)

var builtinModels = []string{
    DefaultModel,
    "deepseek-coder",
}

// BuiltinConfig 返回该 provider 的内建配置。
func BuiltinConfig() config.ProviderConfig {
    return config.ProviderConfig{
        Name:      Name,
        Driver:    DriverName,
        BaseURL:   DefaultBaseURL,
        Model:     DefaultModel,
        Models:    append([]string(nil), builtinModels...),
        APIKeyEnv: DefaultAPIKeyEnv,
    }
}
```

**2. 在builtin.go 中添加该 provider：**
```go
import "neo-code/internal/provider/deepseek"

func DefaultConfig() *config.Config {
    cfg := config.Default()
    defaultProvider := openai.BuiltinConfig()
    cfg.Providers = []config.ProviderConfig{
        defaultProvider,
        gemini.BuiltinConfig(),
        openll.BuiltinConfig(),
        deepseek.BuiltinConfig(),  // 新增
    }
    // ...
}
```


### 方式二：实现新驱动（适用于协议不兼容的厂商）

如果目标厂商的 API 协议与 OpenAI 不兼容（如 Anthropic、Google 原生 API），需要实现完整的驱动。

以添加 `anthropic` 为例：

**1. 在 `internal/provider/anthropic/anthropic.go` 中实现核心类型：**

```go
package anthropic

import (
    "context"
    "neo-code/internal/config"
    domain "neo-code/internal/provider"
)

const (
    Name             = "anthropic"
    DriverName       = "anthropic"
    DefaultBaseURL   = "https://api.anthropic.com/v1"
    DefaultModel     = "claude-sonnet-4-20250514"
    DefaultAPIKeyEnv = "ANTHROPIC_API_KEY"
)

type Provider struct {
    cfg    config.ResolvedProviderConfig
    client *http.Client
}

// New 构造函数，接收已解析的配置。
func New(cfg config.ResolvedProviderConfig) (*Provider, error) {
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("anthropic provider: %w", err)
    }
    // ...
    return &Provider{cfg: cfg, client: &http.Client{}}, nil
}

// Chat 实现 domain.Provider 接口。
// 必须支持流式输出（通过 events channel）和 tool calls。
func (p *Provider) Chat(ctx context.Context, req domain.ChatRequest, events chan<- domain.StreamEvent) (domain.ChatResponse, error) {
    // 1. 将 domain.ChatRequest 转换为厂商特定的请求格式
    // 2. 调用厂商 API（流式 SSE）
    // 3. 解析响应，推送 StreamEventTextDelta / StreamEventToolCallStart
    // 4. 返回 domain.ChatResponse
}

// Driver 返回驱动定义，供 Registry 注册使用。
func Driver() domain.DriverDefinition {
    return domain.DriverDefinition{
        Name: Name,
        Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (domain.Provider, error) {
            return New(cfg)
        },
    }
}

// BuiltinConfig 返回内建配置。
func BuiltinConfig() config.ProviderConfig {
    return config.ProviderConfig{
        Name:      Name,
        Driver:    DriverName,
        BaseURL:   DefaultBaseURL,
        Model:     DefaultModel,
        Models:    []string{DefaultModel, "claude-opus-4-20250514"},
        APIKeyEnv: DefaultAPIKeyEnv,
    }
}
```

**2. 在 `internal/provider/builtin/builtin.go` 中同时注册驱动和配置：**

```go
import "neo-code/internal/provider/anthropic"

func Register(registry *provider.Registry) error {
    if registry == nil {
        return errors.New("builtin provider registry is nil")
    }
    if err := registry.Register(openai.Driver()); err != nil {
        return err
    }
    return registry.Register(anthropic.Driver())  // 新增驱动注册
}
```

### 关键接口与类型速查

| 类型 | 位置 | 说明 |
|---|---|---|
| `Provider` 接口 | `internal/provider/provider.go` | 只有一个 `Chat` 方法，签名：`Chat(ctx, ChatRequest, chan<- StreamEvent) (ChatResponse, error)` |
| `ChatRequest` | `internal/provider/types.go` | 包含 `Model`、`SystemPrompt`、`Messages`、`Tools` |
| `ChatResponse` | `internal/provider/types.go` | 包含 `Message`、`FinishReason`、`Usage` |
| `StreamEvent` | `internal/provider/provider.go` | 流式事件，支持 `text_delta` 和 `tool_call_start` 两种类型 |
| `ProviderConfig` | `internal/config/model.go` | 配置层：`Name`、`Driver`、`BaseURL`、`Model`、`Models`、`APIKeyEnv` |
| `DriverDefinition` | `internal/provider/registry.go` | 驱动层：`Name` + `Build` 构造函数 |
| `Registry` | `internal/provider/registry.go` | 驱动注册中心，通过 `Register` 注册、`Build` 构建实例 |

### 设计约束

- **API Key 只从环境变量读取**，不写入 `config.yaml`，不硬编码在源码中。
- **驱动层不持有模型列表**，`Models` 完全由配置层（`ProviderConfig.Models`）控制。
- **厂商差异收敛在 `internal/provider/` 内**，`runtime`、`tui` 等上层模块只依赖统一的 `Provider` 接口和领域类型。
- **`base_url` 不向用户暴露**，用户在 TUI 中只能看到 provider 名称和模型列表。

## 当前状态
NeoCode 目前聚焦于 MVP 闭环：本地对话、工具调用、Session 持久化和终端交互体验。当前版本正在继续向"高质量开源项目"标准收敛，重点补强文档、测试覆盖率和工具能力。
