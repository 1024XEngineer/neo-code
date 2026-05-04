package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestCopyFileTool_DuplicatesContent(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "a.go")
	if err := os.WriteFile(src, []byte("package main"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "a.go",
		"destination_path": filepath.Join("nested", "b.go"),
	})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}
	if !result.Facts.WorkspaceWrite {
		t.Fatalf("expected WorkspaceWrite=true")
	}
	srcData, _ := os.ReadFile(src)
	if string(srcData) != "package main" {
		t.Fatalf("source modified: %q", string(srcData))
	}
	dst := filepath.Join(workspace, "nested", "b.go")
	dstData, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(dstData) != "package main" {
		t.Fatalf("dst content = %q", string(dstData))
	}
	paths, ok := result.Metadata["paths"].([]string)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths metadata = %#v want 1-item slice", result.Metadata["paths"])
	}
}

func TestCopyFileTool_RefusesOverwriteByDefault(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "src.txt")
	dst := filepath.Join(workspace, "dst.txt")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("b"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "src.txt",
		"destination_path": "dst.txt",
	})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected exists error, got %v", err)
	}
	if data, _ := os.ReadFile(dst); string(data) != "b" {
		t.Fatalf("dst was clobbered: %q", string(data))
	}
}

func TestCopyFileTool_OverwriteAllowed(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "src.txt")
	dst := filepath.Join(workspace, "dst.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "src.txt",
		"destination_path": "dst.txt",
		"overwrite":        true,
	})
	if _, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if data, _ := os.ReadFile(dst); string(data) != "new" {
		t.Fatalf("dst content = %q want new", string(data))
	}
	if data, _ := os.ReadFile(src); string(data) != "new" {
		t.Fatalf("src removed unexpectedly: %q", string(data))
	}
}

func TestCopyFileTool_RejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "src.txt",
		"destination_path": filepath.Join("..", "escape.txt"),
	})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestCopyFileTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCopy(workspace)
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: []byte(`{invalid`),
		Workdir:   workspace,
	})
	if err == nil {
		t.Fatalf("expected json error")
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
}

func TestCopyFileTool_RejectsDirectorySource(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	sourceDir := filepath.Join(workspace, "srcdir")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "srcdir",
		"destination_path": "copy.txt",
	})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "must be a file") {
		t.Fatalf("expected directory source error, got %v", err)
	}
}

func TestCopyFileTool_RejectsCanceledContext(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCopy(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "src.txt",
		"destination_path": "dst.txt",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}
