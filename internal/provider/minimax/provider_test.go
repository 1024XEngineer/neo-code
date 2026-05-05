package minimax

import (
	"encoding/json"
	"testing"
)

func TestInjectMiniMaxParams(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectMiniMaxParams(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["reasoning_split"] != true {
		t.Fatalf("expected reasoning_split=true, got %v", raw["reasoning_split"])
	}
	if raw["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking=true, got %v", raw["enable_thinking"])
	}
}

func TestExtractThinkContent_WithTags(t *testing.T) {
	t.Parallel()

	content := "Some text <think>internal reasoning here</think> final answer"
	result := ExtractThinkContent(content)
	if result != "internal reasoning here" {
		t.Fatalf("expected 'internal reasoning here', got %q", result)
	}
}

func TestExtractThinkContent_MultipleTags(t *testing.T) {
	t.Parallel()

	content := "<think>first thought</think> action <think>second thought</think> done"
	result := ExtractThinkContent(content)
	if result != "first thought\nsecond thought" {
		t.Fatalf("expected 'first thought\\nsecond thought', got %q", result)
	}
}

func TestExtractThinkContent_NoTags(t *testing.T) {
	t.Parallel()

	result := ExtractThinkContent("plain text without tags")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}
