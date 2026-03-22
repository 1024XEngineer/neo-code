package provider

import (
	"fmt"
	"strings"

	"go-llm-demo/configs"
)

const modelScopeProviderName = "modelscope"

type ProviderSpec struct {
	Name            string
	BaseURL         string
	HasModelCatalog bool
}

var providerSpecs = []ProviderSpec{
	{Name: modelScopeProviderName, BaseURL: "", HasModelCatalog: true},
	{Name: "deepseek", BaseURL: "https://api.deepseek.com/chat/completions", HasModelCatalog: false},
	{Name: "siliconflow", BaseURL: "https://api.siliconflow.cn/v1/chat/completions", HasModelCatalog: false},
	{Name: "豆包大模型", BaseURL: "https://ark.cn-beijing.volces.com/api/v3/chat/completions", HasModelCatalog: false},
	{Name: "openai", BaseURL: "https://api.openai.com/v1/chat/completions", HasModelCatalog: false},
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

func ProviderSupportsModelCatalog(name string) bool {
	spec, ok := providerSpecByName(name)
	return ok && spec.HasModelCatalog
}

func SupportedModels() []string {
	return SupportedModelsForConfig(configs.GlobalAppConfig)
}

func SupportedModelsForConfig(cfg *configs.AppConfiguration) []string {
	providerName := providerNameFromConfig(cfg)
	if !ProviderSupportsModelCatalog(providerName) {
		return nil
	}

	if cfg != nil && len(cfg.Models.Chat.Models) > 0 {
		models := make([]string, 0, len(cfg.Models.Chat.Models))
		for _, model := range cfg.Models.Chat.Models {
			if strings.TrimSpace(model.Name) != "" {
				models = append(models, model.Name)
			}
		}
		if len(models) > 0 {
			return models
		}
	}

	models := make([]string, len(fallbackSupportedModels))
	copy(models, fallbackSupportedModels)
	return models
}

func DefaultModel() string {
	return DefaultModelForConfig(configs.GlobalAppConfig)
}

func DefaultModelForConfig(cfg *configs.AppConfiguration) string {
	providerName := providerNameFromConfig(cfg)
	if cfg != nil {
		if model := strings.TrimSpace(cfg.AI.Model); model != "" {
			if !ProviderSupportsModelCatalog(providerName) || IsSupportedModelForConfig(cfg, model) {
				return model
			}
		}
		if ProviderSupportsModelCatalog(providerName) {
			if model := strings.TrimSpace(cfg.Models.Chat.DefaultModel); model != "" {
				return model
			}
		}
	}

	supported := SupportedModelsForConfig(cfg)
	if len(supported) > 0 {
		return supported[0]
	}
	return ""
}

func IsSupportedModel(model string) bool {
	return IsSupportedModelForConfig(configs.GlobalAppConfig, model)
}

func IsSupportedModelForConfig(cfg *configs.AppConfiguration, model string) bool {
	target := strings.TrimSpace(model)
	if target == "" {
		return false
	}
	if !ProviderSupportsModelCatalog(providerNameFromConfig(cfg)) {
		return true
	}
	for _, m := range SupportedModelsForConfig(cfg) {
		if m == target {
			return true
		}
	}
	return false
}

func CurrentProvider() string {
	return providerNameFromConfig(configs.GlobalAppConfig)
}

func ResolveChatEndpoint(cfg *configs.AppConfiguration, model string) (string, error) {
	providerName := providerNameFromConfig(cfg)
	if ProviderSupportsModelCatalog(providerName) {
		baseURL, ok := configs.GetChatModelURLFromConfig(cfg, model)
		if !ok || strings.TrimSpace(baseURL) == "" {
			return "", fmt.Errorf("chat model URL is not configured for %q", model)
		}
		return baseURL, nil
	}

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
