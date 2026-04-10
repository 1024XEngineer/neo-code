package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// Resolution 描述一次工作区解析后的根目录与当前工作目录。
type Resolution struct {
	WorkspaceRoot string
	Workdir       string
}

// Resolve 将目标路径解析为存在的绝对目录，并推导所属工作区根目录。
func Resolve(requestedPath string) (Resolution, error) {
	return ResolveFrom("", requestedPath)
}

// ResolveFrom 基于 base 解析 requestedPath，并统一返回 workdir 与 workspaceRoot。
func ResolveFrom(base string, requestedPath string) (Resolution, error) {
	workdir, err := normalizeExistingDirectory(base, requestedPath)
	if err != nil {
		return Resolution{}, err
	}

	workspaceRoot, err := detectWorkspaceRoot(workdir)
	if err != nil {
		return Resolution{}, err
	}

	return Resolution{
		WorkspaceRoot: workspaceRoot,
		Workdir:       workdir,
	}, nil
}

// SameRoot 判断两个工作区根目录在当前平台语义下是否指向同一位置。
func SameRoot(left string, right string) bool {
	leftKey := pathKey(left)
	rightKey := pathKey(right)
	return leftKey != "" && leftKey == rightKey
}

// normalizeExistingDirectory 负责把路径解析成存在的绝对目录。
func normalizeExistingDirectory(base string, requestedPath string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace: resolve current directory: %w", err)
		}
		base = workingDir
	}

	target := strings.TrimSpace(requestedPath)
	if target == "" {
		target = "."
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}

	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path %q: %w", requestedPath, err)
	}
	absolute = filepath.Clean(absolute)

	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path %q: %w", requestedPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace: %q is not a directory", absolute)
	}
	return absolute, nil
}

// detectWorkspaceRoot 优先回溯 Git 根目录，非 Git 场景回退为目标目录自身。
func detectWorkspaceRoot(workdir string) (string, error) {
	current := filepath.Clean(strings.TrimSpace(workdir))
	if current == "" {
		return "", fmt.Errorf("workspace: workdir is empty")
	}

	for {
		gitMarker := filepath.Join(current, ".git")
		if _, err := os.Stat(gitMarker); err == nil {
			return current, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("workspace: inspect git marker %q: %w", gitMarker, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return workdir, nil
		}
		current = parent
	}
}

func pathKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	absolute, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absolute
	}
	trimmed = filepath.Clean(trimmed)
	if goruntime.GOOS == "windows" {
		return strings.ToLower(trimmed)
	}
	return trimmed
}
