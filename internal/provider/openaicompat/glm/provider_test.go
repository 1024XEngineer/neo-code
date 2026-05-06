package glm

import (
	"encoding/json"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestInjectGLMParams_Enabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectGLMParams(body, providertypes.ThinkingConfig{Enabled: true})
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

	kwargs, ok := raw["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs not found")
	}
	if kwargs["enable_thinking"] != true {
		t.Fatalf("expected kwargs.enable_thinking=true, got %v", kwargs["enable_thinking"])
	}
	if kwargs["clear_thinking"] != false {
		t.Fatalf("expected kwargs.clear_thinking=false, got %v", kwargs["clear_thinking"])
	}
}

func TestInjectGLMParams_Disabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectGLMParams(body, providertypes.ThinkingConfig{Enabled: false})
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

	kwargs, ok := raw["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs not found")
	}
	if kwargs["clear_thinking"] != true {
		t.Fatalf("expected kwargs.clear_thinking=true, got %v", kwargs["clear_thinking"])
	}
}
