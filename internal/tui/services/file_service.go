package services

import (
	"path/filepath"
	"strings"

	tuiinfra "neo-code/internal/tui/infra"
)

// CollectWorkspaceFiles 收集工作区候选文件（由 infra 实际执行扫描）。
func CollectWorkspaceFiles(root string, limit int) ([]string, error) {
	return tuiinfra.CollectWorkspaceFiles(root, limit)
}

// SuggestFileMatches 基于 query 从候选集中返回优先级排序后的建议列表。
func SuggestFileMatches(query string, candidates []string, limit int) []string {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}
	prefixMatches := make([]string, 0, limit)
	containsMatches := make([]string, 0, limit)
	for _, candidate := range candidates {
		lower := strings.ToLower(candidate)
		switch {
		case query == "" || strings.HasPrefix(lower, query):
			prefixMatches = append(prefixMatches, candidate)
		case strings.Contains(lower, query):
			containsMatches = append(containsMatches, candidate)
		}
		if len(prefixMatches)+len(containsMatches) >= limit {
			break
		}
	}

	out := append(prefixMatches, containsMatches...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ResolveWorkspaceDirectory 将工作区路径解析为绝对路径，失败时返回空字符串。
func ResolveWorkspaceDirectory(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	if strings.ContainsRune(workdir, '\x00') {
		return ""
	}
	absolute, err := filepath.Abs(workdir)
	if err != nil {
		return ""
	}
	return absolute
}
