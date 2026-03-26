package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
	"neocode/internal/runtime"
)

var (
	themeCanvas   = lipgloss.Color("#08131D")
	themePanel    = lipgloss.Color("#102231")
	themePanelAlt = lipgloss.Color("#0E1C28")
	themeCode     = lipgloss.Color("#09111A")
	themeBorder   = lipgloss.Color("#29516B")
	themeAccent   = lipgloss.Color("#37C8B2")
	themeAccent2  = lipgloss.Color("#F2B84B")
	themeDanger   = lipgloss.Color("#F26C5E")
	themeSuccess  = lipgloss.Color("#7ED957")
	themeText     = lipgloss.Color("#EAF4FF")
	themeMuted    = lipgloss.Color("#9CB4C7")
	themeDim      = lipgloss.Color("#5F7485")
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

func (m model) renderConversationPanel() string {
	subtitle := "Ready for the next prompt"
	if summary, ok := m.state.activeSessionSummary(); ok {
		subtitle = truncateVisual(summary.Title, max(18, m.layout.conversationRect.Width/2))
	}
	if !m.viewport.AtBottom() {
		subtitle += " | reading history"
	}

	active := !m.state.sidebarOpen && m.state.pane != paneCompose
	return renderPanelFrame(
		"Conversation",
		subtitle,
		m.layout.conversationRect.Width,
		m.layout.conversationRect.Height,
		active,
		themeAccent2,
		m.viewport.View(),
	)
}

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

func renderPanelFrame(
	title string,
	subtitle string,
	width int,
	height int,
	active bool,
	accent lipgloss.Color,
	content string,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	bodyWidth := max(8, width-4)
	bodyHeight := max(1, height-3)
	borderColor := themeBorder
	background := themePanel
	if active {
		borderColor = accent
		background = themePanelAlt
	}

	header := alignHeaderParts(
		bodyWidth,
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render(strings.ToUpper(title)),
		lipgloss.NewStyle().Foreground(themeMuted).Render(truncateVisual(subtitle, max(10, bodyWidth/2))),
	)

	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Height(bodyHeight).
		Foreground(themeText).
		Background(background).
		Render(content)

	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(background).
		Padding(0, 1).
		Render(header + "\n" + body)
}

func buildConversation(
	session runtime.Session,
	streaming string,
	width int,
	selectedMessage int,
	selectedCodeBlock int,
) renderedConversation {
	if len(session.Messages) == 0 && strings.TrimSpace(streaming) == "" {
		return buildEmptyConversation(width)
	}

	lines := make([]string, 0, max(16, len(session.Messages)*6))
	rendered := renderedConversation{
		Messages:   make([]renderedMessage, 0, len(session.Messages)+1),
		CodeBlocks: make([]renderedCodeBlock, 0, 8),
	}

	codeIndex := 0
	for idx, message := range session.Messages {
		if len(lines) > 0 {
			lines = append(lines, "")
		}

		cardLines, msgMeta, codeBlocks, nextCodeIndex := renderMessageCard(
			message,
			width,
			idx,
			idx == selectedMessage,
			selectedCodeBlock,
			codeIndex,
			len(lines),
		)
		lines = append(lines, cardLines...)
		rendered.Messages = append(rendered.Messages, msgMeta)
		rendered.CodeBlocks = append(rendered.CodeBlocks, codeBlocks...)
		codeIndex = nextCodeIndex
	}

	if strings.TrimSpace(streaming) != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		cardLines, msgMeta, codeBlocks, _ := renderMessageCard(
			provider.Message{Role: provider.RoleAssistant, Content: streaming},
			width,
			len(rendered.Messages),
			selectedMessage == len(rendered.Messages),
			selectedCodeBlock,
			codeIndex,
			len(lines),
		)
		msgMeta.CopyText = strings.TrimSpace(streaming)
		lines = append(lines, cardLines...)
		rendered.Messages = append(rendered.Messages, msgMeta)
		rendered.CodeBlocks = append(rendered.CodeBlocks, codeBlocks...)
	}

	rendered.Content = strings.Join(lines, "\n")
	rendered.LineCount = len(lines)
	return rendered
}

