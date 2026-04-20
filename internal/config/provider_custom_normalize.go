package config

import (
	"fmt"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// ManualModelOptionalIntUnset 用于区分“未填写可选数值字段”和“显式输入 0”。
const ManualModelOptionalIntUnset = -1

// NormalizeCustomProviderInput 统一归一化 custom provider 的输入字段，并执行协议/模型来源的组合校验。
func NormalizeCustomProviderInput(input SaveCustomProviderInput) (SaveCustomProviderInput, error) {
	normalized := SaveCustomProviderInput{
		Name:                  strings.TrimSpace(input.Name),
		Driver:                normalizeProviderDriver(strings.TrimSpace(input.Driver)),
		BaseURL:               strings.TrimSpace(input.BaseURL),
		ChatAPIMode:           strings.TrimSpace(input.ChatAPIMode),
		ChatEndpointPath:      strings.TrimSpace(input.ChatEndpointPath),
		APIKeyEnv:             strings.TrimSpace(input.APIKeyEnv),
		DiscoveryEndpointPath: strings.TrimSpace(input.DiscoveryEndpointPath),
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

	models, err := normalizeCustomProviderModels(input.Models, true)
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
		var err error
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
		return normalized, nil
	}

	if requiresDiscoveryEndpointPath(normalized.Driver) && strings.TrimSpace(discoveryEndpointPath) == "" {
		return SaveCustomProviderInput{}, fmt.Errorf(
			"config: provider %q model_source discover requires discovery_endpoint_path; "+
				"if provider does not expose discover endpoint, set model_source to manual",
			normalized.Name,
		)
	}
	normalized.DiscoveryEndpointPath = discoveryEndpointPath
	return normalized, nil
}

// NormalizeCustomProviderModels 统一归一化 custom provider 的模型描述并校验必填字段和边界条件。
func NormalizeCustomProviderModels(models []providertypes.ModelDescriptor) ([]providertypes.ModelDescriptor, error) {
	return normalizeCustomProviderModels(models, false)
}

// normalizeCustomProviderModels 统一归一化 custom provider 模型列表，并在需要时兼容历史的零值省略语义。
func normalizeCustomProviderModels(
	models []providertypes.ModelDescriptor,
	allowZeroAsUnset bool,
) ([]providertypes.ModelDescriptor, error) {
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
		contextWindow := model.ContextWindow
		if contextWindow == ManualModelOptionalIntUnset || (allowZeroAsUnset && contextWindow == 0) {
			contextWindow = 0
		} else if contextWindow <= 0 {
			return nil, fmt.Errorf("config: models[%d].context_window must be greater than 0", index)
		}
		maxOutputTokens := model.MaxOutputTokens
		if maxOutputTokens == ManualModelOptionalIntUnset || (allowZeroAsUnset && maxOutputTokens == 0) {
			maxOutputTokens = 0
		} else if maxOutputTokens <= 0 {
			return nil, fmt.Errorf("config: models[%d].max_output_tokens must be greater than 0", index)
		}

		key := provider.NormalizeKey(id)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("config: models[%d].id %q is duplicated", index, id)
		}
		seen[key] = struct{}{}

		normalized = append(normalized, providertypes.ModelDescriptor{
			ID:              id,
			Name:            name,
			Description:     strings.TrimSpace(model.Description),
			ContextWindow:   contextWindow,
			MaxOutputTokens: maxOutputTokens,
			CapabilityHints: model.CapabilityHints,
		})
	}
	return normalized, nil
}
