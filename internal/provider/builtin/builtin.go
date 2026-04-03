package builtin

import (
	"errors"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openai"
)

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
