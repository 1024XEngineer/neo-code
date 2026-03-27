package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderRoot(m model) string {
	header := m.renderHeader()
	body := m.renderBody()
	composer := m.renderComposer()
	footer := m.renderFooter()

	root := lipgloss.JoinVertical(lipgloss.Left, header, body, composer, footer)
	root = normalizeScreen(root, m.width, m.height)

	if m.state.sidebarOpen {
		sidebar := m.renderSidebarDrawer()
		root = overlayAt(root, sidebar, m.layout.sidebarRect.X, m.layout.sidebarRect.Y, m.width, m.height)
	}
	return root
}

func (m model) renderHeader() string {
	statusLabel := "IDLE"
	statusColor := themeSuccess
	if m.state.status.IsRunning {
		statusLabel = "RUNNING"
		statusColor = themeAccent2
	}

	activeSession := "No active session"
	if summary, ok := m.state.activeSessionSummary(); ok {
		activeSession = truncateVisual(summary.Title, max(20, m.width/3))
	}

	left := lipgloss.JoinHorizontal(
		lipgloss.Left,
		renderBadge("NEOCODE", themeAccent, themeCanvas),
		" ",
		renderBadge(statusLabel, statusColor, themeCanvas),
		" ",
		renderAction("Sessions", m.state.sidebarOpen, themeAccent),
		" ",
		renderAction("New", false, themeAccent2),
	)

	right := lipgloss.NewStyle().
		Foreground(themeMuted).
		Render("Updated " + formatClockWithSeconds(m.state.lastUpdatedAt))

	line1 := alignHeaderParts(max(10, m.width-2), left, right)
	line2 := lipgloss.NewStyle().Foreground(themeText).Render(truncateVisual(
		fmt.Sprintf(
			"Provider %s  Model %s  Workdir %s",
			emptyFallback(m.state.status.Provider, "-"),
			emptyFallback(m.state.status.Model, "-"),
			emptyFallback(m.state.status.Workdir, "-"),
		),
		max(12, m.width-2),
	))

	scrollState := "Bottom"
	if !m.viewport.AtTop() && !m.viewport.AtBottom() {
		scrollState = "Middle"
	}
	if m.viewport.AtTop() {
		scrollState = "Top"
	}
	scrollHint := scrollState
	if !m.viewport.AtBottom() {
		scrollHint += "  Jump to latest [L]"
	}

	line3 := lipgloss.NewStyle().Foreground(themeMuted).Render(truncateVisual(
		fmt.Sprintf(
			"Session %s  Pane %s  Scroll %s",
			activeSession,
			m.state.pane.Label(),
			scrollHint,
		),
		max(12, m.width-2),
	))

	return lipgloss.NewStyle().
		Width(m.layout.headerRect.Width).
		Height(m.layout.headerRect.Height).
		Padding(0, 1).
		Background(themeCanvas).
		Foreground(themeText).
		Render(strings.Join([]string{line1, line2, line3}, "\n"))
}

func (m model) renderBody() string {
	switch m.layout.mode {
	case layoutWide:
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.renderConversationPanel(),
			" ",
			m.renderRuntimePanel(),
		)
	default:
		return lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderConversationPanel(),
			m.renderRuntimePanel(),
		)
	}
}

func (m model) renderComposer() string {
	subtitle := "Enter sends | Ctrl+J newline"
	if m.state.status.IsRunning {
		subtitle = "Agent running | Enter disabled"
	}

	return renderPanelFrame(
		"Composer",
		subtitle,
		m.layout.composerRect.Width,
		m.layout.composerRect.Height,
		m.state.pane == paneCompose && !m.state.sidebarOpen,
		themeAccent,
		m.composer.View(),
	)
}

func (m model) renderFooter() string {
	left := lipgloss.NewStyle().Foreground(themeMuted).Render("Ready.")
	if strings.TrimSpace(m.state.notice) != "" {
		left = lipgloss.NewStyle().Foreground(activityColor(m.state.noticeTone)).Render(
			truncateVisual(m.state.notice, max(20, m.width/2)),
		)
	} else if m.state.lastError != "" {
		left = lipgloss.NewStyle().Foreground(themeDanger).Render(
			"Error: " + truncateVisual(m.state.lastError, max(20, m.width/2)),
		)
	} else if m.state.status.IsRunning {
		left = lipgloss.NewStyle().Foreground(themeAccent2).Render("Streaming response in progress...")
	}

	right := lipgloss.NewStyle().
		Foreground(themeMuted).
		Render(m.help.ShortHelpView(m.keys.ShortHelp()))

	return lipgloss.NewStyle().
		Width(m.layout.footerRect.Width).
		Background(themeCanvas).
		Foreground(themeMuted).
		Render(alignHeaderParts(m.layout.footerRect.Width, left, right))
}

