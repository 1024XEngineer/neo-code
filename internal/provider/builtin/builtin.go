package builtin

import (
	"errors"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openai"
)

// DefaultConfig 返回默认配置，包含所有内置 provider。
func DefaultConfig() *config.Config {
	cfg := config.Default()
	providers := config.DefaultProviders()

	cfg.Providers = providers
	cfg.SelectedProvider = providers[0].Name
	cfg.CurrentModel = providers[0].Model

	return cfg
}

func NewRegistry() (*provider.Registry, error) {
	registry := provider.NewRegistry()
	if err := Register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func Register(registry *provider.Registry) error {
	if registry == nil {
		return errors.New("builtin provider registry is nil")
	}
	return registry.Register(openai.Driver())
}
