package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSandboxCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, root string, outside string)
		action    func(root string, outside string) Action
		expectErr string
	}{
		{
			name: "read path inside workspace is allowed",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				mustWriteWorkspaceFile(t, filepath.Join(root, "notes.txt"), "hello")
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "notes.txt")
			},
		},
		{
			name: "read traversal is rejected",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, filepath.Join("..", "outside.txt"))
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "absolute path outside workspace is rejected",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, outside)
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "symlinked file outside workspace is rejected",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				mustWriteWorkspaceFile(t, outside, "secret")
				mustSymlinkOrSkip(t, outside, filepath.Join(root, "linked.txt"))
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "linked.txt")
			},
			expectErr: "via symlink",
		},
		{
			name: "symlinked parent directory outside workspace is rejected",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				outsideDir := filepath.Dir(outside)
				if err := os.MkdirAll(outsideDir, 0o755); err != nil {
					t.Fatalf("mkdir outside dir: %v", err)
				}
				mustSymlinkOrSkip(t, outsideDir, filepath.Join(root, "linked-dir"))
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeWrite, "filesystem_write_file", "write_file", root, filepath.Join("linked-dir", "new.txt"))
			},
			expectErr: "via symlink",
		},
		{
			name: "missing nested write path inside workspace is allowed",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeWrite, "filesystem_write_file", "write_file", root, filepath.Join("new", "nested.txt"))
			},
		},
		{
			name: "grep defaults to workspace root when dir is empty",
			action: func(root string, outside string) Action {
				return Action{
					Type: ActionTypeRead,
					Payload: ActionPayload{
						ToolName:          "filesystem_grep",
						Resource:          "filesystem_grep",
						Operation:         "grep",
						Workdir:           root,
						TargetType:        TargetTypeDirectory,
						SandboxTargetType: TargetTypeDirectory,
					},
				}
			},
		},
		{
			name: "bash workdir inside workspace is allowed",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
					t.Fatalf("mkdir scripts: %v", err)
				}
			},
			action: func(root string, outside string) Action {
				return bashAction(root, "pwd", "scripts")
			},
		},
		{
			name: "bash workdir traversal is rejected",
			action: func(root string, outside string) Action {
				return bashAction(root, "pwd", filepath.Join("..", "outside"))
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "webfetch does not trigger workspace checks",
			action: func(root string, outside string) Action {
				return Action{
					Type: ActionTypeRead,
					Payload: ActionPayload{
						ToolName:   "webfetch",
						Resource:   "webfetch",
						Operation:  "fetch",
						Workdir:    root,
						TargetType: TargetTypeURL,
						Target:     "https://example.com",
					},
				}
			},
		},
		{
			name: "missing workspace root is rejected for path action",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", "", "notes.txt")
			},
			expectErr: "workspace root is empty",
		},
		{
			name: "empty file target is deferred to tool validation",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			outsideRoot := t.TempDir()
			outsideFile := filepath.Join(outsideRoot, "outside.txt")
			if tt.prepare != nil {
				tt.prepare(t, root, outsideFile)
			}

			sandbox := NewWorkspaceSandbox()
			err := sandbox.Check(context.Background(), tt.action(root, outsideFile))
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func fileAction(actionType ActionType, toolName string, operation string, workdir string, target string) Action {
	return Action{
		Type: actionType,
		Payload: ActionPayload{
			ToolName:          toolName,
			Resource:          toolName,
			Operation:         operation,
			Workdir:           workdir,
			TargetType:        TargetTypePath,
			Target:            target,
			SandboxTargetType: TargetTypePath,
			SandboxTarget:     target,
		},
	}
}

func bashAction(workdir string, command string, requestedWorkdir string) Action {
	return Action{
		Type: ActionTypeBash,
		Payload: ActionPayload{
			ToolName:          "bash",
			Resource:          "bash",
			Operation:         "command",
			Workdir:           workdir,
			TargetType:        TargetTypeCommand,
			Target:            command,
			SandboxTargetType: TargetTypeDirectory,
			SandboxTarget:     requestedWorkdir,
		},
	}
}

func mustWriteWorkspaceFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustSymlinkOrSkip(t *testing.T, target string, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
}
