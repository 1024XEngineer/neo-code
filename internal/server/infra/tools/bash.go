package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// BashTool 执行shell命令。
type BashTool struct{}

// Name 返回工具名称。
func (b *BashTool) Name() string {
	return "bash"
}

// Description 返回工具描述。
func (b *BashTool) Description() string {
	return "在持久的shell会话中执行给定的bash命令，支持可选超时。"
}

// Run 执行bash工具，使用给定的参数。
// 期望的参数：
//   - command: 要执行的命令（必需）
//   - timeout: 可选的超时时间（毫秒，默认：120000 / 2分钟）
//   - workdir: 运行命令的工作目录（可选，默认：当前目录）
//   - description: 明确描述命令做什么的说明（建议用于日志）
func (b *BashTool) Run(params map[string]interface{}) *ToolResult {
	// 验证必需参数
	commandParam, ok := params["command"]
	if !ok {
		return &ToolResult{
			ToolName: b.Name(),
			Success:  false,
			Error:    "缺少必需参数: command",
		}
	}

	command, ok := commandParam.(string)
	if !ok {
		return &ToolResult{
			ToolName: b.Name(),
			Success:  false,
			Error:    "command 必须是字符串",
		}
	}

	// 解析可选的timeout参数
	timeoutMs := 120000 // 默认2分钟
	if timeoutParam, ok := params["timeout"]; ok {
		switch v := timeoutParam.(type) {
		case float64:
			timeoutMs = int(v)
		case int:
			timeoutMs = v
		case string:
			if parsed, err := parseInt(v); err == nil {
				timeoutMs = parsed
			} else {
				return &ToolResult{
					ToolName: b.Name(),
					Success:  false,
					Error:    "timeout 必须是数字",
				}
			}
		default:
			return &ToolResult{
				ToolName: b.Name(),
				Success:  false,
				Error:    "timeout 必须是数字",
			}
		}
	}

	// 解析可选的workdir参数
	workdir := "." // 默认为当前目录
	if workdirParam, ok := params["workdir"]; ok {
		workdir, ok = workdirParam.(string)
		if !ok {
			return &ToolResult{
				ToolName: b.Name(),
				Success:  false,
				Error:    "workdir 必须是字符串",
			}
		}
	}
	// 描述参数是可选的，但在执行时不使用描述

	// 创建带有超时的上下文
	ctx, cancel := getContextWithTimeout(timeoutMs)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workdir

	// 捕获输出
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// 执行命令
	err := cmd.Run()

	// 准备结果
	result := &ToolResult{
		ToolName: b.Name(),
		Metadata: map[string]interface{}{
			"command":   command,
			"workdir":   workdir,
			"timeoutMs": timeoutMs,
		},
	}

	if err != nil {
		// 检查是否是超时错误
		if ctx.Err() != nil {
			result.Success = false
			result.Error = fmt.Sprintf("命令在 %dms 后超时", timeoutMs)
		} else {
			result.Success = false
			result.Error = fmt.Sprintf("命令执行失败: %v", err)
			if stderrBuf.Len() > 0 {
				result.Error += fmt.Sprintf(": %s", stderrBuf.String())
			}
		}
	} else {
		result.Success = true
		result.Output = stdoutBuf.String()
		if stderrBuf.Len() > 0 {
			// 将stderr包含在输出中，但标记为成功
			result.Output += fmt.Sprintf("\nSTDERR: %s", stderrBuf.String())
		}
	}

	return result
}

// parseInt 将字符串解析为整数的辅助函数
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// getContextWithTimeout 带超时的上下文创建辅助函数
func getContextWithTimeout(timeoutMs int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
}
