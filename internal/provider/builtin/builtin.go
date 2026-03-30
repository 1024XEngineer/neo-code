package builtin

import (
	"errors"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/gemini"
	"neo-code/internal/provider/openai"
	"neo-code/internal/provider/openll"
	"neo-code/internal/provider/qiniuyun"
)

func DefaultConfig() *config.Config {
	cfg := config.Default()
	defaultProvider := openai.BuiltinConfig()
	cfg.Providers = []config.ProviderConfig{
		defaultProvider,
		gemini.BuiltinConfig(),
		openll.BuiltinConfig(),
		qiniuyun.BuiltinConfig(),
	}
	cfg.SelectedProvider = defaultProvider.Name
	cfg.CurrentModel = defaultProvider.Model
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
