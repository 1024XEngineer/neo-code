package bash

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/tools"
)

type stubRunner struct {
	run func(ctx context.Context, binary string, args []string, workdir string) ([]byte, error)
}

func (r stubRunner) CombinedOutput(
	ctx context.Context,
	binary string,
	args []string,
	workdir string,
) ([]byte, error) {
	if r.run != nil {
		return r.run(ctx, binary, args, workdir)
	}
	return []byte("ok"), nil
}

func TestDefaultSecurityExecutorExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	executor := &defaultSecurityExecutor{
		root:    workspace,
		shell:   defaultShell(),
		timeout: 20 * time.Millisecond,
		runner:  stubRunner{},
	}

	tests := []struct {
		name         string
		callWorkdir  string
		command      string
		requestedDir string
		overrideRun  func(ctx context.Context, binary string, args []string, workdir string) ([]byte, error)
		expectErr    string
		expectResult []string
		expectMeta   string
	}{
		{
			name:        "rejects empty command",
			command:     "",
			callWorkdir: workspace,
			expectErr:   "command is empty",
		},
		{
			name:         "rejects escaped workdir",
			command:      "echo hi",
			callWorkdir:  workspace,
			requestedDir: "..",
			expectErr:    "workdir escapes workspace root",
		},
		{
			name:        "handles timeout from runner context",
			command:     "slow",
			callWorkdir: workspace,
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string) ([]byte, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			expectErr:    context.DeadlineExceeded.Error(),
			expectResult: []string{"tool error", "tool: bash"},
			expectMeta:   workspace,
		},
		{
			name:        "applies output truncation on error details",
			command:     "boom",
			callWorkdir: workspace,
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string) ([]byte, error) {
				return []byte(strings.Repeat("x", tools.DefaultOutputLimitBytes+100)), errors.New("exit status 1")
			},
			expectErr:    "exit status 1",
			expectResult: []string{"...[truncated]"},
			expectMeta:   workspace,
		},
		{
			name:        "success returns output and metadata",
			command:     "ok",
			callWorkdir: workspace,
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string) ([]byte, error) {
				return []byte("hello"), nil
			},
			expectResult: []string{"hello"},
			expectMeta:   workspace,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runner := stubRunner{}
			if tt.overrideRun != nil {
				runner.run = tt.overrideRun
			}
			executor.runner = runner

			result, err := executor.Execute(
				context.Background(),
				tools.ToolCallInput{Workdir: tt.callWorkdir},
				tt.command,
				tt.requestedDir,
			)

			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, fragment := range tt.expectResult {
				if !strings.Contains(result.Content, fragment) {
					t.Fatalf("expected content containing %q, got %q", fragment, result.Content)
				}
			}
			if tt.expectMeta != "" {
				got, _ := result.Metadata["workdir"].(string)
				if !strings.Contains(got, tt.expectMeta) {
					t.Fatalf("expected workdir metadata containing %q, got %q", tt.expectMeta, got)
				}
			}
			if tt.expectMeta != "" {
				if _, exists := result.Metadata["ok"]; !exists {
					t.Fatalf("expected ok metadata to exist, got %#v", result.Metadata)
				}
				if _, exists := result.Metadata["exit_code"]; !exists {
					t.Fatalf("expected exit_code metadata to exist, got %#v", result.Metadata)
				}
				if _, exists := result.Metadata["classification"]; !exists {
					t.Fatalf("expected classification metadata to exist, got %#v", result.Metadata)
				}
			}
		})
	}
}

func TestShellCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shell  string
		binary string
	}{
		{name: "powershell", shell: "powershell", binary: "powershell"},
		{name: "pwsh", shell: "pwsh", binary: "powershell"},
		{name: "bash", shell: "bash", binary: "bash"},
		{name: "sh", shell: "sh", binary: "sh"},
		{name: "fallback", shell: "unknown", binary: defaultShell()},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			binary, args := shellCommand(tt.shell, "echo hi")
			if binary != tt.binary {
				t.Fatalf("shellCommand(%q) binary=%q, want %q", tt.shell, binary, tt.binary)
			}
			if len(args) == 0 {
				t.Fatalf("expected non-empty args")
			}
		})
	}
}

func TestResolveWorkdir(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tests := []struct {
		name      string
		root      string
		requested string
		expectErr string
	}{
		{name: "uses root for empty requested", root: workspace, requested: ""},
		{name: "resolves relative path", root: workspace, requested: "sub"},
		{name: "rejects traversal", root: workspace, requested: "..", expectErr: "escapes workspace root"},
		{name: "rejects invalid root", root: string([]byte{0}), requested: "", expectErr: "invalid"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target, err := resolveWorkdir(tt.root, tt.requested)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.expectErr)) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.requested != "" && !filepath.IsAbs(target) {
				t.Fatalf("expected absolute target, got %q", target)
			}
		})
	}
}
