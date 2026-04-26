package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"

	"gopkg.in/yaml.v3"
)

const (
	providersDirName         = "providers"
	customProviderConfigName = "provider.yaml"
)

type customProviderFile struct {
	Name                   string                    `yaml:"name"`
	Driver                 string                    `yaml:"driver"`
	APIKeyEnv              string                    `yaml:"api_key_env"`
	GenerateMaxRetries     int                       `yaml:"generate_max_retries,omitempty"`
	GenerateIdleTimeoutSec int                       `yaml:"generate_idle_timeout_sec,omitempty"`
	ModelSource            string                    `yaml:"model_source,omitempty"`
	ChatAPIMode            string                    `yaml:"chat_api_mode,omitempty"`
	BaseURL                string                    `yaml:"base_url,omitempty"`
	ChatEndpointPath       string                    `yaml:"chat_endpoint_path,omitempty"`
	DiscoveryEndpointPath  string                    `yaml:"discovery_endpoint_path,omitempty"`
	Models                 []customProviderModelFile `yaml:"models,omitempty"`
}

type customProviderModelFile struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name"`
	ContextWindow   *int   `yaml:"context_window,omitempty"`
	MaxOutputTokens *int   `yaml:"max_output_tokens,omitempty"`
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

	models, err := customProviderModels(file.Models)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	normalizedInput, err := NormalizeCustomProviderInput(SaveCustomProviderInput{
		Name:                   strings.TrimSpace(file.Name),
		Driver:                 strings.TrimSpace(file.Driver),
		BaseURL:                strings.TrimSpace(file.BaseURL),
		APIKeyEnv:              strings.TrimSpace(file.APIKeyEnv),
		GenerateMaxRetries:     file.GenerateMaxRetries,
		GenerateIdleTimeoutSec: file.GenerateIdleTimeoutSec,
		ModelSource:            strings.TrimSpace(file.ModelSource),
		ChatAPIMode:            strings.TrimSpace(file.ChatAPIMode),
		ChatEndpointPath:       strings.TrimSpace(file.ChatEndpointPath),
		DiscoveryEndpointPath:  strings.TrimSpace(file.DiscoveryEndpointPath),
		Models:                 models,
	})
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("config: custom provider %q: %w", filepath.Base(providerDir), err)
	}

	cfg := ProviderConfig{
		Name:                   normalizedInput.Name,
		Driver:                 normalizedInput.Driver,
		BaseURL:                normalizedInput.BaseURL,
		APIKeyEnv:              normalizedInput.APIKeyEnv,
		GenerateMaxRetries:     normalizedInput.GenerateMaxRetries,
		GenerateIdleTimeoutSec: normalizedInput.GenerateIdleTimeoutSec,
		ModelSource:            normalizedInput.ModelSource,
		ChatAPIMode:            normalizedInput.ChatAPIMode,
		ChatEndpointPath:       normalizedInput.ChatEndpointPath,
		DiscoveryEndpointPath:  normalizedInput.DiscoveryEndpointPath,
		Models:                 normalizedInput.Models,
		Source:                 ProviderSourceCustom,
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
		name := strings.TrimSpace(model.Name)
		if name == "" {
			return nil, fmt.Errorf("models[%d].name is empty", index)
		}

		descriptor := providertypes.ModelDescriptor{
			ID:              id,
			Name:            name,
			ContextWindow:   ManualModelOptionalIntUnset,
			MaxOutputTokens: ManualModelOptionalIntUnset,
		}
		key := provider.NormalizeKey(id)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("models[%d].id %q is duplicated", index, id)
		}
		seen[key] = struct{}{}
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
	return descriptors, nil
}

// SaveCustomProviderInput 定义自定义 Provider 的持久化字段。
type SaveCustomProviderInput struct {
	Name                   string
	Driver                 string
	BaseURL                string
	ChatAPIMode            string
	ChatEndpointPath       string
	APIKeyEnv              string
	GenerateMaxRetries     int
	GenerateIdleTimeoutSec int
	DiscoveryEndpointPath  string
	ModelSource            string
	Models                 []providertypes.ModelDescriptor
}

// SaveCustomProviderWithModels 保存自定义 provider，并可在 manual 模式下写入手工模型列表。
func SaveCustomProviderWithModels(baseDir string, input SaveCustomProviderInput) error {
	normalizedInput, err := NormalizeCustomProviderInput(input)
	if err != nil {
		return err
	}

	providersDir := filepath.Join(baseDir, providersDirName, normalizedInput.Name)
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		return fmt.Errorf("config: create provider dir: %w", err)
	}

	cfg := customProviderFile{
		Name:                   normalizedInput.Name,
		Driver:                 normalizedInput.Driver,
		APIKeyEnv:              normalizedInput.APIKeyEnv,
		GenerateMaxRetries:     normalizedInput.GenerateMaxRetries,
		GenerateIdleTimeoutSec: normalizedInput.GenerateIdleTimeoutSec,
		ModelSource:            normalizedInput.ModelSource,
		ChatAPIMode:            normalizedInput.ChatAPIMode,
	}

	cfg.BaseURL = normalizedInput.BaseURL
	cfg.ChatEndpointPath = normalizedInput.ChatEndpointPath
	cfg.DiscoveryEndpointPath = normalizedInput.DiscoveryEndpointPath
	if normalizedInput.ModelSource == ModelSourceManual {
		cfg.Models = toCustomProviderModelFiles(normalizedInput.Models)
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

// toCustomProviderModelFiles 将模型描述列表转换为 custom provider.yaml 可持久化格式。
func toCustomProviderModelFiles(models []providertypes.ModelDescriptor) []customProviderModelFile {
	if len(models) == 0 {
		return nil
	}
	items := make([]customProviderModelFile, 0, len(models))
	for _, model := range providertypes.MergeModelDescriptors(models) {
		item := customProviderModelFile{
			ID:   strings.TrimSpace(model.ID),
			Name: strings.TrimSpace(model.Name),
		}
		if model.ContextWindow > 0 {
			value := model.ContextWindow
			item.ContextWindow = &value
		}
		if model.MaxOutputTokens > 0 {
			value := model.MaxOutputTokens
			item.MaxOutputTokens = &value
		}
		items = append(items, item)
	}
	return items
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
	if strings.ContainsAny(trimmed, `/\\`) {
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
