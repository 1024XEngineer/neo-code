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
		expectErr     string
		expectContent string
	}{
		{
			name:          "captures stdout",
			command:       safeEchoCommand(),
			expectContent: "hello",
		},
		{
			name:      "rejects workdir escape",
			command:   safeEchoCommand(),
			workdir:   "..",
			expectErr: "workdir escapes workspace root",
		},
		{
			name:      "rejects empty command",
			command:   "",
			expectErr: "command is empty",
		},
		{
			name:          "runs inside nested workdir",
			command:       safePwdCommand(),
			workdir:       "sub",
			expectContent: normalizeOutputPath(subdir),
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
			if !strings.Contains(normalizeOutputPath(result.Content), normalizeOutputPath(tt.expectContent)) {
				t.Fatalf("expected content containing %q, got %q", tt.expectContent, result.Content)
			}
			if result.IsError {
				t.Fatalf("expected IsError=false, got true")
			}
		})
	}
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
