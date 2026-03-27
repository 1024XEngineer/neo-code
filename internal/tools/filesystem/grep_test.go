package filesystem

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/tools"
)

func TestGrepToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "a.txt"), "hello world\nno match\n")
	mustWriteFile(t, filepath.Join(workspace, "sub", "b.go"), "package main\nprintln(\"hello\")\n")
	mustWriteFile(t, filepath.Join(workspace, "node_modules", "skip.txt"), "hello from dependency\n")

	tests := []struct {
		name           string
		pattern        string
		dir            string
		useRegex       bool
		expectContains []string
		expectErr      string
		expectNoMatch  bool
	}{
		{
			name:           "literal search across workspace",
			pattern:        "hello",
			expectContains: []string{"a.txt:1: hello world", normalizeSlashPath(filepath.Join("sub", "b.go")) + ":2: println(\"hello\")"},
		},
		{
			name:           "regex search scoped to directory",
			pattern:        `println\("hello"\)`,
			dir:            "sub",
			useRegex:       true,
			expectContains: []string{normalizeSlashPath(filepath.Join("sub", "b.go")) + ":2: println(\"hello\")"},
		},
		{
			name:      "invalid regex",
			pattern:   "[",
			useRegex:  true,
			expectErr: "invalid regex",
		},
		{
			name:          "no matches",
			pattern:       "goodbye",
			expectNoMatch: true,
		},
		{
			name:      "invalid scoped dir traversal",
			pattern:   "hello",
			dir:       "..",
			expectErr: "path escapes workspace root",
		},
	}

	tool := NewGrep(workspace)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args, err := json.Marshal(map[string]any{
				"pattern":   tt.pattern,
				"dir":       tt.dir,
				"use_regex": tt.useRegex,
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
			if tt.expectNoMatch {
				if result.Content != "no matches" {
					t.Fatalf("expected no matches, got %q", result.Content)
				}
				return
			}
			for _, expected := range tt.expectContains {
				if !strings.Contains(normalizeSlashPath(result.Content), normalizeSlashPath(expected)) {
					t.Fatalf("expected result to contain %q, got %q", expected, result.Content)
				}
			}
			if strings.Contains(result.Content, "dependency") {
				t.Fatalf("expected node_modules content to be skipped, got %q", result.Content)
			}
		})
	}
}
