package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"neocode/internal/provider"
	"neocode/internal/runtime"
)

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
