package bash

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"neo-code/internal/tools"
)

type stubRunner struct {
	run func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error)
}

func (r stubRunner) CombinedOutput(
	ctx context.Context,
	binary string,
	args []string,
	workdir string,
	env []string,
) ([]byte, error) {
	if r.run != nil {
		return r.run(ctx, binary, args, workdir, env)
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
		overrideRun  func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error)
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
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error) {
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
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error) {
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
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error) {
				return []byte("hello"), nil
			},
			expectResult: []string{"hello"},
			expectMeta:   workspace,
		},
		{
			name:        "git read-only command emits hardened metadata",
			command:     "git status --short",
			callWorkdir: workspace,
			overrideRun: func(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error) {
				lookup := map[string]string{}
				for _, entry := range env {
					if idx := strings.Index(entry, "="); idx > 0 {
						lookup[entry[:idx]] = entry[idx+1:]
					}
				}
				if lookup["GIT_CONFIG_NOSYSTEM"] != "1" {
					return nil, errors.New("missing GIT_CONFIG_NOSYSTEM hardening")
				}
				if lookup["GIT_PAGER"] != "cat" {
					return nil, errors.New("missing GIT_PAGER hardening")
				}
				return []byte("clean"), nil
			},
			expectResult: []string{"clean"},
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
				if _, exists := result.Metadata["env_hardened"]; !exists {
					t.Fatalf("expected env_hardened metadata to exist, got %#v", result.Metadata)
				}
			}
		})
	}
}

func TestBuildCommandEnvRejectsUnstableReadOnlyIntent(t *testing.T) {
	t.Parallel()

	_, _, err := buildCommandEnv(tools.BashSemanticIntent{
		IsGit:          true,
		Classification: tools.BashIntentClassificationReadOnly,
		Subcommand:     "status",
		ParseError:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot safely classify git read-only command") {
		t.Fatalf("buildCommandEnv() error = %v, want unstable classification error", err)
	}
}

func TestBuildCommandEnvAllowsExpandedReadOnlyGitSubcommands(t *testing.T) {
	t.Parallel()

	for _, subcommand := range []string{"diff", "log", "show"} {
		env, hardened, err := buildCommandEnv(tools.BashSemanticIntent{
			IsGit:          true,
			Classification: tools.BashIntentClassificationReadOnly,
			Subcommand:     subcommand,
		})
		if err != nil {
			t.Fatalf("buildCommandEnv(%q) error = %v", subcommand, err)
		}
		if !hardened {
			t.Fatalf("expected env hardening for %q", subcommand)
		}

		lookup := map[string]string{}
		for _, entry := range env {
			if idx := strings.Index(entry, "="); idx > 0 {
				lookup[entry[:idx]] = entry[idx+1:]
			}
		}
		if lookup["GIT_CONFIG_NOSYSTEM"] != "1" {
			t.Fatalf("%q missing GIT_CONFIG_NOSYSTEM hardening", subcommand)
		}
		if lookup["GIT_PAGER"] != "cat" || lookup["PAGER"] != "cat" {
			t.Fatalf("%q missing pager hardening: %+v", subcommand, lookup)
		}
	}
}

func TestSanitizeGitReadOnlyEnv(t *testing.T) {
	t.Parallel()

	env, err := sanitizeGitReadOnlyEnv([]string{
		"PATH=/usr/bin",
		"GIT_PAGER=less",
		"GIT_CONFIG_NOSYSTEM=0",
		"LANG=en_US.UTF-8",
		"PAGER=less",
		"GIT_EXTERNAL_DIFF=script.sh",
	})
	if err != nil {
		t.Fatalf("sanitizeGitReadOnlyEnv() error = %v", err)
	}

	lookup := map[string]string{}
	for _, entry := range env {
		idx := strings.Index(entry, "=")
		if idx <= 0 {
			t.Fatalf("invalid env entry %q", entry)
		}
		lookup[entry[:idx]] = entry[idx+1:]
	}

	if lookup["PATH"] != "/usr/bin" {
		t.Fatalf("PATH should be preserved, got %q", lookup["PATH"])
	}
	if lookup["GIT_CONFIG_NOSYSTEM"] != "1" {
		t.Fatalf("GIT_CONFIG_NOSYSTEM = %q, want 1", lookup["GIT_CONFIG_NOSYSTEM"])
	}
	if lookup["GIT_PAGER"] != "cat" {
		t.Fatalf("GIT_PAGER = %q, want cat", lookup["GIT_PAGER"])
	}
	if lookup["PAGER"] != "cat" {
		t.Fatalf("PAGER = %q, want cat", lookup["PAGER"])
	}
	if lookup["GIT_EXTERNAL_DIFF"] != "" {
		t.Fatalf("GIT_EXTERNAL_DIFF = %q, want empty", lookup["GIT_EXTERNAL_DIFF"])
	}

	wantNull := "/dev/null"
	if runtime.GOOS == "windows" {
		wantNull = "NUL"
	}
	if lookup["GIT_CONFIG_GLOBAL"] != wantNull {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q, want %q", lookup["GIT_CONFIG_GLOBAL"], wantNull)
	}
}

func TestSanitizeGitReadOnlyEnvIgnoresWindowsPseudoEntries(t *testing.T) {
	t.Parallel()

	env, err := sanitizeGitReadOnlyEnv([]string{
		"=::=::\\",
		"=C:=C:\\workspace",
		"PATH=/usr/bin",
	})
	if err != nil {
		t.Fatalf("sanitizeGitReadOnlyEnv() error = %v", err)
	}

	for _, entry := range env {
		if strings.HasPrefix(entry, "=") {
			t.Fatalf("expected pseudo env entry to be dropped, got %q", entry)
		}
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
