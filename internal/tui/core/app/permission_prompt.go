package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	agentruntime "neo-code/internal/runtime"
)

// permissionPromptOption 表示权限审批面板中的一个可选项。
type permissionPromptOption struct {
	Label    string
	Hint     string
	Decision agentruntime.PermissionResolutionDecision
}

var permissionPromptOptions = []permissionPromptOption{
	{
		Label:    "Allow once",
		Hint:     "Approve this request once",
		Decision: agentruntime.PermissionResolutionAllowOnce,
	},
	{
		Label:    "Allow session",
		Hint:     "Approve similar requests for this session",
		Decision: agentruntime.PermissionResolutionAllowSession,
	},
	{
		Label:    "Reject",
		Hint:     "Reject this request",
		Decision: agentruntime.PermissionResolutionReject,
	},
}

// permissionPromptState 保存当前待审批请求与选项状态。
type permissionPromptState struct {
	Request    agentruntime.PermissionRequestPayload
	Selected   int
	Submitting bool
}

// normalizePermissionPromptSelection 保证选项下标始终落在有效范围。
func normalizePermissionPromptSelection(selected int) int {
	if len(permissionPromptOptions) == 0 {
		return 0
	}
	if selected < 0 {
		return len(permissionPromptOptions) - 1
	}
	if selected >= len(permissionPromptOptions) {
		return 0
	}
	return selected
}

// permissionPromptOptionAt 返回指定下标对应的审批选项。
func permissionPromptOptionAt(selected int) permissionPromptOption {
	index := normalizePermissionPromptSelection(selected)
	return permissionPromptOptions[index]
}

// parsePermissionShortcut 将快捷输入映射为审批决策。
func parsePermissionShortcut(input string) (agentruntime.PermissionResolutionDecision, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes", "once":
		return agentruntime.PermissionResolutionAllowOnce, true
	case "a", "always":
		return agentruntime.PermissionResolutionAllowSession, true
	case "n", "no", "reject", "deny":
		return agentruntime.PermissionResolutionReject, true
	default:
		return "", false
	}
}

// formatPermissionPromptLines 构造权限审批面板展示文本。
func formatPermissionPromptLines(state permissionPromptState) []string {
	normalizedIdx := normalizePermissionPromptSelection(state.Selected)
	lines := []string{
		fmt.Sprintf(
			"Permission request: %s (%s)",
			fallbackText(sanitizePermissionDisplayText(state.Request.ToolName), "unknown_tool"),
			fallbackText(sanitizePermissionDisplayText(state.Request.Operation), "unknown"),
		),
		fmt.Sprintf("Target: %s", fallbackText(sanitizePermissionDisplayText(state.Request.Target), "(empty)")),
		"Use Up/Down to choose, Enter to confirm (shortcuts: y=once, a=session, n=reject)",
	}

	for index, item := range permissionPromptOptions {
		prefix := "  "
		if normalizedIdx == index {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s  - %s", prefix, item.Label, item.Hint))
	}

	if state.Submitting {
		lines = append(lines, "Submitting permission decision...")
	}
	return lines
}

// fallbackText 返回去空格后的值；为空时返回默认文案。
func fallbackText(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

// sanitizePermissionDisplayText 清理模型可控的终端展示文本，避免控制字符污染审批界面。
func sanitizePermissionDisplayText(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	var builder strings.Builder
	lastWasSpace := false
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			if !lastWasSpace {
				builder.WriteByte(' ')
				lastWasSpace = true
			}
			continue
		}
		builder.WriteRune(r)
		lastWasSpace = unicode.IsSpace(r)
	}

	return strings.TrimSpace(builder.String())
}

// parsePermissionRequestPayload 解析权限请求事件载荷。
func parsePermissionRequestPayload(payload any) (agentruntime.PermissionRequestPayload, bool) {
	switch typed := payload.(type) {
	case agentruntime.PermissionRequestPayload:
		return typed, true
	case *agentruntime.PermissionRequestPayload:
		if typed == nil {
			return agentruntime.PermissionRequestPayload{}, false
		}
		return *typed, true
	default:
		return agentruntime.PermissionRequestPayload{}, false
	}
}

// parsePermissionResolvedPayload 解析权限决议事件载荷。
func parsePermissionResolvedPayload(payload any) (agentruntime.PermissionResolvedPayload, bool) {
	switch typed := payload.(type) {
	case agentruntime.PermissionResolvedPayload:
		return typed, true
	case *agentruntime.PermissionResolvedPayload:
		if typed == nil {
			return agentruntime.PermissionResolvedPayload{}, false
		}
		return *typed, true
	default:
		return agentruntime.PermissionResolvedPayload{}, false
	}
}

// renderPermissionPrompt 渲染审批输入框内容，替代普通输入框文本编辑状态。
func (a App) renderPermissionPrompt() string {
	if a.pendingPermission == nil {
		return a.input.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, formatPermissionPromptLines(*a.pendingPermission)...)
}
