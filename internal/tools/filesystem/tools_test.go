package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neocode/internal/tools"
)

func TestReadFileRejectsPathEscape(t *testing.T) {
	workdir := t.TempDir()
	tool := NewReadFileTool()

	args, err := json.Marshal(map[string]any{"path": "../secret.txt"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	_, err = tool.Execute(context.Background(), tools.Invocation{
		ID:        "call-1",
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workdir,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workdir") {
		t.Fatalf("expected path escape error, got %v", err)
	}
}

func TestWriteAndReadFile(t *testing.T) {
	workdir := t.TempDir()
	writeTool := NewWriteFileTool()
	readTool := NewReadFileTool()

	writeArgs, err := json.Marshal(map[string]any{
		"path":    "notes/test.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("marshal write args: %v", err)
	}

	if _, err := writeTool.Execute(context.Background(), tools.Invocation{
		ID:        "call-1",
		Name:      writeTool.Name(),
		Arguments: writeArgs,
		Workdir:   workdir,
	}); err != nil {
		t.Fatalf("write tool returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workdir, "notes", "test.txt")); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	readArgs, err := json.Marshal(map[string]any{
		"path": "notes/test.txt",
	})
	if err != nil {
		t.Fatalf("marshal read args: %v", err)
	}

	result, err := readTool.Execute(context.Background(), tools.Invocation{
		ID:        "call-2",
		Name:      readTool.Name(),
		Arguments: readArgs,
		Workdir:   workdir,
	})
	if err != nil {
		t.Fatalf("read tool returned error: %v", err)
	}
	if !strings.Contains(result.Content, "hello world") {
		t.Fatalf("expected file content in result, got %q", result.Content)
	}
}

func TestEditFileReplacesUniqueSnippet(t *testing.T) {
	workdir := t.TempDir()
	target := filepath.Join(workdir, "notes.txt")
	if err := os.WriteFile(target, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := NewEditFileTool()
	args, err := json.Marshal(map[string]any{
		"path":     "notes.txt",
		"old_text": "beta",
		"new_text": "delta",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result, err := tool.Execute(context.Background(), tools.Invocation{
		ID:        "call-edit-1",
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workdir,
	})
	if err != nil {
		t.Fatalf("edit tool returned error: %v", err)
	}
	if !strings.Contains(result.Content, "edited") {
		t.Fatalf("expected success content, got %q", result.Content)
	}

	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if got := string(payload); got != "alpha\ndelta\ngamma\n" {
		t.Fatalf("unexpected edited content: %q", got)
	}
}

func TestEditFileRejectsPathEscape(t *testing.T) {
	workdir := t.TempDir()
	tool := NewEditFileTool()

	args, err := json.Marshal(map[string]any{
		"path":     "../secret.txt",
		"old_text": "alpha",
		"new_text": "beta",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	_, err = tool.Execute(context.Background(), tools.Invocation{
		ID:        "call-edit-escape",
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workdir,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workdir") {
		t.Fatalf("expected path escape error, got %v", err)
	}
}

func TestEditFileRejectsInvalidTargetsAndArguments(t *testing.T) {
	workdir := t.TempDir()
	tool := NewEditFileTool()

	tests := []struct {
		name    string
		args    map[string]any
		setup   func(t *testing.T)
		wantErr string
	}{
		{
			name: "missing old text",
			args: map[string]any{
				"path":     "notes.txt",
				"old_text": "",
				"new_text": "beta",
			},
			wantErr: "old_text is required",
		},
		{
			name: "missing file",
			args: map[string]any{
				"path":     "missing.txt",
				"old_text": "alpha",
				"new_text": "beta",
			},
			wantErr: "cannot find the file specified",
		},
		{
			name: "directory target",
			args: map[string]any{
				"path":     "folder",
				"old_text": "alpha",
				"new_text": "beta",
			},
			setup: func(t *testing.T) {
				if err := os.Mkdir(filepath.Join(workdir, "folder"), 0o755); err != nil {
					t.Fatalf("create directory: %v", err)
				}
			},
			wantErr: "is a directory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}

			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			_, err = tool.Execute(context.Background(), tools.Invocation{
				ID:        "call-edit-invalid",
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   workdir,
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEditFileRejectsMissingAndAmbiguousMatches(t *testing.T) {
	workdir := t.TempDir()
	target := filepath.Join(workdir, "notes.txt")
	if err := os.WriteFile(target, []byte("alpha\nbeta\nbeta\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := NewEditFileTool()
	tests := []struct {
		name    string
		oldText string
		wantErr string
	}{
		{
			name:    "not found",
			oldText: "gamma",
			wantErr: "re-read the file with fs_read_file",
		},
		{
			name:    "ambiguous",
			oldText: "beta",
			wantErr: "provide a longer, unique snippet",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{
				"path":     "notes.txt",
				"old_text": tc.oldText,
				"new_text": "delta",
			})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			_, err = tool.Execute(context.Background(), tools.Invocation{
				ID:        "call-edit-match",
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   workdir,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEditFileRejectsOversizedInputs(t *testing.T) {
	workdir := t.TempDir()
	tool := NewEditFileTool()

	oversizedSource := strings.Repeat("a", maxWriteBytes+1)
	if err := os.WriteFile(filepath.Join(workdir, "source.txt"), []byte(oversizedSource), 0o644); err != nil {
		t.Fatalf("seed oversized source: %v", err)
	}

	args, err := json.Marshal(map[string]any{
		"path":     "source.txt",
		"old_text": "a",
		"new_text": "b",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	_, err = tool.Execute(context.Background(), tools.Invocation{
		ID:        "call-edit-source-limit",
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workdir,
	})
	if err == nil || !strings.Contains(err.Error(), "file exceeds") {
		t.Fatalf("expected oversized source error, got %v", err)
	}

	withinLimitSource := strings.Repeat("a", maxWriteBytes)
	if err := os.WriteFile(filepath.Join(workdir, "expand.txt"), []byte(withinLimitSource), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	args, err = json.Marshal(map[string]any{
		"path":     "expand.txt",
		"old_text": withinLimitSource,
		"new_text": fmt.Sprintf("%sa", withinLimitSource),
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	_, err = tool.Execute(context.Background(), tools.Invocation{
		ID:        "call-edit-result-limit",
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workdir,
	})
	if err == nil || !strings.Contains(err.Error(), "edited content exceeds") {
		t.Fatalf("expected oversized result error, got %v", err)
	}
}
