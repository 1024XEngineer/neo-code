package provider

import (
	"context"
	"testing"

	"neo-code/internal/config"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		registry := NewRegistry()
		service := NewService(manager, registry)
		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		t.Parallel()
		registry := NewRegistry()
		service := NewService(nil, registry)
		if service == nil {
			t.Fatal("expected non-nil service even with nil manager")
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		service := NewService(manager, nil)
		if service == nil {
			t.Fatal("expected non-nil service even with nil registry")
		}
	})
}

func TestServiceListProviders(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		registry := newServiceTestRegistry(t)
		service := NewService(manager, registry)

		items, err := service.ListProviders(context.Background())
		if err != nil {
			t.Fatalf("ListProviders() error = %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one provider")
		}
		if items[0].ID != testProviderName {
			t.Fatalf("expected first provider ID %q, got %q", testProviderName, items[0].ID)
		}
	})

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		_, err := service.ListProviders(context.Background())
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		_, err := service.ListProviders(context.Background())
		if err == nil {
			t.Fatal("expected error for nil manager")
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		service := &Service{manager: manager}
		_, err := service.ListProviders(context.Background())
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		registry := NewRegistry()
		service := NewService(manager, registry)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := service.ListProviders(ctx)
		if err == nil {
			t.Fatal("expected error for canceled context")
		}
	})
}

func TestServiceSelectProvider(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")

		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		registry := newServiceTestRegistry(t)
		service := NewService(manager, registry)

		selection, err := service.SelectProvider(context.Background(), testProviderName)
		if err != nil {
			t.Fatalf("SelectProvider() error = %v", err)
		}
		if selection.ProviderID != testProviderName {
			t.Fatalf("expected provider ID %q, got %q", testProviderName, selection.ProviderID)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		registry := NewRegistry()
		service := NewService(manager, registry)

		_, err := service.SelectProvider(context.Background(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent provider")
		}
	})

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		_, err := service.SelectProvider(context.Background(), testProviderName)
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})
}

func TestServiceListModels(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")

		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		registry := NewRegistry()
		service := NewService(manager, registry)

		models, err := service.ListModels(context.Background())
		if err != nil {
			t.Fatalf("ListModels() error = %v", err)
		}
		if len(models) == 0 {
			t.Fatal("expected at least one model")
		}
	})

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		_, err := service.ListModels(context.Background())
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")

		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		registry := NewRegistry()
		service := NewService(manager, registry)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := service.ListModels(ctx)
		if err == nil {
			t.Fatal("expected error for canceled context")
		}
	})
}

func TestServiceSetCurrentModel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")

		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		registry := NewRegistry()
		service := NewService(manager, registry)

		newModel := "gpt-4o"
		selection, err := service.SetCurrentModel(context.Background(), newModel)
		if err != nil {
			t.Fatalf("SetCurrentModel() error = %v", err)
		}
		if selection.ModelID != newModel {
			t.Fatalf("expected model ID %q, got %q", newModel, selection.ModelID)
		}
	})

	t.Run("empty model ID", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		registry := NewRegistry()
		service := NewService(manager, registry)

		_, err := service.SetCurrentModel(context.Background(), "")
		if err == nil {
			t.Fatal("expected error for empty model ID")
		}
	})

	t.Run("model not found", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		registry := NewRegistry()
		service := NewService(manager, registry)

		_, err := service.SetCurrentModel(context.Background(), "nonexistent-model")
		if err == nil {
			t.Fatal("expected error for nonexistent model")
		}
	})

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		_, err := service.SetCurrentModel(context.Background(), "model")
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})
}

func TestServiceBuild(t *testing.T) {
	t.Parallel()

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
		if err == nil {
			t.Fatal("expected error for nil manager")
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		service := &Service{manager: manager}
		_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
	})
}

func TestServiceValidate(t *testing.T) {
	t.Parallel()

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()
		var service *Service
		err := service.validate()
		if err == nil {
			t.Fatal("expected error for nil service")
		}
	})

	t.Run("nil manager", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		err := service.validate()
		if err == nil {
			t.Fatal("expected error for nil manager")
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		service := &Service{manager: manager}
		err := service.validate()
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		registry := NewRegistry()
		service := NewService(manager, registry)
		err := service.validate()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestSelectModelHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currentModel string
		models       []string
		fallback     string
		expected     string
	}{
		{
			name:         "current model in list",
			currentModel: "gpt-4o",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4o",
		},
		{
			name:         "current model not in list",
			currentModel: "unknown-model",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4.1",
		},
		{
			name:         "empty current model",
			currentModel: "",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4.1",
		},
		{
			name:         "current model with whitespace",
			currentModel: "  gpt-4o  ",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4o",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := selectModel(tt.currentModel, tt.models, tt.fallback)
			if result != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestModelDescriptorsHelper(t *testing.T) {
	t.Parallel()

	t.Run("empty models", func(t *testing.T) {
		t.Parallel()
		result := modelDescriptors(nil)
		if result != nil {
			t.Fatalf("expected nil for empty models, got %v", result)
		}
	})

	t.Run("non-empty models", func(t *testing.T) {
		t.Parallel()
		models := []string{"gpt-4.1", "gpt-4o", "gpt-5.4"}
		result := modelDescriptors(models)
		if len(result) != 3 {
			t.Fatalf("expected 3 descriptors, got %d", len(result))
		}
		for i, desc := range result {
			if desc.ID != models[i] {
				t.Fatalf("expected ID %q, got %q", models[i], desc.ID)
			}
			if desc.Name != models[i] {
				t.Fatalf("expected name %q, got %q", models[i], desc.Name)
			}
		}
	})

	t.Run("models with empty strings", func(t *testing.T) {
		t.Parallel()
		models := []string{"gpt-4.1", "", "gpt-4o", "   ", "gpt-5.4"}
		result := modelDescriptors(models)
		if len(result) != 3 {
			t.Fatalf("expected 3 descriptors after filtering empty strings, got %d", len(result))
		}
	})
}

func testDefaultConfig() *config.Config {
	cfg := config.Default()
	providers := config.DefaultProviders()

	cfg.Providers = providers
	cfg.SelectedProvider = providers[0].Name
	cfg.CurrentModel = providers[0].Model

	return cfg
}

func newServiceTestRegistry(t *testing.T) *Registry {
	t.Helper()

	registry := NewRegistry()
	if err := registry.Register(DriverDefinition{
		Name: testProviderName,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
			return serviceTestProvider{}, nil
		},
	}); err != nil {
		t.Fatalf("register test driver: %v", err)
	}
	return registry
}

type serviceTestProvider struct{}

func (serviceTestProvider) Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error) {
	return ChatResponse{}, nil
}

const (
	testProviderName = "openai"
	testAPIKeyEnv    = "OPENAI_API_KEY"
)
