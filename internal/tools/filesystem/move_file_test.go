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

func TestMoveFileTool_RenamesWithinWorkspace(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "old.go")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewMove(workspace)

	args, _ := json.Marshal(map[string]any{
		"source_path":      "old.go",
		"destination_path": "renamed.go",
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
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !result.Facts.WorkspaceWrite {
		t.Fatalf("expected WorkspaceWrite=true")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source still exists: err=%v", err)
	}
	dst := filepath.Join(workspace, "renamed.go")
	if data, err := os.ReadFile(dst); err != nil {
		t.Fatalf("read dst: %v", err)
	} else if string(data) != "hello" {
		t.Fatalf("dst content = %q want hello", string(data))
	}
	if got, ok := result.Metadata["destination_path"].(string); !ok || !strings.EqualFold(got, dst) {
		t.Fatalf("destination_path metadata = %v want %v", got, dst)
	}
	paths, ok := result.Metadata["paths"].([]string)
	if !ok || len(paths) != 2 {
		t.Fatalf("paths metadata = %#v, want 2-item slice", result.Metadata["paths"])
	}
}

func TestMoveFileTool_RejectsExistingDestinationWithoutOverwrite(t *testing.T) {
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
	tool := NewMove(workspace)
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
		t.Fatalf("dst content modified, got %q want b", string(data))
	}
}

func TestMoveFileTool_OverwritesWhenAllowed(t *testing.T) {
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
	tool := NewMove(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "src.txt",
		"destination_path": "dst.txt",
		"overwrite":        true,
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
	if data, _ := os.ReadFile(dst); string(data) != "new" {
		t.Fatalf("dst content = %q want new", string(data))
	}
}

func TestMoveFileTool_RejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	src := filepath.Join(workspace, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tool := NewMove(workspace)
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

func TestMoveFileTool_RejectsMissingSource(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewMove(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "missing.txt",
		"destination_path": "out.txt",
	})
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if err == nil {
		t.Fatalf("expected error for missing source")
	}
}

func TestMoveFileTool_RejectsEmptyPaths(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewMove(workspace)

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "empty source",
			args: map[string]any{"source_path": "", "destination_path": "x.txt"},
			want: "source_path is required",
		},
		{
			name: "empty destination",
			args: map[string]any{"source_path": "x.txt", "destination_path": ""},
			want: "destination_path is required",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(tc.args)
			_, err := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   workspace,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestMoveFileTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewMove(workspace)
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

func TestMoveFileTool_RejectsDirectorySource(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	sourceDir := filepath.Join(workspace, "srcdir")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	tool := NewMove(workspace)
	args, _ := json.Marshal(map[string]any{
		"source_path":      "srcdir",
		"destination_path": "moved.txt",
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

func TestMoveFileTool_RejectsCanceledContext(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	tool := NewMove(workspace)
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
