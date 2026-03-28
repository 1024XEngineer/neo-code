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

func TestEditToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "main.go")
	repeatedPath := filepath.Join(workspace, "repeated.go")
	unchangedPath := filepath.Join(workspace, "same.txt")

	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(repeatedPath, []byte("old\nold\n"), 0o644); err != nil {
		t.Fatalf("write repeated.go: %v", err)
	}
	if err := os.WriteFile(unchangedPath, []byte("same"), 0o644); err != nil {
		t.Fatalf("write same.txt: %v", err)
	}

	tests := []struct {
		name       string
		path       string
		search     string
		replace    string
		expectErr  string
		expectFile string
	}{
		{
			name:       "replace unique block",
			path:       "main.go",
			search:     "println(\"old\")",
			replace:    "println(\"new\")",
			expectFile: "package main\n\nfunc main() {\n\tprintln(\"new\")\n}\n",
		},
		{
			name:      "search string not found",
			path:      "main.go",
			search:    "missing",
			replace:   "new",
			expectErr: "search_string not found",
		},
		{
			name:      "multiple matches are rejected",
			path:      "repeated.go",
			search:    "old",
			replace:   "new",
			expectErr: "matched 2 locations",
		},
		{
			name:      "replacement with identical content is rejected",
			path:      "same.txt",
			search:    "same",
			replace:   "same",
			expectErr: "replacement produced no changes",
		},
		{
			name:      "path traversal is rejected",
			path:      filepath.Join("..", "escape.txt"),
			search:    "old",
			replace:   "new",
			expectErr: "path escapes workspace root",
		},
	}

	tool := NewEdit(workspace)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{
				"path":           tt.path,
				"search_string":  tt.search,
				"replace_string": tt.replace,
			})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
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
				t.Fatalf("expected result content ok, got %q", result.Content)
			}

			data, err := os.ReadFile(filepath.Join(workspace, tt.path))
			if err != nil {
				t.Fatalf("read updated file: %v", err)
			}
			if string(data) != tt.expectFile {
				t.Fatalf("expected updated file %q, got %q", tt.expectFile, string(data))
			}
		})
	}
}

func TestEditToolSearchStringNotFound(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "main.go"), "package main\n")

	tool := NewEdit(workspace)
	args, err := json.Marshal(map[string]string{
		"path":           "main.go",
		"search_string":  "missing-block",
		"replace_string": "new-block",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	_, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if execErr == nil || !strings.Contains(execErr.Error(), "search_string not found") {
		t.Fatalf("expected search_string not found error, got %v", execErr)
	}
}
