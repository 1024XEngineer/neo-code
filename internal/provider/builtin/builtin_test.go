package builtin

import (
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

func TestDefaultConfigIncludesBuiltinProviders(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if len(cfg.Providers) != 4 {
		t.Fatalf("expected 4 builtin providers, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != config.OpenAIName {
		t.Fatalf("expected first provider %q, got %q", config.OpenAIName, cfg.Providers[0].Name)
	}
	if cfg.Providers[1].Name != config.GeminiName {
		t.Fatalf("expected second provider %q, got %q", config.GeminiName, cfg.Providers[1].Name)
	}
	if cfg.Providers[2].Name != config.OpenLLName {
		t.Fatalf("expected third provider %q, got %q", config.OpenLLName, cfg.Providers[2].Name)
	}
	if cfg.Providers[3].Name != config.QiniuName {
		t.Fatalf("expected fourth provider %q, got %q", config.QiniuName, cfg.Providers[3].Name)
	}
	if cfg.SelectedProvider != config.OpenAIName {
		t.Fatalf("expected selected provider %q, got %q", config.OpenAIName, cfg.SelectedProvider)
	}
	if cfg.CurrentModel != config.OpenAIDefaultModel {
		t.Fatalf("expected current model %q, got %q", config.OpenAIDefaultModel, cfg.CurrentModel)
	}
}

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		registry := provider.NewRegistry()
		err := Register(registry)
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		err := Register(nil)
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
	})
}

func TestDefaultConfigValidates(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.ApplyDefaultsFrom(*cfg)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected default config to validate, got %v", err)
	}
}

func TestDefaultConfigWorkdirIsAbsolute(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.ApplyDefaultsFrom(*cfg)
	if cfg.Workdir == "" {
		t.Fatal("expected workdir to be set")
	}
}
