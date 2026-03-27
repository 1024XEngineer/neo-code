package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/tools"
)

func TestReadFileToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	largeContent := strings.Repeat("chunk-data-", 500)

	if err := os.WriteFile(filepath.Join(workspace, "small.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
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