func buildEmptyConversation(width int) renderedConversation {
	cardWidth := max(24, width)
	lines := []string{
		boxTop(cardWidth, themeBorder),
		boxLine(
			cardWidth,
			themeBorder,
			themePanelAlt,
			lipgloss.NewStyle().Bold(true).Foreground(themeText).Render(" Welcome to NeoCode"),
		),
		boxLine(
			cardWidth,
			themeBorder,
			themePanelAlt,
			lipgloss.NewStyle().Foreground(themeMuted).Render(" Ask for code edits, reviews, or tool-assisted investigation."),
		),
		boxLine(
			cardWidth,
			themeBorder,
			themePanelAlt,
			lipgloss.NewStyle().Foreground(themeMuted).Render(" Press ? for shortcuts, Ctrl+B for sessions, and y to copy code."),
		),
		boxBottom(cardWidth, themeBorder),
	}

	return renderedConversation{
		Content:   strings.Join(lines, "\n"),
		Messages:  nil,
		LineCount: len(lines),
	}
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

func renderMessageCard(
	message provider.Message,
	width int,
	messageIndex int,
	selectedMessage bool,
	selectedCodeBlock int,
	codeIndexStart int,
	lineOffset int,
) ([]string, renderedMessage, []renderedCodeBlock, int) {
	cardWidth := max(24, width)
	bodyWidth := max(12, cardWidth-2)
	textWidth := max(8, bodyWidth-2)

	label, subtitle, accent := messageChrome(message)
	cardBackground := themePanel
	if selectedMessage {
		cardBackground = themePanelAlt
	}

	lines := []string{boxTop(cardWidth, accent)}
	header := alignHeaderParts(
		bodyWidth,
		renderBadge(label, accent, cardBackground),
		lipgloss.NewStyle().Foreground(themeMuted).Render(truncateVisual(subtitle, max(10, bodyWidth/2))),
	)
	lines = append(lines, boxLine(cardWidth, accent, cardBackground, header))

	blocks := parseMessageBlocks(message)
	codeBlocks := make([]renderedCodeBlock, 0, 2)
	copyParts := make([]string, 0, len(blocks)+1)
	currentCodeIndex := codeIndexStart

	for _, block := range blocks {
		switch block.Kind {
		case blockParagraph:
			for _, line := range wrapText(block.Text, textWidth) {
				lines = append(lines, boxLine(
					cardWidth,
					accent,
					cardBackground,
					lipgloss.NewStyle().Foreground(themeText).Render(" "+line),
				))
			}
			copyParts = append(copyParts, block.Text)
		case blockCode:
			lines = append(lines, boxLine(cardWidth, accent, cardBackground, ""))
			language := strings.ToUpper(emptyFallback(block.Language, "code"))
			copyLabel := renderAction("Copy", selectedCodeBlock == currentCodeIndex, themeAccent)
			headerParts := layoutHeaderParts(
				bodyWidth,
				renderBadge(language, themeAccent2, themeCode),
				copyLabel,
			)
			headerLineIndex := lineOffset + len(lines)
			lines = append(lines, boxLine(cardWidth, accent, themeCode, headerParts.Text))
			copyStart := 1 + max(0, headerParts.RightStart)

			startLine := headerLineIndex
			codeLines := strings.Split(normalizeNewlines(block.Text), "\n")
			if len(codeLines) == 0 {
				codeLines = []string{""}
			}
			for _, rawLine := range codeLines {
				wrapped := wrapText(rawLine, max(4, textWidth-2))
				for _, wrappedLine := range wrapped {
					prefix := "  "
					if selectedCodeBlock == currentCodeIndex {
						prefix = "> "
					}
					codeText := lipgloss.NewStyle().Foreground(themeText).Render(prefix + wrappedLine)
					lines = append(lines, boxLine(cardWidth, accent, themeCode, codeText))
				}
			}
			endLine := lineOffset + len(lines) - 1
			codeBlocks = append(codeBlocks, renderedCodeBlock{
				Index:        currentCodeIndex,
				MessageIndex: messageIndex,
				Language:     block.Language,
				Content:      block.Text,
				StartLine:    startLine,
				EndLine:      endLine,
				HeaderLine:   headerLineIndex,
				CopyX1:       copyStart,
				CopyX2:       copyStart + headerParts.RightWidth,
			})
			copyParts = append(copyParts, block.Text)
			currentCodeIndex++
		case blockToolCall:
			lines = append(lines, boxLine(cardWidth, accent, cardBackground, ""))
			headerText := renderBadge("TOOL "+strings.ToUpper(emptyFallback(block.ToolCall.Name, "call")), themeAccent2, themePanel)
			lines = append(lines, boxLine(cardWidth, accent, cardBackground, headerText))
			for _, line := range wrapText(formatToolArguments(block.ToolCall.Arguments), textWidth) {
				lines = append(lines, boxLine(cardWidth, accent, themeCode, lipgloss.NewStyle().Foreground(themeMuted).Render(" "+line)))
			}
			copyParts = append(copyParts, block.ToolCall.Name, block.ToolCall.Arguments)
		case blockToolResult:
			lines = append(lines, boxLine(cardWidth, accent, cardBackground, ""))
			lines = append(lines, boxLine(cardWidth, accent, cardBackground, renderBadge("TOOL RESULT", themeAccent2, themePanel)))
			for _, line := range wrapText(block.Text, textWidth) {
				lines = append(lines, boxLine(cardWidth, accent, themeCode, lipgloss.NewStyle().Foreground(themeMuted).Render(" "+line)))
			}
			copyParts = append(copyParts, block.Text)
		}
	}

	if len(blocks) == 0 {
		lines = append(lines, boxLine(cardWidth, accent, cardBackground, lipgloss.NewStyle().Foreground(themeMuted).Render(" (empty)")))
	}

	lines = append(lines, boxBottom(cardWidth, accent))
	msgMeta := renderedMessage{
		MessageIndex: messageIndex,
		StartLine:    lineOffset,
		EndLine:      lineOffset + len(lines) - 1,
		CopyText:     strings.TrimSpace(strings.Join(copyParts, "\n\n")),
	}
	if msgMeta.CopyText == "" {
		msgMeta.CopyText = strings.TrimSpace(message.Content)
	}

	return lines, msgMeta, codeBlocks, currentCodeIndex
}

func messageChrome(message provider.Message) (label string, subtitle string, accent lipgloss.Color) {
	switch message.Role {
	case provider.RoleUser:
		return "YOU", "prompt", themeAccent
	case provider.RoleAssistant:
		if len(message.ToolCalls) > 0 {
			return "ASSISTANT", fmt.Sprintf("requested %d tool(s)", len(message.ToolCalls)), themeAccent2
		}
		return "ASSISTANT", "response", themeSuccess
	case provider.RoleTool:
		if strings.HasPrefix(strings.TrimSpace(message.Content), "tool error:") {
			return "TOOL", emptyFallback(message.ToolCallID, "tool result"), themeDanger
		}
		return "TOOL", emptyFallback(message.ToolCallID, "tool result"), themeAccent2
	default:
		return strings.ToUpper(emptyFallback(message.Role, "message")), "context", themeBorder
	}
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

func renderSectionTitle(label string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(themeAccent2).Render(label)
}

func renderBadge(label string, color lipgloss.Color, background lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(background).
		Background(color).
		Padding(0, 1).
		Render(label)
}

func renderAction(label string, active bool, color lipgloss.Color) string {
	background := themePanelAlt
	foreground := color
	if active {
		background = color
		foreground = themeCanvas
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(foreground).
		Background(background).
		Padding(0, 1).
		Render(label)
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

func boxTop(width int, color lipgloss.Color) string {
	if width < 2 {
		return strings.Repeat(" ", max(0, width))
	}
	return lipgloss.NewStyle().Foreground(color).Render("+" + strings.Repeat("-", width-2) + "+")
}

func boxBottom(width int, color lipgloss.Color) string {
	if width < 2 {
		return strings.Repeat(" ", max(0, width))
	}
	return lipgloss.NewStyle().Foreground(color).Render("+" + strings.Repeat("-", width-2) + "+")
}

func boxLine(width int, border lipgloss.Color, background lipgloss.Color, content string) string {
	if width < 2 {
		return ""
	}
	bodyWidth := width - 2
	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Background(background).
		Foreground(themeText).
		Render(fitLine(content, bodyWidth))
	return lipgloss.NewStyle().Foreground(border).Render("|") + body + lipgloss.NewStyle().Foreground(border).Render("|")
}

type alignedHeaderParts struct {
	Text       string
	RightStart int
	RightWidth int
}

func layoutHeaderParts(width int, left string, right string) alignedHeaderParts {
	parts := alignedHeaderParts{RightStart: -1}
	if width <= 0 {
		return parts
	}
	if right == "" {
		parts.Text = fitLine(left, width)
		return parts
	}

	right = ansi.Truncate(right, width, "")
	rightWidth := ansi.StringWidth(right)
	if rightWidth == 0 {
		parts.Text = fitLine(left, width)
		return parts
	}
	if rightWidth >= width {
		parts.Text = fitLine(right, width)
		parts.RightStart = 0
		parts.RightWidth = width
		return parts
	}

	leftWidth := max(0, width-rightWidth-1)
	left = ansi.Truncate(left, leftWidth, "")
	spacing := max(1, width-ansi.StringWidth(left)-rightWidth)

	parts.Text = fitLine(left+strings.Repeat(" ", spacing)+right, width)
	parts.RightStart = width - rightWidth
	parts.RightWidth = rightWidth
	return parts
}

func alignHeaderParts(width int, left string, right string) string {
	return layoutHeaderParts(width, left, right).Text
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeScreen(content string, width int, height int) string {
	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, max(height, len(lines)))
	for _, line := range lines {
		normalized = append(normalized, fitLine(line, width))
	}
	for len(normalized) < height {
		normalized = append(normalized, strings.Repeat(" ", width))
	}
	if len(normalized) > height {
		normalized = normalized[:height]
	}
	return strings.Join(normalized, "\n")
}

func overlayAt(base string, overlay string, x int, y int, width int, height int) string {
	baseLines := strings.Split(normalizeScreen(base, width, height), "\n")
	overlayLines := strings.Split(overlay, "\n")

	for idx, overlayLine := range overlayLines {
		target := y + idx
		if target < 0 || target >= len(baseLines) {
			continue
		}
		overlayWidth := ansi.StringWidth(overlayLine)
		if overlayWidth == 0 {
			continue
		}
		if x >= width {
			continue
		}
		if overlayWidth > width-x {
			overlayLine = ansi.Truncate(overlayLine, width-x, "")
			overlayWidth = ansi.StringWidth(overlayLine)
		}

		prefix := ansi.Cut(baseLines[target], 0, x)
		suffix := ""
		if x+overlayWidth < width {
			suffix = ansi.Cut(baseLines[target], x+overlayWidth, width)
		}
		baseLines[target] = fitLine(prefix+overlayLine+suffix, width)
	}

	return strings.Join(baseLines, "\n")
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	padding := width - ansi.StringWidth(line)
	if padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}
