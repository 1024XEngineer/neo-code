package filesystem

import (
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	filterReasonOutsideWorkspace = "outside_workspace"
	filterReasonSensitivePath    = "sensitive_path"
	filterReasonSymlinkEscape    = "symlink_escape"
)

// resultPathFilter 统一对文件路径做工作区边界、敏感路径与符号链接逃逸检查。
type resultPathFilter struct {
	root string
}

// newResultPathFilter 基于工作区根目录创建结果过滤器。
func newResultPathFilter(root string) (*resultPathFilter, error) {
	absoluteRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	return &resultPathFilter{root: filepath.Clean(absoluteRoot)}, nil
}

// evaluate 对候选路径执行统一安全检查，返回相对路径、拒绝原因与是否允许。
func (f *resultPathFilter) evaluate(path string) (relative string, reason string, allowed bool) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", filterReasonOutsideWorkspace, false
	}
	absolutePath = filepath.Clean(absolutePath)
	if !isPathWithinRoot(f.root, absolutePath) {
		return "", filterReasonOutsideWorkspace, false
	}

	relative = normalizeSlashPath(toRelativePath(f.root, absolutePath))
	if isSensitivePath(relative) {
		return "", filterReasonSensitivePath, false
	}

	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err == nil {
		resolvedPath, absErr := filepath.Abs(resolvedPath)
		if absErr != nil || !isPathWithinRoot(f.root, resolvedPath) {
			return "", filterReasonSymlinkEscape, false
		}
	}

	return relative, "", true
}

// isPathWithinRoot 判断目标路径是否仍在工作区根目录之内。
func isPathWithinRoot(root string, candidate string) bool {
	if goruntime.GOOS == "windows" {
		root = strings.ToLower(root)
		candidate = strings.ToLower(candidate)
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// isSensitivePath 判断相对路径是否命中默认敏感路径策略。
func isSensitivePath(relative string) bool {
	normalized := strings.TrimSpace(strings.ToLower(normalizeSlashPath(relative)))
	if normalized == "" || normalized == "." {
		return false
	}

	if normalized == ".git" || strings.HasPrefix(normalized, ".git/") {
		return true
	}

	base := strings.ToLower(filepath.Base(normalized))
	if strings.HasPrefix(base, ".env") {
		return true
	}
	if strings.HasSuffix(base, ".pem") {
		return true
	}

	return false
}
