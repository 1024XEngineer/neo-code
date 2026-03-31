package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/provider"
)

type layout struct {
	stacked       bool
	contentWidth  int
	contentHeight int
	sidebarWidth  int
	sidebarHeight int
	rightWidth    int
	rightHeight   int
	bodyGap       int
}

func (a App) View() string {
	docWidth := max(0, a.width-a.styles.doc.GetHorizontalFrameSize())
	docHeight := max(0, a.height-a.styles.doc.GetVerticalFrameSize())
	if docWidth < 84 || docHeight < 24 {
		return strings.TrimRight(a.styles.doc.Render(lipgloss.Place(docWidth, docHeight, lipgloss.Left, lipgloss.Top, "Window too small.\nPlease resize to at least 84x24.")), "\n")
	}

	lay := a.computeLayout()
	header := a.renderHeader(lay.contentWidth)
	body := a.renderBody(lay)
	helpView := a.renderHelp(lay.contentWidth)
	usedHeight := lipgloss.Height(header) + lipgloss.Height(body) + lipgloss.Height(helpView)
	spacerHeight := max(0, docHeight-usedHeight)
	parts := []string{header, body}
	if spacerHeight > 0 {
		parts = append(parts, lipgloss.NewStyle().Height(spacerHeight).Render(""))
	}
	parts = append(parts, helpView)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return strings.TrimRight(a.styles.doc.Render(lipgloss.Place(docWidth, docHeight, lipgloss.Left, lipgloss.Top, content)), "\n")
}

func (a App) renderHeader(width int) string {
	status := a.state.StatusText
	if a.state.IsAgentRunning {
		status = a.spinner.View() + " " + fallback(status, statusRunning)
	}

	brand := lipgloss.JoinHorizontal(
		lipgloss.Center,
		a.styles.headerBrand.Render("NeoCode"),
	)

	meta := lipgloss.JoinHorizontal(
		lipgloss.Top,
		a.styles.badgeAgent.Render(a.state.CurrentProvider),
		a.styles.badgeUser.Render(a.state.CurrentModel),
		a.styles.badgeMuted.Render(a.focusLabel()),
		a.statusBadge(status),
	)

	spacerWidth := lipgloss.Width(a.styles.headerSpacer.Render(""))
	workdirLabel := a.styles.headerLabel.Render("Workdir")
	workdirWidth := max(12, width-lipgloss.Width(brand)-lipgloss.Width(meta)-(spacerWidth*2)-lipgloss.Width(workdirLabel)-1)
	workdir := lipgloss.JoinHorizontal(
		lipgloss.Center,
		workdirLabel,
		" ",
		a.styles.headerPath.Render(trimMiddle(a.state.CurrentWorkdir, workdirWidth)),
	)

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		brand,
		a.styles.headerSpacer.Render(""),
		workdir,
		a.styles.headerSpacer.Render(""),
		meta,
	)
	return a.styles.headerBar.Width(width).Render(lipgloss.Place(width, 1, lipgloss.Left, lipgloss.Top, header))
}

func (a App) renderBody(lay layout) string {
	sidebar := a.renderSidebar(lay.sidebarWidth, lay.sidebarHeight)
	stream := a.renderWaterfall(lay.rightWidth, lay.rightHeight)
	if lay.stacked {
		return lipgloss.JoinVertical(lipgloss.Left, sidebar, stream)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		sidebar,
		lipgloss.NewStyle().Width(lay.bodyGap).Render(""),
		stream,
	)
}

func (a App) renderSidebar(width int, height int) string {
	style := a.styles.panel
	if a.focus == panelSessions {
		style = a.styles.panelFocused
	}

	frameHeight := style.GetVerticalFrameSize()
	borderWidth := 2
	paddingWidth := style.GetHorizontalFrameSize() - borderWidth
	panelWidth := max(1, width-borderWidth)
	bodyWidth := max(10, panelWidth-paddingWidth)
	header := a.renderSidebarHeader(bodyWidth)
	bodyHeight := max(3, height-frameHeight-lipgloss.Height(header))
	a.sessions.SetSize(bodyWidth, bodyHeight)
	body := a.styles.panelBody.Width(bodyWidth).Height(bodyHeight).Render(a.sessions.View())

	panel := style.Width(panelWidth).Height(max(1, height-frameHeight)).Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, panel)
}

func (a App) renderWaterfall(width int, height int) string {
	if a.state.ActivePicker != pickerNone {
		return lipgloss.Place(
			width,
			height,
			lipgloss.Center,
			lipgloss.Center,
			a.renderPicker(clamp(width-10, 36, 56), clamp(height-6, 10, 14)),
		)
	}

	transcript := a.styles.streamContent.Width(width).Height(a.transcript.Height).Render(a.transcript.View())

	parts := []string{transcript}
	if menu := a.renderCommandMenu(width); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, a.renderPrompt(width))

	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (a App) renderPicker(width int, height int) string {
	frameHeight := a.styles.panelFocused.GetVerticalFrameSize()
	title := modelPickerTitle
	subtitle := modelPickerSubtitle
	body := a.modelPicker.View()
	if a.state.ActivePicker == pickerProvider {
		title = providerPickerTitle
		subtitle = providerPickerSubtitle
		body = a.providerPicker.View()
	}
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		a.styles.panelTitle.Render(title),
		a.styles.panelSubtitle.Render(subtitle),
		body,
	)
	panel := a.styles.panelFocused.
		Width(max(1, width-2)).
		Height(max(1, height-frameHeight)).
		Render(content)
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, panel)
}

