package provider

// Driver 与 OpenAI-compatible 协议常量用于在 config/provider 间共享稳定枚举值，避免字面量漂移。
const (
	DriverOpenAICompat = "openaicompat"
	DriverGemini       = "gemini"
	DriverAnthropic    = "anthropic"

	ChatProtocolOpenAIChatCompletions = "openai_chat_completions"
	ChatProtocolOpenAIResponses       = "openai_responses"
	ChatProtocolGeminiNative          = "gemini_native"
	ChatProtocolAnthropicMessages     = "anthropic_messages"

	DiscoveryProtocolOpenAIModels    = "openai_models"
	DiscoveryProtocolGeminiModels    = "gemini_models"
	DiscoveryProtocolAnthropicModels = "anthropic_models"
	DiscoveryProtocolCustomHTTPJSON  = "custom_http_json"

	AuthStrategyBearer    = "bearer"
	AuthStrategyXAPIKey   = "x_api_key"
	AuthStrategyAnthropic = "anthropic"

	OpenAICompatibleAPIStyleChatCompletions = "chat_completions"
	OpenAICompatibleAPIStyleResponses       = "responses"

	DiscoveryEndpointPathModels = "/models"

	DiscoveryResponseProfileOpenAI  = "openai"
	DiscoveryResponseProfileGemini  = "gemini"
	DiscoveryResponseProfileGeneric = "generic"

	ModelSourceDiscover = "discover"
	ModelSourceManual   = "manual"
)