func (m model) renderSidebarDrawer() string {
	width := m.layout.sidebarRect.Width
	height := m.layout.sidebarRect.Height
	innerWidth := max(12, width-4)
	innerHeight := max(6, height-3)

	lines := []string{
		lipgloss.NewStyle().Foreground(themeMuted).Render("Find sessions"),
		m.sidebarFilter.View(),
		"",
		renderSectionTitle("RECENT"),
	}

	sessions := m.filteredSessions()
	visible := max(1, innerHeight-len(lines)-3)
	start := min(m.state.sidebarScroll, max(0, len(sessions)-visible))
	end := min(len(sessions), start+visible)

	if len(sessions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(themeMuted).Render("No sessions matched the current filter."))
	} else {
		for idx := start; idx < end; idx++ {
			entry := sessions[idx]
			prefix := "  "
			titleColor := themeText
			metaColor := themeMuted
			if idx == m.state.sidebarSelection {
				prefix = "> "
				titleColor = themeAccent
				metaColor = themeAccent2
			}

			badges := make([]string, 0, 2)
			if entry.IsBusy {
				badges = append(badges, "busy")
			}
			if entry.IsActive {
				badges = append(badges, "open")
			}

			lines = append(lines,
				lipgloss.NewStyle().Bold(idx == m.state.sidebarSelection).Foreground(titleColor).Render(
					prefix+truncateVisual(entry.Summary.Title, max(8, innerWidth-3)),
				),
				lipgloss.NewStyle().Foreground(metaColor).Render(
					truncateVisual(compactSessionSubtitle(entry.Summary), max(8, innerWidth-2)),
				),
			)
			if len(badges) > 0 {
				lines = append(lines, lipgloss.NewStyle().Foreground(themeDim).Render(strings.Join(badges, "  ")))
			}
			lines = append(lines, "")
		}
	}

	lines = append(lines,
		renderSectionTitle("KEYS"),
		lipgloss.NewStyle().Foreground(themeMuted).Render("/ search | Enter open | Esc close"),
		lipgloss.NewStyle().Foreground(themeMuted).Render("Ctrl+N new session"),
	)

	return renderPanelFrame(
		"Sessions",
		fmt.Sprintf("%d visible", len(sessions)),
		width,
		height,
		true,
		themeAccent,
		limitLines(strings.Join(lines, "\n"), innerHeight),
	)
}

func (m model) renderHelpOverlay() string {
	panelWidth := min(92, max(54, m.width-6))
	bodyWidth := panelWidth - 4

	sections := []string{
		lipgloss.NewStyle().Bold(true).Foreground(themeText).Render("NeoCode Reader Controls"),
		lipgloss.NewStyle().Foreground(themeMuted).Render("The footer only shows high-signal shortcuts. Press ? anytime for the full map."),
		"",
		renderSectionTitle("CORE"),
		lipgloss.NewStyle().Foreground(themeText).Render("Enter send | Ctrl+J newline | Ctrl+V paste"),
		lipgloss.NewStyle().Foreground(themeText).Render("Ctrl+L clear composer"),
		lipgloss.NewStyle().Foreground(themeText).Render("Ctrl+B toggle sessions | / search sessions | Ctrl+N new session"),
		lipgloss.NewStyle().Foreground(themeText).Render("PgUp/PgDn browse | g/G or Home/End jump | L latest"),
		lipgloss.NewStyle().Foreground(themeText).Render("[ / ] select code block | y copy code | Y copy message"),
		"",
		renderSectionTitle("MOUSE"),
		lipgloss.NewStyle().Foreground(themeText).Render("Wheel scrolls the hovered panel."),
		lipgloss.NewStyle().Foreground(themeText).Render("Click a code block to select it, or click Copy in its header."),
		lipgloss.NewStyle().Foreground(themeText).Render("Click outside the session drawer to close it."),
		"",
		lipgloss.NewStyle().Foreground(themeAccent).Render("Press ? or Esc to return."),
	}

	panel := lipgloss.NewStyle().
		Width(panelWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(themeAccent).
		Background(themePanelAlt).
		Padding(1, 1).
		Render(lipgloss.NewStyle().Width(bodyWidth).Render(strings.Join(sections, "\n")))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}
