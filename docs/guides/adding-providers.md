# 扩展 Provider

本文档说明如何为 NeoCode 添加新的 Provider。

## 架构概览

NeoCode 的 provider 分为两层：

- **配置层**（`ProviderConfig`）：提供 `base_url`、模型列表、API Key 环境变量等元数据
- **驱动层**（`DriverDefinition`）：负责实际的 API 协议差异

一个驱动可被多个 provider 复用（如 `gemini` 和 `openll` 都复用 `openai` 驱动）。

## 方式一：复用已有驱动（推荐）

适用于 OpenAI 兼容接口。只需添加配置文件，无需编写新的驱动代码。

### 示例：添加 DeepSeek

**1. 创建 `internal/provider/deepseek/deepseek.go`：**

```go
package deepseek

import (
    "neo-code/internal/config"
)

const (
    Name             = "deepseek"
    DriverName       = "openai"                      // 复用 openai 驱动
    DefaultBaseURL   = "https://api.deepseek.com/v1"
    DefaultModel     = "deepseek-chat"
    DefaultAPIKeyEnv = "DEEPSEEK_API_KEY"
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

**2. 在 `internal/provider/builtin/builtin.go` 中添加该 provider：**

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

## 方式二：实现新驱动

适用于协议不兼容的厂商（如 Anthropic、Google 原生 API）。

### 示例：添加 Anthropic

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

**2. 在 `internal/provider/builtin/builtin.go` 中注册驱动：**

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

## 关键接口与类型

| 类型 | 位置 | 说明 |
|------|------|------|
| `Provider` 接口 | `internal/provider/provider.go` | 核心接口，定义 `Chat` 方法 |
| `ChatRequest` | `internal/provider/types.go` | 请求结构：`Model`、`SystemPrompt`、`Messages`、`Tools` |
| `ChatResponse` | `internal/provider/types.go` | 响应结构：`Message`、`FinishReason`、`Usage` |
| `StreamEvent` | `internal/provider/provider.go` | 流式事件：`text_delta` 和 `tool_call_start` |
| `ProviderConfig` | `internal/config/model.go` | 配置层：`Name`、`Driver`、`BaseURL`、`Model`、`Models`、`APIKeyEnv` |
| `DriverDefinition` | `internal/provider/registry.go` | 驱动层：`Name` + `Build` 构造函数 |
| `Registry` | `internal/provider/registry.go` | 驱动注册中心 |

## 设计约束

**必须遵守**：

- **API Key 只从环境变量读取**，不写入 `config.yaml`，不硬编码在源码中
- **驱动层不持有模型列表**，`Models` 完全由配置层（`ProviderConfig.Models`）控制
- **厂商差异收敛在 `internal/provider/` 内**，`runtime`、`tui` 等上层模块只依赖统一的 `Provider` 接口
- **`base_url` 不向用户暴露**，用户在 TUI 中只能看到 provider 名称和模型列表

## 相关文档

- [Provider Interface Target Design](../provider-interface-target-design.md)
- [Provider Module Interface](../provider-module-interface.md)
- [Provider Schema Strategy](../provider-schema-strategy.md)
