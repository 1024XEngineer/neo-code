package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// BashTool 执行 shell 命令。
type BashTool struct{}

func (b *BashTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "bash",
		Description: "Execute a bash command in the workspace. Supports optional workdir and timeout, default is 120000ms.",
		Parameters: []ToolParamSpec{
			{Name: "command", Type: "string", Required: true, Description: "The bash command to execute."},
			{Name: "workdir", Type: "string", Description: "Directory within the workspace to execute the command. Defaults to the workspace root.", DefaultValue: "."},
			{Name: "timeout", Type: "integer", Description: "Command timeout in milliseconds.", DefaultValue: 120000},
			{Name: "description", Type: "string", Description: "A brief explanation of the command purpose for logs and auditing."},
		},
	}
}

func (b *BashTool) Run(params map[string]interface{}) *ToolResult {
	command, errRes := requiredString(params, "command")
	if errRes != nil {
		errRes.ToolName = b.Definition().Name
		return errRes
	}
	if denied := guardToolExecution("Bash", command, b.Definition().Name); denied != nil {
		return denied
	}
	timeoutMs, errRes := optionalInt(params, "timeout", 120000)
	if errRes != nil {
		errRes.ToolName = b.Definition().Name
		return errRes
	}
	if timeoutMs < 1 {
		return &ToolResult{ToolName: b.Definition().Name, Success: false, Error: "timeout must be >= 1"}
	}
	workdir, errRes := optionalString(params, "workdir", ".")
	if errRes != nil {
		errRes.ToolName = b.Definition().Name
		return errRes
	}
	workdir, pathErr := ensureWorkspacePath(workdir)
	if pathErr != nil {
		pathErr.ToolName = b.Definition().Name
		return pathErr
	}
	description, errRes := optionalString(params, "description", "")
	if errRes != nil {
		errRes.ToolName = b.Definition().Name
		return errRes
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var shell string
	var shellArgs []string
	switch runtime.GOOS {
	case "linux", "darwin":
		// Linux/macOS: use bash
		shell = "bash"
		shellArgs = []string{"-lc", command}
	case "windows":
		// Windows: use PowerShell
		shell = "powershell"
		shellArgs = []string{"-Command", command}
	default:
		shell = "bash"
		shellArgs = []string{"-lc", command}
	}

	// Use dynamically selected shell and args to create the command
	shell, shellArgs = preferredShellCommand(runtime.GOOS, command, exec.LookPath, shell, shellArgs)
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	cmd.Dir = workdir
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	result := &ToolResult{ToolName: b.Definition().Name, Metadata: map[string]interface{}{"command": command, "workdir": workdir, "timeoutMs": timeoutMs, "description": description}}
	if err != nil {
		if ctx.Err() != nil {
			result.Success = false
			result.Error = fmt.Sprintf("command timed out after %dms", timeoutMs)
			return result
		}
		result.Success = false
		result.Error = fmt.Sprintf("command execution failed: %v", err)
		if stderrBuf.Len() > 0 {
			result.Error += ": " + stderrBuf.String()
		}
		return result
	}
	result.Success = true
	result.Output = stdoutBuf.String()
	if stderrBuf.Len() > 0 {
		result.Output += fmt.Sprintf("\nSTDERR: %s", stderrBuf.String())
	}
	return result
}

type shellLookup func(string) (string, error)

func preferredShellCommand(goos, command string, lookPath shellLookup, shell string, shellArgs []string) (string, []string) {
	switch goos {
	case "windows":
		for _, candidate := range []struct {
			name string
			args []string
		}{
			{name: "powershell", args: []string{"-Command", command}},
			{name: "pwsh", args: []string{"-Command", command}},
			{name: "cmd.exe", args: []string{"/C", command}},
			{name: "cmd", args: []string{"/C", command}},
		} {
			if _, err := lookPath(candidate.name); err == nil {
				return candidate.name, candidate.args
			}
		}
		return "cmd", []string{"/C", command}
	default:
		if _, err := lookPath("bash"); err == nil {
			return "bash", []string{"-lc", command}
		}
		return "sh", []string{"-c", command}
	}
}
