package git

import (
	"context"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestChangedFilesToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewChangedFiles("/nonexistent")
	if tool.Name() != "git_changed_files" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "git_changed_files")
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

func TestChangedFilesToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewChangedFiles("/nonexistent")
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

func TestChangedFilesToolNonGitDirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewChangedFiles(workspace)
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
	if !strings.Contains(result.Content, "total_count: 0") {
		t.Fatalf("expected total_count 0, got %q", result.Content)
	}
}
