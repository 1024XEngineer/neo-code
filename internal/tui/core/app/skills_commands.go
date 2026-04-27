package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/skills"
	tuiservices "neo-code/internal/tui/services"
)

const (
	maxRenderedSkillsCount = 50
	maxSkillFieldLength    = 120
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// skillCommandResultMsg 承载 skills 相关 slash 命令的异步执行结果。
type skillCommandResultMsg struct {
	Notice           string
	Err              error
	RequestSessionID string
}

// handleSkillsCommand 处理 `/skills`，输出当前可用技能列表与会话激活状态。
func (a *App) handleSkillsCommand() tea.Cmd {
	sessionID := strings.TrimSpace(a.state.ActiveSessionID)
	return a.runSkillCommand(sessionID,
		func(ctx context.Context) (string, error) {
			states, err := a.runtime.ListAvailableSkills(ctx, sessionID)
			if err != nil {
				return "", normalizeSkillCommandError(err)
			}
			return formatAvailableSkills(states, sessionID), nil
		},
	)
}

// handleSkillCommand 解析 `/skill ...` 子命令，并分发到 use/off/active。
func (a *App) handleSkillCommand(rest string) tea.Cmd {
	action, argument := splitFirstWord(strings.TrimSpace(rest))
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "use":
		return a.handleSkillUseCommand(argument)
	case "off":
		return a.handleSkillOffCommand(argument)
	case "active":
		if strings.TrimSpace(argument) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageSkillActive))
			return nil
		}
		return a.handleSkillActiveCommand()
	default:
		a.applyInlineCommandError("usage: /skill use <id> | /skill off <id> | /skill active")
		return nil
	}
}

// handleSkillUseCommand 在当前会话激活指定 skill。
func (a *App) handleSkillUseCommand(skillID string) tea.Cmd {
	sessionID, ok := a.requireActiveSessionForSkillCommand()
	if !ok {
		return nil
	}
	normalizedSkillID := strings.TrimSpace(skillID)
	if normalizedSkillID == "" || isSkillUsagePlaceholder(normalizedSkillID) {
		a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageSkillUse))
		return nil
	}

	return a.runSkillCommand(sessionID,
		func(ctx context.Context) (string, error) {
			if err := a.runtime.ActivateSessionSkill(ctx, sessionID, normalizedSkillID); err != nil {
				return "", normalizeSkillCommandError(err)
			}
			return fmt.Sprintf("Skill activated: %s", sanitizeSkillDisplayText(normalizedSkillID, "(unknown)")), nil
		},
	)
}

// handleSkillOffCommand 在当前会话停用指定 skill。
func (a *App) handleSkillOffCommand(skillID string) tea.Cmd {
	sessionID, ok := a.requireActiveSessionForSkillCommand()
	if !ok {
		return nil
	}
	normalizedSkillID := strings.TrimSpace(skillID)
	if normalizedSkillID == "" || isSkillUsagePlaceholder(normalizedSkillID) {
		a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageSkillOff))
		return nil
	}

	return a.runSkillCommand(sessionID,
		func(ctx context.Context) (string, error) {
			if err := a.runtime.DeactivateSessionSkill(ctx, sessionID, normalizedSkillID); err != nil {
				return "", normalizeSkillCommandError(err)
			}
			return fmt.Sprintf("Skill deactivated: %s", sanitizeSkillDisplayText(normalizedSkillID, "(unknown)")), nil
		},
	)
}

// isSkillUsagePlaceholder 判断入参是否还是 help 文案中的占位符（例如 <id>）。
func isSkillUsagePlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">")
}

// handleSkillActiveCommand 输出当前会话激活技能状态（含缺失项标记）。
func (a *App) handleSkillActiveCommand() tea.Cmd {
	sessionID, ok := a.requireActiveSessionForSkillCommand()
	if !ok {
		return nil
	}
	return a.runSkillCommand(sessionID,
		func(ctx context.Context) (string, error) {
			states, err := a.runtime.ListSessionSkills(ctx, sessionID)
			if err != nil {
				return "", normalizeSkillCommandError(err)
			}
			return formatSessionSkills(states), nil
		},
	)
}

// requireActiveSessionForSkillCommand 校验 skills 会话命令所需的 session 上下文是否存在。
func (a *App) requireActiveSessionForSkillCommand() (string, bool) {
	sessionID := strings.TrimSpace(a.state.ActiveSessionID)
	if sessionID != "" {
		return sessionID, true
	}
	a.applyInlineCommandError("skill command requires an active session; send one message first or switch session via /session")
	return "", false
}

