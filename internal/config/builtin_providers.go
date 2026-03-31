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

var openAIModels = []string{
	OpenAIDefaultModel,
	"gpt-4o",
	"gpt-5.4",
	"gpt-5.3-codex",
}

var geminiModels = []string{
	GeminiDefaultModel,
	"gemini-2.5-pro",
	"gemini-2.0-flash",
}

var openLLModels = []string{
	OpenLLDefaultModel,
	"gpt-5.3-codex",
	"gpt-5.3-turbo",
}

var qiniuModels = []string{
	QiniuDefaultModel,
	"openai/gpt-5.2-codex",
}

// OpenAIProvider 返回 OpenAI provider 的默认配置。
func OpenAIProvider() ProviderConfig {
	return ProviderConfig{
		Name:      OpenAIName,
		Driver:    "openai",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		Models:    append([]string(nil), openAIModels...),
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
	}
}

// GeminiProvider 返回 Gemini provider 的默认配置。
func GeminiProvider() ProviderConfig {
	return ProviderConfig{
		Name:      GeminiName,
		Driver:    "openai",
		BaseURL:   GeminiDefaultBaseURL,
		Model:     GeminiDefaultModel,
		Models:    append([]string(nil), geminiModels...),
		APIKeyEnv: GeminiDefaultAPIKeyEnv,
	}
}

// OpenLLProvider 返回 OpenLL provider 的默认配置。
func OpenLLProvider() ProviderConfig {
	return ProviderConfig{
		Name:      OpenLLName,
		Driver:    "openai",
		BaseURL:   OpenLLDefaultBaseURL,
		Model:     OpenLLDefaultModel,
		Models:    append([]string(nil), openLLModels...),
		APIKeyEnv: OpenLLDefaultAPIKeyEnv,
	}
}

func QiniuProvider() ProviderConfig {
	return ProviderConfig{
		Name:      QiniuName,
		Driver:    "openai",
		BaseURL:   QiniuDefaultBaseURL,
		Model:     QiniuDefaultModel,
		Models:    append([]string(nil), qiniuModels...),
		APIKeyEnv: QiniuDefaultAPIKeyEnv,
	}
}

// DefaultProviders 返回所有内置 provider 配置列表。
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		OpenLLProvider(),
		QiniuProvider(),
	}
}
