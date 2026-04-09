package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	providersDirName                = "providers"
	customProviderConfigName        = "provider.yaml"
	defaultOpenAICompatibleAPIStyle = "chat_completions"
)

type customProviderFile struct {
	Name             string                      `yaml:"name"`
	Driver           string                      `yaml:"driver"`
	DefaultModel     string                      `yaml:"default_model"`
	APIKeyEnv        string                      `yaml:"api_key_env"`
	BaseURL          string                      `yaml:"base_url,omitempty"`
	OpenAICompatible customOpenAICompatibleFile  `yaml:"openai_compatible,omitempty"`
	Gemini           customGeminiProviderFile    `yaml:"gemini,omitempty"`
	Anthropic        customAnthropicProviderFile `yaml:"anthropic,omitempty"`
}

type customOpenAICompatibleFile struct {
	Profile  string `yaml:"profile,omitempty"`
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

// loadCustomProviders 扫描 baseDir/providers 下的一级子目录，并将其中的 provider.yaml 解析为运行时配置。
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
	if err := yaml.Unmarshal(data, &file); err != nil {
		return ProviderConfig{}, fmt.Errorf("config: parse %s: %w", providerPath, err)
	}
	if strings.TrimSpace(file.DefaultModel) != "" {
		return ProviderConfig{}, fmt.Errorf(
			"config: custom provider %q does not support default_model; models must come from remote discovery",
			filepath.Base(providerDir),
		)
	}

	cfg := ProviderConfig{
		Name:           strings.TrimSpace(file.Name),
		Driver:         strings.TrimSpace(file.Driver),
		BaseURL:        strings.TrimSpace(resolveCustomProviderBaseURL(file)),
		APIKeyEnv:      strings.TrimSpace(file.APIKeyEnv),
		APIStyle:       strings.TrimSpace(file.OpenAICompatible.APIStyle),
		DeploymentMode: strings.TrimSpace(file.Gemini.DeploymentMode),
		APIVersion:     strings.TrimSpace(file.Anthropic.APIVersion),
		Source:         ProviderSourceCustom,
	}

	if NormalizeProviderDriver(cfg.Driver) == "openaicompat" && strings.TrimSpace(cfg.APIStyle) == "" {
		cfg.APIStyle = defaultOpenAICompatibleAPIStyle
	}

	if err := cfg.Validate(); err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	return cfg, nil
}

// resolveCustomProviderBaseURL 根据 driver 对应的配置块提取最终生效的接入地址。
func resolveCustomProviderBaseURL(file customProviderFile) string {
	for _, value := range []string{
		file.BaseURL,
		file.OpenAICompatible.BaseURL,
		file.Gemini.BaseURL,
		file.Anthropic.BaseURL,
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
