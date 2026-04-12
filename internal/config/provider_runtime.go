package config

import (
	"strings"

	"neo-code/internal/provider"
)

// normalizeConfigKey 统一规范 config 层比较使用的字符串键，避免大小写和空白造成分支漂移。
func normalizeConfigKey(value string) string {
	return provider.NormalizeKey(value)
}

// normalizeProviderName 统一规范 provider 名称，供 config 层查找、去重与比较逻辑复用。
func normalizeProviderName(name string) string {
	return provider.NormalizeKey(name)
}

// normalizeProviderDriver 统一规范 driver 名称，供 config 层校验和配置解析分支复用。
func normalizeProviderDriver(driver string) string {
	return provider.NormalizeProviderDriver(driver)
}

// providerIdentityFromConfig 根据 provider 配置构造用于去重与缓存的规范化连接身份。
func providerIdentityFromConfig(cfg ProviderConfig) (provider.ProviderIdentity, error) {
	apiStyle := cfg.APIStyle
	if normalizeProviderDriver(cfg.Driver) == provider.DriverOpenAICompat && strings.TrimSpace(apiStyle) == "" {
		apiStyle = provider.OpenAICompatibleAPIStyleChatCompletions
	}
	return provider.NormalizeProviderIdentity(provider.ProviderIdentity{
		Driver:         cfg.Driver,
		BaseURL:        cfg.BaseURL,
		APIStyle:       apiStyle,
		DeploymentMode: cfg.DeploymentMode,
		APIVersion:     cfg.APIVersion,
	})
}

// ToRuntimeConfig 将解析后的 provider 配置收敛为 provider 层使用的最小运行时输入。
func (p ResolvedProviderConfig) ToRuntimeConfig() provider.RuntimeConfig {
	return provider.RuntimeConfig{
		Name:           p.Name,
		Driver:         p.Driver,
		BaseURL:        p.BaseURL,
		DefaultModel:   p.Model,
		APIKey:         p.APIKey,
		APIStyle:       p.APIStyle,
		DeploymentMode: p.DeploymentMode,
		APIVersion:     p.APIVersion,
	}
}
