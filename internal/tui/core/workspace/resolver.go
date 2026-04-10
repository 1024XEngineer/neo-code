package workspace

import (
	agentworkspace "neo-code/internal/workspace"
	"strings"
)

// ResolveWorkspacePath 解析并校验工作区路径，确保返回存在且可用的目录绝对路径。
func ResolveWorkspacePath(base string, requested string) (string, error) {
	resolved, err := agentworkspace.ResolveFrom(base, requested)
	if err != nil {
		return "", err
	}
	return resolved.Workdir, nil
}

// SelectSessionWorkdir 优先返回会话工作目录，缺失时回退到默认工作目录。
func SelectSessionWorkdir(sessionWorkdir string, defaultWorkdir string) string {
	workdir := strings.TrimSpace(sessionWorkdir)
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(defaultWorkdir)
}
