package commands

import (
	"context"
	"fmt"
	"strings"

	agentruntime "neo-code/internal/runtime"
)

// WorkspaceSwitcher 定义切换当前工作区上下文所需的最小 runtime 能力。
type WorkspaceSwitcher interface {
	SwitchWorkspace(ctx context.Context, input agentruntime.WorkspaceSwitchInput) (agentruntime.WorkspaceSwitchResult, error)
}

// SessionWorkdirCommandResult 表示工作目录命令执行结果。
type SessionWorkdirCommandResult struct {
	Notice           string
	Workdir          string
	WorkspaceRoot    string
	WorkspaceChanged bool
	ResetToDraft     bool
	Err              error
}

// ExecuteSessionWorkdirCommand 执行 /cwd 命令的核心流程，返回统一结果结构。
func ExecuteSessionWorkdirCommand(
	runtime WorkspaceSwitcher,
	sessionID string,
	currentWorkdir string,
	raw string,
	parseCommand func(string) (string, error),
) SessionWorkdirCommandResult {
	requested, err := parseCommand(raw)
	if err != nil {
		return SessionWorkdirCommandResult{Err: err}
	}

	if strings.TrimSpace(requested) == "" {
		workdir := strings.TrimSpace(currentWorkdir)
		if workdir == "" {
			return SessionWorkdirCommandResult{Err: fmt.Errorf("usage: /cwd <path>")}
		}
		return SessionWorkdirCommandResult{
			Notice:  fmt.Sprintf("[System] Current workspace is %s.", workdir),
			Workdir: workdir,
		}
	}

	result, err := runtime.SwitchWorkspace(context.Background(), agentruntime.WorkspaceSwitchInput{
		SessionID:     strings.TrimSpace(sessionID),
		RequestedPath: requested,
	})
	if err != nil {
		return SessionWorkdirCommandResult{Err: err}
	}
	notice := fmt.Sprintf("[System] Draft workspace switched to %s.", result.Workdir)
	if strings.TrimSpace(sessionID) != "" {
		if result.ResetToDraft {
			notice = fmt.Sprintf("[System] Workspace switched to %s. Started a new draft in the target workspace.", result.Workdir)
		} else {
			notice = fmt.Sprintf("[System] Session workspace switched to %s.", result.Workdir)
		}
	}
	return SessionWorkdirCommandResult{
		Notice:           notice,
		Workdir:          result.Workdir,
		WorkspaceRoot:    result.WorkspaceRoot,
		WorkspaceChanged: result.WorkspaceChanged,
		ResetToDraft:     result.ResetToDraft,
	}
}
