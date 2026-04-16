package config

import (
	"bytes"
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
	Name             string                      `yaml:"name"`
	Driver           string                      `yaml:"driver"`
	APIKeyEnv        string                      `yaml:"api_key_env"`
	BaseURL          string                      `yaml:"base_url,omitempty"`
	Models           []customProviderModelFile   `yaml:"models,omitempty"`
	OpenAICompatible customOpenAICompatibleFile  `yaml:"openai_compatible,omitempty"`
	Gemini           customGeminiProviderFile    `yaml:"gemini,omitempty"`
	Anthropic        customAnthropicProviderFile `yaml:"anthropic,omitempty"`
}

type customProviderModelFile struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name,omitempty"`
	ContextWindow   *int   `yaml:"context_window,omitempty"`
	MaxOutputTokens *int   `yaml:"max_output_tokens,omitempty"`
}

type customOpenAICompatibleFile struct {
	BaseURL  string `yaml:"base_url"`
	APIStyle string `yaml:"api_style,omitempty"`
}

type customGeminiProviderFile struct {
	BaseURL        string `yaml:"base_url,omitempty"`
	DeploymentMode string `yaml:"deployment_mode,omitempty"`
}

type customAnthropicProviderFile struct {
	BaseURL    string `yaml:"base_url,omitempty"`
	APIVersion string `yaml:"api_version,omitempty"`
}

type customProviderSettings struct {
	BaseURL        string
	APIStyle       string
	DeploymentMode string
	APIVersion     string
}

// loadCustomProviders 扫描 baseDir/providers 下的一层子目录，并将其中的 provider.yaml 解析为运行时配置。
func loadCustomProviders(baseDir string) ([]ProviderConfig, error) {
	providersDir := filepath.Join(strings.TrimSpace(baseDir), providersDirName)
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		if os.IsNotExist(err) {
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

	settings := resolveCustomProviderSettings(file)
	models, err := customProviderModels(file.Models)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	cfg := ProviderConfig{
		Name:           strings.TrimSpace(file.Name),
		Driver:         strings.TrimSpace(file.Driver),
		BaseURL:        settings.BaseURL,
		APIKeyEnv:      strings.TrimSpace(file.APIKeyEnv),
		APIStyle:       settings.APIStyle,
		DeploymentMode: settings.DeploymentMode,
		APIVersion:     settings.APIVersion,
		Models:         models,
		Source:         ProviderSourceCustom,
	}

	if normalizeProviderDriver(cfg.Driver) == provider.DriverOpenAICompat && strings.TrimSpace(cfg.APIStyle) == "" {
		cfg.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions
	}

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
		settings.APIStyle = strings.TrimSpace(file.OpenAICompatible.APIStyle)
	case provider.DriverGemini:
		settings.BaseURL = strings.TrimSpace(file.Gemini.BaseURL)
		settings.DeploymentMode = strings.TrimSpace(file.Gemini.DeploymentMode)
	case provider.DriverAnthropic:
		settings.BaseURL = strings.TrimSpace(file.Anthropic.BaseURL)
		settings.APIVersion = strings.TrimSpace(file.Anthropic.APIVersion)
	default:
		settings.BaseURL = strings.TrimSpace(file.BaseURL)
	}

	return settings
}
