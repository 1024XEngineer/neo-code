package provider

import "testing"

func TestModelDescriptorsFromIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		modelIDs []string
		want     []ModelDescriptor
	}{
		{
			name:     "empty slice",
			modelIDs: []string{},
			want:     nil,
		},
		{
			name:     "nil slice",
			modelIDs: nil,
			want:     nil,
		},
		{
			name:     "single model ID",
			modelIDs: []string{"gpt-4"},
			want: []ModelDescriptor{
				{ID: "gpt-4", Name: "gpt-4"},
			},
		},
		{
			name:     "multiple model IDs",
			modelIDs: []string{"gpt-4", "gpt-3.5-turbo"},
			want: []ModelDescriptor{
				{ID: "gpt-4", Name: "gpt-4"},
				{ID: "gpt-3.5-turbo", Name: "gpt-3.5-turbo"},
			},
		},
		{
			name:     "IDs with whitespace",
			modelIDs: []string{"  gpt-4  ", "\tgpt-3.5-turbo\t"},
			want: []ModelDescriptor{
				{ID: "gpt-4", Name: "gpt-4"},
				{ID: "gpt-3.5-turbo", Name: "gpt-3.5-turbo"},
			},
		},
		{
			name:     "empty strings are skipped",
			modelIDs: []string{"gpt-4", "", "  ", "gpt-3.5-turbo"},
			want: []ModelDescriptor{
				{ID: "gpt-4", Name: "gpt-4"},
				{ID: "gpt-3.5-turbo", Name: "gpt-3.5-turbo"},
			},
		},
		{
			name:     "all empty strings",
			modelIDs: []string{"", "  ", "\t"},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelDescriptorsFromIDs(tt.modelIDs)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d descriptors, got %d", len(tt.want), len(got))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID || got[i].Name != tt.want[i].Name {
					t.Fatalf("descriptor %d: expected %+v, got %+v", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestFirstPositiveInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []any
		want   int
	}{
		{
			name:   "empty values",
			values: []any{},
			want:   0,
		},
		{
			name:   "int positive",
			values: []any{int(100)},
			want:   100,
		},
		{
			name:   "int negative returns zero",
			values: []any{int(-100)},
			want:   0,
		},
		{
			name:   "int zero returns zero",
			values: []any{int(0)},
			want:   0,
		},
		{
			name:   "int32 positive",
			values: []any{int32(200)},
			want:   200,
		},
		{
			name:   "int32 negative returns zero",
			values: []any{int32(-200)},
			want:   0,
		},
		{
			name:   "int64 positive",
			values: []any{int64(300)},
			want:   300,
		},
		{
			name:   "int64 negative returns zero",
			values: []any{int64(-300)},
			want:   0,
		},
		{
			name:   "float32 positive",
			values: []any{float32(400.5)},
			want:   400,
		},
		{
			name:   "float32 negative returns zero",
			values: []any{float32(-400.5)},
			want:   0,
		},
		{
			name:   "float64 positive",
			values: []any{float64(500.7)},
			want:   500,
		},
		{
			name:   "float64 negative returns zero",
			values: []any{float64(-500.7)},
			want:   0,
		},
		{
			name:   "first positive wins",
			values: []any{0, -10, 100, 200},
			want:   100,
		},
		{
			name:   "all negative or zero",
			values: []any{-1, -2, 0, -3},
			want:   0,
		},
		{
			name:   "mixed types",
			values: []any{int(0), int32(-10), int64(1000), float64(2000)},
			want:   1000,
		},
		{
			name:   "non-numeric type ignored",
			values: []any{"100", 200},
			want:   200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstPositiveInt(tt.values...)
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

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
