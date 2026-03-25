package provider

import (
	"fmt"
	"strings"

	"go-llm-demo/configs"
)

const modelScopeProviderName = "modelscope"

type ProviderSpec struct {
	Name         string
	BaseURL      string
	DefaultModel string
}

var providerSpecs = []ProviderSpec{
	{Name: modelScopeProviderName, BaseURL: "https://api-inference.modelscope.cn/v1/chat/completions", DefaultModel: "Qwen/Qwen3-Coder-480B-A35B-Instruct"},
	{Name: "deepseek", BaseURL: "https://api.deepseek.com/chat/completions", DefaultModel: "deepseek-chat"},
	{Name: "openll", BaseURL: "https://www.openll.top/v1/chat/completions", DefaultModel: "gpt-5.4"},
	{Name: "siliconflow", BaseURL: "https://api.siliconflow.cn/v1/chat/completions", DefaultModel: "zai-org/GLM-4.6"},
	{Name: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3/chat/completions", DefaultModel: "doubao-pro-v1"},
	{Name: "openai", BaseURL: "https://api.openai.com/v1/chat/completions", DefaultModel: "gpt-5.4"},
}

var providerIndex = func() map[string]ProviderSpec {
	index := make(map[string]ProviderSpec, len(providerSpecs))
	for _, spec := range providerSpecs {
		index[strings.ToLower(spec.Name)] = spec
	}
	return index
}()

func NormalizeProviderName(name string) (string, bool) {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		return "", false
	}
	spec, ok := providerIndex[strings.ToLower(normalized)]
	if !ok {
		return "", false
	}
	return spec.Name, true
}

func SupportedProviders() []string {
	providers := make([]string, 0, len(providerSpecs))
	for _, spec := range providerSpecs {
		providers = append(providers, spec.Name)
	}
	return providers
}

func DefaultModel() string {
	return DefaultModelForConfig(configs.GlobalAppConfig)
}

func DefaultModelForConfig(cfg *configs.AppConfiguration) string {
	providerName := providerNameFromConfig(cfg)
	if cfg != nil {
		if model := strings.TrimSpace(cfg.AI.Model); model != "" {
			return model
		}
	}
	if spec, ok := providerSpecByName(providerName); ok {
		return strings.TrimSpace(spec.DefaultModel)
	}
	return ""
}

func DefaultModelForProvider(name string) string {
	spec, ok := providerSpecByName(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(spec.DefaultModel)
}

func CurrentProvider() string {
	return providerNameFromConfig(configs.GlobalAppConfig)
}

func ResolveChatEndpoint(cfg *configs.AppConfiguration, model string) (string, error) {
	_ = model
	providerName := providerNameFromConfig(cfg)
	spec, ok := providerSpecByName(providerName)
	if !ok {
		return "", fmt.Errorf("unsupported ai.provider: %s", providerName)
	}
	if strings.TrimSpace(spec.BaseURL) == "" {
		return "", fmt.Errorf("provider %q does not have a configured chat URL", providerName)
	}
	return spec.BaseURL, nil
}

func providerNameFromConfig(cfg *configs.AppConfiguration) string {
	if cfg != nil {
		trimmed := strings.TrimSpace(cfg.AI.Provider)
		if trimmed == "" {
			return modelScopeProviderName
		}
		if normalized, ok := NormalizeProviderName(cfg.AI.Provider); ok {
			return normalized
		}
		return trimmed
	}
	return modelScopeProviderName
}

func providerSpecByName(name string) (ProviderSpec, bool) {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return ProviderSpec{}, false
	}
	spec, ok := providerIndex[strings.ToLower(normalized)]
	return spec, ok
}
