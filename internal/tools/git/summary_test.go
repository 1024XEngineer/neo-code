package git

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestSummaryToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewSummary("/nonexistent")
	if tool.Name() != "git_summary" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "git_summary")
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

func TestSummaryToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewSummary("/nonexistent")
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: []byte(`{invalid`),
	})
	if err == nil {
		t.Fatalf("expected error for invalid JSON, got result: %+v", result)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError result")
	}
}

func TestSummaryToolNonGitDirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewSummary(workspace)
	args := mustArgs(t, map[string]any{})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "in_git_repo: false") {
		t.Fatalf("expected non-git summary, got %q", result.Content)
	}
}

func mustArgs(t *testing.T, v map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
