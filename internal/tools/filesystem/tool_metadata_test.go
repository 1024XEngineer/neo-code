package filesystem

import (
	"errors"
	"testing"

	"neo-code/internal/tools"
)

func TestFilesystemToolMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolName    string
		description string
		schema      map[string]any
		policy      tools.MicroCompactPolicy
	}{
		{
			name:        "copy",
			toolName:    NewCopy("/workspace").Name(),
			description: NewCopy("/workspace").Description(),
			schema:      NewCopy("/workspace").Schema(),
			policy:      NewCopy("/workspace").MicroCompactPolicy(),
		},
		{
			name:        "move",
			toolName:    NewMove("/workspace").Name(),
			description: NewMove("/workspace").Description(),
			schema:      NewMove("/workspace").Schema(),
			policy:      NewMove("/workspace").MicroCompactPolicy(),
		},
		{
			name:        "create dir",
			toolName:    NewCreateDir("/workspace").Name(),
			description: NewCreateDir("/workspace").Description(),
			schema:      NewCreateDir("/workspace").Schema(),
			policy:      NewCreateDir("/workspace").MicroCompactPolicy(),
		},
		{
			name:        "delete file",
			toolName:    NewDelete("/workspace").Name(),
			description: NewDelete("/workspace").Description(),
			schema:      NewDelete("/workspace").Schema(),
			policy:      NewDelete("/workspace").MicroCompactPolicy(),
		},
		{
			name:        "remove dir",
			toolName:    NewRemoveDir("/workspace").Name(),
			description: NewRemoveDir("/workspace").Description(),
			schema:      NewRemoveDir("/workspace").Schema(),
			policy:      NewRemoveDir("/workspace").MicroCompactPolicy(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.toolName == "" {
				t.Fatal("tool name should not be empty")
			}
			if tt.description == "" {
				t.Fatal("description should not be empty")
			}
			if got, _ := tt.schema["type"].(string); got != "object" {
				t.Fatalf("schema type = %q, want object", got)
			}
			required, ok := tt.schema["required"].([]string)
			if !ok || len(required) == 0 {
				t.Fatalf("required schema fields missing: %#v", tt.schema["required"])
			}
			if tt.policy != tools.MicroCompactPolicyCompact {
				t.Fatalf("policy = %q, want compact", tt.policy)
			}
		})
	}
}

func TestMoveCrossDeviceHelper(t *testing.T) {
	t.Parallel()

	if !isCrossDeviceLinkError(errors.New("rename failed: cross-device link")) {
		t.Fatal("cross-device error should be detected")
	}
	if !isCrossDeviceLinkError(errors.New("EXDEV: invalid cross-device link")) {
		t.Fatal("EXDEV error should be detected")
	}
	if isCrossDeviceLinkError(errors.New("permission denied")) {
		t.Fatal("unrelated error should not be detected as cross-device")
	}
	if isCrossDeviceLinkError(nil) {
		t.Fatal("nil error should not be detected as cross-device")
	}
}
