package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// BashTool 执行 shell 命令。
type BashTool struct{}

func (b *BashTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "bash",
		Description: "在工作区内执行 bash 命令。支持可选 workdir 和 timeout，默认 120000ms。",
		Parameters: []ToolParamSpec{
			{Name: "command", Type: "string", Required: true, Description: "要执行的 bash 命令。"},
			{Name: "workdir", Type: "string", Description: "工作区内命令执行目录，默认工作区根目录。"},
			{Name: "timeout", Type: "integer", Description: "命令超时时间，单位毫秒，默认 120000。"},
			{Name: "description", Type: "string", Description: "对命令目的的简短说明，便于日志和审计。"},
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
		return &ToolResult{ToolName: b.Definition().Name, Success: false, Error: "timeout 必须 >= 1"}
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
	description, _ := optionalString(params, "description", "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = workdir
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	result := &ToolResult{ToolName: b.Definition().Name, Metadata: map[string]interface{}{"command": command, "workdir": workdir, "timeoutMs": timeoutMs, "description": description}}
	if err != nil {
		if ctx.Err() != nil {
			result.Success = false
			result.Error = fmt.Sprintf("命令在 %dms 后超时", timeoutMs)
			return result
		}
		result.Success = false
		result.Error = fmt.Sprintf("命令执行失败: %v", err)
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
