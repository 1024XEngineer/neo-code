package provider_test

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openai"
)

type stubProvider struct{}

func (stubProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, nil
}

func stubDriver(driverType string) provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: driverType,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return stubProvider{}, nil
		},
	}
}

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()

	registry := provider.NewRegistry()
	if err := registry.Register(openai.Driver()); err != nil {
		t.Fatalf("register openai driver: %v", err)
	}
	return registry
}

func TestRegistryBuildsRegisteredDriverCaseInsensitively(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	got, err := registry.Build(context.Background(), config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{
			Name:      "openai-main",
			Driver:    "OPENAI",
			BaseURL:   config.OpenAIDefaultBaseURL,
			Model:     config.OpenAIDefaultModel,
			APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
		},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := got.(*openai.Provider); !ok {
		t.Fatalf("expected openai.Provider, got %T", got)
	}
}

func TestRegistryUnknownDriverReturnsTypedError(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	_, err := registry.Build(context.Background(), config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{Driver: "missing"},
	})
	if !errors.Is(err, provider.ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got %v", err)
	}
}

func TestRegistryRejectsDuplicateDriverRegistration(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	if err := registry.Register(stubDriver("custom")); err != nil {
		t.Fatalf("initial Register() error = %v", err)
	}
	if err := registry.Register(stubDriver("CUSTOM")); err == nil {
		t.Fatalf("expected duplicate driver registration to fail")
	}
}
