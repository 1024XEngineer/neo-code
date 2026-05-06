package builtin

import (
	"errors"

	"neo-code/internal/provider"
	"neo-code/internal/provider/anthropic"
	"neo-code/internal/provider/deepseek"
	"neo-code/internal/provider/gemini"
	"neo-code/internal/provider/mimo"
	"neo-code/internal/provider/minimax"
	"neo-code/internal/provider/openaicompat"
	"neo-code/internal/provider/openaicompat/glm"
	"neo-code/internal/provider/openaicompat/qwen"
)

func NewRegistry() (*provider.Registry, error) {
	registry := provider.NewRegistry()
	if err := register(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func register(registry *provider.Registry) error {
	if registry == nil {
		return errors.New("builtin provider registry is nil")
	}
	drivers := []provider.DriverDefinition{
		openaicompat.Driver(),
		gemini.Driver(),
		anthropic.Driver(),
		deepseek.Driver(),
		qwen.Driver(),
		glm.Driver(),
		mimo.Driver(),
		minimax.Driver(),
	}
	for _, d := range drivers {
		if err := registry.Register(d); err != nil {
			return err
		}
	}
	return nil
}
