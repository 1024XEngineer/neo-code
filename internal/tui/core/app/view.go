package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	providertypes "neo-code/internal/provider/types"
	tuicomponents "neo-code/internal/tui/components"
	tuiutils "neo-code/internal/tui/core/utils"
	tuistate "neo-code/internal/tui/state"
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

const headerBarHeight = 1

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
	status := tuicomponents.CompactStatusText(a.state.StatusText, max(18, width/3))
	if a.state.IsAgentRunning {
		if a.runProgressKnown {
			progressBar := a.progress
			progressBar.Width = tuiutils.Clamp(width/7, 12, 26)
			progressLabel := tuiutils.Fallback(strings.TrimSpace(a.runProgressLabel), tuiutils.Fallback(status, statusRunning))
			status = progressBar.ViewAs(a.runProgressValue) + " " + progressLabel
		} else {
			status = a.spinner.View() + " " + tuiutils.Fallback(status, statusRunning)
		}
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
		a.styles.headerPath.Render(tuiutils.TrimMiddle(a.state.CurrentWorkdir, workdirWidth)),
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
			a.renderPicker(tuiutils.Clamp(width-10, 36, 56), tuiutils.Clamp(height-6, 10, 14)),
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
	if a.state.ActivePicker == pickerFile {
		title = filePickerTitle
		subtitle = filePickerSubtitle
		body = a.fileBrowser.View()
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
	return box.Width(boxWidth).Render(a.renderPermissionPrompt())
}

func (a App) renderSidebarHeader(width int) string {
	title := a.styles.panelTitle.Render(sidebarTitle)
	filterWidth := max(6, width-lipgloss.Width(title)-1)
	titleRow := lipgloss.JoinHorizontal(
		lipgloss.Center,
		title,
		lipgloss.NewStyle().Width(1).Render(""),
		a.styles.panelSubtitle.Render(tuiutils.TrimRunes(sidebarFilterHint, filterWidth)),
	)
	openRow := a.styles.panelSubtitle.Render(tuiutils.TrimRunes(sidebarOpenHint, width))
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

func (a App) renderMessageBlockWithCopy(message providertypes.Message, width int, startCopyID int) (string, []copyCodeButtonBinding) {
	switch message.Role {
	case roleEvent:
		return a.styles.inlineNotice.Width(width).Render("  > " + wrapPlain(message.Content, max(16, width-6))), nil
	case roleError:
		return a.styles.inlineError.Width(width).Render("  ! " + wrapPlain(message.Content, max(16, width-6))), nil
	case roleSystem:
		return a.styles.inlineSystem.Width(width).Render("  - " + wrapPlain(message.Content, max(16, width-6))), nil
	}

	maxMessageWidth := tuiutils.Clamp(int(float64(width)*0.84), 24, width)
	tag := messageTagAgent
	tagStyle := a.styles.messageAgentTag
	bodyStyle := a.styles.messageBody
	blockAlign := lipgloss.Left

	switch message.Role {
	case roleUser:
		maxMessageWidth = tuiutils.Clamp(int(float64(width)*0.68), 24, width)
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
	if a.state.ActivePicker != pickerNone || len(a.commandMenu.Items()) == 0 {
		return ""
	}
	title := commandMenuTitle
	if strings.TrimSpace(a.commandMenuMeta.Title) != "" {
		title = a.commandMenuMeta.Title
	}
	body := strings.TrimSpace(a.commandMenu.View())
	if body == "" {
		return ""
	}
	return tuicomponents.RenderCommandMenu(tuicomponents.CommandMenuData{
		Title:          title,
		Body:           body,
		Width:          width,
		ContainerStyle: a.styles.commandMenu,
		TitleStyle:     a.styles.commandMenuTitle,
	})
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
	return tuicomponents.NormalizeBlockRightEdge(content, maxWidth)
}

func trimRenderedTrailingWhitespace(content string) string {
	return tuicomponents.TrimRenderedTrailingWhitespace(content)
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
	return tuicomponents.CompactStatusText(text, limit)
}

func (a App) focusLabel() string {
	return tuiutils.FocusLabelFromPanel(
		a.focus,
		focusLabelSessions,
		focusLabelTranscript,
		focusLabelActivity,
		focusLabelComposer,
	)
}

func (a App) activityPreviewHeight() int {
	return tuicomponents.ActivityPreviewHeight(len(a.activities))
}

func (a App) renderActivityPreview(width int) string {
	if len(a.activities) == 0 {
		return ""
	}
	content := a.activity.View()

	return a.renderPanel(
		activityTitle,
		activitySubtitle,
		content,
		width,
		a.activityPreviewHeight(),
		a.focus == panelActivity,
	)
}

func (a App) renderActivityLine(entry tuistate.ActivityEntry, width int) string {
	return tuicomponents.RenderActivityLine(entry, width)
}

func (a App) computeLayout() layout {
	contentWidth := max(0, a.width-a.styles.doc.GetHorizontalFrameSize())
	helpHeight := a.helpHeight(contentWidth)
	headerHeight := headerBarHeight
	contentHeight := max(1, a.height-a.styles.doc.GetVerticalFrameSize()-headerHeight-helpHeight)
	lay := layout{contentWidth: contentWidth, contentHeight: contentHeight}
	if contentWidth < 110 {
		lay.stacked = true
		lay.sidebarWidth = contentWidth
		lay.sidebarHeight = tuiutils.Clamp(contentHeight/3, 9, 13)
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

// helpHeight 仅计算帮助区高度，避免在 layout 计算阶段触发完整渲染。
func (a App) helpHeight(width int) int {
	a.help.ShowAll = a.state.ShowHelp
	return lipgloss.Height(a.styles.footer.Width(width).Render(a.help.View(a.keys)))
}

func (a App) isFilteringSessions() bool {
	return a.sessions.FilterState() != list.Unfiltered
}
