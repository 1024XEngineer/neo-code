package mimo

import (
	"encoding/json"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestInjectThinkingParams_Enabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := InjectThinkingParams(body, providertypes.ThinkingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	thinking, ok := raw["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking not found or wrong type")
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking.type=enabled, got %v", thinking["type"])
	}
}

func TestInjectThinkingParams_Disabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := InjectThinkingParams(body, providertypes.ThinkingConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	thinking, ok := raw["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking not found")
	}
	if thinking["type"] != "disabled" {
		t.Fatalf("expected thinking.type=disabled, got %v", thinking["type"])
	}
}
