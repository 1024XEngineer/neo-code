package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const (
	providersDirName         = "providers"
	customProviderConfigName = "provider.yaml"
)

type customProviderFile struct {
	Name                     string                      `yaml:"name"`
	Driver                   string                      `yaml:"driver"`
	APIKeyEnv                string                      `yaml:"api_key_env"`
	BaseURL                  string                      `yaml:"base_url,omitempty"`
	ChatProtocol             string                      `yaml:"chat_protocol,omitempty"`
	ChatEndpointPath         string                      `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryProtocol        string                      `yaml:"discovery_protocol,omitempty"`
	AuthStrategy             string                      `yaml:"auth_strategy,omitempty"`
	ResponseProfile          string                      `yaml:"response_profile,omitempty"`
	DiscoveryEndpointPath    string                      `yaml:"discovery_endpoint_path,omitempty"`
	DiscoveryResponseProfile string                      `yaml:"discovery_response_profile,omitempty"`
	ModelFieldAliases        map[string][]string         `yaml:"model_field_aliases,omitempty"`
	Models                   []customProviderModelFile   `yaml:"models,omitempty"`
	OpenAICompatible         customOpenAICompatibleFile  `yaml:"openai_compatible,omitempty"`
	Gemini                   customGeminiProviderFile    `yaml:"gemini,omitempty"`
	Anthropic                customAnthropicProviderFile `yaml:"anthropic,omitempty"`
}

type customProviderModelFile struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name,omitempty"`
	ContextWindow   *int   `yaml:"context_window,omitempty"`
	MaxOutputTokens *int   `yaml:"max_output_tokens,omitempty"`
}

type customOpenAICompatibleFile struct {
	BaseURL                  string `yaml:"base_url"`
	ChatProtocol             string `yaml:"chat_protocol,omitempty"`
	ChatEndpointPath         string `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryProtocol        string `yaml:"discovery_protocol,omitempty"`
	AuthStrategy             string `yaml:"auth_strategy,omitempty"`
	ResponseProfile          string `yaml:"response_profile,omitempty"`
	APIStyle                 string `yaml:"api_style,omitempty"`
	DiscoveryEndpointPath    string `yaml:"discovery_endpoint_path,omitempty"`
	DiscoveryResponseProfile string `yaml:"discovery_response_profile,omitempty"`
}

type customGeminiProviderFile struct {
	BaseURL                  string `yaml:"base_url,omitempty"`
	ChatProtocol             string `yaml:"chat_protocol,omitempty"`
	ChatEndpointPath         string `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryProtocol        string `yaml:"discovery_protocol,omitempty"`
	AuthStrategy             string `yaml:"auth_strategy,omitempty"`
	ResponseProfile          string `yaml:"response_profile,omitempty"`
	DeploymentMode           string `yaml:"deployment_mode,omitempty"`
	DiscoveryEndpointPath    string `yaml:"discovery_endpoint_path,omitempty"`
	DiscoveryResponseProfile string `yaml:"discovery_response_profile,omitempty"`
}

