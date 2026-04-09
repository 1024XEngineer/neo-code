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

func TestReadFileToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	largeContent := strings.Repeat("chunk-data-", 500)

	if err := os.WriteFile(filepath.Join(workspace, "small.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("API_KEY=test"), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	largePath := filepath.Join(workspace, "nested", "large.txt")
	if err := os.WriteFile(largePath, []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	tests := []struct {
		name          string
		path          string
		expectErr     string
		expectContent string
		expectChunks  int
	}{
		{
			name:          "read relative path",
			path:          "small.txt",
			expectContent: "hello world",
		},
		{
			name:          "read absolute path with chunk emitter",
			path:          largePath,
			expectContent: largeContent,
			expectChunks:  2,
		},
		{
			name:      "missing path",
			path:      "",
			expectErr: "path is required",
		},
		{
			name:      "reject path traversal",
			path:      filepath.Join("..", "outside.txt"),
			expectErr: "path escapes workspace root",
		},
		{
			name:      "reject sensitive file",
			path:      ".env",
			expectErr: "blocked by security policy (sensitive_path)",
		},
	}

	tool := New(workspace)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"path": tt.path})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			chunks := 0
			result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   workspace,
				EmitChunk: func(chunk []byte) {
					if len(chunk) > 0 {
						chunks++
					}
				},
			})

			if tt.expectErr != "" {
				if execErr == nil || !strings.Contains(execErr.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, execErr)
				}
				return
			}
			if execErr != nil {
				t.Fatalf("unexpected error: %v", execErr)
			}
			if result.Content != tt.expectContent {
				t.Fatalf("expected content length %d, got %d", len(tt.expectContent), len(result.Content))
			}
			if result.Metadata["path"] == "" {
				t.Fatalf("expected metadata path")
			}
			if tt.expectChunks > 0 && chunks < tt.expectChunks {
				t.Fatalf("expected at least %d chunks, got %d", tt.expectChunks, chunks)
			}
		})
	}
}

func TestReadFileToolErrorFormattingAndTruncation(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	largeContent := strings.Repeat("abcdefghij", 7000)
	largePath := filepath.Join(workspace, "large.txt")
	if err := os.WriteFile(largePath, []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	tool := New(workspace)
	tests := []struct {
		name           string
		arguments      []byte
		expectErr      string
		expectContent  []string
		expectTruncate bool
	}{
		{
			name:          "invalid json arguments",
			arguments:     []byte(`{invalid`),
			expectErr:     "invalid character",
			expectContent: []string{"tool error", "tool: filesystem_read_file", "reason: invalid arguments"},
		},
		{
			name:           "large file is truncated",
			arguments:      mustMarshalFSArgs(t, map[string]string{"path": "large.txt"}),
			expectContent:  []string{"...[truncated]"},
			expectTruncate: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunks := 0
			result, err := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: tt.arguments,
				Workdir:   workspace,
				EmitChunk: func(chunk []byte) {
					if len(chunk) > 0 {
						chunks++
					}
				},
			})

			if tt.expectErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.expectErr)) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, fragment := range tt.expectContent {
				if !strings.Contains(result.Content, fragment) {
					t.Fatalf("expected content containing %q, got %q", fragment, result.Content)
				}
			}
			if truncated, _ := result.Metadata["truncated"].(bool); truncated != tt.expectTruncate {
				t.Fatalf("expected truncated=%v, got %#v", tt.expectTruncate, result.Metadata["truncated"])
			}
			if tt.expectTruncate && chunks == 0 {
				t.Fatalf("expected chunk emitter to receive truncated content")
			}
		})
	}
}
