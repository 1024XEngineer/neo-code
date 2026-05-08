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

func TestSearchSymbolToolMetadata(t *testing.T) {
	t.Parallel()

	tool := NewSearchSymbol(repository.NewService(), "/workspace")
	if tool.Name() != "codebase_search_symbol" {
		t.Fatalf("Name() = %q, want %q", tool.Name(), "codebase_search_symbol")
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
	if _, hasSymbol := props["symbol"]; !hasSymbol {
		t.Fatalf("Schema should have symbol property")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyCompact {
		t.Fatalf("MicroCompactPolicy() = %v, want Compact", tool.MicroCompactPolicy())
	}
}

func TestSearchSymbolToolInvalidJSON(t *testing.T) {
	t.Parallel()

	tool := NewSearchSymbol(repository.NewService(), "/workspace")
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

func TestSearchSymbolToolMissingSymbol(t *testing.T) {
	t.Parallel()

	tool := NewSearchSymbol(repository.NewService(), "/workspace")
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("expected no error for missing symbol, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError result")
	}
	if !strings.Contains(result.Content, "missing required argument") {
		t.Fatalf("expected missing argument message, got %q", result.Content)
	}
}

func TestSearchSymbolToolFindsGoSymbol(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `package main

func Hello(name string) string {
	return "hi " + name
}

type MyStruct struct {
	Field int
}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchSymbol(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"symbol": "Hello"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "returned_count: 1") {
		t.Fatalf("expected 1 hit, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "Hello") {
		t.Fatalf("expected 'Hello' in result, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "function") {
		t.Fatalf("expected 'function' kind, got %q", result.Content)
	}
}

func TestSearchSymbolToolNoResults(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\nfunc foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchSymbol(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"symbol": "NonExistent"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "returned_count: 0") {
		t.Fatalf("expected 0 hits, got %q", result.Content)
	}
}

func TestSearchSymbolToolReturnsSignatureOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `package main

func longFunction(a string, b int, c float64) string {
	// This is a very long function body that should not be included
	return "result"
}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewSearchSymbol(repository.NewService(), workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: mustArgs(t, map[string]any{"symbol": "longFunction"}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain the signature but NOT the body
	if !strings.Contains(result.Content, "func longFunction") {
		t.Fatalf("expected signature, got %q", result.Content)
	}
	if strings.Contains(result.Content, "return \"result\"") {
		t.Fatalf("expected signature only, but got function body in %q", result.Content)
	}
}
