package filesystem

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/tools"
)

func TestGlobToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(workspace, "README.md"), "# readme\n")
	mustWriteFile(t, filepath.Join(workspace, "internal", "app", "app.go"), "package app\n")
	mustWriteFile(t, filepath.Join(workspace, "node_modules", "skip.go"), "package skip\n")

	tests := []struct {
		name           string
		pattern        string
		dir            string
		expectContains []string
		expectErr      string
		expectNoMatch  bool
	}{
		{
			name:           "glob go files recursively",
			pattern:        "**/*.go",
			expectContains: []string{"main.go", normalizeSlashPath(filepath.Join("internal", "app", "app.go"))},
		},
		{
			name:           "scope to directory",
			pattern:        "**/*.go",
			dir:            "internal",
			expectContains: []string{normalizeSlashPath(filepath.Join("internal", "app", "app.go"))},
		},
		{
			name:          "no matches",
			pattern:       "**/*.py",
			expectNoMatch: true,
		},
		{
			name:      "empty pattern",
			pattern:   "",
			expectErr: "pattern is required",
		},
	}

	tool := NewGlob(workspace)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args, err := json.Marshal(map[string]string{
				"pattern": tt.pattern,
				"dir":     tt.dir,
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
			normalizedContent := normalizeSlashPath(result.Content)
			for _, expected := range tt.expectContains {
				if !strings.Contains(normalizedContent, normalizeSlashPath(expected)) {
					t.Fatalf("expected result to contain %q, got %q", expected, result.Content)
				}
			}
			if strings.Contains(normalizedContent, "node_modules") {
				t.Fatalf("expected node_modules files to be skipped, got %q", result.Content)
			}
		})
	}
}

func TestBuildGlobMatcherRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	_, err := buildGlobMatcher(string([]byte{0xff}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "utf-8") {
		t.Fatalf("expected invalid utf-8 error, got %v", err)
	}
}
