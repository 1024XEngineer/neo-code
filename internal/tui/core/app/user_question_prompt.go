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
	lines = append(lines, fmt.Sprintf("Required: %s", map[bool]string{true: "yes", false: "no"}[request.Required]))
	lines = append(lines, fmt.Sprintf("Allow skip: %s", map[bool]string{true: "yes", false: "no"}[request.AllowSkip]))
	if len(request.Options) > 0 {
		lines = append(lines, "Options:")
		for index, option := range request.Options {
			lines = append(lines, fmt.Sprintf("  %d. %s", index+1, formatUserQuestionOptionDisplay(option)))
		}
	}
	switch kind {
	case "single_choice":
		lines = append(lines, "Input one option label or index and press Enter.")
	case "multi_choice":
		lines = append(lines, "Input comma-separated option labels or indices and press Enter.")
		if request.MaxChoices > 0 {
			lines = append(lines, fmt.Sprintf("Max choices: %d", request.MaxChoices))
		}
	default:
		lines = append(lines, "Type answer and press Enter.")
	}
	lines = append(lines, "Use /skip to skip when allowed.")
	if state.Submitting {
		lines = append(lines, "Submitting user question answer...")
	}
	return lines
}

// formatUserQuestionOptionDisplay 统一格式化 ask_user 选项展示文本，避免 map 原始输出难以阅读。
func formatUserQuestionOptionDisplay(option any) string {
	switch typed := option.(type) {
	case string:
		label := strings.TrimSpace(typed)
		if label != "" {
			return label
		}
	case map[string]any:
		label := strings.TrimSpace(anyToString(typed["label"]))
		desc := strings.TrimSpace(anyToString(typed["description"]))
		if label != "" && desc != "" {
			return fmt.Sprintf("%s - %s", label, desc)
		}
		if label != "" {
			return label
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%v", option))
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
