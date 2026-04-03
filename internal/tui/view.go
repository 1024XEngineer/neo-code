package tui

import (
	"fmt"
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
	status := compactStatusText(a.state.StatusText, max(18, width/3))
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
	if activity := a.renderActivityPreview(width); activity != "" {
		parts = append(parts, activity)
	}
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

	// Account for frame and padding when sizing the composer container.
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
	rendered, _ := a.renderMessageBlockWithCopy(message, width, 1)
	return rendered
}

func (a App) renderMessageBlockWithCopy(message provider.Message, width int, startCopyID int) (string, []copyCodeButtonBinding) {
	switch message.Role {
	case roleEvent:
		return a.styles.inlineNotice.Width(width).Render("  > " + wrapPlain(message.Content, max(16, width-6))), nil
	case roleError:
		return a.styles.inlineError.Width(width).Render("  ! " + wrapPlain(message.Content, max(16, width-6))), nil
	case roleSystem:
		return a.styles.inlineSystem.Width(width).Render("  - " + wrapPlain(message.Content, max(16, width-6))), nil
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

	var (
		contentBlock string
		copyButtons  []copyCodeButtonBinding
	)
	if message.Role == roleUser {
		contentBlock = bodyStyle.Render(wrapPlain(content, max(16, maxMessageWidth-2)))
	} else {
		contentBlock, copyButtons = a.renderMessageContentWithCopy(content, maxMessageWidth-2, bodyStyle, startCopyID)
	}
	parts := []string{tagStyle.Render(tag), contentBlock}
	block := lipgloss.JoinVertical(blockAlign, parts...)

	if message.Role == roleUser {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, block), nil
	}
	return block, copyButtons
}

func (a App) renderCommandMenu(width int) string {
	input := strings.TrimSpace(a.input.Value())

	if suggestions := a.matchingFileReferences(a.input.Value()); len(suggestions) > 0 {
		lines := make([]string, 0, len(suggestions)+1)
		lines = append(lines, a.styles.commandMenuTitle.Render(fileMenuTitle))
		for idx, suggestion := range suggestions {
			usageStyle := a.styles.commandUsage
			if idx == 0 {
				usageStyle = a.styles.commandUsageMatch
			}
			lines = append(lines, lipgloss.JoinHorizontal(
				lipgloss.Top,
				usageStyle.Render("@"+suggestion),
				lipgloss.NewStyle().Width(2).Render(""),
				a.styles.commandDesc.Render("workspace file reference"),
			))
		}
		return a.styles.commandMenu.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	if isWorkspaceCommandInput(input) {
		lines := []string{
			a.styles.commandMenuTitle.Render(shellMenuTitle),
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				a.styles.commandUsageMatch.Render(workspaceCommandUsage),
				lipgloss.NewStyle().Width(2).Render(""),
				a.styles.commandDesc.Render(trimMiddle(a.state.CurrentWorkdir, max(24, width-28))),
			),
		}
		return a.styles.commandMenu.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	suggestions := a.matchingSlashCommands(input)
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
	// Keep help content stretched to full width to avoid clipping at borders.
	return a.styles.footer.Width(width).Render(helpContent)
}

func (a App) renderMessageContent(content string, width int, bodyStyle lipgloss.Style) string {
	rendered, _ := a.renderMessageContentWithCopy(content, width, bodyStyle, 1)
	return rendered
}

func (a App) renderMessageContentWithCopy(content string, width int, bodyStyle lipgloss.Style, startCopyID int) (string, []copyCodeButtonBinding) {
	if a.markdownRenderer == nil {
		return bodyStyle.Render(emptyMessageText), nil
	}

	segments := splitMarkdownSegments(content)
	if len(segments) == 1 && segments[0].Kind == markdownSegmentText {
		rendered, err := a.markdownRenderer.Render(content, max(16, width-2))
		if err != nil {
			return bodyStyle.Render(emptyMessageText), nil
		}
		rendered = trimRenderedTrailingWhitespace(rendered)
		return bodyStyle.Render(normalizeBlockRightEdge(rendered, max(1, width))), nil
	}

	renderedParts := make([]string, 0, len(segments))
	copyBindings := make([]copyCodeButtonBinding, 0, 2)
	nextCopyID := startCopyID

	for _, segment := range segments {
		switch segment.Kind {
		case markdownSegmentText:
			if strings.TrimSpace(segment.Text) == "" {
				continue
			}
			rendered, err := a.markdownRenderer.Render(segment.Text, max(16, width-2))
			if err != nil {
				continue
			}
			rendered = trimRenderedTrailingWhitespace(rendered)
			renderedParts = append(renderedParts, bodyStyle.Render(normalizeBlockRightEdge(rendered, max(1, width))))
		case markdownSegmentCode:
			code := strings.TrimRight(segment.Code, "\n")
			if code == "" {
				continue
			}
			buttonText := fmt.Sprintf(copyCodeButton, nextCopyID)
			button := a.styles.codeCopyButton.Render(buttonText)
			renderedCode, err := a.markdownRenderer.Render(segment.Fenced, max(16, width-2))
			if err != nil {
				codeTextWidth := max(8, width-4)
				renderedCode = a.styles.codeBlock.Width(width).Render(a.styles.codeText.Width(codeTextWidth).Render(wrapCodeBlock(code, codeTextWidth)))
			}
			codeBlock := lipgloss.JoinVertical(
				lipgloss.Left,
				button,
				trimRenderedTrailingWhitespace(renderedCode),
			)
			renderedParts = append(renderedParts, codeBlock)
			copyBindings = append(copyBindings, copyCodeButtonBinding{ID: nextCopyID, Code: code})
			nextCopyID++
		}
	}

	if len(renderedParts) == 0 {
		return bodyStyle.Render(emptyMessageText), nil
	}
	return lipgloss.JoinVertical(lipgloss.Left, renderedParts...), copyBindings
}

func normalizeBlockRightEdge(content string, maxWidth int) string {
	if strings.TrimSpace(content) == "" {
		return content
	}

	lines := strings.Split(content, "\n")
	targetWidth := 0
	for _, line := range lines {
		targetWidth = max(targetWidth, lipgloss.Width(line))
	}
	targetWidth = clamp(targetWidth, 1, maxWidth)

	padStyle := lipgloss.NewStyle().Width(targetWidth)
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		normalized = append(normalized, padStyle.Render(line))
	}
	return strings.Join(normalized, "\n")
}

func trimRenderedTrailingWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
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

func compactStatusText(text string, limit int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.Join(strings.Fields(line), " ")
		if limit > 0 {
			return trimMiddle(line, limit)
		}
		return line
	}
	return ""
}

func (a App) focusLabel() string {
	switch a.focus {
	case panelSessions:
		return focusLabelSessions
	case panelTranscript:
		return focusLabelTranscript
	case panelActivity:
		return focusLabelActivity
	default:
		return focusLabelComposer
	}
}

func (a App) activityPreviewHeight() int {
	if len(a.activities) == 0 {
		return 0
	}
	return 6
}

func (a App) renderActivityPreview(width int) string {
	if len(a.activities) == 0 {
		return ""
	}

	entries := a.activities
	if len(entries) > activityPreviewEntries {
		entries = entries[len(entries)-activityPreviewEntries:]
	}

	lines := make([]string, 0, len(entries))
	bodyWidth := max(10, width-4)
	for _, entry := range entries {
		lines = append(lines, a.renderActivityLine(entry, bodyWidth))
	}

	return a.renderPanel(
		activityTitle,
		activitySubtitle,
		strings.Join(lines, "\n"),
		width,
		a.activityPreviewHeight(),
		a.focus == panelActivity,
	)
}

func (a App) renderActivityLine(entry activityEntry, width int) string {
	timeLabel := entry.Time.Format("15:04:05")
	kindLabel := strings.ToUpper(fallback(strings.TrimSpace(entry.Kind), "event"))

	text := entry.Title
	if strings.TrimSpace(entry.Detail) != "" {
		text = text + ": " + entry.Detail
	}

	return trimMiddle(timeLabel+" "+kindLabel+" "+strings.Join(strings.Fields(text), " "), max(12, width))
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
