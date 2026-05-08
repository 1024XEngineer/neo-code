package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuiservices "neo-code/internal/tui/services"
)

// userQuestionPromptState 保存当前待回答 ask_user 请求与提交流程状态。
type userQuestionPromptState struct {
	Request    tuiservices.UserQuestionRequestedPayload
	Submitting bool
}

// parseUserQuestionRequestedPayload 解析 ask_user 提问事件载荷。
func parseUserQuestionRequestedPayload(payload any) (tuiservices.UserQuestionRequestedPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.UserQuestionRequestedPayload:
		return typed, true
	case *tuiservices.UserQuestionRequestedPayload:
		if typed == nil {
			return tuiservices.UserQuestionRequestedPayload{}, false
		}
		return *typed, true
	default:
		return tuiservices.UserQuestionRequestedPayload{}, false
	}
}

// parseUserQuestionResolvedPayload 解析 ask_user 回答/跳过/超时事件载荷。
func parseUserQuestionResolvedPayload(payload any) (tuiservices.UserQuestionResolvedPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.UserQuestionResolvedPayload:
		return typed, true
	case *tuiservices.UserQuestionResolvedPayload:
		if typed == nil {
			return tuiservices.UserQuestionResolvedPayload{}, false
		}
		return *typed, true
	default:
		return tuiservices.UserQuestionResolvedPayload{}, false
	}
}

// formatUserQuestionPromptLines 构造 ask_user 提示面板展示文本。
func formatUserQuestionPromptLines(state userQuestionPromptState) []string {
	request := state.Request
	kind := fallbackText(strings.TrimSpace(request.Kind), "text")
	title := fallbackText(sanitizePermissionDisplayText(request.Title), "(untitled question)")
	description := sanitizePermissionDisplayText(request.Description)
	if description == "" {
		description = "(no description)"
	}

	lines := []string{
		fmt.Sprintf("Question: %s", title),
		fmt.Sprintf("Kind: %s", kind),
		fmt.Sprintf("Description: %s", description),
	}
	if len(request.Options) > 0 {
		lines = append(lines, "Options:")
		for index, option := range request.Options {
			lines = append(lines, fmt.Sprintf("  %d. %v", index+1, option))
		}
	}
	lines = append(lines, "Type answer and press Enter. Use /skip to skip when allowed.")
	if state.Submitting {
		lines = append(lines, "Submitting user question answer...")
	}
	return lines
}

// renderUserQuestionPrompt 渲染 ask_user 输入框内容。
func (a App) renderUserQuestionPrompt() string {
	if a.pendingUserQuestion == nil {
		return a.input.View()
	}
	lines := formatUserQuestionPromptLines(*a.pendingUserQuestion)
	lines = append(lines, "")
	lines = append(lines, a.input.View())
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
