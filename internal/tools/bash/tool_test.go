package bash

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/tools"
)

type scriptedSecurityExecutor struct {
	calls                int
	lastCall             tools.ToolCallInput
	lastCommand          string
	lastRequestedWorkdir string
	result               tools.ToolResult
	err                  error
}

func (e *scriptedSecurityExecutor) Execute(
	ctx context.Context,
	call tools.ToolCallInput,
	command string,
	requestedWorkdir string,
) (tools.ToolResult, error) {
	e.calls++
	e.lastCall = call
	e.lastCommand = command
	e.lastRequestedWorkdir = requestedWorkdir
	return e.result, e.err
}

func TestToolExecute(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	executor := &scriptedSecurityExecutor{
		result: tools.ToolResult{
			Name:    "bash",
			Content: "hello",
			Metadata: map[string]any{
				"workdir": workspace,
			},
		},
	}
	tool := NewWithExecutor(workspace, defaultShell(), 3*time.Second, executor)

	args, err := json.Marshal(map[string]string{
		"command": "echo hello",
		"workdir": "sub",
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
		t.Fatalf("unexpected error: %v", execErr)
	}
	if executor.calls != 1 {
		t.Fatalf("expected executor to be called once, got %d", executor.calls)
	}
	if executor.lastCommand != "echo hello" {
		t.Fatalf("expected command to be forwarded, got %q", executor.lastCommand)
	}
	if executor.lastRequestedWorkdir != "sub" {
		t.Fatalf("expected requested workdir to be forwarded, got %q", executor.lastRequestedWorkdir)
	}
	if executor.lastCall.Workdir != workspace {
		t.Fatalf("expected call workdir to be preserved, got %q", executor.lastCall.Workdir)
	}
	if result.Content != "hello" {
		t.Fatalf("expected executor result content, got %q", result.Content)
	}
	if result.IsError {
		t.Fatalf("expected non-error result")
	}
}

func TestToolExecuteErrorFormattingAndTruncation(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tests := []struct {
		name           string
		arguments      []byte
		executorResult tools.ToolResult
		executorErr    error
		expectErr      string
		expectContent  []string
		expectMetadata bool
		expectCalls    int
	}{
		{
			name:          "invalid json arguments",
			arguments:     []byte(`{invalid`),
			expectErr:     "invalid character",
			expectContent: []string{"tool error", "tool: bash", "reason: invalid arguments"},
			expectCalls:   0,
		},
		{
			name:      "command failure returns formatted error",
			arguments: mustMarshalArgs(t, map[string]string{"command": "bad", "workdir": ""}),
			executorResult: tools.NewErrorResult(
				"bash",
				tools.NormalizeErrorReason("bash", errors.New("exit status 1")),
				"boom",
				map[string]any{"workdir": workspace},
			),
			executorErr: errors.New("exit status 1"),
			expectErr:   "exit status 1",
			expectContent: []string{
				"tool error",
				"tool: bash",
				"reason:",
				"boom",
			},
			expectMetadata: true,
			expectCalls:    1,
		},
		{
			name:      "large output is truncated",
			arguments: mustMarshalArgs(t, map[string]string{"command": "large", "workdir": ""}),
			executorResult: tools.ApplyOutputLimit(
				tools.ToolResult{
					Name:    "bash",
					Content: strings.Repeat("x", tools.DefaultOutputLimitBytes+100),
					Metadata: map[string]any{
						"workdir": workspace,
					},
				},
				tools.DefaultOutputLimitBytes,
			),
			expectContent: []string{
				"...[truncated]",
			},
			expectMetadata: true,
			expectCalls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &scriptedSecurityExecutor{
				result: tt.executorResult,
				err:    tt.executorErr,
			}
			tool := NewWithExecutor(workspace, defaultShell(), 3*time.Second, executor)

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

			if executor.calls != tt.expectCalls {
				t.Fatalf("expected executor calls=%d, got %d", tt.expectCalls, executor.calls)
			}
			for _, fragment := range tt.expectContent {
				if !strings.Contains(result.Content, fragment) {
					t.Fatalf("expected content to contain %q, got %q", fragment, result.Content)
				}
			}
			if tt.expectMetadata && result.Metadata["workdir"] == "" {
				t.Fatalf("expected workdir metadata, got %#v", result.Metadata)
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
