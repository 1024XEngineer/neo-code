package bash

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"neo-code/internal/tools"
)

func TestToolExecute(t *testing.T) {
	workspace := t.TempDir()
	subdir := filepath.Join(workspace, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	tests := []struct {
		name          string
		command       string
		workdir       string
		callWorkdir   string
		expectErr     string
		expectContent string
	}{
		{
			name:          "captures stdout",
			command:       safeEchoCommand(),
			callWorkdir:   workspace,
			expectContent: "hello",
		},
		{
			name:        "rejects workdir escape",
			command:     safeEchoCommand(),
			workdir:     "..",
			callWorkdir: workspace,
			expectErr:   "workdir escapes workspace root",
		},
		{
			name:        "rejects empty command",
			command:     "",
			callWorkdir: workspace,
			expectErr:   "command is empty",
		},
		{
			name:          "runs inside nested workdir",
			command:       safePwdCommand(),
			workdir:       "sub",
			callWorkdir:   workspace,
			expectContent: normalizeOutputPath(subdir),
		},
		{
			name:          "uses tool root when call workdir is empty",
			command:       safePwdCommand(),
			callWorkdir:   "",
			expectContent: normalizeOutputPath(workspace),
		},
	}

	tool := New(workspace, defaultShell(), 3*time.Second)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{
				"command": tt.command,
				"workdir": tt.workdir,
			})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: args,
				Workdir:   tt.callWorkdir,
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
			if !strings.Contains(normalizeOutputPath(result.Content), normalizeOutputPath(tt.expectContent)) {
				t.Fatalf("expected content containing %q, got %q", tt.expectContent, result.Content)
			}
			if result.IsError {
				t.Fatalf("expected IsError=false, got true")
			}
		})
	}
}

func TestToolExecuteErrorFormattingAndTruncation(t *testing.T) {
	workspace := t.TempDir()
	tool := New(workspace, defaultShell(), 3*time.Second)

	tests := []struct {
		name           string
		arguments      []byte
		expectErr      string
		expectContent  []string
		expectMetadata bool
		expectTruncate bool
	}{
		{
			name:          "invalid json arguments",
			arguments:     []byte(`{invalid`),
			expectErr:     "invalid character",
			expectContent: []string{"tool error", "tool: bash", "reason: invalid arguments"},
		},
		{
			name:      "command failure returns formatted error",
			arguments: mustMarshalArgs(t, map[string]string{"command": failingCommand(), "workdir": ""}),
			expectErr: commandFailureFragment(),
			expectContent: []string{
				"tool error",
				"tool: bash",
				"reason:",
				commandFailureOutput(),
			},
			expectMetadata: true,
		},
		{
			name:      "large output is truncated",
			arguments: mustMarshalArgs(t, map[string]string{"command": largeOutputCommand(), "workdir": ""}),
			expectContent: []string{
				"...[truncated]",
			},
			expectMetadata: true,
			expectTruncate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: tt.arguments,
				Workdir:   workspace,
			})

			if tt.expectErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.expectErr)) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, fragment := range tt.expectContent {
				if !strings.Contains(result.Content, fragment) {
					t.Fatalf("expected content to contain %q, got %q", fragment, result.Content)
				}
			}
			if tt.expectMetadata && result.Metadata["workdir"] == "" {
				t.Fatalf("expected workdir metadata, got %#v", result.Metadata)
			}
			if truncated, _ := result.Metadata["truncated"].(bool); truncated != tt.expectTruncate {
				t.Fatalf("expected truncated=%v, got %#v", tt.expectTruncate, result.Metadata["truncated"])
			}
		})
	}
}

func mustMarshalArgs(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return data
}

func safeEchoCommand() string {
	if goruntime.GOOS == "windows" {
		return "Write-Output 'hello'"
	}
	return "printf 'hello'"
}

func safePwdCommand() string {
	if goruntime.GOOS == "windows" {
		return "(Get-Location).Path"
	}
	return "pwd"
}

func failingCommand() string {
	if goruntime.GOOS == "windows" {
		return "Write-Output 'boom'; exit 1"
	}
	return "printf 'boom\\n'; exit 1"
}

func commandFailureFragment() string {
	return "exit status 1"
}

func commandFailureOutput() string {
	return "boom"
}

func largeOutputCommand() string {
	if goruntime.GOOS == "windows" {
		return "$text = 'x' * 70000; Write-Output $text"
	}
	return "i=0; while [ $i -lt 70000 ]; do printf x; i=$((i+1)); done"
}

func defaultShell() string {
	if goruntime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

func normalizeOutputPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if goruntime.GOOS == "windows" {
		return strings.ToLower(strings.ReplaceAll(trimmed, "/", `\`))
	}
	return strings.ReplaceAll(trimmed, `\`, "/")
}
