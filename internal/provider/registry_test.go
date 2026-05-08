package provider_test

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat"
	providertypes "neo-code/internal/provider/types"
)

type stubProvider struct{}

func (stubProvider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	_ = ctx
	_ = req
	return providertypes.BudgetEstimate{
		EstimateSource: provider.EstimateSourceLocal,
	}, nil
}

func (stubProvider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	return nil
}

func stubDriver(driverType string) provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: driverType,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return stubProvider{}, nil
		},
		Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return nil, nil
		},
	}
}

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()

	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}
	return registry
}

func TestRegistryBuildsRegisteredDriverCaseInsensitively(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	got, err := registry.Build(context.Background(), provider.RuntimeConfig{
		Name:           "openai-main",
		Driver:         "OPENAICOMPAT",
		BaseURL:        config.OpenAIDefaultBaseURL,
		DefaultModel:   config.OpenAIDefaultModel,
		APIKeyEnv:      "TEST_OPENAI_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := got.(*openaicompat.Provider); !ok {
		t.Fatalf("expected openaicompat.Provider, got %T", got)
	}
}

func TestRegistryUnknownDriverReturnsTypedError(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	_, err := registry.Build(context.Background(), provider.RuntimeConfig{Driver: "missing"})
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

		expectedModels := []providertypes.ModelDescriptor{
			{ID: "model-1", Name: "Model 1"},
			{ID: "model-2", Name: "Model 2"},
		}

		registry := provider.NewRegistry()
		driver := provider.DriverDefinition{
			Name: "test-driver",
			Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
				return stubProvider{}, nil
			},
			Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
				return expectedModels, nil
			},
		}
		if err := registry.Register(driver); err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		got, err := registry.DiscoverModels(context.Background(), provider.RuntimeConfig{Driver: "test-driver"})
		if err != nil {
			t.Fatalf("DiscoverModels() error = %v", err)
		}
		if len(got) != len(expectedModels) {
			t.Fatalf("expected %d models, got %d", len(expectedModels), len(got))
		}
	})

	t.Run("unknown driver", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		_, err := registry.DiscoverModels(context.Background(), provider.RuntimeConfig{Driver: "missing"})
		if !errors.Is(err, provider.ErrDriverNotFound) {
			t.Fatalf("expected ErrDriverNotFound, got %v", err)
		}
	})

	t.Run("driver without discovery function", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		err := registry.Register(provider.DriverDefinition{
			Name: "manual-only",
			Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
				return stubProvider{}, nil
			},
		})
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		_, err = registry.DiscoverModels(context.Background(), provider.RuntimeConfig{Driver: "manual-only"})
		if err == nil || !provider.IsDiscoveryConfigError(err) {
			t.Fatalf("expected discovery config error, got %v", err)
		}
	})
}

func TestRegistrySupports(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	if err := registry.Register(stubDriver("openaicompat")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tests := []struct {
		driverType string
		want       bool
	}{
		{"openaicompat", true},
		{"OPENAICOMPAT", true},
		{"OpenAICompat", true},
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

func TestRegistrySupportsDiscovery(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	if err := registry.Register(stubDriver("discoverable")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(provider.DriverDefinition{
		Name: "manual-only",
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return stubProvider{}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tests := []struct {
		driverType string
		want       bool
	}{
		{"discoverable", true},
		{"DISCOVERABLE", true},
		{"manual-only", false},
		{"MANUAL-ONLY", false},
		{"missing", false},
	}

	for _, tt := range tests {
		t.Run(tt.driverType, func(t *testing.T) {
			if got := registry.SupportsDiscovery(tt.driverType); got != tt.want {
				t.Fatalf("SupportsDiscovery(%q) = %v, want %v", tt.driverType, got, tt.want)
			}
		})
	}
}

func TestRegistryValidateCatalogIdentity(t *testing.T) {
	t.Parallel()

	t.Run("openaicompat catalog identity accepts default protocol settings", func(t *testing.T) {
		t.Parallel()

		registry := newTestRegistry(t)
		err := registry.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:  "OPENAICOMPAT",
			BaseURL: config.OpenAIDefaultBaseURL,
		})
		if err != nil {
			t.Fatalf("expected catalog identity validation to pass, got %v", err)
		}
	})

	t.Run("missing driver returns typed error", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		err := registry.ValidateCatalogIdentity(provider.ProviderIdentity{Driver: "missing"})
		if !errors.Is(err, provider.ErrDriverNotFound) {
			t.Fatalf("expected ErrDriverNotFound, got %v", err)
		}
	})

	t.Run("driver without validator passes", func(t *testing.T) {
		t.Parallel()

		registry := provider.NewRegistry()
		err := registry.Register(provider.DriverDefinition{
			Name: "manual-only",
			Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
				return stubProvider{}, nil
			},
		})
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		err = registry.ValidateCatalogIdentity(provider.ProviderIdentity{Driver: "manual-only"})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
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
			Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
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
