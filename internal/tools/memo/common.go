package memo

import (
	"fmt"
	"strings"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

// nilServiceError 构造 memo 工具缺少 service 依赖时的统一错误结果。
func nilServiceError(toolName string) (tools.ToolResult, error) {
	err := fmt.Errorf("%s: service is nil", toolName)
	return tools.NewErrorResult(toolName, tools.NormalizeErrorReason(toolName, err), "", nil), err
}

// invalidArgumentsError 构造 memo 工具参数解析失败时的统一错误结果。
func invalidArgumentsError(toolName string, err error) (tools.ToolResult, error) {
	wrappedErr := fmt.Errorf("%s: %w", toolName, err)
	return tools.NewErrorResult(toolName, "invalid arguments", wrappedErr.Error(), nil), wrappedErr
}

// memoScopePropertySchema 返回 memo 工具统一的 scope 参数 schema 描述。
func memoScopePropertySchema() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Optional scope filter: all, user, or project.",
		"enum":        []string{"all", "user", "project"},
	}
}

// parseMemoScope 解析 memo scope，并根据 allowAll 决定是否接受 all。
func parseMemoScope(raw string, allowAll bool) (memo.Scope, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		if allowAll {
			return memo.ScopeAll, nil
		}
		return memo.ScopeProject, fmt.Errorf("memo: scope is required")
	}
	switch memo.Scope(normalized) {
	case memo.ScopeUser:
		return memo.ScopeUser, nil
	case memo.ScopeProject:
		return memo.ScopeProject, nil
	case memo.ScopeAll:
		if allowAll {
			return memo.ScopeAll, nil
		}
	}
	return "", fmt.Errorf("memo: unsupported scope %q", raw)
}
