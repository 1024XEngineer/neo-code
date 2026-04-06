package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/security"
)

type stubTool struct {
	name        string
	description string
	schema      map[string]any
	result      ToolResult
	err         error
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return s.description }
func (s stubTool) Schema() map[string]any {
	return s.schema
}
func (s stubTool) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	return s.result, s.err
}

func TestRegistryGetSpecsSorted(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "z_tool", description: "last", schema: map[string]any{"type": "object"}})
	registry.Register(stubTool{name: "a_tool", description: "first", schema: map[string]any{"type": "object"}})

	specs := registry.GetSpecs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "a_tool" || specs[1].Name != "z_tool" {
		t.Fatalf("expected sorted specs, got %+v", specs)
	}
}

func TestRegistryExecute(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{
		name:        "ok_tool",
		description: "success tool",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name:    "ok_tool",
			Content: "done",
		},
	})
	registry.Register(stubTool{
		name:        "error_tool",
		description: "error tool",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name: "error_tool",
		},
		err: errors.New("boom"),
	})
	registry.Register(stubTool{
		name:        "content_error_tool",
		description: "tool preserves own error content",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name:    "content_error_tool",
			Content: "explicit failure",
		},
		err: errors.New("boom"),
	})

	tests := []struct {
		name          string
		input         ToolCallInput
		expectErr     string
		expectContent string
		expectIsError bool
	}{
		{
			name: "dispatch success",
			input: ToolCallInput{
				ID:   "call-1",
				Name: "ok_tool",
			},
			expectContent: "done",
		},
		{
			name: "unknown tool",
			input: ToolCallInput{
				ID:   "call-2",
				Name: "missing_tool",
			},
			expectErr:     "tool: not found",
			expectContent: "tool error",
			expectIsError: true,
		},
		{
			name: "tool error falls back to returned error text",
			input: ToolCallInput{
				ID:   "call-3",
				Name: "error_tool",
			},
			expectErr:     "boom",
			expectContent: "tool error",
			expectIsError: true,
		},
		{
			name: "tool error preserves explicit content",
			input: ToolCallInput{
				ID:   "call-4",
				Name: "content_error_tool",
			},
			expectErr:     "boom",
			expectContent: "explicit failure",
			expectIsError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := registry.Execute(context.Background(), tt.input)
			if tt.expectErr != "" {
				if err == nil || err.Error() != tt.expectErr {
					t.Fatalf("expected error %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.ToolCallID != tt.input.ID {
				t.Fatalf("expected tool call id %q, got %q", tt.input.ID, result.ToolCallID)
			}
			if tt.expectContent != "" && !strings.Contains(result.Content, tt.expectContent) {
				t.Fatalf("expected content containing %q, got %q", tt.expectContent, result.Content)
			}
			if result.IsError != tt.expectIsError {
				t.Fatalf("expected IsError=%v, got %v", tt.expectIsError, result.IsError)
			}
		})
	}
}

func TestRegistryHelpers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(nil)
	registry.Register(stubTool{name: "a_tool", description: "first", schema: map[string]any{"type": "object"}})

	if !registry.Supports("a_tool") {
		t.Fatalf("expected registry to support a_tool")
	}
	if registry.Supports("missing") {
		t.Fatalf("did not expect registry to support missing tool")
	}

	schemas := registry.ListSchemas()
	if len(schemas) != 1 || schemas[0].Name != "a_tool" {
		t.Fatalf("unexpected schemas: %+v", schemas)
	}

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "a_tool" {
		t.Fatalf("unexpected specs: %+v", specs)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.ListAvailableSpecs(canceled, SpecListInput{})
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRegistryRememberSessionDecisionUnsupported(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	err := registry.RememberSessionDecision("session-1", security.Action{}, SessionPermissionScopeAlways)
	if err == nil {
		t.Fatalf("expected unsupported error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}
