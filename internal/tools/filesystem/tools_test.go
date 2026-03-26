package filesystem

import (
	"context"
	"encoding/json"
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
