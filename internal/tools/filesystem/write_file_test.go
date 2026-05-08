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
	properties, _ := schema["properties"].(map[string]any)
	if _, ok := properties["verify_after_write"]; !ok {
		t.Fatalf("expected verify_after_write in schema properties")
	}
	if _, ok := properties["verification_scope"]; !ok {
		t.Fatalf("expected verification_scope in schema properties")
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
			if !result.Facts.WorkspaceWrite {
				t.Fatalf("expected WorkspaceWrite=true for fresh write, got false")
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

func TestWriteFileToolInvalidArgumentsFormatting(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewWrite(workspace)

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: []byte(`{invalid`),
		Workdir:   workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("expected invalid json error, got %v", err)
	}
	for _, fragment := range []string{"tool error", "tool: filesystem_write_file", "reason: invalid arguments"} {
		if !strings.Contains(result.Content, fragment) {
			t.Fatalf("expected content containing %q, got %q", fragment, result.Content)
		}
	}
	if !result.IsError {
		t.Fatalf("expected error result, got %#v", result)
	}
}

func TestWriteFileToolNoopWriteMetadata(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewWrite(workspace)
	target := filepath.Join(workspace, "same.txt")
	if err := os.WriteFile(target, []byte("same"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	args, err := json.Marshal(map[string]string{
		"path":    "same.txt",
		"content": "same",
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
		Workdir:   workspace,
	})
	if execErr != nil {
		t.Fatalf("Execute() error = %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("result content = %q, want ok", result.Content)
	}
	noop, ok := result.Metadata["noop_write"].(bool)
	if !ok || !noop {
		t.Fatalf("noop_write metadata = %#v, want true", result.Metadata["noop_write"])
	}
	unchanged, ok := result.Metadata["content_unchanged"].(bool)
	if !ok || !unchanged {
		t.Fatalf("content_unchanged metadata = %#v, want true", result.Metadata["content_unchanged"])
	}
	if result.Facts.WorkspaceWrite {
		t.Fatalf("expected WorkspaceWrite=false for noop write, got true")
	}
}

func TestWriteFileToolVerifyAfterWriteFacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tool := NewWrite(workspace)

	t.Run("fresh write emits verification passed facts", func(t *testing.T) {
		args, err := json.Marshal(map[string]any{
			"path":               "verified.txt",
			"content":            "verified-content",
			"verify_after_write": true,
		})
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
			Name:      tool.Name(),
			Arguments: args,
			Workdir:   workspace,
		})
		if execErr != nil {
			t.Fatalf("Execute() error = %v", execErr)
		}
		if !result.Facts.VerificationPerformed || !result.Facts.VerificationPassed {
			t.Fatalf("verification facts = %+v, want performed=true passed=true", result.Facts)
		}
		if !result.Facts.WorkspaceWrite {
			t.Fatalf("expected WorkspaceWrite=true for fresh write, got false")
		}
		if token, _ := result.Metadata["written_content"].(string); token != "verified-content" {
			t.Fatalf("written_content = %#v, want verified-content", result.Metadata["written_content"])
		}
		if !strings.HasPrefix(result.Facts.VerificationScope, "artifact:") {
			t.Fatalf("verification scope = %q, want artifact:*", result.Facts.VerificationScope)
		}
		if reason, _ := result.Metadata["verification_reason"].(string); reason != "write_readback_match" {
			t.Fatalf("verification_reason = %#v, want write_readback_match", result.Metadata["verification_reason"])
		}
	})

	t.Run("noop write emits verification passed facts", func(t *testing.T) {
		target := filepath.Join(workspace, "same-verified.txt")
		if err := os.WriteFile(target, []byte("same"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		args, err := json.Marshal(map[string]any{
			"path":               "same-verified.txt",
			"content":            "same",
			"verify_after_write": true,
			"verification_scope": "artifact:same-verified.txt",
		})
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
			Name:      tool.Name(),
			Arguments: args,
			Workdir:   workspace,
		})
		if execErr != nil {
			t.Fatalf("Execute() error = %v", execErr)
		}
		if !result.Facts.VerificationPerformed || !result.Facts.VerificationPassed {
			t.Fatalf("verification facts = %+v, want performed=true passed=true", result.Facts)
		}
		if result.Facts.WorkspaceWrite {
			t.Fatalf("expected WorkspaceWrite=false for noop write, got true")
		}
		if result.Facts.VerificationScope != "artifact:same-verified.txt" {
			t.Fatalf("verification scope = %q", result.Facts.VerificationScope)
		}
		if token, _ := result.Metadata["written_content"].(string); token != "same" {
			t.Fatalf("written_content = %#v, want same", result.Metadata["written_content"])
		}
		if reason, _ := result.Metadata["verification_reason"].(string); reason != "write_content_match_noop" {
			t.Fatalf("verification_reason = %#v, want write_content_match_noop", result.Metadata["verification_reason"])
		}
	})
}
