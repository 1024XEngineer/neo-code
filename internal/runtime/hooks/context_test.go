package hooks

import (
	"reflect"
	"testing"
	"time"
)

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

func TestHookContextCloneDeepCopyStructFields(t *testing.T) {
	t.Parallel()

	type nested struct {
		Flag bool
	}
	type metaPayload struct {
		Attrs map[string]string
		Items []int
		Ref   *nested
	}

	original := HookContext{
		Metadata: map[string]any{
			"struct": metaPayload{
				Attrs: map[string]string{"k": "v"},
				Items: []int{1, 2, 3},
				Ref:   &nested{Flag: true},
			},
		},
	}

	cloned := original.Clone()
	payload, ok := cloned.Metadata["struct"].(metaPayload)
	if !ok {
		t.Fatalf("struct metadata type = %T, want metaPayload", cloned.Metadata["struct"])
	}

	payload.Attrs["k"] = "changed"
	payload.Items[0] = 99
	payload.Ref.Flag = false
	cloned.Metadata["struct"] = payload

	originPayload := original.Metadata["struct"].(metaPayload)
	if got := originPayload.Attrs["k"]; got != "v" {
		t.Fatalf("original Attrs[k] = %q, want v", got)
	}
	if got := originPayload.Items[0]; got != 1 {
		t.Fatalf("original Items[0] = %d, want 1", got)
	}
	if got := originPayload.Ref.Flag; got != true {
		t.Fatalf("original Ref.Flag = %v, want true", got)
	}
}

func TestHookContextCloneNoMetadata(t *testing.T) {
	t.Parallel()

	original := HookContext{RunID: "run-1", SessionID: "session-1"}
	cloned := original.Clone()
	if cloned.RunID != original.RunID || cloned.SessionID != original.SessionID {
		t.Fatalf("Clone() basic fields mismatch: got %+v, want %+v", cloned, original)
	}
	if cloned.Metadata != nil {
		t.Fatalf("Clone().Metadata = %#v, want nil", cloned.Metadata)
	}
}

func TestCloneMetadataValueNil(t *testing.T) {
	t.Parallel()

	if got := cloneMetadataValue(nil); got != nil {
		t.Fatalf("cloneMetadataValue(nil) = %#v, want nil", got)
	}
}

func TestDeepCloneValueEdgeCases(t *testing.T) {
	t.Parallel()

	invalid := deepCloneValue(reflect.Value{})
	if invalid.IsValid() {
		t.Fatalf("deepCloneValue(invalid).IsValid() = true, want false")
	}

	var nilPtr *int
	clonedNilPtr := deepCloneValue(reflect.ValueOf(nilPtr))
	if !clonedNilPtr.IsNil() {
		t.Fatalf("cloned nil pointer should be nil")
	}

	var nilMap map[string]int
	clonedNilMap := deepCloneValue(reflect.ValueOf(nilMap))
	if !clonedNilMap.IsNil() {
		t.Fatalf("cloned nil map should be nil")
	}

	var nilSlice []int
	clonedNilSlice := deepCloneValue(reflect.ValueOf(nilSlice))
	if !clonedNilSlice.IsNil() {
		t.Fatalf("cloned nil slice should be nil")
	}

	// 走到 interface nil 分支。
	nilIfaceValue := reflect.New(reflect.TypeOf((*any)(nil)).Elem()).Elem()
	clonedNilIface := deepCloneValue(nilIfaceValue)
	if !clonedNilIface.IsNil() {
		t.Fatalf("cloned nil interface should be nil")
	}

	arr := [2]map[string]int{
		{"a": 1},
		{"b": 2},
	}
	clonedArr := deepCloneValue(reflect.ValueOf(arr)).Interface().([2]map[string]int)
	clonedArr[0]["a"] = 99
	if arr[0]["a"] != 1 {
		t.Fatalf("array nested map shared, got %d, want 1", arr[0]["a"])
	}

	// time.Time 的内部字段不可 set，可覆盖 struct 分支中的 CanSet=false 路径。
	valueWithTime := struct {
		When time.Time
	}{
		When: time.Now(),
	}
	clonedWithTime := deepCloneValue(reflect.ValueOf(valueWithTime)).Interface().(struct {
		When time.Time
	})
	if !clonedWithTime.When.Equal(valueWithTime.When) {
		t.Fatalf("cloned struct with time mismatch")
	}
}
