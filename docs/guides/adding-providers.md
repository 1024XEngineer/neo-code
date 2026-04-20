# 扩展 Provider

本文档说明在当前架构下如何为 NeoCode 添加/扩展 Provider。

## 架构与边界

当前主链路：`runtime -> provider -> 协议实现 / SDK 实现`。

- `internal/config` 负责 provider 配置装配与校验。
- `internal/provider` 根包只保留最小公共契约（`RuntimeConfig`、`ProviderIdentity`、错误分类与少量 helper）。
- `internal/provider/openaicompat` 负责 OpenAI-compatible 协议细节（含 `chat/completions` 与 `responses`）。
- `internal/provider/gemini`、`internal/provider/anthropic` 是基于官方 SDK 的薄适配器。
- `runtime` 只消费统一流事件，不感知厂商协议细节。

## 方式一：新增 OpenAI-compatible Provider（推荐）

适用于“接口兼容 OpenAI”的网关或模型服务。

### 1. 在 `internal/config/provider.go` 增加内置配置

```go
const (
	DeepSeekName             = "deepseek"
	DeepSeekDefaultBaseURL   = "https://api.deepseek.com/v1"
	DeepSeekDefaultModel     = "deepseek-chat"
	DeepSeekDefaultAPIKeyEnv = "DEEPSEEK_API_KEY"
)

func DeepSeekProvider() ProviderConfig {
	return ProviderConfig{
		Name:                  DeepSeekName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               DeepSeekDefaultBaseURL,
		Model:                 DeepSeekDefaultModel,
		APIKeyEnv:             DeepSeekDefaultAPIKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
	}
}
```

### 2. 在 `DefaultProviders()` 注册

```go
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		OpenLLProvider(),
		QiniuProvider(),
		DeepSeekProvider(),
	}
}
```

### 3. 配置环境变量并验证

```bash
export DEEPSEEK_API_KEY="your-api-key"
go run ./cmd/neocode
```

## 方式二：实现新 Driver

适用于协议与现有三类驱动都不兼容的厂商。

### 1. 新建 `internal/provider/<driver>/`

实现至少以下内容：

- `driver.go`：暴露 `Driver()`，返回 `provider.DriverDefinition`
- `provider.go`：`New(cfg provider.RuntimeConfig)` 和 `Generate(...)`
-（可选）`request.go` / `stream.go` / `discovery_*.go`

### 2. 在 provider 注册处接入

将新 `DriverDefinition` 注册到 provider registry（沿用现有注册入口模式）。

### 3. 在 `internal/config/provider.go` 增加内置 provider（如果需要）

`Driver` 指向新驱动名，并补默认模型与 `api_key_env`。

## 自定义 provider.yaml（外部接入）

路径：`~/.neocode/providers/<name>/provider.yaml`

示例（OpenAI-compatible + responses 直连）：

```yaml
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1/text/chatcompletion_v2
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: discover
chat_api_mode: responses
chat_endpoint_path: /
discovery_endpoint_path: /models
```

说明：

- `chat_api_mode` 仅 `openaicompat` 生效，可选值：`chat_completions` / `responses`。
- `chat_endpoint_path` 为 `/` 表示直连 `base_url`；为空时会按 `chat_api_mode` 自动回填默认子路径（`/chat/completions` 或 `/responses`）。
- `model_source: manual` 时必须提供 `models`，且会忽略 `discovery_endpoint_path`。

## 测试要求

新增或修改 provider 后，至少执行：

```bash
go test ./internal/provider/...
go test ./internal/config/...
go test ./internal/runtime/...
```

涉及协议变更时，优先补：

- 请求组装与响应解析
- tool call 解析
- 流式事件映射
- discovery 错误分类与边界
