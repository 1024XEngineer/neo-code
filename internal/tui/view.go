package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"github.com/dust/neo-code/internal/provider"
)

type layout struct {
	stacked       bool
	contentWidth  int
	contentHeight int
	sidebarWidth  int
	sidebarHeight int
	rightWidth    int
	rightHeight   int
}

func (a App) View() string {
	if a.width < 84 || a.height < 24 {
		return a.styles.doc.Render("Window too small.\nPlease resize to at least 84x24.")
	}

	lay := a.computeLayout()
	header := a.renderHeader()
	body := a.renderBody(lay)
	helpView := a.renderHelp(lay.contentWidth)
	return a.styles.doc.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, helpView))
}

func (a App) renderHeader() string {
	status := a.state.StatusText
	if a.state.IsAgentRunning {
		status = a.spinner.View() + " " + fallback(status, statusRunning)
	}

	brand := lipgloss.JoinHorizontal(
		lipgloss.Center,
		a.styles.headerBrand.Render("NeoCode"),
		a.styles.headerSpacer.Render(""),
		a.styles.headerSub.Render("immersive coding agent"),
	)

	meta := lipgloss.JoinHorizontal(
		lipgloss.Top,
		a.styles.badgeAgent.Render("Provider "+a.state.CurrentProvider),
		a.styles.badgeUser.Render("Model "+a.state.CurrentModel),
		a.styles.badgeMuted.Render("Focus "+a.focusLabel()),
		a.statusBadge(status),
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		brand,
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			a.styles.headerMeta.Render("Workdir "+trimMiddle(a.state.CurrentWorkdir, max(28, a.width/3))),
			a.styles.headerSpacer.Render(""),
			meta,
		),
	)
}

func (a App) renderBody(lay layout) string {
	sidebar := a.renderSidebar(lay.sidebarWidth, lay.sidebarHeight)
	stream := a.renderWaterfall(lay.rightWidth, lay.rightHeight)
	if lay.stacked {
		return lipgloss.JoinVertical(lipgloss.Left, sidebar, stream)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, stream)
}

func (a App) renderSidebar(width int, height int) string {
	return a.renderPanel(sidebarTitle, sidebarSubtitle, a.sessions.View(), width, height, a.focus == panelSessions)
}

func (a App) renderWaterfall(width int, height int) string {
	if a.state.ShowModelPicker {
		return lipgloss.Place(
			width,
			height,
			lipgloss.Center,
			lipgloss.Center,
			a.renderModelPicker(clamp(width-10, 36, 56), clamp(height-6, 10, 14)),
		)
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		a.styles.streamTitle.Render(fallback(a.state.ActiveSessionTitle, draftSessionTitle)),
		a.styles.headerSpacer.Render(""),
		a.styles.streamMeta.Render(fmt.Sprintf("%d messages", len(a.activeMessages))),
	)
	subline := lipgloss.JoinHorizontal(
		lipgloss.Top,
		a.styles.streamMeta.Render("Active model "+a.state.CurrentModel),
		a.styles.headerSpacer.Render(""),
		a.styles.streamMeta.Render(fallback(a.state.CurrentTool, a.state.StatusText)),
	)
	transcript := a.styles.streamContent.Width(width).Height(a.transcript.Height).Render(a.transcript.View())

	parts := []string{header, subline, transcript}
	if menu := a.renderCommandMenu(width); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, a.renderPrompt(width))

	return lipgloss.NewStyle().Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (a App) renderModelPicker(width int, height int) string {
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		a.styles.panelTitle.Render(modelPickerTitle),
		a.styles.panelSubtitle.Render(modelPickerSubtitle),
		a.modelPicker.View(),
	)
	return a.styles.panelFocused.Width(width).Height(height).Render(content)
}

func (a App) renderPrompt(width int) string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		a.styles.inputMeta.Render(composerHintText),
		a.styles.inputLine.Width(width).Render(a.input.View()),
	)
}

func (a App) renderCommandMenu(width int) string {
	suggestions := a.matchingSlashCommands(strings.TrimSpace(a.input.Value()))
	if len(suggestions) == 0 {
		return ""
	}

	lines := make([]string, 0, len(suggestions)+1)
	lines = append(lines, a.styles.commandMenuTitle.Render(commandMenuTitle))
	for _, suggestion := range suggestions {
		usageStyle := a.styles.commandUsage
		if suggestion.Match {
			usageStyle = a.styles.commandUsageMatch
		}
		lines = append(lines, lipgloss.JoinHorizontal(
			lipgloss.Top,
			usageStyle.Render(suggestion.Command.Usage),
			lipgloss.NewStyle().Width(2).Render(""),
			a.styles.commandDesc.Render(suggestion.Command.Description),
		))
	}

	return a.styles.commandMenu.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (a App) commandMenuHeight(width int) int {
	menu := a.renderCommandMenu(width)
	if strings.TrimSpace(menu) == "" {
		return 0
	}
	return lipgloss.Height(menu)
}

func (a App) renderHelp(width int) string {
	a.help.ShowAll = a.state.ShowHelp
	return a.styles.footer.Width(width).Render(a.help.View(a.keys))
}

func (a App) renderPanel(title string, subtitle string, body string, width int, height int, focused bool) string {
	style := a.styles.panel
	if focused {
		style = a.styles.panelFocused
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		a.styles.panelTitle.Render(title),
		lipgloss.NewStyle().Width(2).Render(""),
		a.styles.panelSubtitle.Render(subtitle),
	)
	bodyHeight := max(3, height-lipgloss.Height(header)-2)
	panelBody := a.styles.panelBody.Width(max(10, width-4)).Height(bodyHeight).Render(body)
	return style.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, panelBody))
}