func (a App) renderPrompt(width int) string {
	box := a.styles.inputBox
	if a.focus == panelInput && a.state.ActivePicker == pickerNone {
		box = a.styles.inputBoxFocused
	}

	// 计算边框和内边距占用的空间
	boxWidth := a.composerBoxWidth(width)

	return box.Width(boxWidth).Render(a.input.View())
}

func (a App) renderSidebarHeader(width int) string {
	title := a.styles.panelTitle.Render(sidebarTitle)
	filterWidth := max(6, width-lipgloss.Width(title)-1)
	titleRow := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		lipgloss.NewStyle().Width(1).Render(""),
		a.styles.panelSubtitle.Render(trimRunes(sidebarFilterHint, filterWidth)),
	)
	openRow := a.styles.panelSubtitle.Render(trimRunes(sidebarOpenHint, width))
	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.Place(width, 1, lipgloss.Left, lipgloss.Top, titleRow),
		lipgloss.Place(width, 1, lipgloss.Left, lipgloss.Top, openRow),
	)
}

func (a App) renderPanel(title string, subtitle string, body string, width int, height int, focused bool) string {
	style := a.styles.panel
	if focused {
		style = a.styles.panelFocused
	}

	frameHeight := style.GetVerticalFrameSize()
	borderWidth := 2
	paddingWidth := style.GetHorizontalFrameSize() - borderWidth
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		a.styles.panelTitle.Render(title),
		lipgloss.NewStyle().Width(2).Render(""),
		a.styles.panelSubtitle.Render(subtitle),
	)
	panelWidth := max(1, width-borderWidth)
	bodyWidth := max(10, panelWidth-paddingWidth)
	bodyHeight := max(3, height-frameHeight-lipgloss.Height(header))
	panelBody := a.styles.panelBody.Width(bodyWidth).Height(bodyHeight).Render(body)
	panel := style.Width(panelWidth).Height(max(1, height-frameHeight)).Render(lipgloss.JoinVertical(lipgloss.Left, header, panelBody))
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, panel)
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

	maxMessageWidth := clamp(int(float64(width)*0.84), 24, width)
	tag := messageTagAgent
	tagStyle := a.styles.messageAgentTag
	bodyStyle := a.styles.messageBody
	blockAlign := lipgloss.Left

	switch message.Role {
	case roleUser:
		maxMessageWidth = clamp(int(float64(width)*0.68), 24, width)
		tag = messageTagUser
		tagStyle = a.styles.messageUserTag
		bodyStyle = a.styles.messageUserBody
		blockAlign = lipgloss.Right
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

	contentBlock := a.renderMessageContent(content, maxMessageWidth-2, bodyStyle)
	if message.Role == roleUser {
		contentBlock = lipgloss.PlaceHorizontal(maxMessageWidth, lipgloss.Right, contentBlock)
	}

	block := lipgloss.JoinVertical(
		blockAlign,
		tagStyle.Render(tag),
		contentBlock,
	)

	if message.Role == roleUser {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, block)
	}
	return block
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
	helpContent := a.help.View(a.keys)
	// 确保帮助视图填充整个宽度，避免边框断裂
	return a.styles.footer.Width(width).Render(helpContent)
}

func (a App) renderMessageContent(content string, width int, bodyStyle lipgloss.Style) string {
	parts := strings.Split(content, "```")
	if len(parts) == 1 {
		return bodyStyle.Render(wrapPlain(content, max(16, width-2)))
	}

	blocks := make([]string, 0, len(parts))
	for i, part := range parts {
		if i%2 == 0 {
			trimmed := strings.Trim(part, "\n")
			if trimmed == "" {
				continue
			}
			blocks = append(blocks, bodyStyle.Render(wrapPlain(trimmed, max(16, width-2))))
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
		return bodyStyle.Render(emptyMessageText)
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (a App) statusBadge(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return a.styles.badgeError.Render(text)
	case strings.Contains(lower, "cancel"):
		return a.styles.badgeWarning.Render(text)
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
	contentWidth := max(0, a.width-a.styles.doc.GetHorizontalFrameSize())
	headerHeight := lipgloss.Height(a.renderHeader(contentWidth))
	helpHeight := lipgloss.Height(a.renderHelp(contentWidth))
	contentHeight := max(1, a.height-a.styles.doc.GetVerticalFrameSize()-headerHeight-helpHeight)
	lay := layout{contentWidth: contentWidth, contentHeight: contentHeight}
	if contentWidth < 110 {
		lay.stacked = true
		lay.sidebarWidth = contentWidth
		lay.sidebarHeight = clamp(contentHeight/3, 9, 13)
		lay.rightWidth = contentWidth
		lay.rightHeight = max(10, contentHeight-lay.sidebarHeight)
		return lay
	}

	lay.bodyGap = 1
	lay.sidebarWidth = 22
	lay.sidebarHeight = contentHeight
	lay.rightWidth = max(24, contentWidth-lay.sidebarWidth-lay.bodyGap)
	lay.rightHeight = contentHeight
	return lay
}

func (a App) isFilteringSessions() bool {
	return a.sessions.FilterState() != list.Unfiltered
}
