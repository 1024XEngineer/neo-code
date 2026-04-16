package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	tuicomponents "neo-code/internal/tui/components"
	tuiutils "neo-code/internal/tui/core/utils"
	tuistate "neo-code/internal/tui/state"
)

type layout struct {
	contentWidth  int
	contentHeight int
}

const headerBarHeight = 2

const (
	pickerPanelHorizontalInset = 8
	pickerPanelVerticalInset   = 4
	pickerPanelMinWidth        = 42
	pickerPanelMaxWidth        = 72
	pickerPanelMinHeight       = 14
	pickerPanelMaxHeight       = 24
	pickerListMinWidth         = 28
	pickerListMinHeight        = 8
	pickerHeaderRows           = 2
)

type pickerLayoutSpec struct {
	panelWidth  int
	panelHeight int
	listWidth   int
	listHeight  int
}

func (a App) View() string {
	docWidth := max(0, a.width-a.styles.doc.GetHorizontalFrameSize())
	docHeight := max(0, a.height-a.styles.doc.GetVerticalFrameSize())
	if docWidth < 60 || docHeight < 20 {
		return strings.TrimRight(a.styles.doc.Render(lipgloss.Place(docWidth, docHeight, lipgloss.Left, lipgloss.Top, "Window too small.\nPlease resize to at least 60x20.")), "\n")
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
		if a.runProgressKnown {
			progressLabel := tuiutils.Fallback(strings.TrimSpace(a.runProgressLabel), tuiutils.Fallback(status, statusRunning))
			percent := int(a.runProgressValue*100 + 0.5)
			status = fmt.Sprintf("%d%% %s", percent, progressLabel)
		} else if status != statusThinking {
			status = tuiutils.Fallback(status, statusRunning)
		}
	}
	status = tuiutils.Fallback(status, statusReady)

	model := tuiutils.Fallback(strings.TrimSpace(a.state.CurrentModel), "unknown-model")
	headerText := fmt.Sprintf("NeoCode | %s | %s", model, status)
	if lipgloss.Width(headerText) > width {
		headerText = tuiutils.TrimMiddle(headerText, max(8, width))
	}
	return a.styles.headerBar.Width(width).Height(headerBarHeight).Render(headerText)
}

func (a App) renderBody(lay layout) string {
	return a.renderWaterfall(lay.contentWidth, lay.contentHeight)
}

// waterfallMetrics 统一计算瀑布区各组件高度，确保渲染、布局与命中区域使用同一组尺寸。
func (a App) waterfallMetrics(width int, height int) (int, int, int, int) {
	activityHeight := a.activityPreviewHeight()
	menuHeight := a.commandMenuHeight(width)
	transcriptHeight := max(6, height-activityHeight-menuHeight)
	return transcriptHeight, activityHeight, menuHeight, 0
}

func (a App) renderWaterfall(width int, height int) string {
	if a.state.ActivePicker != pickerNone {
		pickerLayout := a.buildPickerLayout(width, height)
		return lipgloss.Place(
			width,
			height,
			lipgloss.Center,
			lipgloss.Center,
			a.renderPicker(pickerLayout.panelWidth, pickerLayout.panelHeight),
		)
	}

	transcriptHeight, _, _, _ := a.waterfallMetrics(width, height)

	transcript := a.styles.streamContent.Width(width).Height(transcriptHeight).Render(a.transcript.View())

	parts := []string{transcript}
	if a.state.IsAgentRunning && a.state.StatusText == statusThinking {
		thinkingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			Italic(true)
		parts = append(parts, thinkingStyle.Render("Thinking..."))
	}
	if activity := a.renderActivityPreview(width); activity != "" {
		parts = append(parts, activity)
	}
	if menu := a.renderCommandMenu(width); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, a.renderPrompt(width))

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, content)
}

