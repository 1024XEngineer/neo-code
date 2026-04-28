package hooks

import "testing"

func TestHookContextCloneDeepCopyMetadata(t *testing.T) {
	t.Parallel()

	original := HookContext{
		RunID:     "run-1",
		SessionID: "session-1",
		Metadata: map[string]any{
			"slice": []any{"a", map[string]any{"k": "v"}},
			"map":   map[string]any{"nested": []string{"x", "y"}},
		},
	}

	cloned := original.Clone()
	metadataSlice, ok := cloned.Metadata["slice"].([]any)
	if !ok {
		t.Fatalf("slice metadata type = %T, want []any", cloned.Metadata["slice"])
	}
	nestedMap, ok := metadataSlice[1].(map[string]any)
	if !ok {
		t.Fatalf("nested map type = %T, want map[string]any", metadataSlice[1])
	}
	nestedMap["k"] = "changed"

	clonedMap, ok := cloned.Metadata["map"].(map[string]any)
	if !ok {
		t.Fatalf("map metadata type = %T, want map[string]any", cloned.Metadata["map"])
	}
	nestedSlice, ok := clonedMap["nested"].([]string)
	if !ok {
		t.Fatalf("nested slice type = %T, want []string", clonedMap["nested"])
	}
	nestedSlice[0] = "changed"

	originalSlice := original.Metadata["slice"].([]any)
	originalNestedMap := originalSlice[1].(map[string]any)
	if got := originalNestedMap["k"]; got != "v" {
		t.Fatalf("original nested map value = %v, want v", got)
	}
	originalMap := original.Metadata["map"].(map[string]any)
	originalNestedSlice := originalMap["nested"].([]string)
	if got := originalNestedSlice[0]; got != "x" {
		t.Fatalf("original nested slice value = %q, want x", got)
	}
}
