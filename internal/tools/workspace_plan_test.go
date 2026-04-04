package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/security"
)

func TestResolveWorkspaceTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T) (ToolCallInput, string, string)
		wantErr   string
		assertion func(t *testing.T, root string, target string)
	}{
		{
			name: "falls back when workspace plan is missing",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				return ToolCallInput{}, root, "a.txt"
			},
			assertion: func(t *testing.T, root string, target string) {
				t.Helper()
				if !strings.HasSuffix(target, filepath.Join(root, "a.txt")) {
					t.Fatalf("expected target inside root, got %q", target)
				}
			},
		},
		{
			name: "fallback resolver error bubbles up",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				return ToolCallInput{}, t.TempDir(), "a.txt"
			},
			wantErr: "resolver failed",
		},
		{
			name: "uses validated workspace plan target",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				path := filepath.Join(root, "main.go")
				if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				plan := mustBuildWorkspacePlan(t, root, "main.go", security.TargetTypePath, security.ActionTypeRead)
				return ToolCallInput{WorkspacePlan: plan}, root, "main.go"
			},
			assertion: func(t *testing.T, root string, target string) {
				t.Helper()
				if !strings.HasSuffix(target, filepath.Join(root, "main.go")) {
					t.Fatalf("expected planned target, got %q", target)
				}
			},
		},
		{
			name: "rejects mismatched requested target",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				path := filepath.Join(root, "a.txt")
				if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				plan := mustBuildWorkspacePlan(t, root, "a.txt", security.TargetTypePath, security.ActionTypeRead)
				return ToolCallInput{WorkspacePlan: plan}, root, "b.txt"
			},
			wantErr: "workspace plan target mismatch",
		},
		{
			name: "rejects changed anchor before execution",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				path := filepath.Join(root, "a.txt")
				if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				plan := mustBuildWorkspacePlan(t, root, "a.txt", security.TargetTypePath, security.ActionTypeRead)
				if err := os.WriteFile(path, []byte("longer-content"), 0o644); err != nil {
					t.Fatalf("mutate file: %v", err)
				}
				return ToolCallInput{WorkspacePlan: plan}, root, "a.txt"
			},
			wantErr: "changed before execution",
		},
		{
			name: "rejects target type mismatch",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
					t.Fatalf("mkdir scripts: %v", err)
				}
				plan := mustBuildWorkspacePlan(t, root, "scripts", security.TargetTypeDirectory, security.ActionTypeBash)
				return ToolCallInput{WorkspacePlan: plan}, root, "scripts"
			},
			wantErr: "target type",
		},
		{
			name: "allows type mismatch when expected type is empty",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
					t.Fatalf("mkdir scripts: %v", err)
				}
				plan := mustBuildWorkspacePlan(t, root, "scripts", security.TargetTypeDirectory, security.ActionTypeBash)
				return ToolCallInput{WorkspacePlan: plan}, root, "scripts"
			},
			assertion: func(t *testing.T, root string, target string) {
				t.Helper()
				if !strings.HasSuffix(target, filepath.Join(root, "scripts")) {
					t.Fatalf("expected planned scripts target, got %q", target)
				}
			},
		},
		{
			name: "invalid plan root is rejected from validate",
			setup: func(t *testing.T) (ToolCallInput, string, string) {
				t.Helper()
				return ToolCallInput{
					WorkspacePlan: &security.WorkspaceExecutionPlan{
						Target: "a.txt",
					},
				}, t.TempDir(), "a.txt"
			},
			wantErr: "workspace plan root is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call, root, requested := tt.setup(t)
			expectedType := security.TargetTypePath
			if tt.name == "allows type mismatch when expected type is empty" {
				expectedType = ""
			}

			resolvedRoot, target, err := ResolveWorkspaceTarget(
				call,
				expectedType,
				root,
				requested,
				func(resolveRoot string, resolveRequested string) (string, error) {
					if tt.name == "fallback resolver error bubbles up" {
						return "", errors.New("resolver failed")
					}
					return testPathResolver(resolveRoot, resolveRequested)
				},
			)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolvedRoot == "" {
				t.Fatalf("expected non-empty resolved root")
			}
			if tt.assertion != nil {
				tt.assertion(t, resolvedRoot, target)
			}
		})
	}
}

func mustBuildWorkspacePlan(
	t *testing.T,
	root string,
	target string,
	targetType security.TargetType,
	actionType security.ActionType,
) *security.WorkspaceExecutionPlan {
	t.Helper()

	plan, err := security.NewWorkspaceSandbox().Check(context.Background(), security.Action{
		Type: actionType,
		Payload: security.ActionPayload{
			ToolName:          "test_tool",
			Resource:          "test_tool",
			Operation:         "test_op",
			Workdir:           root,
			TargetType:        targetType,
			Target:            target,
			SandboxTargetType: targetType,
			SandboxTarget:     target,
		},
	})
	if err != nil {
		t.Fatalf("build workspace plan: %v", err)
	}
	if plan == nil {
		t.Fatalf("expected non-nil workspace plan")
	}
	return plan
}

func testPathResolver(root string, requested string) (string, error) {
	path := requested
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Abs(path)
}
