package codebase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

func TestReadToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewRead(repository.NewService(), "/workspace")
	if tool.Name() != "codebase_read" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "codebase_read")
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
		t.Fatalf("Schema properties should be a map, got %T", schema["properties"])
	}
	if _, hasPath := props["path"]; !hasPath {
		t.Fatalf("Schema should have path property")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyPreserveHistory {
		t.Fatalf("MicroCompactPolicy() = %v, want PreserveHistory", tool.MicroCompactPolicy())
	}
}

func TestReadToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewRead(repository.NewService(), "/workspace")
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

func TestReadToolMissingPath(t *testing.T) {
	t.Parallel()

	tool := NewRead(repository.NewService(), "/workspace")
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{}),
	})
	if err == nil {
		t.Fatalf("expected error for missing path")
	}
	if !result.IsError {
		t.Fatalf("expected IsError result")
	}
}

func TestReadToolFileNotFound(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewRead(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"path": "nonexistent.go"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "file not found") {
		t.Fatalf("expected 'file not found' message, got %q", result.Content)
	}
}

func TestReadToolReadsFileContent(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := "package main\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewRead(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"path": "main.go"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "package main") {
		t.Fatalf("expected file content, got %q", result.Content)
	}
	metaPath, _ := result.Metadata["path"].(string)
	if metaPath != "main.go" {
		t.Fatalf("expected metadata path 'main.go', got %v", result.Metadata["path"])
	}
}

func TestReadToolPathTraversalRejected(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewRead(repository.NewService(), workspace)
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"path": "../../etc/passwd"}),
	})
	if err == nil {
		t.Fatalf("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected 'escapes workspace root' error, got %v", err)
	}
}

func TestReadToolFileInSubdirectory(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	subdir := filepath.Join(workspace, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "nested.go"), []byte("nested content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewRead(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"path": "sub/nested.go"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "nested content") {
		t.Fatalf("expected file content, got %q", result.Content)
	}
}
