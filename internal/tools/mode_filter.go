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
		ToolNameTodoWrite,
		ToolNameAskUser:
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
	if action.Type == security.ActionTypeInteraction {
		return true
	}
	return action.Type == security.ActionTypeWrite &&
		strings.EqualFold(strings.TrimSpace(action.Payload.Operation), ToolNameTodoWrite)
}

const ()

// isPlanModeOnlyTool 判断工具是否仅限 plan 模式可见。
func isPlanModeOnlyTool(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), ToolNameAskUser)
}

const (
	errAskUserNotAvailableInCurrentMode = "ask_user is not available in current mode"
)
