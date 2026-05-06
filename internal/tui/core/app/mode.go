package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	agentsession "neo-code/internal/session"
)

const planAccent = "#f59e0b"

func normalizeAgentModeValue(mode string) agentsession.AgentMode {
	return agentsession.NormalizeAgentMode(agentsession.AgentMode(strings.TrimSpace(mode)))
}

func (a *App) currentAgentMode() agentsession.AgentMode {
	return normalizeAgentModeValue(a.state.CurrentAgentMode)
}

func (a *App) setCurrentAgentMode(mode string) {
	normalized := normalizeAgentModeValue(mode)
	a.state.CurrentAgentMode = string(normalized)
	a.state.RunContext.Mode = string(normalized)
	a.applyModeTheme(normalized)
}

func (a *App) toggleAgentMode() agentsession.AgentMode {
	next := agentsession.AgentModeBuild
	if a.currentAgentMode() == agentsession.AgentModeBuild {
		next = agentsession.AgentModePlan
	}
	a.setCurrentAgentMode(string(next))
	return next
}

func formatAgentModeLabel(mode string) string {
	return strings.ToUpper(string(normalizeAgentModeValue(mode)))
}

func modeAccent(mode agentsession.AgentMode) string {
	if mode == agentsession.AgentModePlan {
		return planAccent
	}
	return purpleAccent
}

func (a *App) applyModeTheme(mode agentsession.AgentMode) {
	accent := lipgloss.Color(modeAccent(mode))

	a.styles.panelFocused = a.styles.panelFocused.BorderForeground(accent)
	a.styles.sessionMetaFocus = a.styles.sessionMetaFocus.Foreground(accent)
	a.styles.commandMenuTitle = a.styles.commandMenuTitle.Foreground(accent)
	a.styles.commandUsageMatch = a.styles.commandUsageMatch.Foreground(accent)
	a.styles.inputPrefix = a.styles.inputPrefix.Foreground(accent).Bold(true)
	a.styles.inputBoxFocused = a.styles.inputBoxFocused.BorderForeground(accent)
	a.styles.startupPrompt = a.styles.startupPrompt.Foreground(accent).Bold(true)

	a.input.Cursor.Style = a.input.Cursor.Style.Foreground(accent)
	a.input.FocusedStyle.Prompt = a.input.FocusedStyle.Prompt.Foreground(accent).Bold(true)
	a.input.BlurredStyle.Prompt = a.input.BlurredStyle.Prompt.Foreground(accent).Bold(true)

	a.spinner.Style = a.spinner.Style.Foreground(accent)
	a.help.Styles.ShortKey = a.help.Styles.ShortKey.Foreground(accent).Bold(true)
	a.help.Styles.FullKey = a.help.Styles.FullKey.Foreground(accent).Bold(true)
}
