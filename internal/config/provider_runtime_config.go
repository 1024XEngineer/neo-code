package config

import "neo-code/internal/provider"

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
