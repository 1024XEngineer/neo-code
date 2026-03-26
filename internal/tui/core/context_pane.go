package core

import (
	"fmt"
	"strings"

	"go-llm-demo/internal/tui/components"
	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type contextSection struct {
	title  string
	accent string
	lines  []string
}

type scrollPaneModel struct {
	viewport.Model
}

type contextPaneModel struct {
	scrollPaneModel
}

type chatPaneModel struct {
	scrollPaneModel
	layout components.RenderedChatLayout
}

func newScrollPaneModel() scrollPaneModel {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return scrollPaneModel{Model: vp}
}

func (p *scrollPaneModel) syncSize(width, height int) {
	p.Width = width
	p.Height = height
}

func (p *scrollPaneModel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.Model, cmd = p.Model.Update(msg)
	return cmd
}

func (p *scrollPaneModel) focus() tea.Cmd {
	return nil
}

func (p *scrollPaneModel) blur() {}

func (p *scrollPaneModel) refreshContent(content string) {
	p.SetContent(content)
	maxOffset := maxInt(0, countLines(content)-p.Height)
	if p.YOffset > maxOffset {
		p.YOffset = maxOffset
	}
	if p.YOffset < 0 {
		p.YOffset = 0
	}
}

func (p scrollPaneModel) renderView(content string) string {
	view := p.Model
	view.SetContent(content)
	return view.View()
}

func newChatPaneModel() chatPaneModel {
	return chatPaneModel{scrollPaneModel: newScrollPaneModel()}
}

func (p *chatPaneModel) refresh(mode state.Mode, todo todoPanelModel, messages []components.Message, autoScroll bool) bool {
	content, layout := p.render(mode, todo, messages)
	wasAtBottom := p.AtBottom()
	p.layout = layout
	p.refreshContent(content)
	if countLines(content) <= p.Height {
		p.YOffset = 0
		return true
	}
	if autoScroll || wasAtBottom {
		p.GotoBottom()
		return true
	}
	return p.AtBottom()
}

func (p chatPaneModel) renderView(mode state.Mode, todo todoPanelModel, messages []components.Message) string {
	content, _ := p.render(mode, todo, messages)
	return p.scrollPaneModel.renderView(content)
}

func (p *chatPaneModel) render(mode state.Mode, todo todoPanelModel, messages []components.Message) (string, components.RenderedChatLayout) {
	if mode == state.ModeTodo {
		layout := components.RenderedChatLayout{}
		return components.TodoList{
			Todos:   todo.items(),
			Cursor:  todo.selectedIndex(),
			Width:   p.Width,
			Focused: true,
		}.Render(), layout
	}

	layout := components.MessageList{Messages: messages, Width: p.Width}.RenderLayout()
	return layout.Content, layout
}

func (p chatPaneModel) contentPosition(mouseY, mouseX int) (int, int, bool) {
	contentTop := components.PanelVerticalFrameSize()/2 + 1
	contentLeft := components.PanelHorizontalFrameSize() / 2
	contentBottom := contentTop + p.Height
	if mouseY < contentTop || mouseY >= contentBottom {
		return 0, 0, false
	}
	if mouseX < contentLeft {
		return 0, 0, false
	}
	return p.YOffset + (mouseY - contentTop), mouseX - contentLeft + 1, true
}

func (p chatPaneModel) clickableRegion(row, col int) (components.ClickableRegion, bool) {
	return findClickableRegion(p.layout.Regions, row, col)
}

func newContextPaneModel() contextPaneModel {
	return contextPaneModel{scrollPaneModel: newScrollPaneModel()}
}

func (p *contextPaneModel) refresh(content string) {
	p.refreshContent(content)
}

func (m Model) renderContextBody(width int) string {
	return renderContextSections(width,
		m.sessionContextSection(),
		m.runtimeContextSection(),
		m.toolContextSection(width),
		m.recentActivitySection(width),
		m.todoContextSection(width),
	)
}

func (m Model) sessionContextSection() contextSection {
	userCount, assistantCount, systemCount := m.messageRoleCounts()
	stats := m.chat.MemoryStats
	summary := m.workbenchSummary(maxInt(12, len([]rune(strings.TrimSpace(m.chat.WorkspaceRoot)))))

	return newContextSection(
		"Session",
		"#98C379",
		fmt.Sprintf("Model: %s", fallbackContextValue(summary.model, "unknown")),
		fmt.Sprintf("Mode: %s", summary.mode),
		fmt.Sprintf(
			"Messages: %d total (%d you / %d neo / %d system)",
			len(m.chat.Messages),
			userCount,
			assistantCount,
			systemCount,
		),
		fmt.Sprintf(
			"Memory: %d total | %d session | %d persistent",
			stats.TotalItems,
			stats.SessionItems,
			stats.PersistentItems,
		),
		fmt.Sprintf("Workspace: %s", workspacePreview(strings.TrimSpace(m.chat.WorkspaceRoot), maxInt(24, m.ui.Width/3))),
	)
}

func (m Model) runtimeContextSection() contextSection {
	summary := m.workbenchSummary(m.ui.Width)

	lines := []string{
		fmt.Sprintf("Status: %s", summary.status),
		fmt.Sprintf("Response: %s", summary.response),
		fmt.Sprintf("Auto-scroll: %s", boolLabel(summary.autoScroll)),
	}

	if summary.focus != "" {
		lines = append(lines, fmt.Sprintf("Focus: %s", summary.focus))
	}
	if summary.apiKeyAttention {
		lines = append(lines, "API key: needs attention")
	}

	return newContextSection("Runtime", "#61AFEF", lines...)
}

func (m Model) toolContextSection(width int) contextSection {
	lines := make([]string, 0, 6)
	title := "Tool Activity"
	accent := "#61AFEF"

	if m.chat.PendingApproval != nil {
		title = "Approval Required"
		accent = "#D19A66"
		pending := m.chat.PendingApproval
		toolName := fallbackContextValue(pending.Call.Tool, "unknown")
		target := fallbackContextValue(pending.Target, "unknown")
		lines = append(lines,
			fmt.Sprintf("Tool: %s", toolName),
			fmt.Sprintf("Target: %s", target),
			"Action: /y allow once | /n reject",
		)
	} else {
		status := "idle"
		if m.chat.ToolExecuting {
			status = "running"
		}
		lines = append(lines, fmt.Sprintf("Status: %s", status))
	}

	if toolLine := m.latestToolRequestLine(); toolLine != "" {
		lines = append(lines, toolLine)
	}
	lines = append(lines, m.latestToolResultLines(width)...)

	if len(lines) == 0 {
		lines = append(lines, "No tool activity yet.")
	}

	return newContextSection(title, accent, lines...)
}

func (m Model) recentActivitySection(width int) contextSection {
	previews := make([]string, 0, 3)
	for i := len(m.chat.Messages) - 1; i >= 0 && len(previews) < 3; i-- {
		msg := m.chat.Messages[i]
		if msg.Role == "system" {
			continue
		}
		content := previewPlainText(msg.Content, maxInt(20, width-8))
		if content == "" {
			continue
		}
		speaker := "Neo"
		if msg.Role == "user" {
			speaker = "You"
		}
		previews = append(previews, fmt.Sprintf("%s: %s", speaker, content))
	}

	if len(previews) == 0 {
		previews = append(previews, "No conversation yet.")
	} else {
		reverseStrings(previews)
	}

	return newContextSection("Recent Activity", "#E5C07B", previews...)
}

func (m Model) todoContextSection(width int) contextSection {
	items := m.todo.items()
	pendingCount, activeCount, doneCount := countTodosByStatus(items)

	lines := []string{
		fmt.Sprintf(
			"Counts: %d pending | %d in progress | %d done",
			pendingCount,
			activeCount,
			doneCount,
		),
	}

	if len(items) == 0 {
		lines = append(lines, "No todo items yet.")
	} else {
		lines = append(lines, m.renderTodoSummary(width)...)
	}

	return newContextSection("Todo Snapshot", "#98C379", lines...)
}

func renderContextSection(section contextSection, width int) string {
	if strings.TrimSpace(section.title) == "" || width <= 0 {
		return ""
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(section.accent)).
		Render(strings.TrimSpace(section.title))

	lines := make([]string, 0, len(section.lines)+1)
	lines = append(lines, header)
	for _, line := range section.lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, ansi.Hardwrap(line, maxInt(1, width), true))
	}

	return strings.Join(lines, "\n")
}

