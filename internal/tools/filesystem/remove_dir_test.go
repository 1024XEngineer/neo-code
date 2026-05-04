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

func TestRemoveDirTool_RemovesEmptyDir(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "empty"})
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
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
}

func TestRemoveDirTool_RefusesNonEmptyWithoutForce(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "full")
	child := filepath.Join(dir, "x.txt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "full"})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil {
		t.Fatalf("expected error for non-empty directory")
	}
	if _, err := os.Stat(child); err != nil {
		t.Fatalf("child file destroyed: %v", err)
	}
}

func TestRemoveDirTool_ForceRemovesRecursive(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "tree")
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{
		"path":  "tree",
		"force": true,
	})
	if _, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
}

func TestRemoveDirTool_MissingDirReturnsNoop(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "phantom"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got, _ := result.Metadata["noop_write"].(bool); !got {
		t.Fatalf("noop_write = %v", result.Metadata["noop_write"])
	}
	if got, _ := result.Metadata["removed"].(bool); got {
		t.Fatalf("removed = %v want false", result.Metadata["removed"])
	}
}

func TestRemoveDirTool_RejectsFile(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	target := filepath.Join(workspace, "afile.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "afile.txt"})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected directory-required error, got %v", err)
	}
}

func TestRemoveDirTool_RejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewRemoveDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": filepath.Join("..", "escape")})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestRemoveDirTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewRemoveDir(workspace)
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
