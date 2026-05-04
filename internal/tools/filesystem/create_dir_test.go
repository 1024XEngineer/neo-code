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

func TestCreateDirTool_RecursiveByDefault(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCreateDir(workspace)
	args, _ := json.Marshal(map[string]any{
		"path": filepath.Join("a", "b", "c"),
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
	target := filepath.Join(workspace, "a", "b", "c")
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: info=%v err=%v", info, err)
	}
	if got, _ := result.Metadata["created"].(bool); !got {
		t.Fatalf("created metadata = %v want true", result.Metadata["created"])
	}
}

func TestCreateDirTool_NonRecursiveFailsForMissingParent(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCreateDir(workspace)
	args, _ := json.Marshal(map[string]any{
		"path":      filepath.Join("missing", "child"),
		"recursive": false,
	})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil {
		t.Fatalf("expected error for missing parent")
	}
}

func TestCreateDirTool_ExistingDirReturnsNoop(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "existing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewCreateDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "existing"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got, _ := result.Metadata["noop_write"].(bool); !got {
		t.Fatalf("noop_write metadata = %v", result.Metadata["noop_write"])
	}
	if got, _ := result.Metadata["created"].(bool); got {
		t.Fatalf("created metadata = %v want false", result.Metadata["created"])
	}
}

func TestCreateDirTool_RejectsExistingFile(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	target := filepath.Join(workspace, "blocker")
	if err := os.WriteFile(target, []byte("file"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewCreateDir(workspace)
	args, _ := json.Marshal(map[string]any{"path": "blocker"})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected file-blocking error, got %v", err)
	}
}

func TestCreateDirTool_RejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCreateDir(workspace)
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

func TestCreateDirTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewCreateDir(workspace)
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
