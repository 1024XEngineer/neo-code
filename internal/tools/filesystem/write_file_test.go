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

func TestWriteFileToolMetadataAndExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewWrite(workspace)

	if tool.Name() != writeFileToolName {
		t.Fatalf("expected tool name %q, got %q", writeFileToolName, tool.Name())
	}
	if tool.Description() == "" {
		t.Fatalf("expected non-empty description")
	}
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Fatalf("expected schema object, got %+v", schema)
	}

	tests := []struct {
		name       string
		ctx        func() context.Context
		path       string
		content    string
		expectErr  string
		expectPath string
	}{
		{
			name:       "creates nested file",
			ctx:        context.Background,
			path:       filepath.Join("nested", "dir", "note.txt"),
			content:    "hello",
			expectPath: filepath.Join(workspace, "nested", "dir", "note.txt"),
		},
		{
			name:      "rejects empty path",
			ctx:       context.Background,
			path:      "",
			content:   "hello",
			expectErr: "path is required",
		},
		{
			name:      "rejects path traversal",
			ctx:       context.Background,
			path:      filepath.Join("..", "escape.txt"),
			content:   "hello",
			expectErr: "path escapes workspace root",
		},
		{
			name: "respects canceled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			path:      "canceled.txt",
			content:   "hello",
			expectErr: "context canceled",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{
				"path":    tt.path,
				"content": tt.content,
			})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, execErr := tool.Execute(tt.ctx(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   workspace,
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
			if result.Content != "ok" {
				t.Fatalf("expected ok result, got %q", result.Content)
			}

			data, err := os.ReadFile(tt.expectPath)
			if err != nil {
				t.Fatalf("read written file: %v", err)
			}
			if string(data) != tt.content {
				t.Fatalf("expected content %q, got %q", tt.content, string(data))
			}
		})
	}
}
