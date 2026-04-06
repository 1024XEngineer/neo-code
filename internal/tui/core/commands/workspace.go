package commands

import (
	"context"
	"fmt"
	"strings"

	agentruntime "neo-code/internal/runtime"
)

// SessionWorkdirSetter 定义设置会话工作目录所需的最小 runtime 能力。
type SessionWorkdirSetter interface {
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentruntime.Session, error)
}

// SessionWorkdirCommandResult 表示工作目录命令执行结果。
type SessionWorkdirCommandResult struct {
	Notice  string
	Workdir string
	Err     error
}

// ExecuteSessionWorkdirCommand 执行 /cwd 命令的核心流程，返回统一结果结构。
func ExecuteSessionWorkdirCommand(
	runtime SessionWorkdirSetter,
	sessionID string,
	currentWorkdir string,
	raw string,
	parseCommand func(string) (string, error),
	resolveWorkspacePath func(string, string) (string, error),
	selectSessionWorkdir func(string, string) string,
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

	if strings.TrimSpace(sessionID) == "" {
		workdir, err := resolveWorkspacePath(currentWorkdir, requested)
		if err != nil {
			return SessionWorkdirCommandResult{Err: err}
		}
		return SessionWorkdirCommandResult{
			Notice:  fmt.Sprintf("[System] Draft workspace switched to %s.", workdir),
			Workdir: workdir,
		}
	}

	session, err := runtime.SetSessionWorkdir(context.Background(), sessionID, requested)
	if err != nil {
		return SessionWorkdirCommandResult{Err: err}
	}

	workdir := selectSessionWorkdir(session.Workdir, currentWorkdir)
	return SessionWorkdirCommandResult{
		Notice:  fmt.Sprintf("[System] Session workspace switched to %s.", workdir),
		Workdir: workdir,
	}
}
