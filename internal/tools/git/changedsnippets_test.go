package git

import (
	"context"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestChangedSnippetsToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewChangedSnippets("/nonexistent")
	if tool.Name() != "git_changed_snippets" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "git_changed_snippets")
	}
	if tool.Description() == "" {
		t.Fatalf("Description() should not be empty")
	}
	if tool.Schema() == nil {
		t.Fatalf("Schema() should not be nil")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyCompact {
		t.Fatalf("MicroCompactPolicy() = %v, want Compact", tool.MicroCompactPolicy())
	}
}

func TestChangedSnippetsToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewChangedSnippets("/nonexistent")
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: []byte(`{invalid`),
	})
	if err == nil {
		t.Fatalf("expected error for invalid JSON, got result: %+v", result)
	}
	if !result.IsError {
		t.Fatalf("expected IsError result")
	}
}

func TestChangedSnippetsToolNonGitDirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewChangedSnippets(workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "returned_count: 0") {
		t.Fatalf("expected empty file list, got %q", result.Content)
	}
}
