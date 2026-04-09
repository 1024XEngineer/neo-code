package commands

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// WorkspaceSwitchCommandResult 表示 `/cwd` 命令解析后的统一结果。
type WorkspaceSwitchCommandResult struct {
	Notice   string
	Workdir  string
	Relaunch bool
	Err      error
}

// ExecuteWorkspaceSwitchCommand 负责解析 `/cwd`，并产出是否需要重启到新工作区的结果。
func ExecuteWorkspaceSwitchCommand(
	currentWorkdir string,
	startupWorkdir string,
	raw string,
	parseCommand func(string) (string, error),
	resolveWorkspacePath func(string, string) (string, error),
) WorkspaceSwitchCommandResult {
	requested, err := parseCommand(raw)
	if err != nil {
		return WorkspaceSwitchCommandResult{Err: err}
	}

	currentWorkdir = strings.TrimSpace(currentWorkdir)
	startupWorkdir = strings.TrimSpace(startupWorkdir)
	activeWorkspaceRoot := startupWorkdir
	if activeWorkspaceRoot == "" {
		activeWorkspaceRoot = currentWorkdir
	}
	if strings.TrimSpace(requested) == "" {
		workdir := activeWorkspaceRoot
		if workdir == "" {
			return WorkspaceSwitchCommandResult{Err: fmt.Errorf("usage: /cwd <path>")}
		}
		return WorkspaceSwitchCommandResult{
			Notice:  fmt.Sprintf("[System] Current workspace is %s.", workdir),
			Workdir: workdir,
		}
	}

	resolvedWorkdir, err := resolveWorkspacePath(activeWorkspaceRoot, requested)
	if err != nil {
		return WorkspaceSwitchCommandResult{Err: err}
	}
	if sameWorkspacePath(resolvedWorkdir, activeWorkspaceRoot) {
		return WorkspaceSwitchCommandResult{
			Notice:  fmt.Sprintf("[System] Workspace already set to %s.", resolvedWorkdir),
			Workdir: resolvedWorkdir,
		}
	}

	return WorkspaceSwitchCommandResult{
		Notice:   fmt.Sprintf("[System] Switching workspace to %s.", resolvedWorkdir),
		Workdir:  resolvedWorkdir,
		Relaunch: true,
	}
}

// sameWorkspacePath 负责在跨平台场景下比较两个工作区路径是否指向同一目录。
func sameWorkspacePath(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}

	normalizedLeft := filepath.Clean(left)
	normalizedRight := filepath.Clean(right)
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(normalizedLeft, normalizedRight)
	}
	return normalizedLeft == normalizedRight
}
