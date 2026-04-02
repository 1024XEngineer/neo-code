package provider

import "testing"

func TestDescriptorFromRawModelNormalizesUsefulFields(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"id":                "gpt-test",
		"name":              "GPT Test",
		"description":       "custom model",
		"context_window":    float64(128000),
		"max_output_tokens": float64(8192),
		"capabilities": map[string]any{
			"tool_call": true,
			"vision":    false,
			"notes":     "ignored",
		},
		"experimental": map[string]any{
			"tier": "beta",
		},
	}

	descriptor, ok := DescriptorFromRawModel(raw)
	if !ok {
		t.Fatal("expected descriptor to be normalized")
	}
	if descriptor.ID != "gpt-test" || descriptor.Name != "GPT Test" {
		t.Fatalf("unexpected descriptor identity: %+v", descriptor)
	}
	if descriptor.ContextWindow != 128000 || descriptor.MaxOutputTokens != 8192 {
		t.Fatalf("expected token metadata to be normalized, got %+v", descriptor)
	}
	if !descriptor.Capabilities["tool_call"] {
		t.Fatalf("expected tool_call capability, got %+v", descriptor.Capabilities)
	}
	if _, ok := descriptor.Capabilities["notes"]; ok {
		t.Fatalf("expected non-bool capability values to be ignored, got %+v", descriptor.Capabilities)
	}
}

func TestMergeModelDescriptorsPrefersEarlierSourceAndBackfillsUsefulFields(t *testing.T) {
	t.Parallel()

	manual := []ModelDescriptor{{
		ID:   "gpt-test",
		Name: "Manual Name",
	}}
	discovered := []ModelDescriptor{{
		ID:              "gpt-test",
		Name:            "Discovered Name",
		ContextWindow:   64000,
		MaxOutputTokens: 4096,
		Capabilities: map[string]bool{
			"tool_call": true,
		},
	}}

	merged := MergeModelDescriptors(manual, discovered)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged model, got %d", len(merged))
	}
	if merged[0].Name != "Manual Name" {
		t.Fatalf("expected earlier source to win for name, got %+v", merged[0])
	}
	if merged[0].ContextWindow != 64000 {
		t.Fatalf("expected metadata to be backfilled, got %+v", merged[0])
	}
	if !merged[0].Capabilities["tool_call"] {
		t.Fatalf("expected capabilities to be backfilled, got %+v", merged[0].Capabilities)
	}
}
