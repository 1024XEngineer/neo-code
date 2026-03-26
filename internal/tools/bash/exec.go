package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"neocode/internal/tools"
)

const (
	defaultTimeout  = 15 * time.Second
	maxOutputLength = 32 * 1024
)

// ExecTool runs shell commands inside the configured workdir.
type ExecTool struct {
	shell   string
	timeout time.Duration
}

// NewExecTool constructs the shell execution tool.
func NewExecTool(shell string) *ExecTool {
	return &ExecTool{
		shell:   shell,
		timeout: defaultTimeout,
	}
}

// Name returns the stable tool name.
func (t *ExecTool) Name() string {
	return "bash_exec"
}

// Description describes the tool for the model.
func (t *ExecTool) Description() string {
	return "Execute a non-interactive shell command inside the current workdir with a timeout."
}

// Schema returns the JSON schema for tool arguments.
func (t *ExecTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the shell command and returns combined stdout/stderr.
func (t *ExecTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return tools.Result{}, fmt.Errorf("command is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := buildCommand(runCtx, t.shell, args.Command)
	cmd.Dir = call.Workdir

	output, err := cmd.CombinedOutput()
	content := strings.TrimSpace(string(output))
	content = truncate(content, maxOutputLength)

	if runCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("command timed out after %s", t.timeout)
	}

	if content == "" {
		content = "[no output]"
	}

	result := tools.Result{
		Content: content,
		Metadata: map[string]any{
			"shell":   t.shell,
			"workdir": call.Workdir,
		},
	}

	if err != nil {
		result.IsError = true
		result.Content = fmt.Sprintf("%s\n\ncommand failed: %v", content, err)
	}

	return result, err
}

func buildCommand(ctx context.Context, shell, command string) *exec.Cmd {
	base := strings.ToLower(filepath.Base(shell))
	switch base {
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", command)
	case "cmd", "cmd.exe":
		return exec.CommandContext(ctx, shell, "/C", command)
	default:
		return exec.CommandContext(ctx, shell, "-lc", command)
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n\n[truncated]"
}
