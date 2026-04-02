package config

const (
	OpenAIName             = "openai"
	OpenAIDefaultBaseURL   = "https://api.openai.com/v1"
	OpenAIDefaultModel     = "gpt-4.1"
	OpenAIDefaultAPIKeyEnv = "OPENAI_API_KEY"

	GeminiName             = "gemini"
	GeminiDefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta/openai"
	GeminiDefaultModel     = "gemini-2.5-flash"
	GeminiDefaultAPIKeyEnv = "GEMINI_API_KEY"

	OpenLLName             = "openll"
	OpenLLDefaultBaseURL   = "https://www.openll.top/v1"
	OpenLLDefaultModel     = "gpt-5.4"
	OpenLLDefaultAPIKeyEnv = "AI_API_KEY"

	QiniuName             = "qiniu"
	QiniuDefaultBaseURL   = "https://api.qnaigc.com/v1"
	QiniuDefaultModel     = "openai/gpt-5"
	QiniuDefaultAPIKeyEnv = "QINIU_API_KEY"
)

// OpenAIProvider returns the builtin OpenAI provider definition.
func OpenAIProvider() ProviderConfig {
	return ProviderConfig{
		Name:      OpenAIName,
		Driver:    "openai",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
	}
}

// GeminiProvider returns the builtin Gemini provider definition.
func GeminiProvider() ProviderConfig {
	return ProviderConfig{
		Name:      GeminiName,
		Driver:    "openai",
		BaseURL:   GeminiDefaultBaseURL,
		Model:     GeminiDefaultModel,
		APIKeyEnv: GeminiDefaultAPIKeyEnv,
	}
}

// OpenLLProvider returns the builtin OpenLL provider definition.
func OpenLLProvider() ProviderConfig {
	return ProviderConfig{
		Name:      OpenLLName,
		Driver:    "openai",
		BaseURL:   OpenLLDefaultBaseURL,
		Model:     OpenLLDefaultModel,
		APIKeyEnv: OpenLLDefaultAPIKeyEnv,
	}
}

// QiniuProvider returns the builtin Qiniu provider definition.
func QiniuProvider() ProviderConfig {
	return ProviderConfig{
		Name:      QiniuName,
		Driver:    "openai",
		BaseURL:   QiniuDefaultBaseURL,
		Model:     QiniuDefaultModel,
		APIKeyEnv: QiniuDefaultAPIKeyEnv,
	}
}

// DefaultProviders returns all builtin provider definitions.
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		OpenLLProvider(),
		QiniuProvider(),
	}
}
