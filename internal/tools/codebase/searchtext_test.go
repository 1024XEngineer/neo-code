package codebase

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

func TestSearchTextToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewSearchText(repository.NewService(), "/workspace")
	if tool.Name() != "codebase_search_text" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "codebase_search_text")
	}
	if tool.Description() == "" {
		t.Fatalf("Description() should not be empty")
	}
	schema := tool.Schema()
	if schema == nil {
		t.Fatalf("Schema() should not be nil")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema properties should be a map")
	}
	if _, hasQuery := props["query"]; !hasQuery {
		t.Fatalf("Schema should have query property")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyCompact {
		t.Fatalf("MicroCompactPolicy() = %v, want Compact", tool.MicroCompactPolicy())
	}
}

func TestSearchTextToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewSearchText(repository.NewService(), "/workspace")
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

func TestSearchTextToolMissingQuery(t *testing.T) {
	t.Parallel()

	tool := NewSearchText(repository.NewService(), "/workspace")
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("expected no error for missing query, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError result")
	}
	if !strings.Contains(result.Content, "missing required argument") {
		t.Fatalf("expected missing argument message, got %q", result.Content)
	}
}

func TestSearchTextToolFindsMatches(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "test.go"), []byte("func Hello() {\n\treturn \"hello\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "other.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchText(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"query": "Hello"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "returned_count: 1") {
		t.Fatalf("expected 1 hit, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "test.go") {
		t.Fatalf("expected 'test.go' in result, got %q", result.Content)
	}
}

func TestSearchTextToolNoMatches(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "test.go"), []byte("func Hello() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchText(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"query": "NonExistentSymbol"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "returned_count: 0") {
		t.Fatalf("expected 0 hits, got %q", result.Content)
	}
}

func TestSearchTextToolRespectsScopeDir(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "root.go"), []byte("found"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	sub := filepath.Join(workspace, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.go"), []byte("found"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchText(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"query": "found", "scope_dir": "sub"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only find nested.go, not root.go
	if strings.Contains(result.Content, "root.go") {
		t.Fatalf("expected scope_dir to limit results, got root.go in %q", result.Content)
	}
	if !strings.Contains(result.Content, "nested.go") {
		t.Fatalf("expected nested.go in scoped results, got %q", result.Content)
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
