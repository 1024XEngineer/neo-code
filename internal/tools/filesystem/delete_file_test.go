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

func TestDeleteFileTool_RemovesExistingFile(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	target := filepath.Join(workspace, "doomed.txt")
	if err := os.WriteFile(target, []byte("bye"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewDelete(workspace)
	args, _ := json.Marshal(map[string]any{"path": "doomed.txt"})
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
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	if got, _ := result.Metadata["deleted"].(bool); !got {
		t.Fatalf("deleted metadata = %v want true", result.Metadata["deleted"])
	}
}

func TestDeleteFileTool_MissingFileReturnsNoop(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewDelete(workspace)
	args, _ := json.Marshal(map[string]any{"path": "ghost.txt"})
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
	if got, _ := result.Metadata["noop_write"].(bool); !got {
		t.Fatalf("noop_write metadata = %v", result.Metadata["noop_write"])
	}
	if got, _ := result.Metadata["deleted"].(bool); got {
		t.Fatalf("deleted metadata = %v want false", result.Metadata["deleted"])
	}
}

func TestDeleteFileTool_RejectsDirectory(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "subdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tool := NewDelete(workspace)
	args, _ := json.Marshal(map[string]any{"path": "subdir"})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory error, got %v", err)
	}
}

func TestDeleteFileTool_RejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewDelete(workspace)
	args, _ := json.Marshal(map[string]any{"path": filepath.Join("..", "escape.txt")})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestDeleteFileTool_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewDelete(workspace)
	args, _ := json.Marshal(map[string]any{"path": ""})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path required, got %v", err)
	}
}

func TestDeleteFileTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewDelete(workspace)
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
