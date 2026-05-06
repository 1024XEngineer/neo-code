package deepseek

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

func TestInjectThinkingParams_WithEffort(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := InjectThinkingParams(body, providertypes.ThinkingConfig{
		Enabled: true,
		Effort:  "max",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["reasoning_effort"] != "max" {
		t.Fatalf("expected reasoning_effort=max, got %v", raw["reasoning_effort"])
	}
}

func TestExtractContinuity(t *testing.T) {
	t.Parallel()

	msg := &providertypes.Message{
		Role: providertypes.RoleAssistant,
	}
	ExtractContinuity(msg, "thinking content here")

	if len(msg.ThinkingMetadata) == 0 {
		t.Fatalf("expected ThinkingMetadata to be set")
	}

	var meta map[string]string
	if err := json.Unmarshal(msg.ThinkingMetadata, &meta); err != nil {
		t.Fatalf("unmarshal ThinkingMetadata: %v", err)
	}
	if meta["reasoning_content"] != "thinking content here" {
		t.Fatalf("expected reasoning_content, got %v", meta)
	}
}

func TestExtractContinuity_EmptySkips(t *testing.T) {
	t.Parallel()

	msg := &providertypes.Message{}
	ExtractContinuity(msg, "")
	if len(msg.ThinkingMetadata) != 0 {
		t.Fatalf("expected empty ThinkingMetadata")
	}

	ExtractContinuity(nil, "content")
	// nil should not panic
}
