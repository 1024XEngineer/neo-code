package security

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// ResolveWorkspacePath 按 workspace sandbox 的既有语义解析并校验工作区路径。
func ResolveWorkspacePath(root string, target string) (string, string, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return "", "", errors.New("security: workspace root is empty")
	}

	absoluteRoot, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return "", "", fmt.Errorf("security: resolve workspace root: %w", err)
	}

	canonicalRoot, _, err := resolveCanonicalWorkspaceRoot(cleanedPathKey(absoluteRoot))
	if err != nil {
		return "", "", err
	}

	absoluteTarget, err := ResolveWorkspacePathFromRoot(canonicalRoot, target)
	if err != nil {
		return "", "", err
	}
	return canonicalRoot, absoluteTarget, nil
}

// ResolveWorkspacePathFromRoot 在已知 canonical workspace root 的前提下解析并校验目标路径。
func ResolveWorkspacePathFromRoot(root string, target string) (string, error) {
	absoluteTarget, err := absoluteWorkspaceTarget(root, target)
	if err != nil {
		return "", err
	}
	if !isWithinWorkspace(root, absoluteTarget) {
		return "", fmt.Errorf("security: path %q escapes workspace root: %w", target, ErrWorkspaceBoundaryViolation)
	}
	if _, err := ensureNoSymlinkEscape(root, absoluteTarget, target); err != nil {
		return "", err
	}
	return absoluteTarget, nil
}

// ResolveWorkspaceWalkPathFromRoot 在已知 canonical workspace root 的前提下，
// 为遍历热路径做轻量校验：普通文件只做 containment，符号链接条目再回落到完整校验。
func ResolveWorkspaceWalkPathFromRoot(root string, target string, entry fs.DirEntry) (string, error) {
	absoluteTarget, err := absoluteWorkspaceTarget(root, target)
	if err != nil {
		return "", err
	}
	if !isWithinWorkspace(root, absoluteTarget) {
		return "", fmt.Errorf("security: path %q escapes workspace root: %w", target, ErrWorkspaceBoundaryViolation)
	}
	if isVerifiedRegularWalkEntry(entry) {
		return absoluteTarget, nil
	}
	if _, err := ensureNoSymlinkEscape(root, absoluteTarget, target); err != nil {
		return "", err
	}
	return absoluteTarget, nil
}

// isVerifiedRegularWalkEntry 判断 WalkDir 条目是否可安全走普通文件快速路径。
// 对 Type()==0 的条目会再调用 Info 二次确认，避免“未知类型”误判为普通文件而绕过符号链接校验。
func isVerifiedRegularWalkEntry(entry fs.DirEntry) bool {
	if entry == nil {
		return false
	}
	entryType := entry.Type()
	if !entryType.IsRegular() {
		return false
	}
	if entryType != 0 {
		return true
	}
	info, err := entry.Info()
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
