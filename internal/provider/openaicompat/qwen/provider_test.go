package qwen

import (
	"encoding/json"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestInjectQwenParams_Enabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectQwenParams(body, providertypes.ThinkingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking=true, got %v", raw["enable_thinking"])
	}
	if raw["temperature"] != 0.6 {
		t.Fatalf("expected temperature=0.6, got %v", raw["temperature"])
	}
	if raw["top_p"] != 0.95 {
		t.Fatalf("expected top_p=0.95, got %v", raw["top_p"])
	}
}

func TestInjectQwenParams_Disabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectQwenParams(body, providertypes.ThinkingConfig{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["enable_thinking"] != false {
		t.Fatalf("expected enable_thinking=false, got %v", raw["enable_thinking"])
	}
	if raw["temperature"] != 0.7 {
		t.Fatalf("expected temperature=0.7, got %v", raw["temperature"])
	}
	if raw["top_p"] != 0.8 {
		t.Fatalf("expected top_p=0.8, got %v", raw["top_p"])
	}
}

func TestInjectQwenParams_PreservesExistingTemp(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true,"temperature":0.3}`)
	result, err := injectQwenParams(body, providertypes.ThinkingConfig{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["temperature"] != 0.3 {
		t.Fatalf("expected temperature=0.3 (preserved), got %v", raw["temperature"])
	}
}
