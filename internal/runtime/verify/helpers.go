package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// verifierMetadataResult 统一生成 metadata 缺失时的 required/optional 结果。
func verifierMetadataResult(name string, required bool, metadataKey string, skipSummary string) VerificationResult {
	if !required {
		return VerificationResult{
			Name:    name,
			Status:  VerificationPass,
			Summary: skipSummary,
			Reason:  "optional verifier skipped",
		}
	}
	return VerificationResult{
		Name:    name,
		Status:  VerificationSoftBlock,
		Summary: fmt.Sprintf("%s is required but missing", metadataKey),
		Reason:  fmt.Sprintf("missing %s metadata", metadataKey),
	}
}

// verificationDeniedResult 统一生成路径越界等安全拒绝结果。
func verificationDeniedResult(name string, summary string, reason string, evidence map[string]any) VerificationResult {
	return VerificationResult{
		Name:       name,
		Status:     VerificationFail,
		Summary:    summary,
		Reason:     reason,
		ErrorClass: ErrorClassPermissionDenied,
		Evidence:   evidence,
	}
}

// metadataStringSlice 从 metadata 中解析字符串列表。
func metadataStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return compactStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if normalized := strings.TrimSpace(fmt.Sprintf("%v", item)); normalized != "" {
				out = append(out, normalized)
			}
		}
		return out
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	default:
		return nil
	}
}

// metadataStringMapSlice 从 metadata 中解析 map[string][]string。
func metadataStringMapSlice(metadata map[string]any, key string) map[string][]string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	normalized := make(map[string][]string)
	switch typed := raw.(type) {
	case map[string][]string:
		for path, values := range typed {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			for _, value := range values {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				normalized[path] = append(normalized[path], value)
			}
		}
	case map[string]any:
		for path, value := range typed {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			values := metadataStringSlice(map[string]any{"value": value}, "value")
			if len(values) == 0 {
				continue
			}
			normalized[path] = values
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// compactStrings 会去除空白与空字符串，返回紧凑切片。
func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := strings.TrimSpace(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

// resolvePathWithinWorkdir 将 verifier 输入路径解析为绝对路径，并确保路径仍位于 workdir 内部。
func resolvePathWithinWorkdir(workdir string, rawPath string) (string, error) {
	normalizedWorkdir := strings.TrimSpace(workdir)
	if normalizedWorkdir == "" {
		return "", fmt.Errorf("workdir is required")
	}
	workdirAbs, err := filepath.Abs(filepath.Clean(normalizedWorkdir))
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}

	normalizedPath := strings.TrimSpace(rawPath)
	if normalizedPath == "" {
		return "", fmt.Errorf("path is required")
	}
	resolvedPath := normalizedPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(workdirAbs, resolvedPath)
	}
	resolvedPath, err = filepath.Abs(filepath.Clean(resolvedPath))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	if err := ensurePathWithinBase(workdirAbs, resolvedPath); err != nil {
		return "", fmt.Errorf("path %q is outside workdir", rawPath)
	}

	// 对已存在路径追加真实路径校验，防止通过符号链接逃逸 workdir。
	workdirReal, err := filepath.EvalSymlinks(workdirAbs)
	if err != nil {
		workdirReal = workdirAbs
	}
	resolvedReal, err := filepath.EvalSymlinks(resolvedPath)
	switch {
	case err == nil:
		if err := ensurePathWithinBase(workdirReal, resolvedReal); err != nil {
			return "", fmt.Errorf("path %q resolves outside workdir", rawPath)
		}
	case os.IsNotExist(err):
		// 目标不存在时由上层 verifier 按 missing 处理；此处只做已存在路径的边界约束。
	default:
		return "", fmt.Errorf("resolve symlink path: %w", err)
	}
	return resolvedPath, nil
}

// ensurePathWithinBase 校验目标路径仍位于基准目录内，避免路径穿越或边界越权。
func ensurePathWithinBase(base string, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return fmt.Errorf("check path relation: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("outside base path")
	}
	return nil
}
