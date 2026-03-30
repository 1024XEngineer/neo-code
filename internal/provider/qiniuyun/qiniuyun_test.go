package qiniuyun

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
	if len(cfg.Models) != 4 {
		t.Fatalf("expected 4 builtin models, got %+v", cfg.Models)
	}
	if cfg.Models[0] != "z-ai/glm-5" {
		t.Fatalf("expected first builtin model %q, got %+v", "z-ai/glm-5", cfg.Models)
	}
	if cfg.Models[len(cfg.Models)-1] != DefaultModel {
		t.Fatalf("expected builtin models to include default model %q, got %+v", DefaultModel, cfg.Models)
	}
}
