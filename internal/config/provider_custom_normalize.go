package config

import (
	"fmt"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// NormalizeCustomProviderInput 统一归一化 custom provider 输入，并执行模型来源与 discovery 配置的组合校验。
func NormalizeCustomProviderInput(input SaveCustomProviderInput) (SaveCustomProviderInput, error) {
	normalized := SaveCustomProviderInput{
		Name:                   strings.TrimSpace(input.Name),
		Driver:                 normalizeProviderDriver(strings.TrimSpace(input.Driver)),
		BaseURL:                strings.TrimSpace(input.BaseURL),
		ChatAPIMode:            strings.TrimSpace(input.ChatAPIMode),
		ChatEndpointPath:       strings.TrimSpace(input.ChatEndpointPath),
		APIKeyEnv:              strings.TrimSpace(input.APIKeyEnv),
		GenerateMaxRetries:     normalizeOptionalGenerateInt(input.GenerateMaxRetries),
		GenerateMaxRetriesSet:  input.GenerateMaxRetriesSet || input.GenerateMaxRetries > 0,
		GenerateIdleTimeoutSec: normalizeOptionalGenerateInt(input.GenerateIdleTimeoutSec),
		DiscoveryEndpointPath:  strings.TrimSpace(input.DiscoveryEndpointPath),
	}

	if err := validateCustomProviderName(normalized.Name); err != nil {
		return SaveCustomProviderInput{}, err
	}
	if normalized.Driver == "" {
		return SaveCustomProviderInput{}, fmt.Errorf("config: provider %q driver is empty", normalized.Name)
	}

	rawModelSource := strings.TrimSpace(input.ModelSource)
	normalized.ModelSource = NormalizeModelSource(rawModelSource)
	if rawModelSource != "" && normalized.ModelSource == "" {
		return SaveCustomProviderInput{}, fmt.Errorf(
			"config: provider %q unsupported model_source %q",
			normalized.Name,
			rawModelSource,
		)
	}
	if normalized.ModelSource == "" {
		normalized.ModelSource = ModelSourceDiscover
	}

	models, err := normalizeCustomProviderModels(input.Models)
	if err != nil {
		return SaveCustomProviderInput{}, err
	}
	normalized.Models = models

	chatAPIMode, err := provider.NormalizeProviderChatAPIMode(normalized.ChatAPIMode)
	if err != nil {
		return SaveCustomProviderInput{}, fmt.Errorf("config: normalize provider chat api mode: %w", err)
	}

	normalizedDiscoveryEndpointPath := normalized.DiscoveryEndpointPath
	if normalized.ModelSource == ModelSourceManual {
		normalizedDiscoveryEndpointPath = ""
	} else if requiresDiscoveryEndpointPath(normalized.Driver) && strings.TrimSpace(normalizedDiscoveryEndpointPath) == "" {
		return SaveCustomProviderInput{}, fmt.Errorf(
			"config: provider %q model_source discover requires discovery_endpoint_path; "+
				"if provider does not expose discover endpoint, set model_source to manual",
			normalized.Name,
		)
	}

	chatEndpointPath, err := provider.NormalizeProviderChatEndpointPath(normalized.ChatEndpointPath)
	if err != nil {
		return SaveCustomProviderInput{}, fmt.Errorf("config: normalize provider chat endpoint path: %w", err)
	}
	discoveryEndpointPath := ""
	if normalized.ModelSource != ModelSourceManual && strings.TrimSpace(normalizedDiscoveryEndpointPath) != "" {
		discoveryEndpointPath, err = provider.NormalizeProviderDiscoverySettings(
			normalized.Driver,
			normalizedDiscoveryEndpointPath,
		)
		if err != nil {
			return SaveCustomProviderInput{}, fmt.Errorf("config: normalize provider discovery settings: %w", err)
		}
	}

	if normalized.Driver == provider.DriverOpenAICompat {
		normalized.ChatAPIMode = chatAPIMode
		normalized.ChatEndpointPath = chatEndpointPath
	} else {
		normalized.ChatAPIMode = ""
		normalized.ChatEndpointPath = ""
	}
	if normalized.ModelSource == ModelSourceManual {
		if len(normalized.Models) == 0 {
			return SaveCustomProviderInput{}, fmt.Errorf(
				"config: provider %q manual model source requires non-empty models",
				normalized.Name,
			)
		}
		normalized.DiscoveryEndpointPath = ""
		return normalized, validateNormalizedCustomProviderInput(normalized)
	}

	normalized.DiscoveryEndpointPath = discoveryEndpointPath
	return normalized, validateNormalizedCustomProviderInput(normalized)
}

// normalizeOptionalGenerateInt 归一化可选生成控制字段，仅保留调用方原始输入，避免在保存前静默吞掉非法值。
func normalizeOptionalGenerateInt(value int) int {
	return value
}

// validateNormalizedCustomProviderInput 复用统一的 provider 配置校验，避免 custom provider 保存与加载路径出现两套规则。
func validateNormalizedCustomProviderInput(input SaveCustomProviderInput) error {
	cfg := ProviderConfig{
		Name:                   input.Name,
		Driver:                 input.Driver,
		BaseURL:                input.BaseURL,
		APIKeyEnv:              input.APIKeyEnv,
		GenerateMaxRetries:     input.GenerateMaxRetries,
		GenerateMaxRetriesSet:  input.GenerateMaxRetriesSet,
		GenerateIdleTimeoutSec: input.GenerateIdleTimeoutSec,
		ModelSource:            input.ModelSource,
		ChatAPIMode:            input.ChatAPIMode,
		ChatEndpointPath:       input.ChatEndpointPath,
		DiscoveryEndpointPath:  input.DiscoveryEndpointPath,
		Models:                 input.Models,
		Source:                 ProviderSourceCustom,
	}
	return cfg.Validate()
}

// NormalizeCustomProviderModels 统一归一化 custom provider 用户可写模型，并校验必填字段与不允许的 metadata。
func NormalizeCustomProviderModels(models []providertypes.ModelDescriptor) ([]providertypes.ModelDescriptor, error) {
	return normalizeCustomProviderModels(models)
}

// normalizeCustomProviderModels 统一清洗 custom provider 用户可写模型，并拒绝任何 metadata 字段输入。
func normalizeCustomProviderModels(models []providertypes.ModelDescriptor) ([]providertypes.ModelDescriptor, error) {
	if len(models) == 0 {
		return nil, nil
	}

	normalized := make([]providertypes.ModelDescriptor, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for index, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			return nil, fmt.Errorf("config: models[%d].id is empty", index)
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			return nil, fmt.Errorf("config: models[%d].name is empty", index)
		}
		if strings.TrimSpace(model.Description) != "" {
			return nil, fmt.Errorf("config: models[%d].description is not supported", index)
		}
		if model.ContextWindow != 0 {
			return nil, fmt.Errorf("config: models[%d].context_window is not supported", index)
		}
		if model.MaxOutputTokens != 0 {
			return nil, fmt.Errorf("config: models[%d].max_output_tokens is not supported", index)
		}
		if model.CapabilityHints != (providertypes.ModelCapabilityHints{}) {
			return nil, fmt.Errorf("config: models[%d].capability_hints is not supported", index)
		}

		key := provider.NormalizeKey(id)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("config: models[%d].id %q is duplicated", index, id)
		}
		seen[key] = struct{}{}

		normalized = append(normalized, providertypes.ModelDescriptor{
			ID:   id,
			Name: name,
		})
	}
	return normalized, nil
}
