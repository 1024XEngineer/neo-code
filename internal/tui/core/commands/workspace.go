package commands

import (
	"context"
	"fmt"
	"strings"

	agentsession "neo-code/internal/session"
	agentworkspace "neo-code/internal/workspace"
)

// SessionWorkdirUpdater 定义更新会话工作目录所需的最小 runtime 能力。
type SessionWorkdirUpdater interface {
	UpdateSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error)
}

// SessionWorkdirCommandResult 表示工作目录命令执行结果。
type SessionWorkdirCommandResult struct {
	Notice          string
	Workdir         string
	WorkspaceRoot   string
	RequiresRebuild bool
	Err             error
}

// ExecuteSessionWorkdirCommand 执行 /cwd 命令的核心流程，返回统一结果结构。
func ExecuteSessionWorkdirCommand(
	runtime SessionWorkdirUpdater,
	sessionID string,
	currentWorkspaceRoot string,
	currentWorkdir string,
	raw string,
	parseCommand func(string) (string, error),
	resolveWorkspace func(string, string) (agentworkspace.Resolution, error),
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
		resolved, err := resolveWorkspace(currentWorkdir, requested)
		if err != nil {
			return SessionWorkdirCommandResult{Err: err}
		}
		notice := fmt.Sprintf("[System] Draft workspace switched to %s.", resolved.Workdir)
		if !agentworkspace.SameRoot(currentWorkspaceRoot, resolved.WorkspaceRoot) {
			notice = fmt.Sprintf("[System] Workspace switched to %s.", resolved.Workdir)
		}
		return SessionWorkdirCommandResult{
			Notice:          notice,
			Workdir:         resolved.Workdir,
			WorkspaceRoot:   resolved.WorkspaceRoot,
			RequiresRebuild: !agentworkspace.SameRoot(currentWorkspaceRoot, resolved.WorkspaceRoot),
		}
	}

	resolved, err := resolveWorkspace(currentWorkdir, requested)
	if err != nil {
		return SessionWorkdirCommandResult{Err: err}
	}
	if !agentworkspace.SameRoot(currentWorkspaceRoot, resolved.WorkspaceRoot) {
		return SessionWorkdirCommandResult{
			Notice:          fmt.Sprintf("[System] Workspace switched to %s.", resolved.Workdir),
			Workdir:         resolved.Workdir,
			WorkspaceRoot:   resolved.WorkspaceRoot,
			RequiresRebuild: true,
		}
	}

	session, err := runtime.UpdateSessionWorkdir(context.Background(), sessionID, requested)
	if err != nil {
		return SessionWorkdirCommandResult{Err: err}
	}

	workdir := selectSessionWorkdir(session.Workdir, currentWorkdir)
	return SessionWorkdirCommandResult{
		Notice:        fmt.Sprintf("[System] Session workspace switched to %s.", workdir),
		Workdir:       workdir,
		WorkspaceRoot: currentWorkspaceRoot,
	}
}
