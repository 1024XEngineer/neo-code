package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	tuiservices "neo-code/internal/tui/services"
)

// permissionPromptOption 表示权限审批面板中的一个可选项。
type permissionPromptOption struct {
	Label    string
	Shortcut string
	Decision tuiservices.PermissionResolutionDecision
}

var permissionPromptOptions = []permissionPromptOption{
	{
		Label:    "Allow once",
		Shortcut: "y",
		Decision: tuiservices.DecisionAllowOnce,
	},
	{
		Label:    "Allow session",
		Shortcut: "a",
		Decision: tuiservices.DecisionAllowSession,
	},
	{
		Label:    "Reject",
		Shortcut: "n",
		Decision: tuiservices.DecisionReject,
	},
}

// permissionPromptState 保存当前待审批请求与选项状态。
type permissionPromptState struct {
	Request    tuiservices.PermissionRequestPayload
	Selected   int
	Submitting bool
}

// autoPermissionApprovalState 记录 Full Access 自动审批中的请求，用于回执失败后的状态恢复。
type autoPermissionApprovalState struct {
	Request tuiservices.PermissionRequestPayload
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
func parsePermissionShortcut(input string) (tuiservices.PermissionResolutionDecision, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes", "once":
		return tuiservices.DecisionAllowOnce, true
	case "a", "always":
		return tuiservices.DecisionAllowSession, true
	case "n", "no", "reject", "deny":
		return tuiservices.DecisionReject, true
	default:
		return "", false
	}
}

// formatPermissionPromptLines 构造权限审批面板展示文本。
func formatPermissionPromptLines(state permissionPromptState) []string {
	normalizedIdx := normalizePermissionPromptSelection(state.Selected)
	target := fallbackText(sanitizePermissionDisplayText(state.Request.Target), "(empty)")
	if len(target) > 72 {
		target = target[:69] + "..."
	}
	lines := []string{
		fmt.Sprintf(
			"Permission request: %s (%s)",
			fallbackText(sanitizePermissionDisplayText(state.Request.ToolName), "unknown_tool"),
			fallbackText(sanitizePermissionDisplayText(state.Request.Operation), "unknown"),
		),
		fmt.Sprintf("Target: %s", target),
		"Keys: Up/Down Enter | y once | a session | n reject",
	}

	for index, item := range permissionPromptOptions {
		prefix := "  "
		if normalizedIdx == index {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", prefix, item.Shortcut, item.Label))
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
func parsePermissionRequestPayload(payload any) (tuiservices.PermissionRequestPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.PermissionRequestPayload:
		return typed, true
	case *tuiservices.PermissionRequestPayload:
		if typed == nil {
			return tuiservices.PermissionRequestPayload{}, false
		}
		return *typed, true
	default:
		return tuiservices.PermissionRequestPayload{}, false
	}
}

// parsePermissionResolvedPayload 解析权限决议事件载荷。
func parsePermissionResolvedPayload(payload any) (tuiservices.PermissionResolvedPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.PermissionResolvedPayload:
		return typed, true
	case *tuiservices.PermissionResolvedPayload:
		if typed == nil {
			return tuiservices.PermissionResolvedPayload{}, false
		}
		return *typed, true
	default:
		return tuiservices.PermissionResolvedPayload{}, false
	}
}

// renderPermissionPrompt 渲染审批输入框内容，替代普通输入框文本编辑状态。
func (a App) renderPermissionPrompt() string {
	if a.pendingPermission == nil {
		return a.input.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, formatPermissionPromptLines(*a.pendingPermission)...)
}
