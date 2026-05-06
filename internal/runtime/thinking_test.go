package runtime

import (
	"reflect"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestResolveThinkingConfig(t *testing.T) {
	t.Parallel()

	t.Run("unsupported returns nil", func(t *testing.T) {
		t.Parallel()

		cfg, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking: providertypes.ModelCapabilityStateUnsupported,
		}, nil, true)
		if err != nil {
			t.Fatalf("resolveThinkingConfig() error = %v", err)
		}
		if cfg != nil {
			t.Fatalf("expected nil config, got %+v", cfg)
		}
	})

	t.Run("unknown follows global toggle", func(t *testing.T) {
		t.Parallel()

		cfg, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking: providertypes.ModelCapabilityStateUnknown,
		}, nil, false)
		if err != nil {
			t.Fatalf("resolveThinkingConfig() error = %v", err)
		}
		if cfg == nil || cfg.Enabled {
			t.Fatalf("expected disabled config, got %+v", cfg)
		}
	})

	t.Run("override and effort validation", func(t *testing.T) {
		t.Parallel()

		enabled := false
		cfg, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking:              providertypes.ModelCapabilityStateSupported,
			ThinkingEfforts:       []string{"low", "high"},
			ThinkingDefaultEffort: "low",
		}, &ThinkingOverride{Enabled: &enabled, Effort: "high"}, true)
		if err != nil {
			t.Fatalf("resolveThinkingConfig() error = %v", err)
		}
		if cfg == nil || cfg.Enabled || cfg.Effort != "high" {
			t.Fatalf("expected disabled high-effort config, got %+v", cfg)
		}
	})

	t.Run("force enabled wins over override", func(t *testing.T) {
		t.Parallel()

		enabled := false
		cfg, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking:             providertypes.ModelCapabilityStateSupported,
			ThinkingForceEnabled: true,
		}, &ThinkingOverride{Enabled: &enabled}, false)
		if err != nil {
			t.Fatalf("resolveThinkingConfig() error = %v", err)
		}
		if cfg == nil || !cfg.Enabled {
			t.Fatalf("expected forced enabled config, got %+v", cfg)
		}
	})

	t.Run("unsupported effort returns error", func(t *testing.T) {
		t.Parallel()

		_, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking:        providertypes.ModelCapabilityStateSupported,
			ThinkingEfforts: []string{"low"},
		}, &ThinkingOverride{Effort: "high"}, true)
		if err == nil {
			t.Fatal("expected effort validation error")
		}
	})

	t.Run("empty effort list clears default effort", func(t *testing.T) {
		t.Parallel()

		cfg, err := resolveThinkingConfig(providertypes.ModelCapabilityHints{
			Thinking:              providertypes.ModelCapabilityStateSupported,
			ThinkingDefaultEffort: "medium",
		}, nil, true)
		if err != nil {
			t.Fatalf("resolveThinkingConfig() error = %v", err)
		}
		if cfg == nil || cfg.Effort != "" {
			t.Fatalf("expected empty effort, got %+v", cfg)
		}
	})
}

func TestContainsEffortAndHintsLookup(t *testing.T) {
	t.Parallel()

	if !containsEffort([]string{"low", "high"}, "high") {
		t.Fatal("expected effort to be found")
	}
	if containsEffort([]string{"low"}, "max") {
		t.Fatal("did not expect unknown effort to be found")
	}

	models := []providertypes.ModelDescriptor{
		{
			ID: "model-a",
			CapabilityHints: providertypes.ModelCapabilityHints{
				Thinking: providertypes.ModelCapabilityStateSupported,
			},
		},
	}
	hints := modelCapabilityHintsForRequest("model-a", models)
	if hints.Thinking != providertypes.ModelCapabilityStateSupported {
		t.Fatalf("unexpected hints: %+v", hints)
	}
	if got := modelCapabilityHintsForRequest("missing", models); !reflect.DeepEqual(got, providertypes.ModelCapabilityHints{}) {
		t.Fatalf("expected zero hints for missing model, got %+v", got)
	}
}

func TestServiceThinkingToggle(t *testing.T) {
	t.Parallel()

	svc := NewWithFactory(nil, nil, nil, nil, nil)
	if !svc.IsThinkingEnabled() {
		t.Fatal("expected default thinking toggle to be enabled")
	}

	svc.SetThinkingEnabled(false)
	if svc.IsThinkingEnabled() {
		t.Fatal("expected thinking toggle to be disabled")
	}
}