// runSkillCommand 统一封装 skills 相关本地命令的异步执行与结果消息封装。
func (a *App) runSkillCommand(sessionID string, run func(context.Context) (string, error)) tea.Cmd {
	return tuiservices.RunLocalCommandCmd(
		run,
		func(notice string, err error) tea.Msg {
			return skillCommandResultMsg{Notice: notice, Err: err, RequestSessionID: sessionID}
		},
	)
}

// normalizeSkillCommandError 将 gateway 不支持等底层错误映射为可读的命令反馈。
func normalizeSkillCommandError(err error) error {
	if err == nil {
		return nil
	}
	if isGatewayUnsupportedActionError(err) {
		return errors.New("gateway does not support skills management; please upgrade gateway and client to the latest version")
	}
	return err
}

// formatAvailableSkills 渲染 `/skills` 输出，包含可见技能清单与当前激活标记。
func formatAvailableSkills(states []tuiservices.AvailableSkillState, sessionID string) string {
	if len(states) == 0 {
		return "No skills found in local registry."
	}
	rows := make([]string, 0, min(len(states), maxRenderedSkillsCount)+3)
	header := "Available skills:"
	if strings.TrimSpace(sessionID) != "" {
		header += " (active marks from current session)"
	}
	rows = append(rows, header)
	visibleCount := min(len(states), maxRenderedSkillsCount)
	for _, state := range states[:visibleCount] {
		scope := strings.TrimSpace(string(state.Descriptor.Scope))
		if scope == "" {
			scope = "explicit"
		}
		status := "inactive"
		if state.Active {
			status = "active"
		}
		description := sanitizeSkillDisplayText(state.Descriptor.Description, "-")
		id := sanitizeSkillDisplayText(state.Descriptor.ID, "(unknown)")
		source := sanitizeSkillDisplayText(formatSkillDisplaySource(state.Descriptor.Source), "unknown")
		version := sanitizeSkillDisplayText(state.Descriptor.Version, "-")
		scope = sanitizeSkillDisplayText(scope, "explicit")
		rows = append(rows, fmt.Sprintf(
			"- %s [%s] scope=%s source=%s version=%s | %s",
			id,
			status,
			scope,
			source,
			version,
			description,
		))
	}
	if len(states) > visibleCount {
		rows = append(rows, fmt.Sprintf("... and %d more skills", len(states)-visibleCount))
	}
	return strings.Join(rows, "\n")
}

// formatSkillDisplaySource 组装 skills 来源展示文本，优先展示层级信息以便定位覆盖关系。
func formatSkillDisplaySource(source skills.Source) string {
	kind := strings.TrimSpace(string(source.Kind))
	layer := strings.TrimSpace(string(source.Layer))
	switch {
	case layer != "" && kind != "":
		return layer + "/" + kind
	case layer != "":
		return layer
	default:
		return kind
	}
}

// formatSessionSkills 渲染 `/skill active` 输出，并明确缺失技能状态。
func formatSessionSkills(states []tuiservices.SessionSkillState) string {
	if len(states) == 0 {
		return "No active skills in current session."
	}
	normalized := append([]tuiservices.SessionSkillState(nil), states...)
	sort.Slice(normalized, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(normalized[i].SkillID)) <
			strings.ToLower(strings.TrimSpace(normalized[j].SkillID))
	})

	rows := make([]string, 0, len(normalized)+1)
	rows = append(rows, "Active skills:")
	for _, state := range normalized {
		if state.Missing {
			rows = append(rows, fmt.Sprintf("- %s [missing]", sanitizeSkillDisplayText(state.SkillID, "(unknown)")))
			continue
		}
		if state.Descriptor == nil {
			rows = append(rows, fmt.Sprintf("- %s [active]", sanitizeSkillDisplayText(state.SkillID, "(unknown)")))
			continue
		}
		description := sanitizeSkillDisplayText(state.Descriptor.Description, "-")
		id := sanitizeSkillDisplayText(state.Descriptor.ID, "(unknown)")
		rows = append(rows, fmt.Sprintf("- %s [active] %s", id, description))
	}
	return strings.Join(rows, "\n")
}

// sanitizeSkillDisplayText 清理并截断技能展示文本，避免控制字符污染和超长输出影响渲染。
func sanitizeSkillDisplayText(value string, fallback string) string {
	cleaned := sanitizePermissionDisplayText(ansiEscapePattern.ReplaceAllString(value, ""))
	if strings.TrimSpace(cleaned) == "" {
		cleaned = strings.TrimSpace(fallback)
	}
	if strings.TrimSpace(cleaned) == "" {
		return ""
	}
	if len([]rune(cleaned)) <= maxSkillFieldLength {
		return cleaned
	}
	return string([]rune(cleaned)[:maxSkillFieldLength-3]) + "..."
}
