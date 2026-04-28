package provider

import "time"

// Driver 是 config/provider 间共享的稳定枚举，避免字面量分支漂移。
const (
	DriverOpenAICompat = "openaicompat"
	DriverGemini       = "gemini"
	DriverAnthropic    = "anthropic"

	DiscoveryEndpointPathModels = "/models"
)

const (
	// DefaultGenerateMaxRetries 定义生成链路默认额外重试次数，不含首次尝试。
	DefaultGenerateMaxRetries = 5
	// MaxGenerateMaxRetries 定义生成链路允许的额外重试次数上限，避免异常配置导致极长重试循环。
	MaxGenerateMaxRetries = 20
	// DefaultGenerateStartTimeout 定义生成链路等待首个有效 payload 的默认窗口。
	DefaultGenerateStartTimeout = 60 * time.Second
	// DefaultGenerateIdleTimeout 定义首包后默认的流空闲超时窗口。
	DefaultGenerateIdleTimeout = 5 * time.Minute
	// DefaultGenerateRetryBaseWait 定义生成链路重试退避的基础等待时长。
	DefaultGenerateRetryBaseWait = 1 * time.Second
	// DefaultGenerateRetryMaxWait 定义生成链路重试退避的最大等待时长。
	DefaultGenerateRetryMaxWait = 7 * time.Second
	// DefaultSDKRequestTimeout 定义非生成链路访问外部模型 SDK 的统一保底超时。
	DefaultSDKRequestTimeout = 10 * time.Minute
)

// NormalizeGenerateMaxRetries 归一化生成链路额外重试次数，负值回退到默认值。
func NormalizeGenerateMaxRetries(value int) int {
	if value < 0 {
		return DefaultGenerateMaxRetries
	}
	if value > MaxGenerateMaxRetries {
		return MaxGenerateMaxRetries
	}
	return value
}

// NormalizeGenerateStartTimeout 归一化生成链路首包超时，非正值回退到默认值。
func NormalizeGenerateStartTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return DefaultGenerateStartTimeout
	}
	return value
}

// NormalizeGenerateIdleTimeout 归一化生成链路空闲超时，非正值回退到默认值。
func NormalizeGenerateIdleTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return DefaultGenerateIdleTimeout
	}
	return value
}
