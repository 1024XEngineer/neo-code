package openll

import (
	"testing"

	"neo-code/internal/provider/openai"
)

func TestBuiltinConfigUsesOpenAICompatibleDriver(t *testing.T) {
	t.Parallel()

	cfg := BuiltinConfig()
	if cfg.Name != Name {
		t.Fatalf("expected provider name %q, got %q", Name, cfg.Name)
	}
	if cfg.Driver != openai.DriverName {
		t.Fatalf("expected driver %q, got %q", openai.DriverName, cfg.Driver)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", DefaultBaseURL, cfg.BaseURL)
	}
	if cfg.Model != DefaultModel {
		t.Fatalf("expected default model %q, got %q", DefaultModel, cfg.Model)
	}
	if cfg.APIKeyEnv != DefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", DefaultAPIKeyEnv, cfg.APIKeyEnv)
	}
	if len(cfg.Models) < 3 {
		t.Fatalf("expected builtin models to be predeclared, got %+v", cfg.Models)
	}
	if !containsString(cfg.Models, "gpt-5.4") {
		t.Fatalf("expected builtin models to include gpt-5.4, got %+v", cfg.Models)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
