package runner

import (
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// CapSigner 在 runner 端负责验证 capability token 和 workspace 边界。
type CapSigner struct {
	workdirAllowlist []string
}

// NewCapSigner 创建 runner 端的安全校验器。
func NewCapSigner(workdirAllowlist []string) *CapSigner {
	return &CapSigner{
		workdirAllowlist: workdirAllowlist,
	}
}

// VerifyToolRequest 验证工具执行请求是否被允许。
// 检查：
// 1. 工具名是否被允许（默认所有工具允许，除非有 capability token）
// 2. 路径是否在工作区 allowlist 内
func (s *CapSigner) VerifyToolRequest(req ToolExecutionRequest, workdir string) error {
	_ = req // reserved for future capability token validation
	_ = workdir
	return nil // no additional checks for MVP; capability token validation added later
}

// VerifyPath 验证目标路径是否在 allowlist 范围内。
func (s *CapSigner) VerifyPath(targetPath string) error {
	if len(s.workdirAllowlist) == 0 {
		return nil // 无限制
	}

	normalized := normalizePath(targetPath)
	for _, allowed := range s.workdirAllowlist {
		base := normalizePath(allowed)
		if base == "" {
			continue
		}
		if normalized == base || strings.HasPrefix(normalized, base+"/") {
			return nil
		}
	}
	return ErrCapabilityPathNotAllowed
}

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	if goruntime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}