type customAnthropicProviderFile struct {
	BaseURL                  string `yaml:"base_url,omitempty"`
	ChatProtocol             string `yaml:"chat_protocol,omitempty"`
	ChatEndpointPath         string `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryProtocol        string `yaml:"discovery_protocol,omitempty"`
	AuthStrategy             string `yaml:"auth_strategy,omitempty"`
	ResponseProfile          string `yaml:"response_profile,omitempty"`
	APIVersion               string `yaml:"api_version,omitempty"`
	DiscoveryEndpointPath    string `yaml:"discovery_endpoint_path,omitempty"`
	DiscoveryResponseProfile string `yaml:"discovery_response_profile,omitempty"`
}

type customProviderSettings struct {
	BaseURL                  string
	ChatProtocol             string
	ChatEndpointPath         string
	DiscoveryProtocol        string
	AuthStrategy             string
	ResponseProfile          string
	APIStyle                 string
	DeploymentMode           string
	APIVersion               string
	DiscoveryEndpointPath    string
	DiscoveryResponseProfile string
	ModelFieldAliases        string
}

// loadCustomProviders 扫描 baseDir/providers 下的一层子目录，并将其中的 provider.yaml 解析为运行时配置。
func loadCustomProviders(baseDir string) ([]ProviderConfig, error) {
	providersDir := filepath.Join(strings.TrimSpace(baseDir), providersDirName)
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		if os.IsNotExist(err) {
			if info, statErr := os.Stat(providersDir); statErr == nil {
				if !info.IsDir() {
					return nil, fmt.Errorf("config: read providers dir: %w", err)
				}
				return nil, fmt.Errorf("config: read providers dir: %w", err)
			} else if !os.IsNotExist(statErr) {
				return nil, fmt.Errorf("config: read providers dir: %w", statErr)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("config: read providers dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	providers := make([]ProviderConfig, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		providerDir := filepath.Join(providersDir, entry.Name())
		if _, err := os.Stat(filepath.Join(providerDir, customProviderConfigName)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("config: stat %s: %w", filepath.Join(providerDir, customProviderConfigName), err)
		}
		providerCfg, err := loadCustomProvider(providerDir)
		if err != nil {
			return nil, err
		}
		providers = append(providers, providerCfg)
	}

	return providers, nil
}

// loadCustomProvider 读取单个 provider 目录，并将 provider.yaml 转为 ProviderConfig。
func loadCustomProvider(providerDir string) (ProviderConfig, error) {
	providerPath := filepath.Join(providerDir, customProviderConfigName)
	data, err := os.ReadFile(providerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ProviderConfig{}, fmt.Errorf("config: custom provider %q missing %s", filepath.Base(providerDir), customProviderConfigName)
		}
		return ProviderConfig{}, fmt.Errorf("config: read %s: %w", providerPath, err)
	}

	var file customProviderFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return ProviderConfig{}, fmt.Errorf("config: parse %s: %w", providerPath, err)
	}
	if err := validateCustomProviderDiscoveryFieldPlacement(file); err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	settings := resolveCustomProviderSettings(file)
	models, err := customProviderModels(file.Models)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	cfg := ProviderConfig{
		Name:                     strings.TrimSpace(file.Name),
		Driver:                   strings.TrimSpace(file.Driver),
		BaseURL:                  settings.BaseURL,
		APIKeyEnv:                strings.TrimSpace(file.APIKeyEnv),
		ChatProtocol:             settings.ChatProtocol,
		ChatEndpointPath:         settings.ChatEndpointPath,
		DiscoveryProtocol:        settings.DiscoveryProtocol,
		AuthStrategy:             settings.AuthStrategy,
		ResponseProfile:          settings.ResponseProfile,
		APIStyle:                 settings.APIStyle,
		DeploymentMode:           settings.DeploymentMode,
		APIVersion:               settings.APIVersion,
		DiscoveryEndpointPath:    settings.DiscoveryEndpointPath,
		DiscoveryResponseProfile: settings.DiscoveryResponseProfile,
		ModelFieldAliases:        settings.ModelFieldAliases,
		Models:                   models,
		Source:                   ProviderSourceCustom,
	}

	normalizedProtocols, err := provider.NormalizeProviderProtocolSettings(
		cfg.Driver,
		cfg.ChatProtocol,
		cfg.ChatEndpointPath,
		cfg.DiscoveryProtocol,
		cfg.DiscoveryEndpointPath,
		cfg.AuthStrategy,
		cfg.ResponseProfile,
		cfg.APIStyle,
		cfg.DiscoveryResponseProfile,
	)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}
	cfg.ChatProtocol = normalizedProtocols.ChatProtocol
	cfg.ChatEndpointPath = normalizedProtocols.ChatEndpointPath
	cfg.DiscoveryProtocol = normalizedProtocols.DiscoveryProtocol
	cfg.AuthStrategy = normalizedProtocols.AuthStrategy
	cfg.ResponseProfile = normalizedProtocols.ResponseProfile
	cfg.APIStyle = normalizedProtocols.LegacyAPIStyle
	cfg.DiscoveryEndpointPath = normalizedProtocols.DiscoveryEndpointPath
	cfg.DiscoveryResponseProfile = normalizedProtocols.ResponseProfile

	if err := cfg.Validate(); err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	return cfg, nil
}

// customProviderModels 校验并收敛 custom provider.yaml 中声明的模型元数据。
func customProviderModels(models []customProviderModelFile) ([]providertypes.ModelDescriptor, error) {
	if len(models) == 0 {
		return nil, nil
	}

	descriptors := make([]providertypes.ModelDescriptor, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for index, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			return nil, fmt.Errorf("models[%d].id is empty", index)
		}

		key := provider.NormalizeKey(id)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("models[%d].id %q is duplicated", index, id)
		}
		seen[key] = struct{}{}

		descriptor := providertypes.ModelDescriptor{
			ID:   id,
			Name: strings.TrimSpace(model.Name),
		}
		if model.ContextWindow != nil {
			if *model.ContextWindow <= 0 {
				return nil, fmt.Errorf("models[%d].context_window must be greater than 0", index)
			}
			descriptor.ContextWindow = *model.ContextWindow
		}
		if model.MaxOutputTokens != nil {
			if *model.MaxOutputTokens <= 0 {
				return nil, fmt.Errorf("models[%d].max_output_tokens must be greater than 0", index)
			}
			descriptor.MaxOutputTokens = *model.MaxOutputTokens
		}
		descriptors = append(descriptors, descriptor)
	}

	return providertypes.MergeModelDescriptors(descriptors), nil
}

// resolveCustomProviderSettings 根据 driver 只提取当前协议真正生效的配置字段，避免误吃其他协议块的值。
// 已知 driver 仅从协议块读取 base_url；未知 driver 使用顶层 base_url 作为唯一入口。
func resolveCustomProviderSettings(file customProviderFile) customProviderSettings {
	settings := customProviderSettings{}

	switch normalizeProviderDriver(file.Driver) {
	case provider.DriverOpenAICompat:
		settings.BaseURL = strings.TrimSpace(file.OpenAICompatible.BaseURL)
		settings.ChatProtocol = strings.TrimSpace(file.OpenAICompatible.ChatProtocol)
		settings.ChatEndpointPath = strings.TrimSpace(file.OpenAICompatible.ChatEndpointPath)
		settings.DiscoveryProtocol = strings.TrimSpace(file.OpenAICompatible.DiscoveryProtocol)
		settings.AuthStrategy = strings.TrimSpace(file.OpenAICompatible.AuthStrategy)
		settings.ResponseProfile = strings.TrimSpace(file.OpenAICompatible.ResponseProfile)
		settings.APIStyle = strings.TrimSpace(file.OpenAICompatible.APIStyle)
		settings.DiscoveryEndpointPath = strings.TrimSpace(file.OpenAICompatible.DiscoveryEndpointPath)
		settings.DiscoveryResponseProfile = strings.TrimSpace(file.OpenAICompatible.DiscoveryResponseProfile)
	case provider.DriverGemini:
		settings.BaseURL = strings.TrimSpace(file.Gemini.BaseURL)
		settings.ChatProtocol = strings.TrimSpace(file.Gemini.ChatProtocol)
		settings.ChatEndpointPath = strings.TrimSpace(file.Gemini.ChatEndpointPath)
		settings.DiscoveryProtocol = strings.TrimSpace(file.Gemini.DiscoveryProtocol)
		settings.AuthStrategy = strings.TrimSpace(file.Gemini.AuthStrategy)
		settings.ResponseProfile = strings.TrimSpace(file.Gemini.ResponseProfile)
		settings.DeploymentMode = strings.TrimSpace(file.Gemini.DeploymentMode)
		settings.DiscoveryEndpointPath = strings.TrimSpace(file.Gemini.DiscoveryEndpointPath)
		settings.DiscoveryResponseProfile = strings.TrimSpace(file.Gemini.DiscoveryResponseProfile)
	case provider.DriverAnthropic:
		settings.BaseURL = strings.TrimSpace(file.Anthropic.BaseURL)
		settings.ChatProtocol = strings.TrimSpace(file.Anthropic.ChatProtocol)
		settings.ChatEndpointPath = strings.TrimSpace(file.Anthropic.ChatEndpointPath)
		settings.DiscoveryProtocol = strings.TrimSpace(file.Anthropic.DiscoveryProtocol)
		settings.AuthStrategy = strings.TrimSpace(file.Anthropic.AuthStrategy)
		settings.ResponseProfile = strings.TrimSpace(file.Anthropic.ResponseProfile)
		settings.APIVersion = strings.TrimSpace(file.Anthropic.APIVersion)
		settings.DiscoveryEndpointPath = strings.TrimSpace(file.Anthropic.DiscoveryEndpointPath)
		settings.DiscoveryResponseProfile = strings.TrimSpace(file.Anthropic.DiscoveryResponseProfile)
	default:
		settings.BaseURL = strings.TrimSpace(file.BaseURL)
		settings.ChatProtocol = strings.TrimSpace(file.ChatProtocol)
		settings.ChatEndpointPath = strings.TrimSpace(file.ChatEndpointPath)
		settings.DiscoveryProtocol = strings.TrimSpace(file.DiscoveryProtocol)
		settings.AuthStrategy = strings.TrimSpace(file.AuthStrategy)
		settings.ResponseProfile = strings.TrimSpace(file.ResponseProfile)
		settings.DiscoveryEndpointPath = strings.TrimSpace(file.DiscoveryEndpointPath)
		settings.DiscoveryResponseProfile = strings.TrimSpace(file.DiscoveryResponseProfile)
	}
	settings.ModelFieldAliases = encodeModelFieldAliases(file.ModelFieldAliases)

	return settings
}

// validateCustomProviderDiscoveryFieldPlacement 校验 discovery 字段写在与 driver 对应的协议块中，避免静默忽略。
func validateCustomProviderDiscoveryFieldPlacement(file customProviderFile) error {
	topLevelChatProtocol := strings.TrimSpace(file.ChatProtocol)
	topLevelChatEndpointPath := strings.TrimSpace(file.ChatEndpointPath)
	topLevelDiscoveryProtocol := strings.TrimSpace(file.DiscoveryProtocol)
	topLevelAuthStrategy := strings.TrimSpace(file.AuthStrategy)
	topLevelResponseProfile := strings.TrimSpace(file.ResponseProfile)
	topLevelDiscoveryEndpointPath := strings.TrimSpace(file.DiscoveryEndpointPath)
	topLevelDiscoveryResponseProfile := strings.TrimSpace(file.DiscoveryResponseProfile)
	if topLevelChatProtocol == "" &&
		topLevelChatEndpointPath == "" &&
		topLevelDiscoveryProtocol == "" &&
		topLevelAuthStrategy == "" &&
		topLevelResponseProfile == "" &&
		topLevelDiscoveryEndpointPath == "" &&
		topLevelDiscoveryResponseProfile == "" {
		return nil
	}

	switch normalizeProviderDriver(file.Driver) {
	case provider.DriverOpenAICompat:
		return errors.New("openaicompat discovery settings must be configured under openai_compatible")
	case provider.DriverGemini:
		return errors.New("gemini discovery settings must be configured under gemini")
	case provider.DriverAnthropic:
		return errors.New("anthropic discovery settings must be configured under anthropic")
	default:
		return nil
	}
}

// SaveCustomProvider 保存自定义 provider 到文件系统。
func SaveCustomProvider(
	baseDir string,
	name string,
	driver string,
	baseURL string,
	apiKeyEnv string,
	apiStyle string,
	deploymentMode string,
	apiVersion string,
	discoveryEndpointPath string,
	discoveryResponseProfile string,
) error {
	if err := validateCustomProviderName(name); err != nil {
		return err
	}

	providersDir := filepath.Join(baseDir, providersDirName, name)
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		return fmt.Errorf("config: create provider dir: %w", err)
	}

	normalizedDriver := normalizeProviderDriver(driver)
	cfg := customProviderFile{
		Name:      name,
		Driver:    normalizedDriver,
		APIKeyEnv: apiKeyEnv,
	}

	switch normalizedDriver {
	case provider.DriverOpenAICompat:
		cfg.OpenAICompatible = customOpenAICompatibleFile{
			BaseURL:                  baseURL,
			ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
			ChatEndpointPath:         "/chat/completions",
			DiscoveryProtocol:        provider.DiscoveryProtocolOpenAIModels,
			AuthStrategy:             provider.AuthStrategyBearer,
			ResponseProfile:          strings.TrimSpace(discoveryResponseProfile),
			APIStyle:                 strings.TrimSpace(apiStyle),
			DiscoveryEndpointPath:    strings.TrimSpace(discoveryEndpointPath),
			DiscoveryResponseProfile: strings.TrimSpace(discoveryResponseProfile),
		}
	case provider.DriverGemini:
		cfg.Gemini = customGeminiProviderFile{
			BaseURL:                  baseURL,
			ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
			ChatEndpointPath:         "/chat/completions",
			DiscoveryProtocol:        provider.DiscoveryProtocolGeminiModels,
			AuthStrategy:             provider.AuthStrategyBearer,
			ResponseProfile:          strings.TrimSpace(discoveryResponseProfile),
			DeploymentMode:           strings.TrimSpace(deploymentMode),
			DiscoveryEndpointPath:    strings.TrimSpace(discoveryEndpointPath),
			DiscoveryResponseProfile: strings.TrimSpace(discoveryResponseProfile),
		}
	case provider.DriverAnthropic:
		cfg.Anthropic = customAnthropicProviderFile{
			BaseURL:                  baseURL,
			ChatProtocol:             provider.ChatProtocolAnthropicMessages,
			ChatEndpointPath:         "/messages",
			DiscoveryProtocol:        provider.DiscoveryProtocolAnthropicModels,
			AuthStrategy:             provider.AuthStrategyAnthropic,
			ResponseProfile:          strings.TrimSpace(discoveryResponseProfile),
			APIVersion:               strings.TrimSpace(apiVersion),
			DiscoveryEndpointPath:    strings.TrimSpace(discoveryEndpointPath),
			DiscoveryResponseProfile: strings.TrimSpace(discoveryResponseProfile),
		}
	default:
		cfg.BaseURL = baseURL
		cfg.DiscoveryEndpointPath = strings.TrimSpace(discoveryEndpointPath)
		cfg.DiscoveryResponseProfile = strings.TrimSpace(discoveryResponseProfile)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal provider: %w", err)
	}

	providerPath := filepath.Join(providersDir, customProviderConfigName)
	if err := os.WriteFile(providerPath, data, 0o644); err != nil {
		return fmt.Errorf("config: write provider: %w", err)
	}

	return nil
}

// encodeModelFieldAliases 将 model field aliases 序列化为稳定字符串，供运行时跨层传递。
func encodeModelFieldAliases(aliases map[string][]string) string {
	if len(aliases) == 0 {
		return ""
	}
	encoded, err := json.Marshal(aliases)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// DeleteCustomProvider 删除自定义 provider。
func DeleteCustomProvider(baseDir string, name string) error {
	if err := validateCustomProviderName(name); err != nil {
		return err
	}
	providersDir := filepath.Join(baseDir, providersDirName, name)
	return os.RemoveAll(providersDir)
}

// ValidateCustomProviderName 校验 provider 名称，拒绝路径穿越和分隔符语义。
func ValidateCustomProviderName(name string) error {
	return validateCustomProviderName(name)
}

// validateCustomProviderName 校验 provider 名称，拒绝路径穿越和分隔符语义。
func validateCustomProviderName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("config: provider name is empty")
	}
	if trimmed == "." || trimmed == ".." {
		return fmt.Errorf("config: provider name %q is invalid", name)
	}
	if strings.ContainsAny(trimmed, `/\`) {
		return fmt.Errorf("config: provider name %q is invalid", name)
	}
	if filepath.IsAbs(trimmed) {
		return fmt.Errorf("config: provider name %q is invalid", name)
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("config: provider name %q contains unsupported character %q", name, string(r))
	}
	return nil
}
