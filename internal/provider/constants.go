package provider

import "time"

// Driver 与 OpenAI-compatible 协议常量用于在 config/provider 间共享稳定枚举值，避免字面量漂移。
const (
	DriverOpenAICompat = "openaicompat"
	DriverGemini       = "gemini"
	DriverAnthropic    = "anthropic"

	DiscoveryEndpointPathModels = "/models"
)

// DefaultSDKRequestTimeout 定义 provider 层对外部模型 SDK 请求的统一超时，避免流式请求无限悬挂。
const DefaultSDKRequestTimeout = 10 * time.Minute