func renderContextSections(width int, sections ...contextSection) string {
	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		if block := renderContextSection(section, width); strings.TrimSpace(block) != "" {
			rendered = append(rendered, block)
		}
	}

	return lipgloss.NewStyle().
		Width(maxInt(1, width)).
		Render(strings.Join(rendered, "\n\n"))
}

func newContextSection(title string, accent string, lines ...string) contextSection {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}

	return contextSection{
		title:  title,
		accent: accent,
		lines:  filtered,
	}
}

func (m Model) messageRoleCounts() (userCount int, assistantCount int, systemCount int) {
	for _, msg := range m.chat.Messages {
		switch msg.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "system":
			systemCount++
		}
	}
	return userCount, assistantCount, systemCount
}

func (m Model) latestToolRequestLine() string {
	content, ok := m.latestSystemMessage(isTransientToolStatusMessage)
	if !ok {
		return ""
	}

	fields := parseTaggedFields(content, toolStatusPrefix)
	toolName := fallbackContextValue(fields["tool"], "unknown")
	target := firstContextValue(fields, "file", "path", "workdir")
	if target == "" {
		return fmt.Sprintf("Request: %s", toolName)
	}
	return fmt.Sprintf("Request: %s -> %s", toolName, target)
}

func (m Model) latestToolResultLines(width int) []string {
	content, ok := m.latestSystemMessage(isToolContextMessage)
	if !ok {
		return nil
	}

	fields := parseToolContextFields(content)
	toolName := fallbackContextValue(fields["tool"], "unknown")
	success := strings.EqualFold(fields["success"], "true")
	status := "failed"
	if success {
		status = "success"
	}

	lines := []string{fmt.Sprintf("Last result: %s (%s)", toolName, status)}
	if summary := previewPlainText(fields["preview"], maxInt(20, width-10)); summary != "" {
		lines = append(lines, fmt.Sprintf("Preview: %s", summary))
	}
	return lines
}

