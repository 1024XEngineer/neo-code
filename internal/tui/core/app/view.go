package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
const transcriptScrollbarWidth = 3

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
	workdir := tuiutils.Fallback(strings.TrimSpace(a.state.CurrentWorkdir), "-")
	headerText := fmt.Sprintf("NeoCode | %s | %s | cwd: %s", model, status, workdir)
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
	activityHeight := 0
	todoHeight := a.todoPreviewHeight()
	menuHeight := a.commandMenuHeight(width)
	promptHeight := lipgloss.Height(a.renderPrompt(width))
	transcriptHeight := max(6, height-activityHeight-todoHeight-menuHeight-promptHeight)
	return transcriptHeight, activityHeight, menuHeight, todoHeight
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

	if a.logViewerVisible {
		return a.renderLogViewer(width, height)
	}

	transcriptContent := a.transcript.View()
	transcript := a.renderTranscriptWithScrollbar(width, transcriptContent)

	parts := []string{transcript}
	if a.state.IsAgentRunning && a.state.StatusText == statusThinking {
		thinkingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			Italic(true)
		parts = append(parts, thinkingStyle.Render("Thinking..."))
	}
	if todo := a.renderTodoPreview(width); todo != "" {
		parts = append(parts, todo)
	}
	if menu := a.renderCommandMenu(width); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, a.renderPrompt(width))

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, content)
}

func (a App) renderTranscriptWithScrollbar(totalWidth int, content string) string {
	scrollbarWidth := a.transcriptScrollbarWidth(totalWidth)
	if scrollbarWidth <= 0 {
		return a.styles.streamContent.Render(content)
	}

	contentWidth := max(1, totalWidth-scrollbarWidth)
	contentView := a.styles.streamContent.Width(contentWidth).Render(content)
	scrollbar := a.renderTranscriptScrollbar(scrollbarWidth, max(1, a.transcript.Height))
	return lipgloss.JoinHorizontal(lipgloss.Top, contentView, scrollbar)
}

func (a App) transcriptScrollbarWidth(totalWidth int) int {
	if totalWidth <= transcriptScrollbarWidth {
		return 0
	}
	return transcriptScrollbarWidth
}