func (a App) buildPickerLayout(contentWidth int, contentHeight int) pickerLayoutSpec {
	panelWidth := tuiutils.Clamp(contentWidth-pickerPanelHorizontalInset, pickerPanelMinWidth, pickerPanelMaxWidth)
	panelHeight := tuiutils.Clamp(contentHeight-pickerPanelVerticalInset, pickerPanelMinHeight, pickerPanelMaxHeight)

	frameWidth := a.styles.panelFocused.GetHorizontalFrameSize()
	frameHeight := a.styles.panelFocused.GetVerticalFrameSize()
	listWidth := max(pickerListMinWidth, panelWidth-frameWidth)
	listHeight := max(pickerListMinHeight, panelHeight-frameHeight-pickerHeaderRows)

	return pickerLayoutSpec{
		panelWidth:  panelWidth,
		panelHeight: panelHeight,
		listWidth:   listWidth,
		listHeight:  listHeight,
	}
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
	if a.state.ActivePicker == pickerSession {
		title = sessionPickerTitle
		subtitle = sessionPickerSubtitle
		body = a.sessionPicker.View()
	}
	if a.state.ActivePicker == pickerFile {
		title = filePickerTitle
		subtitle = filePickerSubtitle
		body = a.fileBrowser.View()
	}
	if a.state.ActivePicker == pickerHelp {
		title = helpPickerTitle
		subtitle = helpPickerSubtitle
		body = a.helpPicker.View()
	}
	if a.state.ActivePicker == pickerProviderAdd {
		title = providerAddTitle
		subtitle = providerAddSubtitle
		body = a.renderProviderAddForm()
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

func (a App) renderProviderAddForm() string {
	if a.providerAddForm == nil {
		return "No form active"
	}

	var sb strings.Builder
	driver := provider.NormalizeProviderDriver(a.providerAddForm.Driver)
	baseURLRequired := driver == provider.DriverAnthropic || (driver != provider.DriverOpenAICompat && driver != provider.DriverGemini)
	visible := providerAddVisibleFields(a.providerAddForm.Driver)
	clampProviderAddStep(a.providerAddForm)

	type renderField struct {
		label    string
		value    string
		required bool
		note     string
	}
	fields := make([]renderField, 0, len(visible))
	for _, fieldID := range visible {
		switch fieldID {
		case providerAddFieldName:
			fields = append(fields, renderField{label: "Name", value: a.providerAddForm.Name, required: true})
		case providerAddFieldDriver:
			fields = append(fields, renderField{label: "Driver", value: a.providerAddForm.Driver, required: true})
		case providerAddFieldBaseURL:
			note := ""
			if strings.TrimSpace(a.providerAddForm.BaseURL) == "" && (driver == provider.DriverOpenAICompat || driver == provider.DriverGemini) {
				note = "留空会自动填充默认地址"
			}
			fields = append(fields, renderField{
				label:    "Base URL",
				value:    a.providerAddForm.BaseURL,
				required: baseURLRequired,
				note:     note,
			})
		case providerAddFieldAPIStyle:
			note := ""
			if strings.TrimSpace(a.providerAddForm.APIStyle) == "" {
				note = "默认 chat_completions"
			}
			fields = append(fields, renderField{label: "API Style", value: a.providerAddForm.APIStyle, note: note})
		case providerAddFieldDeploymentMode:
			fields = append(fields, renderField{label: "Deployment Mode", value: a.providerAddForm.DeploymentMode})
		case providerAddFieldAPIVersion:
			fields = append(fields, renderField{label: "API Version", value: a.providerAddForm.APIVersion})
		case providerAddFieldAPIKey:
			fields = append(fields, renderField{label: "API Key", value: maskedSecret(a.providerAddForm.APIKey), required: true})
		}
	}

	for i, field := range fields {
		prefix := "  "
		if i == a.providerAddForm.Step {
			prefix = "> "
		}
		sb.WriteString(prefix + field.label + ": ")
		sb.WriteString(field.value)
		if field.required {
			sb.WriteString(" *")
		}
		if field.note != "" {
			sb.WriteString("  (" + field.note + ")")
		}
		sb.WriteString("\n")
	}

	if a.providerAddForm.Error != "" {
		label := "[Prompt]"
		if a.providerAddForm.ErrorIsHard {
			label = "[Error]"
		}
		sb.WriteString("\n" + label + " " + a.providerAddForm.Error + "\n")
	}

	sb.WriteString("\n[Tab] switch field  [Enter] confirm  [Esc] cancel")

	return sb.String()
}

// maskedSecret 将敏感输入渲染为固定掩码，避免在终端界面泄露明文。
func maskedSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "******"
}

func (a App) renderPrompt(width int) string {
	if a.pendingPermission != nil {
		box := a.styles.inputBoxFocused
		return box.Width(width).Margin(1, 0, 0, 0).Render(a.renderPermissionPrompt())
	}

	box := a.styles.inputBox
	if a.focus == panelInput && a.state.ActivePicker == pickerNone {
		box = a.styles.inputBoxFocused
	}

	return box.Width(width).Margin(1, 0, 0, 0).Render(a.input.View())
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
		return a.styles.inlineNotice.Width(width).Render("  > " + wrapPlain(renderMessagePartsForDisplay(message.Parts), max(16, width-6))), nil
	case roleError:
		return a.styles.inlineError.Width(width).Render("  ! " + wrapPlain(renderMessagePartsForDisplay(message.Parts), max(16, width-6))), nil
	case roleSystem:
		return a.styles.inlineSystem.Width(width).Render("  - " + wrapPlain(renderMessagePartsForDisplay(message.Parts), max(16, width-6))), nil
	}

	maxMessageWidth := tuiutils.Clamp(int(float64(width)*0.84), 24, width)
	tag := messageTagAgent
	tagStyle := a.styles.messageAgentTag
	bodyStyle := a.styles.messageBody

	switch message.Role {
	case roleUser:
		maxMessageWidth = tuiutils.Clamp(int(float64(width)*0.68), 24, width)
		tag = messageTagUser
		tagStyle = a.styles.messageUserTag
		bodyStyle = a.styles.messageUserBody
	case roleTool:
		return "", nil
	}

	content := strings.TrimSpace(renderMessagePartsForDisplay(message.Parts))
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
	tagLine := tagStyle.Render(tag)
	blockAlign := lipgloss.Left
	if message.Role == roleUser {
		blockAlign = lipgloss.Right
		rightInset := bodyStyle.GetMarginRight() - tagStyle.GetPaddingRight()
		if rightInset < 0 {
			rightInset = 0
		}
		if rightInset > 0 {
			tagLine = tagStyle.Copy().MarginRight(rightInset).Render(tag)
		}
	}
	parts := []string{tagLine, contentBlock}
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
	return layout{contentWidth: contentWidth, contentHeight: contentHeight}
}

// helpHeight 仅计算帮助区高度，避免在 layout 计算阶段触发完整渲染。
func (a App) helpHeight(width int) int {
	a.help.ShowAll = a.state.ShowHelp
	return lipgloss.Height(a.styles.footer.Width(width).Render(a.help.View(a.keys)))
}
