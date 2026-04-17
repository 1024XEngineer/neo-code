package session

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const projectsDirName = "projects"

// projectDirectory 负责根据工作区根目录计算当前会话数据库所在目录。
func projectDirectory(baseDir string, workspaceRoot string) string {
	return filepath.Join(baseDir, projectsDirName, HashWorkspaceRoot(workspaceRoot))
}

// databasePath 返回当前工作区级 SQLite 数据库文件路径。
func databasePath(baseDir string, workspaceRoot string) string {
	return filepath.Join(projectDirectory(baseDir, workspaceRoot), sessionDatabaseFileName)
}

// assetsDirectory 返回当前工作区附件根目录。
func assetsDirectory(baseDir string, workspaceRoot string) string {
	return filepath.Join(projectDirectory(baseDir, workspaceRoot), assetsDirName)
}

// HashWorkspaceRoot 为规范化后的工作区根目录生成稳定哈希，供 session 和 memo 等包共享。
func HashWorkspaceRoot(workspaceRoot string) string {
	key := WorkspacePathKey(workspaceRoot)
	if key == "" {
		key = "unknown"
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// WorkspacePathKey 生成工作区路径的稳定比较键，Windows 下兼容大小写不敏感。
func WorkspacePathKey(workspaceRoot string) string {
	normalized := NormalizeWorkspaceRoot(workspaceRoot)
	if normalized == "" {
		return ""
	}
	if goruntime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

// NormalizeWorkspaceRoot 将工作区根目录规范化为绝对清洗路径。
func NormalizeWorkspaceRoot(workspaceRoot string) string {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		return ""
	}

	absolute, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absolute
	}
	return filepath.Clean(trimmed)
}

// EffectiveWorkdir 优先返回会话工作目录，缺失时回退到默认工作目录。
// 供 runtime、TUI 等上层模块统一调用，避免在多处重复实现回退逻辑。
func EffectiveWorkdir(sessionWorkdir string, defaultWorkdir string) string {
	workdir := strings.TrimSpace(sessionWorkdir)
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(defaultWorkdir)
}

// ResolveExistingDir 将路径解析为存在的绝对目录路径，用于启动校验和运行时路径规范化。
// 要求路径非空、可解析为绝对路径、存在且为目录。
func ResolveExistingDir(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("workdir is empty")
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve workdir %q: %w", trimmed, err)
	}

	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workdir %q: %w", trimmed, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %q is not a directory", absolute)
	}

	return filepath.Clean(absolute), nil
}