func (m Model) latestSystemMessage(match func(string) bool) (string, bool) {
	for i := len(m.chat.Messages) - 1; i >= 0; i-- {
		msg := m.chat.Messages[i]
		if msg.Role != "system" || !match(msg.Content) {
			continue
		}
		return msg.Content, true
	}
	return "", false
}

func parseTaggedFields(content string, prefix string) map[string]string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), prefix))
	fields := strings.Fields(trimmed)
	result := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}

func parseToolContextFields(content string) map[string]string {
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), toolContextPrefix)), "\n")
	fields := map[string]string{}
	activeBlock := ""
	blockLines := make([]string, 0)

	flushBlock := func() {
		if activeBlock == "" || len(blockLines) == 0 {
			return
		}
		fields[activeBlock] = strings.Join(blockLines, "\n")
		blockLines = blockLines[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "output:", "error:":
			flushBlock()
			activeBlock = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if activeBlock != "" {
			blockLines = append(blockLines, line)
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	flushBlock()

	preview := strings.TrimSpace(fields["error"])
	if preview == "" {
		preview = strings.TrimSpace(fields["output"])
	}
	fields["preview"] = preview
	return fields
}

func firstContextValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func countTodosByStatus(items []services.Todo) (pendingCount int, activeCount int, doneCount int) {
	for _, item := range items {
		switch item.Status {
		case services.TodoCompleted:
			doneCount++
		case services.TodoInProgress:
			activeCount++
		default:
			pendingCount++
		}
	}
	return pendingCount, activeCount, doneCount
}

func (m Model) renderTodoSummary(width int) []string {
	items := m.todo.items()
	limit := minInt(4, len(items))
	lines := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		item := items[i]
		lines = append(lines, fmt.Sprintf("%s %s", todoStatusPrefix(item.Status), previewText(item.Content, maxInt(12, width-4))))
	}
	if len(items) > limit {
		lines = append(lines, fmt.Sprintf("+%d more", len(items)-limit))
	}
	return lines
}

func todoStatusPrefix(status services.TodoStatus) string {
	switch status {
	case services.TodoCompleted:
		return "[x]"
	case services.TodoInProgress:
		return "[-]"
	default:
		return "[ ]"
	}
}

func previewPlainText(text string, limit int) string {
	return previewText(strings.Join(strings.Fields(text), " "), limit)
}

func previewText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func fallbackContextValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
