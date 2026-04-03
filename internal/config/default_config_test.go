package config

import "testing"

func TestDefaultConfigIncludesBuiltinProviders(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if len(cfg.Providers) != 4 {
		t.Fatalf("expected 4 builtin providers, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != OpenAIName {
		t.Fatalf("expected first provider %q, got %q", OpenAIName, cfg.Providers[0].Name)
	}
	if cfg.Providers[1].Name != GeminiName {
		t.Fatalf("expected second provider %q, got %q", GeminiName, cfg.Providers[1].Name)
	}
	if cfg.Providers[2].Name != OpenLLName {
		t.Fatalf("expected third provider %q, got %q", OpenLLName, cfg.Providers[2].Name)
	}
	if cfg.Providers[3].Name != QiniuName {
		t.Fatalf("expected fourth provider %q, got %q", QiniuName, cfg.Providers[3].Name)
	}
	if cfg.SelectedProvider != OpenAIName {
		t.Fatalf("expected selected provider %q, got %q", OpenAIName, cfg.SelectedProvider)
	}
	if cfg.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected current model %q, got %q", OpenAIDefaultModel, cfg.CurrentModel)
	}
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
