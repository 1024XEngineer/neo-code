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

func TestRegistryDiscoverModels(t *testing.T) {
	t.Parallel()

	t.Run("driver with discovery function", func(t *testing.T) {
		t.Parallel()

		expectedModels := []provider.ModelDescriptor{
			{ID: "model-1", Name: "Model 1"},
			{ID: "model-2", Name: "Model 2"},
		}

		registry := provider.NewRegistry()
		driver := provider.DriverDefinition{
			Name: "test-driver",
			Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
				return stubProvider{}, nil
			},
			Discover: func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]provider.ModelDescriptor, error) {
				return expectedModels, nil
			},
		}
		if err := registry.Register(driver); err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		got, err := registry.DiscoverModels(context.Background(), config.ResolvedProviderConfig{
			ProviderConfig: config.ProviderConfig{Driver: "test-driver"},
		})
		if err != nil {
			t.Fatalf("DiscoverModels() error = %v", err)
		}
		if len(got) != len(expectedModels) {
			t.Fatalf("expected %d models, got %d", len(expectedModels), len(got))
		}
	})

	t.Run("driver without discovery function", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		driver := provider.DriverDefinition{
			Name: "test-driver",
			Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
				return stubProvider{}, nil
			},
			Discover: nil,
		}
		if err := registry.Register(driver); err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		got, err := registry.DiscoverModels(context.Background(), config.ResolvedProviderConfig{
			ProviderConfig: config.ProviderConfig{Driver: "test-driver"},
		})
		if err != nil {
			t.Fatalf("DiscoverModels() error = %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil models, got %v", got)
		}
	})

	t.Run("unknown driver", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		_, err := registry.DiscoverModels(context.Background(), config.ResolvedProviderConfig{
			ProviderConfig: config.ProviderConfig{Driver: "missing"},
		})
		if !errors.Is(err, provider.ErrDriverNotFound) {
			t.Fatalf("expected ErrDriverNotFound, got %v", err)
		}
	})
}

func TestRegistrySupports(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	if err := registry.Register(stubDriver("openai")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tests := []struct {
		driverType string
		want       bool
	}{
		{"openai", true},
		{"OPENAI", true},
		{"OpenAI", true},
		{"missing", false},
		{"MISSING", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.driverType, func(t *testing.T) {
			got := registry.Supports(tt.driverType)
			if got != tt.want {
				t.Fatalf("Supports(%q) = %v, want %v", tt.driverType, got, tt.want)
			}
		})
	}
}

func TestRegistryRegisterErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()

		var registry *provider.Registry
		err := registry.Register(stubDriver("test"))
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
		if err.Error() != "provider: registry is nil" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty driver name", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		err := registry.Register(provider.DriverDefinition{
			Name: "   ",
			Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
				return nil, nil
			},
		})
		if err == nil {
			t.Fatal("expected error for empty driver name")
		}
		if err.Error() != "provider: driver name is empty" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil build function", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		err := registry.Register(provider.DriverDefinition{
			Name:  "test",
			Build: nil,
		})
		if err == nil {
			t.Fatal("expected error for nil build function")
		}
		if err.Error() != `provider: driver "test" build func is nil` {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRegistryDriverWithNilMap(t *testing.T) {
	t.Parallel()

	registry := &provider.Registry{}
	err := registry.Register(stubDriver("test"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !registry.Supports("test") {
		t.Fatal("expected registry to support test driver")
	}
}
