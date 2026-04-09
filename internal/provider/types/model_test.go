package types

import "testing"

func TestDescriptorFromRawModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    map[string]any
		want   ModelDescriptor
		wantOK bool
	}{
		{
			name:   "empty map returns false",
			raw:    map[string]any{},
			wantOK: false,
		},
		{
			name: "id from model field",
			raw: map[string]any{
				"model": "gpt-4.1",
			},
			want:   ModelDescriptor{ID: "gpt-4.1", Name: "gpt-4.1"},
			wantOK: true,
		},
		{
			name: "full descriptor",
			raw: map[string]any{
				"id":                "gpt-4.1",
				"display_name":      "GPT-4.1",
				"description":       "desc",
				"context_window":    128000,
				"max_output_tokens": 16384,
			},
			want: ModelDescriptor{
				ID:              "gpt-4.1",
				Name:            "GPT-4.1",
				Description:     "desc",
				ContextWindow:   128000,
				MaxOutputTokens: 16384,
			},
			wantOK: true,
		},
		{
			name: "capabilities map becomes hints",
			raw: map[string]any{
				"id":                 "gpt-4o-mini",
				"max_context_tokens": 64000,
				"capabilities": map[string]any{
					"tool_call":   true,
					"image_input": false,
				},
			},
			want: ModelDescriptor{
				ID:            "gpt-4o-mini",
				Name:          "gpt-4o-mini",
				ContextWindow: 64000,
				CapabilityHints: ModelCapabilityHints{
					ToolCalling: ModelCapabilityStateSupported,
					ImageInput:  ModelCapabilityStateUnsupported,
				},
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := DescriptorFromRawModel(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v", tt.wantOK, ok)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Fatalf("expected descriptor %+v, got %+v", tt.want, got)
			}
		})
	}
}

func TestMergeModelDescriptors(t *testing.T) {
	t.Parallel()

	a := []ModelDescriptor{{ID: "m1", Name: "Model1"}}
	b := []ModelDescriptor{{ID: "m2", Name: "Model2"}, {ID: "m1", Description: "fallback"}}

	merged := MergeModelDescriptors(a, b)
	if len(merged) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(merged))
	}

	var m1 *ModelDescriptor
	for i := range merged {
		if merged[i].ID == "m1" {
			m1 = &merged[i]
			break
		}
	}
	if m1 == nil {
		t.Fatalf("expected m1 to be present")
	}
	if m1.Name != "Model1" {
		t.Fatalf("expected Name=Model1 from first source, got %q", m1.Name)
	}
	if m1.Description != "fallback" {
		t.Fatalf("expected Description=fallback from second source, got %q", m1.Description)
	}
}

func TestDescriptorsFromIDs(t *testing.T) {
	t.Parallel()

	result := DescriptorsFromIDs([]string{"gpt-4.1", "gpt-4.1-mini"})
	if len(result) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(result))
	}
	if result[0].ID != "gpt-4.1" {
		t.Fatalf("expected first ID=gpt-4.1, got %q", result[0].ID)
	}
	if result[1].Name != "gpt-4.1-mini" {
		t.Fatalf("expected second Name=gpt-4.1-mini, got %q", result[1].Name)
	}
}

func TestFirstNonEmptyString(t *testing.T) {
	t.Parallel()

	if got := firstNonEmptyString("", "  ", "hello", "world"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := firstNonEmptyString("", "  "); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFirstPositiveInt(t *testing.T) {
	t.Parallel()

	if got := firstPositiveInt(0, -1, 42, 100); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := firstPositiveInt(int32(5)); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	if got := firstPositiveInt(int64(10)); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
	if got := firstPositiveInt(float64(3.14)); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	if got := firstPositiveInt(0, -5); got != 0 {
		t.Fatalf("expected 0 when none positive, got %d", got)
	}
}

func TestModelCapabilityHintsFromValue(t *testing.T) {
	t.Parallel()

	result := modelCapabilityHintsFromValue(map[string]any{
		"tool_call":   true,
		"image_input": false,
		"ignored":     "notbool",
	})
	if result.ToolCalling != ModelCapabilityStateSupported {
		t.Fatalf("expected tool calling supported, got %+v", result)
	}
	if result.ImageInput != ModelCapabilityStateUnsupported {
		t.Fatalf("expected image input unsupported, got %+v", result)
	}
	if result := modelCapabilityHintsFromValue("not a map"); result != (ModelCapabilityHints{}) {
		t.Fatalf("expected empty hints for non-map, got %+v", result)
	}
}

func TestMergeModelCapabilityHints(t *testing.T) {
	t.Parallel()

	primary := ModelCapabilityHints{
		ToolCalling: ModelCapabilityStateSupported,
	}
	secondary := ModelCapabilityHints{
		ToolCalling:   ModelCapabilityStateUnsupported,
		ImageInput:    ModelCapabilityStateUnsupported,
		ReasoningMode: ModelReasoningModeConfigurable,
	}

	result := mergeModelCapabilityHints(primary, secondary)
	if result.ToolCalling != ModelCapabilityStateSupported {
		t.Fatalf("expected primary tool calling to win, got %+v", result)
	}
	if result.ImageInput != ModelCapabilityStateUnsupported {
		t.Fatalf("expected image input to be backfilled, got %+v", result)
	}
	if result.ReasoningMode != ModelReasoningModeConfigurable {
		t.Fatalf("expected reasoning mode to be backfilled, got %+v", result)
	}
}

func TestMergeModelDescriptorFallback(t *testing.T) {
	t.Parallel()

	primary := ModelDescriptor{ID: "m1"}
	secondary := ModelDescriptor{
		Name:            "Fallback",
		Description:     "desc",
		ContextWindow:   8000,
		MaxOutputTokens: 4096,
	}

	result := mergeModelDescriptor(primary, secondary)
	if result.Name != "Fallback" {
		t.Fatalf("expected Name=Fallback from secondary, got %q", result.Name)
	}
	if result.ContextWindow != 8000 {
		t.Fatalf("expected ContextWindow=8000 from secondary, got %d", result.ContextWindow)
	}
	if result.MaxOutputTokens != 4096 {
		t.Fatalf("expected MaxOutputTokens=4096 from secondary, got %d", result.MaxOutputTokens)
	}
}