func (a App) renderMessageBlock(message provider.Message, width int) string {
	switch message.Role {
	case roleEvent:
		return a.styles.inlineNotice.Width(width).Render("  > " + wrapPlain(message.Content, max(16, width-6)))
	case roleError:
		return a.styles.inlineError.Width(width).Render("  ! " + wrapPlain(message.Content, max(16, width-6)))
	case roleSystem:
		return a.styles.inlineSystem.Width(width).Render("  - " + wrapPlain(message.Content, max(16, width-6)))
	}

	maxMessageWidth := clamp(width, 24, max(24, int(float64(width)*0.92)))
	tag := messageTagAgent
	tagStyle := a.styles.messageAgentTag
	bodyStyle := a.styles.messageBody

	switch message.Role {
	case roleUser:
		tag = messageTagUser
		tagStyle = a.styles.messageUserTag
		bodyStyle = a.styles.messageUserBody
	case roleTool:
		tag = messageTagTool
		tagStyle = a.styles.messageToolTag
		bodyStyle = a.styles.messageToolBody
	}

	content := strings.TrimSpace(message.Content)
	if content == "" && len(message.ToolCalls) > 0 {
		names := make([]string, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			names = append(names, call.Name)
		}
		content = "Tool calls: " + strings.Join(names, ", ")
	}
	if content == "" {
		content = emptyMessageText
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tagStyle.Render(tag),
		a.renderMessageContent(content, maxMessageWidth-2, bodyStyle),
	)
}

func (a App) renderMessageContent(content string, width int, bodyStyle lipgloss.Style) string {
	parts := strings.Split(content, "```")
	if len(parts) == 1 {
		return bodyStyle.Width(width).Render(wrapPlain(content, max(16, width-2)))
	}

	blocks := make([]string, 0, len(parts))
	for i, part := range parts {
		if i%2 == 0 {
			trimmed := strings.Trim(part, "\n")
			if trimmed == "" {
				continue
			}
			blocks = append(blocks, bodyStyle.Width(width).Render(wrapPlain(trimmed, max(16, width-2))))
			continue
		}

		code := strings.Trim(part, "\n")
		lines := strings.Split(code, "\n")
		if len(lines) > 1 && !strings.Contains(lines[0], " ") && !strings.Contains(lines[0], "\t") {
			code = strings.Join(lines[1:], "\n")
		}
		blocks = append(blocks, a.styles.codeBlock.Width(width).Render(a.styles.codeText.Width(max(10, width-4)).Render(code)))
	}

	if len(blocks) == 0 {
		return bodyStyle.Width(width).Render(emptyMessageText)
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (a App) statusBadge(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return a.styles.badgeError.Render(text)
	case a.state.IsAgentRunning || strings.Contains(lower, "running") || strings.Contains(lower, "thinking"):
		return a.styles.badgeWarning.Render(text)
	default:
		return a.styles.badgeSuccess.Render(text)
	}
}

func (a App) focusLabel() string {
	switch a.focus {
	case panelSessions:
		return focusLabelSessions
	case panelTranscript:
		return focusLabelTranscript
	default:
		return focusLabelComposer
	}
}

func (a App) computeLayout() layout {
	contentWidth := max(80, a.width-4)
	helpHeight := 2
	if a.state.ShowHelp {
		helpHeight = 6
	}

	contentHeight := max(18, a.height-7-helpHeight)
	lay := layout{contentWidth: contentWidth, contentHeight: contentHeight}
	if contentWidth < 110 {
		lay.stacked = true
		lay.sidebarWidth = contentWidth
		lay.sidebarHeight = clamp(contentHeight/3, 9, 13)
		lay.rightWidth = contentWidth
		lay.rightHeight = max(10, contentHeight-lay.sidebarHeight)
		return lay
	}

	lay.sidebarWidth = 30
	lay.sidebarHeight = contentHeight
	lay.rightWidth = contentWidth - lay.sidebarWidth
	lay.rightHeight = contentHeight
	return lay
}

func (a App) isFilteringSessions() bool {
	return a.sessions.FilterState() != list.Unfiltered
}
