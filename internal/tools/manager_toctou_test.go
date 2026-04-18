package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"neo-code/internal/security"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/filesystem"
)

type mutatingWorkspaceSandbox struct {
	base   *security.WorkspaceSandbox
	mutate func(plan *security.WorkspaceExecutionPlan) error
}

func (s *mutatingWorkspaceSandbox) Check(
	ctx context.Context,
	action security.Action,
) (*security.WorkspaceExecutionPlan, error) {
	plan, err := s.base.Check(ctx, action)
	if err != nil {
		return nil, err
	}
	if plan == nil || s.mutate == nil {
		return plan, nil
	}
	if err := s.mutate(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func TestDefaultManagerTOCTOUScenarios(t *testing.T) {
	// t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, workspace string) (*tools.Registry, tools.ToolCallInput, func(plan *security.WorkspaceExecutionPlan) error)
		asserts func(t *testing.T, workspace string, result tools.ToolResult, err error)
	}{
		{
			name: "read file blocks symlink swap after sandbox check",
			setup: func(t *testing.T, workspace string) (*tools.Registry, tools.ToolCallInput, func(plan *security.WorkspaceExecutionPlan) error) {
				// t.Helper()

				safePath := filepath.Join(workspace, "safe.txt")
				if err := os.WriteFile(safePath, []byte("safe"), 0o644); err != nil {
					t.Fatalf("write safe file: %v", err)
				}
				outsideDir := t.TempDir()
				outsidePath := filepath.Join(outsideDir, "outside.txt")
				if err := os.WriteFile(outsidePath, []byte("outside-secret"), 0o644); err != nil {
					t.Fatalf("write outside file: %v", err)
				}

				linkPath := filepath.Join(workspace, "swap-link.txt")
				requireSymlinkSupport(t, safePath, linkPath+"_probe")
				if err := os.Symlink(safePath, linkPath); err != nil {
					t.Skipf("symlink not supported in this environment: %v", err)
				}

				registry := tools.NewRegistry()
				registry.Register(filesystem.New(workspace))

				args, err := json.Marshal(map[string]string{"path": "swap-link.txt"})
				if err != nil {
					t.Fatalf("marshal args: %v", err)
				}

				mutate := func(plan *security.WorkspaceExecutionPlan) error {
					if err := os.Remove(linkPath); err != nil {
						return err
					}
					return os.Symlink(outsidePath, linkPath)
				}

				return registry, tools.ToolCallInput{
					Name:      "filesystem_read_file",
					Arguments: args,
					Workdir:   workspace,
				}, mutate
			},
			asserts: func(t *testing.T, workspace string, result tools.ToolResult, err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "changed before execution") {
					t.Fatalf("expected TOCTOU change error, got %v", err)
				}
				if strings.Contains(result.Content, "outside-secret") {
					t.Fatalf("expected outside content to stay unread, got %q", result.Content)
				}
			},
		},
		{
			name: "write file blocks parent replacement after sandbox check",
			setup: func(t *testing.T, workspace string) (*tools.Registry, tools.ToolCallInput, func(plan *security.WorkspaceExecutionPlan) error) {
				t.Helper()

				parent := filepath.Join(workspace, "data")
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatalf("mkdir parent: %v", err)
				}
				outsideDir := t.TempDir()
				requireSymlinkSupport(t, outsideDir, filepath.Join(workspace, "_write_link_probe"))
				swapped := filepath.Join(workspace, "data")

				registry := tools.NewRegistry()
				registry.Register(filesystem.NewWrite(workspace))

				args, err := json.Marshal(map[string]string{
					"path":    filepath.Join("data", "new.txt"),
					"content": "hello",
				})
				if err != nil {
					t.Fatalf("marshal args: %v", err)
				}

				mutate := func(plan *security.WorkspaceExecutionPlan) error {
					if err := os.Remove(swapped); err != nil {
						return err
					}
					return os.Symlink(outsideDir, swapped)
				}

				return registry, tools.ToolCallInput{
					Name:      "filesystem_write_file",
					Arguments: args,
					Workdir:   workspace,
				}, mutate
			},
			asserts: func(t *testing.T, workspace string, result tools.ToolResult, err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "changed before execution") {
					t.Fatalf("expected TOCTOU change error, got %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(workspace, "data", "new.txt")); statErr == nil {
					t.Fatalf("expected write to be blocked before file creation")
				}
			},
		},
		{
			name: "bash blocks workdir replacement after sandbox check",
			setup: func(t *testing.T, workspace string) (*tools.Registry, tools.ToolCallInput, func(plan *security.WorkspaceExecutionPlan) error) {
				t.Helper()

				scriptsDir := filepath.Join(workspace, "scripts")
				if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
					t.Fatalf("mkdir scripts dir: %v", err)
				}
				outsideDir := t.TempDir()
				requireSymlinkSupport(t, outsideDir, filepath.Join(workspace, "_bash_link_probe"))

				registry := tools.NewRegistry()
				registry.Register(bash.New(workspace, shellForTOCTOUTest(), 3*time.Second))

				args, err := json.Marshal(map[string]string{
					"command": safeEchoCommandForTOCTOUTest(),
					"workdir": "scripts",
				})
				if err != nil {
					t.Fatalf("marshal args: %v", err)
				}

				mutate := func(plan *security.WorkspaceExecutionPlan) error {
					if err := os.Remove(scriptsDir); err != nil {
						return err
					}
					return os.Symlink(outsideDir, scriptsDir)
				}

				return registry, tools.ToolCallInput{
					Name:      "bash",
					Arguments: args,
					Workdir:   workspace,
				}, mutate
			},
			asserts: func(t *testing.T, workspace string, result tools.ToolResult, err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "changed before execution") {
					t.Fatalf("expected TOCTOU change error, got %v", err)
				}
				if strings.Contains(result.Content, "ok") {
					t.Fatalf("expected command not to run, got %q", result.Content)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// t.Parallel()

			workspace := t.TempDir()
			registry, input, mutate := tt.setup(t, workspace)

			engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
			if err != nil {
				t.Fatalf("new static gateway: %v", err)
			}
			sandbox := &mutatingWorkspaceSandbox{
				base:   security.NewWorkspaceSandbox(),
				mutate: mutate,
			}
			manager, err := tools.NewManager(registry, engine, sandbox)
			if err != nil {
				t.Fatalf("new manager: %v", err)
			}

			result, execErr := manager.Execute(context.Background(), input)
			tt.asserts(t, workspace, result, execErr)
		})
	}
}

func shellForTOCTOUTest() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

func safeEchoCommandForTOCTOUTest() string {
	if runtime.GOOS == "windows" {
		return "Write-Output 'ok'"
	}
	return "printf 'ok'"
}

func requireSymlinkSupport(t *testing.T, target string, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Skipf("symlink created but missing: %v", err)
	}
	_ = os.Remove(link)
}
