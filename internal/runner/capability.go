package runner

import (
	"encoding/json"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"neo-code/internal/security"
)

// CapSigner 在 runner 端负责验证 capability token 和 workspace 边界。
type CapSigner struct {
	capVerifier      *security.CapabilitySigner
	workdirAllowlist []string
}

// NewCapSigner 创建 runner 端的安全校验器。
func NewCapSigner(workdirAllowlist []string) *CapSigner {
	return &CapSigner{
		workdirAllowlist: workdirAllowlist,
	}
}

// SetCapVerifier 设置用于验证 capability token 签名的验签器。
func (s *CapSigner) SetCapVerifier(verifier *security.CapabilitySigner) {
	s.capVerifier = verifier
}

// VerifyToolRequest 验证工具执行请求是否被允许。
// 检查：
// 1. CapabilityToken 签名、TTL、工具白名单（如果提供了 token）
// 2. 路径是否在工作区 allowlist 内
func (s *CapSigner) VerifyToolRequest(req ToolExecutionRequest, workdir string) error {
	// 如果提供了 capability token，验证其签名和权限
	if req.CapabilityToken != nil {
		if s.capVerifier != nil {
			if err := s.capVerifier.Verify(*req.CapabilityToken); err != nil {
				return ErrCapabilitySignatureInvalid
			}
		}
		if err := req.CapabilityToken.ValidateAt(time.Now()); err != nil {
			return ErrCapabilityTokenExpired
		}
		if !isToolAllowed(req.CapabilityToken.AllowedTools, req.ToolName) {
			return ErrCapabilityToolNotAllowed
		}
	}

	// 验证路径是否在 allowlist 内
	if req.Arguments != nil {
		if err := s.verifyPathsInArgs(req.Arguments, workdir); err != nil {
			return err
		}
	}

	return nil
}

// verifyPathsInArgs 检查参数中的路径是否在 allowlist 范围内。
func (s *CapSigner) verifyPathsInArgs(args json.RawMessage, workdir string) error {
	if len(s.workdirAllowlist) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil
	}
	for _, v := range m {
		str, ok := v.(string)
		if !ok {
			continue
		}
		if looksLikePath(str) {
			resolved := resolvePath(str, workdir)
			if err := s.VerifyPath(resolved); err != nil {
				return err
			}
		}
	}
	return nil
}

// looksLikePath 判断字符串是否看起来像文件路径。
func looksLikePath(s string) bool {
	if strings.Contains(s, "://") {
		return false
	}
	return strings.Contains(s, "/") || strings.Contains(s, "\\") ||
		strings.HasPrefix(s, ".") || filepath.IsAbs(s)
}

// resolvePath 将 target 基于 workdir 解析为绝对路径，用于 allowlist 比较。
func resolvePath(target string, workdir string) string {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(filepath.ToSlash(trimmed), "/") {
		return trimmed
	}
	base := strings.TrimSpace(workdir)
	if base != "" {
		return filepath.Join(base, trimmed)
	}
	return trimmed
}

// isToolAllowed 判断工具名是否在 token 允许列表中。
func isToolAllowed(allowedTools []string, toolName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	for _, allowed := range allowedTools {
		if strings.ToLower(strings.TrimSpace(allowed)) == normalized {
			return true
		}
	}
	return false
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
