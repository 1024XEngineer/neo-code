package tools

import (
	"strings"

	"neo-code/internal/security"
)

// EnrichToolResultFacts 基于权限动作与工具本地事实补齐结构化执行事实。
// 注意：此处不信任外部工具 metadata 中的 workspace/verification 字段，避免越过信任边界。
func EnrichToolResultFacts(action security.Action, result ToolResult) ToolResult {
	facts := result.Facts
	if !facts.WorkspaceWrite {
		facts.WorkspaceWrite = defaultWorkspaceWriteFromAction(action)
	}
	if facts.VerificationPassed {
		facts.VerificationPerformed = true
	}
	facts.VerificationScope = strings.TrimSpace(facts.VerificationScope)
	if !facts.VerificationPerformed {
		facts.VerificationPassed = false
		facts.VerificationScope = ""
	}

	result.Facts = facts
	return result
}

// defaultWorkspaceWriteFromAction 按权限动作类型推导默认写入事实，仅明确写能力才标记为写入。
func defaultWorkspaceWriteFromAction(action security.Action) bool {
	switch action.Type {
	case security.ActionTypeRead:
		return false
	case security.ActionTypeWrite:
		return true
	default:
		return false
	}
}
