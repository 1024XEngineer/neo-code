package config

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
			if got.ID != tt.want.ID {
				t.Fatalf("expected ID=%q, got %q", tt.want.ID, got.ID)
			}
			if got.Name != tt.want.Name {
				t.Fatalf("expected Name=%q, got %q", tt.want.Name, got.Name)
			}
			if got.Description != tt.want.Description {
				t.Fatalf("expected Description=%q, got %q", tt.want.Description, got.Description)
			}
			if got.ContextWindow != tt.want.ContextWindow {
				t.Fatalf("expected ContextWindow=%d, got %d", tt.want.ContextWindow, got.ContextWindow)
			}
			if got.MaxOutputTokens != tt.want.MaxOutputTokens {
				t.Fatalf("expected MaxOutputTokens=%d, got %d", tt.want.MaxOutputTokens, got.MaxOutputTokens)
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

func TestBoolMapValue(t *testing.T) {
	t.Parallel()

	result := boolMapValue(map[string]any{"a": true, "b": "notbool", "c": false})
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if !result["a"] {
		t.Fatalf("expected a=true")
	}
	if result["c"] {
		t.Fatalf("expected c=false")
	}

	if result := boolMapValue("not a map"); result != nil {
		t.Fatalf("expected nil for non-map, got %v", result)
	}
}

func TestMergeStringBoolMaps(t *testing.T) {
	t.Parallel()

	primary := map[string]bool{"a": true}
	secondary := map[string]bool{"b": false, "a": false}

	result := mergeStringBoolMaps(primary, secondary)
	if !result["a"] {
		t.Fatalf("expected a=true (primary should win)")
	}
	if result["b"] {
		t.Fatalf("expected b=false")
	}

	if result := mergeStringBoolMaps(nil, nil); result != nil {
		t.Fatalf("expected nil for both empty")
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
