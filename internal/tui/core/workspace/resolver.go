package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspacePath 解析并校验工作区路径，确保返回存在且可用的目录绝对路径。
func ResolveWorkspacePath(base string, requested string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace: resolve current directory: %w", err)
		}
		base = workingDir
	}

	target := strings.TrimSpace(requested)
	if target == "" {
		target = "."
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}

	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace: %q is not a directory", absolute)
	}
	return filepath.Clean(absolute), nil
}

// SelectSessionWorkdir 优先返回会话工作目录，缺失时回退到默认工作目录。
func SelectSessionWorkdir(sessionWorkdir string, defaultWorkdir string) string {
	workdir := strings.TrimSpace(sessionWorkdir)
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(defaultWorkdir)
}
