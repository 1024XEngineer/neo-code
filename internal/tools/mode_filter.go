package tools

import (
	"strings"

	"neo-code/internal/security"
)

// isReadOnlyVisibleTool 判断工具在只读阶段是否可见。
func isReadOnlyVisibleTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ToolNameFilesystemReadFile,
		ToolNameFilesystemGrep,
		ToolNameFilesystemGlob,
		ToolNameWebFetch,
		ToolNameMemoRecall,
		ToolNameMemoList,
		ToolNameTodoWrite:
		return true
	default:
		return false
	}
}

// isReadOnlyActionAllowed 判断当前权限动作是否属于只读阶段允许执行的范围。
func isReadOnlyActionAllowed(action security.Action) bool {
	if action.Type == security.ActionTypeRead {
		return true
	}
	return action.Type == security.ActionTypeWrite &&
		strings.EqualFold(strings.TrimSpace(action.Payload.Operation), ToolNameTodoWrite)
}
