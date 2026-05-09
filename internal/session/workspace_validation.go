package session

import (
	"os"
	"path/filepath"
	"strings"
)

const releaseCheckWorkspaceDirName = "neocode-web-release-check"

// WorkspaceRootExists 判断工作区根目录是否存在且为目录，供启动阶段清洗索引时复用。
func WorkspaceRootExists(workspaceRoot string) bool {
	normalized := NormalizeWorkspaceRoot(workspaceRoot)
	if normalized == "" {
		return false
	}
	info, err := os.Stat(normalized)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsTemporaryReleaseCheckWorkspaceRoot 判断路径是否属于临时发布检查工作区。
// 当前仅覆盖已确认命中的 neocode-web-release-check 临时目录，避免误删普通 build 目录。
func IsTemporaryReleaseCheckWorkspaceRoot(workspaceRoot string) bool {
	normalized := NormalizeWorkspaceRoot(workspaceRoot)
	if normalized == "" {
		return false
	}

	releaseCheckRoot := NormalizeWorkspaceRoot(filepath.Join(os.TempDir(), releaseCheckWorkspaceDirName))
	if releaseCheckRoot == "" {
		return false
	}

	pathKey := WorkspacePathKey(normalized)
	rootKey := WorkspacePathKey(releaseCheckRoot)
	if pathKey == "" || rootKey == "" {
		return false
	}
	if pathKey == rootKey {
		return true
	}
	return strings.HasPrefix(pathKey, rootKey+string(filepath.Separator))
}

// IsPersistentWorkspaceRoot 判断工作区是否适合保留在持久化索引中。
func IsPersistentWorkspaceRoot(workspaceRoot string) bool {
	if !WorkspaceRootExists(workspaceRoot) {
		return false
	}
	return !IsTemporaryReleaseCheckWorkspaceRoot(workspaceRoot)
}
