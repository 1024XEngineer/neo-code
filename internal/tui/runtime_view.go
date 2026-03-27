package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) renderRuntimePanel() string {
	subtitle := "tools + activity"
	if len(m.state.toolsForSession(m.state.activeSessionID)) > 0 {
		subtitle = fmt.Sprintf("%d tool(s) running", len(m.state.toolsForSession(m.state.activeSessionID)))
	}

	return renderPanelFrame(
		"Runtime",
		subtitle,
		m.layout.runtimeRect.Width,
		m.layout.runtimeRect.Height,
		false,
		themeBorder,
		m.runtimeViewport.View(),
	)
}

func buildRuntimeContent(m *model) string {
	width := max(20, m.layout.runtimeRect.inner().Width)
	lines := []string{
		renderSectionTitle("SNAPSHOT"),
	}

	if session, ok := m.state.activeSession(m.runtime); ok {
		stats := collectSessionStats(session)
		lines = append(lines,
			lipgloss.NewStyle().Foreground(themeText).Render(truncateVisual(session.Title, width)),
			lipgloss.NewStyle().Foreground(themeMuted).Render(
				fmt.Sprintf("%d user  %d assistant  %d tool", stats.UserMessages, stats.AssistantMessages, stats.ToolMessages),
			),
			lipgloss.NewStyle().Foreground(themeMuted).Render(
				fmt.Sprintf("%d tool request(s)", stats.ToolRequests),
			),
		)
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeMuted).Render("No session loaded."))
	}

	lines = append(lines, "", renderSectionTitle("ACTIVE TOOLS"))
	activeTools := m.state.toolsForSession(m.state.activeSessionID)
	if len(activeTools) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeMuted).Render("No tool is running right now."))
	} else {
		for _, toolState := range activeTools {
			lines = append(lines,
				lipgloss.NewStyle().Foreground(themeAccent).Render(truncateVisual(toolState.Call.Name, width)),
				lipgloss.NewStyle().Foreground(themeMuted).Render("started "+formatClockWithSeconds(toolState.StartedAt)),
			)
		}
	}

	lines = append(lines, "", renderSectionTitle("ACTIVITY"))
	activityLimit := max(4, m.layout.runtimeRect.inner().Height/3)
	entries := recentActivities(m.state.activities, m.state.activeSessionID, activityLimit)
	if len(entries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeMuted).Render("No runtime events yet."))
	} else {
		for _, entry := range entries {
			lines = append(lines, renderActivityEntry(entry, width)...)
		}
	}

	return strings.Join(lines, "\n")
}

func renderActivityEntry(entry activityEntry, width int) []string {
	label := renderBadge(activityLabel(entry.Tone), activityColor(entry.Tone), themePanel)
	titleWidth := max(10, width-ansi.StringWidth(label)-1)
	title := lipgloss.NewStyle().Foreground(themeText).Render(truncateVisual(entry.Title, titleWidth))
	lines := []string{fmt.Sprintf("%s %s", label, title)}
	if detail := strings.TrimSpace(entry.Detail); detail != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeMuted).Render("  "+truncateVisual(detail, max(10, width-2))))
	}
	return lines
}

func activityColor(tone activityTone) lipgloss.Color {
	switch tone {
	case toneRunning:
		return themeAccent2
	case toneSuccess:
		return themeSuccess
	case toneError:
		return themeDanger
	default:
		return themeAccent
	}
}

func activityLabel(tone activityTone) string {
	switch tone {
	case toneRunning:
		return "RUN"
	case toneSuccess:
		return "OK"
	case toneError:
		return "ERR"
	default:
		return "INFO"
	}
}
