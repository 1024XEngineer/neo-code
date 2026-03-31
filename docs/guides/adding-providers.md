# 扩展 Provider

本文档说明如何为 NeoCode 添加新的 Provider。

## 架构概览

NeoCode 的 provider 架构采用**集中式配置 + 驱动复用**的设计：

### 两层分离

- **配置层**（`internal/config/builtin_providers.go`）：集中管理所有 provider 的元数据
  - Provider 名称、Driver 名称
  - Base URL、默认模型、模型列表
  - API Key 环境变量名

- **驱动层**（`internal/provider/openai/` 等）：负责实际的 API 协议实现
  - 请求构造、响应解析
  - 流式输出、Tool Call 处理

### 驱动复用

一个驱动可被多个 provider 复用。当前所有内置 provider 都复用 `openai` 驱动：

```go
// internal/config/builtin_providers.go
OpenAIProvider()   // Driver: "openai"
GeminiProvider()   // Driver: "openai" (OpenAI-compatible API)
OpenLLProvider()   // Driver: "openai" (OpenAI-compatible API)
```

## 方式一：添加 OpenAI 兼容 Provider（推荐）

适用于 OpenAI 兼容接口（大多数第三方服务）。只需在配置层添加，无需编写新驱动。

### 步骤：添加 DeepSeek

**1. 在 `internal/config/builtin_providers.go` 添加常量和配置：**

```go
const (
    // ... 现有常量 ...

    DeepSeekName             = "deepseek"
    DeepSeekDefaultBaseURL   = "https://api.deepseek.com/v1"
    DeepSeekDefaultModel     = "deepseek-chat"
    DeepSeekDefaultAPIKeyEnv = "DEEPSEEK_API_KEY"
)

var deepSeekModels = []string{
    DeepSeekDefaultModel,
    "deepseek-coder",
}

// DeepSeekProvider 返回 DeepSeek provider 的默认配置。
func DeepSeekProvider() ProviderConfig {
    return ProviderConfig{
        Name:      DeepSeekName,
        Driver:    "openai",                    // 复用 openai 驱动
        BaseURL:   DeepSeekDefaultBaseURL,
        Model:     DeepSeekDefaultModel,
        Models:    append([]string(nil), deepSeekModels...),
        APIKeyEnv: DeepSeekDefaultAPIKeyEnv,
    }
}
```

**2. 在 `DefaultProviders()` 中注册：**

```go
func DefaultProviders() []ProviderConfig {
    return []ProviderConfig{
        OpenAIProvider(),
        GeminiProvider(),
        OpenLLProvider(),
        DeepSeekProvider(),  // 新增
    }
}
```

**3. 设置环境变量并测试：**

```bash
export DEEPSEEK_API_KEY="your-api-key"
go run ./cmd/neocode
```

### 优点

- ✅ 无需编写新代码，只需配置
- ✅ 自动继承 openai 驱动的所有功能（流式、Tool Call）
- ✅ 配置集中管理，易于维护
- ✅ 用户无需修改 YAML 文件

## 方式二：实现新驱动

适用于协议不兼容的厂商（如 Anthropic、Google 原生 API）。

### 步骤：添加 Anthropic

**1. 在 `internal/provider/anthropic/anthropic.go` 实现驱动：**

```go
package anthropic

import (
    "context"
    "net/http"
    
    "neo-code/internal/config"
    domain "neo-code/internal/provider"
)

const (
    Name           = "anthropic"
    DefaultBaseURL = "https://api.anthropic.com/v1"
)

// Provider 实现 domain.Provider 接口
type Provider struct {
    cfg    config.ResolvedProviderConfig
    client *http.Client
}

// New 构造函数
func New(cfg config.ResolvedProviderConfig) (*Provider, error) {
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("anthropic provider: %w", err)
    }
    return &Provider{
        cfg:    cfg,
        client: &http.Client{Timeout: 60 * time.Second},
    }, nil
}

// Chat 实现流式对话接口
func (p *Provider) Chat(ctx context.Context, req domain.ChatRequest, events chan<- domain.StreamEvent) (domain.ChatResponse, error) {
    // 1. 将 domain.ChatRequest 转换为 Anthropic API 格式
    // 2. 调用 Anthropic API（流式 SSE）
    // 3. 解析响应，推送 StreamEventTextDelta / StreamEventToolCallStart
    // 4. 返回 domain.ChatResponse
}

// Driver 返回驱动定义
func Driver() domain.DriverDefinition {
    return domain.DriverDefinition{
        Name: Name,
        Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (domain.Provider, error) {
            return New(cfg)
        },
    }
}
```