func (a App) renderTranscriptScrollbar(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	track := strings.Repeat(" ", width)
	trackStyle := lipgloss.NewStyle().Background(lipgloss.Color(purpleBg2))
	thumbStyle := lipgloss.NewStyle().Background(lipgloss.Color(purpleAccent))

	maxOffset := a.transcriptMaxOffset()
	thumbHeight := height
	thumbTop := 0

	if maxOffset > 0 {
		totalLines := max(1, a.transcript.TotalLineCount())
		visibleLines := max(1, a.transcript.VisibleLineCount())
		thumbHeight = max(1, min(height, (visibleLines*height+totalLines-1)/totalLines))
		if height > thumbHeight {
			thumbTop = (a.transcript.YOffset*(height-thumbHeight) + maxOffset/2) / maxOffset
			thumbTop = max(0, min(thumbTop, height-thumbHeight))
		}
	}

	lines := make([]string, 0, height)
	for row := 0; row < height; row++ {
		if row >= thumbTop && row < thumbTop+thumbHeight {
			lines = append(lines, thumbStyle.Render(track))
			continue
		}
		lines = append(lines, trackStyle.Render(track))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
	if a.providerAddForm.Stage == providerAddFormStageManualModels {
		var sb strings.Builder
		sb.WriteString("Manual Model JSON（id/name 必填）\n")
		sb.WriteString("[Shift+Tab] 返回字段页  [Enter] 提交  [Esc] 取消\n\n")
		content := strings.TrimSpace(a.providerAddForm.ManualModelsJSON)
		if content == "" {
			content = providerAddManualModelsJSONTemplate
		}
		sb.WriteString(content)
		if a.providerAddForm.Error != "" {
			label := "[Prompt]"
			if a.providerAddForm.ErrorIsHard {
				label = "[Error]"
			}
			sb.WriteString("\n\n" + label + " " + a.providerAddForm.Error)
		}
		return sb.String()
	}

	var sb strings.Builder
	driver := provider.NormalizeProviderDriver(a.providerAddForm.Driver)
	baseURLRequired := driver == provider.DriverAnthropic || (driver != provider.DriverOpenAICompat && driver != provider.DriverGemini)
	visible := providerAddVisibleFields(a.providerAddForm.Driver, a.providerAddForm.ModelSource)
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
		case providerAddFieldModelSource:
			note := "discover: 远端发现模型；manual: 手工 JSON 模型列表"
			fields = append(fields, renderField{
				label:    "Model Source",
				value:    a.providerAddForm.ModelSource,
				required: true,
				note:     note,
			})
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
		case providerAddFieldChatEndpointPath:
			note := ""
			trimmedPath := strings.TrimSpace(a.providerAddForm.ChatEndpointPath)
			if trimmedPath == "" || trimmedPath == "/" {
				note = "留空或\"/\" 使用直连 base_url"
			} else {
				note = "以 \"/\" 开头的端点路径"
			}
			fields = append(fields, renderField{label: "Chat Endpoint", value: a.providerAddForm.ChatEndpointPath, note: note})
		case providerAddFieldDiscoveryEndpointPath:
			note := ""
			if strings.TrimSpace(a.providerAddForm.DiscoveryEndpointPath) == "" {
				note = "OpenAI-compatible 默认 /models"
			}
			fields = append(fields, renderField{
				label: "Discovery Endpoint",
				value: a.providerAddForm.DiscoveryEndpointPath,
				note:  note,
			})
		case providerAddFieldAPIKeyEnv:
			fields = append(fields, renderField{label: "API Key Env", value: a.providerAddForm.APIKeyEnv, required: true})
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

	sb.WriteString("\n[Tab] switch field  [Up/Down or K/J] change option  [Enter] confirm  [Esc] cancel")

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

func (a App) renderMessageBlockWithCopy(message providertypes.Message, width int, startCopyID int, showTag ...bool) (string, []copyCodeButtonBinding) {
	includeTag := true
	if len(showTag) > 0 {
		includeTag = showTag[0]
	}

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
	if message.Role == roleAssistant && !includeTag {
		return contentBlock, copyButtons
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
	lines := []string{helpContent}
	errorLine := a.footerErrorLine(width)
	if strings.TrimSpace(errorLine) != "" {
		lines = append([]string{errorLine}, lines...)
	}
	footerContent := strings.Join(lines, "\n")
	// Keep help content stretched to full width to avoid clipping at borders.
	return a.styles.footer.Width(width).Render(footerContent)
}

func (a App) footerErrorLine(width int) string {
	if width <= 0 {
		return ""
	}

	message := strings.TrimSpace(a.footerErrorText)
	if message == "" {
		return ""
	}
	if !a.footerErrorUntil.IsZero() && a.now().After(a.footerErrorUntil) {
		return ""
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(errorRed)).
		Width(width).
		Render(compactStatusText(message, max(8, width)))
}

func (a App) renderMessageContentWithCopy(content string, width int, bodyStyle lipgloss.Style, startCopyID int) (string, []copyCodeButtonBinding) {
	if a.markdownRenderer == nil {
		return bodyStyle.Render(emptyMessageText), nil
	}
	rendered, err := a.markdownRenderer.Render(content, max(16, width-2))
	if err != nil {
		return bodyStyle.Render(emptyMessageText), nil
	}
	rendered = trimRenderedTrailingWhitespace(rendered)
	return bodyStyle.Render(normalizeBlockRightEdge(rendered, max(1, width))), nil
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
		focusLabelTodo,
		focusLabelComposer,
	)
}

func (a App) activityPreviewHeight() int {
	return 0
}

func (a App) renderActivityPreview(width int) string {
	_ = a
	_ = width
	_ = activityTitle
	_ = activitySubtitle
	return ""
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
	return lipgloss.Height(a.renderHelp(width))
}

func (a App) renderLogViewer(width int, height int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(purpleAccent)).
		Bold(true).
		Width(max(1, width-4))

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(oliveGray)).
		Width(max(1, width-4))

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(oliveGray)).
		Width(20)

	levelStyle := lipgloss.NewStyle().
		Width(8)

	sourceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText)).
		Width(15)

	msgStyle := lipgloss.NewStyle()

	lines := []string{
		titleStyle.Render("  Log Viewer  "),
		headerStyle.Render("  Time                 Level     Source          Message"),
		"",
	}

	maxOffset := a.logViewerMaxOffset(height)
	offset := max(0, min(a.logViewerOffset, maxOffset))
	rows := a.logViewerRows(height)

	if len(a.logEntries) == 0 {
		lines = append(lines, headerStyle.Render("  No log entries"))
	} else {
		for row := 0; row < rows; row++ {
			i := len(a.logEntries) - 1 - (offset + row)
			if i < 0 {
				break
			}
			entry := a.logEntries[i]
			ts := entry.Timestamp.Format("15:04:05")
			level := ansi.Cut(entry.Level, 0, 8)
			source := ansi.Cut(entry.Source, 0, 15)
			msg := entry.Message
			msgWidth := max(0, width-50)
			if msgWidth > 0 && ansi.StringWidth(msg) > msgWidth {
				msg = ansi.Cut(msg, 0, msgWidth)
			}
			if msgWidth == 0 {
				msg = ""
			}
			lines = append(lines, timeStyle.Render(ts)+" "+levelStyle.Render(level)+" "+sourceStyle.Render(source)+" "+msgStyle.Render(msg))
		}
	}

	positionCurrent := 0
	positionTotal := 0
	if len(a.logEntries) > 0 {
		positionCurrent = offset + 1
		positionTotal = maxOffset + 1
	}
	lines = append(lines, "")
	lines = append(lines, headerStyle.Render(fmt.Sprintf("  Use Up/Down/PgUp/PgDn to scroll (%d/%d) · Ctrl+L or Esc to close", positionCurrent, positionTotal)))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	panelStyle := a.styles.panelFocused.Width(width).Height(height)
	return panelStyle.Render(content)
}