**2. 在 `internal/provider/builtin/builtin.go` 注册驱动：**

```go
import (
    "neo-code/internal/provider/anthropic"
    "neo-code/internal/provider/openai"
)

func Register(registry *provider.Registry) error {
    if registry == nil {
        return errors.New("builtin provider registry is nil")
    }
    if err := registry.Register(openai.Driver()); err != nil {
        return err
    }
    return registry.Register(anthropic.Driver())  // 新增
}
```

**3. 在 `internal/config/builtin_providers.go` 添加配置：**

```go
const (
    // ... 现有常量 ...

    AnthropicName             = "anthropic"
    AnthropicDefaultBaseURL   = "https://api.anthropic.com/v1"
    AnthropicDefaultModel     = "claude-sonnet-4-20250514"
    AnthropicDefaultAPIKeyEnv = "ANTHROPIC_API_KEY"
)

var anthropicModels = []string{
    AnthropicDefaultModel,
    "claude-opus-4-20250514",
    "claude-3-5-sonnet-20241022",
}

func AnthropicProvider() ProviderConfig {
    return ProviderConfig{
        Name:      AnthropicName,
        Driver:    "anthropic",               // 使用新的 anthropic 驱动
        BaseURL:   AnthropicDefaultBaseURL,
        Model:     AnthropicDefaultModel,
        Models:    append([]string(nil), anthropicModels...),
        APIKeyEnv: AnthropicDefaultAPIKeyEnv,
    }
}

func DefaultProviders() []ProviderConfig {
    return []ProviderConfig{
        OpenAIProvider(),
        GeminiProvider(),
        OpenLLProvider(),
        AnthropicProvider(),  // 新增
    }
}
```

## 关键接口与类型

### 核心接口

| 类型 | 位置 | 说明 |
|------|------|------|
| `Provider` | `internal/provider/types.go` | 核心接口，定义 `Chat` 方法 |
| `DriverDefinition` | `internal/provider/registry.go` | 驱动定义：`Name` + `Build` 构造函数 |
| `Registry` | `internal/provider/registry.go` | 驱动注册中心 |

### 数据结构

| 类型 | 位置 | 说明 |
|------|------|------|
| `ChatRequest` | `internal/provider/types.go` | 请求：`Model`、`SystemPrompt`、`Messages`、`Tools` |
| `ChatResponse` | `internal/provider/types.go` | 响应：`Message`、`FinishReason`、`Usage` |
| `StreamEvent` | `internal/provider/types.go` | 流式事件：`TextDelta`、`ToolCallStart` |
| `ProviderConfig` | `internal/config/model.go` | 配置：`Name`、`Driver`、`BaseURL`、`Model`、`Models`、`APIKeyEnv` |

## 设计约束

### 必须遵守

✅ **配置集中管理**
- 所有内置 provider 配置统一在 `internal/config/builtin_providers.go`
- 不再为每个 provider 创建独立的包

✅ **API Key 安全**
- 只从环境变量读取，不写入 `config.yaml`
- 不硬编码在源码中

✅ **驱动职责清晰**
- 驱动只负责协议构造与响应解析
- 不持有模型列表、base_url 等配置信息

✅ **架构分层**
- 厂商差异收敛在 `internal/provider/` 内
- `runtime`、`tui` 等上层模块只依赖统一的 `Provider` 接口
- `base_url` 不在 TUI 中展示给用户

### 最佳实践

1. **优先复用现有驱动**
   - 大多数 OpenAI 兼容服务无需编写新驱动
   - 只需在配置层添加即可

2. **配置即代码**
   - provider 配置随代码版本发布
   - 用户无需手动配置 providers 列表

3. **测试覆盖**
   - 新驱动必须添加完整的单元测试
   - 使用 `httptest.NewServer` 模拟 HTTP 调用
   - 不使用真实 API Key

## 示例：当前内置 Provider

```go
// internal/config/builtin_providers.go

func DefaultProviders() []ProviderConfig {
    return []ProviderConfig{
        OpenAIProvider(),  // OpenAI 官方 API
        GeminiProvider(),  // Google Gemini (OpenAI-compatible)
        OpenLLProvider(),  // OpenLL 服务 (OpenAI-compatible)
    }
}
```

所有内置 provider 都复用 `openai` 驱动，配置集中在 `builtin_providers.go`。

