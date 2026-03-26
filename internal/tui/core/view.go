package core

import (
	"fmt"
	"strings"

	"go-llm-demo/internal/tui/components"
	"go-llm-demo/internal/tui/state"
	"go-llm-demo/internal/tui/todo"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

type layoutState struct {
	totalWidth         int
	totalHeight        int
	mainHeight         int
	composerHeight     int
	footerHeight       int
	stacked            bool
	conversationWidth  int
	conversationHeight int
	contextWidth       int
	contextHeight      int
}

type appKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

type workbenchSummary struct {
	mode            string
	focus           string
	status          string
	model           string
	workspace       string
	response        string
	memoryTotal     int
	autoScroll      bool
	busy            bool
	apiKeyAttention bool
}

func currentModeLabel(mode state.Mode) string {
	switch mode {
	case state.ModeHelp:
		return "help"
	case state.ModeTodo:
		return "todo"
	default:
		return "chat"
	}
}

func currentFocusLabel(mode state.Mode, focused string) string {
	switch mode {
	case state.ModeHelp:
		if strings.TrimSpace(focused) == "composer" {
			return "composer"
		}
		return "help"
	case state.ModeTodo:
		return "todo"
	}

	switch strings.TrimSpace(focused) {
	case "conversation", "context", "composer":
		return strings.TrimSpace(focused)
	case "input":
		return "composer"
	default:
		return "composer"
	}
}

func (k appKeyMap) ShortHelp() []key.Binding {
	return k.short
}

func (k appKeyMap) FullHelp() [][]key.Binding {
	return k.full
}

var chatCommandBindings = []key.Binding{
	key.NewBinding(key.WithKeys("/help"), key.WithHelp("/help", "toggle help")),
	key.NewBinding(key.WithKeys("/todo"), key.WithHelp("/todo", "open todos")),
	key.NewBinding(key.WithKeys("/memory"), key.WithHelp("/memory", "memory stats")),
	key.NewBinding(key.WithKeys("/clear-memory"), key.WithHelp("/clear-memory", "clear memory")),
	key.NewBinding(key.WithKeys("/clear"), key.WithHelp("/clear", "clear session")),
	key.NewBinding(key.WithKeys("/pwd"), key.WithHelp("/pwd", "show workspace")),
	key.NewBinding(key.WithKeys("/apikey"), key.WithHelp("/apikey", "switch key env")),
	key.NewBinding(key.WithKeys("/exit"), key.WithHelp("/exit", "quit")),
}

var chatActionBindings = []key.Binding{
	key.NewBinding(key.WithKeys("enter", "f5", "f8"), key.WithHelp("enter", "send")),
	key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"), key.WithHelp("shift+enter", "newline")),
	key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "focus panes")),
	key.NewBinding(key.WithKeys("pgup", "pgdn"), key.WithHelp("pgup/dn", "scroll")),
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("up/down", "history")),
	key.NewBinding(key.WithKeys("wheel"), key.WithHelp("wheel", "scroll chat")),
}

var helpActionBindings = []key.Binding{
	key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "close help")),
	key.NewBinding(key.WithKeys("enter", "f5", "f8"), key.WithHelp("enter", "send input")),
	key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"), key.WithHelp("shift+enter", "newline")),
	key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "focus panes")),
	key.NewBinding(key.WithKeys("/help"), key.WithHelp("/help", "close help")),
	key.NewBinding(key.WithKeys("pgup", "pgdn"), key.WithHelp("pgup/dn", "scroll")),
}

func (m Model) currentHelpKeyMap() appKeyMap {
	switch m.ui.Mode {
	case state.ModeTodo:
		return appKeyMap{
			short: []key.Binding{todo.Keys.Up, todo.Keys.Down, todo.Keys.Done, todo.Keys.Delete, todo.Keys.Back},
			full: [][]key.Binding{
				{todo.Keys.Up, todo.Keys.Down, todo.Keys.Done},
				{todo.Keys.Delete, todo.Keys.Add, todo.Keys.Back},
			},
		}
	case state.ModeHelp:
		return appKeyMap{
			short: []key.Binding{helpActionBindings[0], helpActionBindings[1], helpActionBindings[2], helpActionBindings[3]},
			full: [][]key.Binding{
				helpActionBindings,
				chatCommandBindings,
			},
		}
	default:
		return appKeyMap{
			short: []key.Binding{chatActionBindings[0], chatActionBindings[1], chatActionBindings[2], chatCommandBindings[0]},
			full: [][]key.Binding{
				chatActionBindings,
				chatCommandBindings,
			},
		}
	}
}

func (m Model) View() string {
	if m.ui.Width < 72 || m.ui.Height < 18 {
		return "Window too small"
	}

	layout := m.layout
	if layout.totalWidth == 0 || layout.totalHeight == 0 {
		layout = layoutState{
			totalWidth:         m.ui.Width,
			totalHeight:        m.ui.Height,
			mainHeight:         maxInt(7, m.ui.Height-9),
			composerHeight:     7,
			footerHeight:       2,
			stacked:            m.ui.Width < 110,
			conversationWidth:  m.ui.Width,
			conversationHeight: maxInt(7, m.ui.Height-9),
			contextWidth:       m.ui.Width,
			contextHeight:      maxInt(7, m.ui.Height-9),
		}
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderWorkspace(layout),
		components.RenderPanelSpec(m.composerPanelSpec(layout)),
		m.renderFooterBar(layout),
	)

	return components.RenderCanvas(layout.totalWidth, layout.totalHeight, content)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func (m Model) renderWorkspace(layout layoutState) string {
	primary := m.conversationPanelSpec(layout)
	if m.ui.Mode == state.ModeHelp {
		primary = m.helpPanelSpec(layout)
	}
	return m.renderWorkspacePanels(layout, primary, m.contextPanelSpec(layout))
}

func (m Model) renderWorkspacePanels(layout layoutState, primary components.PanelSpec, secondary components.PanelSpec) string {
	columnGap := " "
	rowGap := strings.Repeat(" ", maxInt(1, layout.totalWidth))
	left := components.RenderPanelSpec(primary)
	right := components.RenderPanelSpec(secondary)

	if layout.stacked {
		return lipgloss.NewStyle().Width(layout.totalWidth).Render(
			lipgloss.JoinVertical(lipgloss.Left, left, rowGap, right),
		)
	}
	return lipgloss.NewStyle().Width(layout.totalWidth).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, left, columnGap, right),
	)
}

func (m Model) conversationPanelSpec(layout layoutState) components.PanelSpec {
	viewportView := m.viewport
	title := "Conversation"
	hint := fmt.Sprintf("%d message(s) | scroll %d%%", len(m.chat.Messages), int(viewportView.ScrollPercent()*100))
	if m.ui.Mode == state.ModeTodo {
		title = "Todo"
		hint = fmt.Sprintf("%d item(s)", len(m.todo.items()))
	}

	return components.PanelSpec{
		Title:   title,
		Hint:    hint,
		Body:    viewportView.renderView(m.ui.Mode, m.todo, m.toComponentMessages()),
		Width:   layout.conversationWidth,
		Height:  layout.conversationHeight,
		Focused: m.activePanel() == "conversation" || m.activePanel() == "todo",
		Accent:  "#E5C07B",
	}
}

func (m Model) helpPanelSpec(layout layoutState) components.PanelSpec {
	return components.PanelSpec{
		Title:   "Help",
		Hint:    "workspace guide",
		Body:    components.RenderHelp(components.PanelInnerWidth(layout.conversationWidth), m.renderHelpContent()),
		Width:   layout.conversationWidth,
		Height:  layout.conversationHeight,
		Focused: m.activePanel() == "help",
		Accent:  "#D19A66",
	}
}

func (m Model) contextPanelSpec(layout layoutState) components.PanelSpec {
	body := m.renderContextBody(components.PanelInnerWidth(layout.contextWidth))
	return components.PanelSpec{
		Title:   "Context",
		Hint:    fmt.Sprintf("runtime snapshot | scroll %d%%", int(m.context.ScrollPercent()*100)),
		Body:    m.context.renderView(body),
		Width:   layout.contextWidth,
		Height:  layout.contextHeight,
		Focused: m.activePanel() == "context",
		Accent:  "#61AFEF",
	}
}

func (m Model) composerPanelSpec(layout layoutState) components.PanelSpec {
	profile := m.composerProfile(strings.TrimSpace(m.textarea.Value()))
	return components.PanelSpec{
		Title:   "Composer",
		Hint:    profile.hint,
		Body:    m.renderInputArea(),
		Width:   layout.totalWidth,
		Height:  layout.composerHeight,
		Focused: m.activePanel() == "composer",
		Accent:  "#D19A66",
	}
}

func (m Model) renderFooterBar(layout layoutState) string {
	summary := m.workbenchSummary(layout.totalWidth)
	statusLine := components.StatusBar{
		Mode:      summary.mode,
		Focus:     summary.focus,
		Model:     summary.model,
		MemoryCnt: summary.memoryTotal,
		Status:    summary.status,
		Busy:      summary.busy,
		Width:     layout.totalWidth,
	}.Render()

	helpLine := components.RenderFooterLine(m.renderShortHelp(), summary.footerMeta(layout.totalWidth), layout.totalWidth)
	return lipgloss.JoinVertical(lipgloss.Left, statusLine, helpLine)
}

func (m Model) renderShortHelp() string {
	helpView := m.help
	helpView.Width = m.ui.Width
	helpView.ShowAll = false
	return strings.ReplaceAll(helpView.ShortHelpView(m.currentHelpKeyMap().ShortHelp()), "\n", " | ")
}

func (m Model) renderHelpContent() string {
	helpView := m.help
	helpView.Width = m.ui.Width
	helpView.ShowAll = true
	return helpView.View(m.currentHelpKeyMap())
}

func (m Model) activePanel() string {
	return currentFocusLabel(m.ui.Mode, m.ui.Focused)
}

func (m Model) workbenchSummary(width int) workbenchSummary {
	response := "idle"
	if m.chat.Generating {
		response = "streaming"
	}

	return workbenchSummary{
		mode:            currentModeLabel(m.ui.Mode),
		focus:           currentFocusLabel(m.ui.Mode, m.ui.Focused),
		status:          m.statusText(),
		model:           strings.TrimSpace(m.chat.ActiveModel),
		workspace:       workspacePreview(strings.TrimSpace(m.chat.WorkspaceRoot), maxInt(12, width/3)),
		response:        response,
		memoryTotal:     m.chat.MemoryStats.TotalItems,
		autoScroll:      m.ui.AutoScroll,
		busy:            m.statusBusy(),
		apiKeyAttention: !m.chat.APIKeyReady,
	}
}

func (s workbenchSummary) footerMeta(width int) string {
	parts := []string{
		"workspace " + s.workspace,
		"auto-scroll " + boolLabel(s.autoScroll),
	}
	return strings.Join(parts, " | ")
}

func workspacePreview(workspace string, width int) string {
	if workspace == "" {
		return "unknown"
	}
	if len(workspace) <= width {
		return workspace
	}
	if width < 12 {
		return workspace[:width]
	}
	head := width/2 - 2
	tail := width - head - 3
	return workspace[:head] + "..." + workspace[len(workspace)-tail:]
}

func boolLabel(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (m Model) toComponentMessages() []components.Message {
	messages := make([]components.Message, len(m.chat.Messages))
	for i, msg := range m.chat.Messages {
		messages[i] = components.Message{
			Role:      msg.Role,
			Content:   displayMessageContent(msg.Role, msg.Content),
			Timestamp: msg.Timestamp,
			Streaming: msg.Streaming,
		}
	}
	return messages
}

func displayMessageContent(role, content string) string {
	if role == "system" && isResumeSummaryMessage(content) {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), resumeSummaryPrefix))
	}
	return content
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *Model) configureLayout() {
	if m.ui.Width <= 0 || m.ui.Height <= 0 {
		return
	}

	composerHeight := m.desiredComposerPanelHeight()
	footerHeight := 2
	mainHeight := m.ui.Height - composerHeight - footerHeight
	if mainHeight < 7 {
		mainHeight = 7
	}

	stacked := m.ui.Width < 110
	conversationWidth := m.ui.Width
	contextWidth := m.ui.Width
	conversationHeight := mainHeight
	contextHeight := mainHeight

	if stacked {
		contextHeight = maxInt(8, mainHeight/3)
		conversationHeight = maxInt(8, mainHeight-contextHeight-1)
		contextHeight = maxInt(8, mainHeight-conversationHeight-1)
	} else {
		contextWidth = clampInt(m.ui.Width*30/100, 28, 40)
		conversationWidth = maxInt(40, m.ui.Width-contextWidth-1)
	}

	m.layout = layoutState{
		totalWidth:         m.ui.Width,
		totalHeight:        m.ui.Height,
		mainHeight:         mainHeight,
		composerHeight:     composerHeight,
		footerHeight:       footerHeight,
		stacked:            stacked,
		conversationWidth:  conversationWidth,
		conversationHeight: conversationHeight,
		contextWidth:       contextWidth,
		contextHeight:      contextHeight,
	}

	m.textarea.syncWidth(components.PanelInnerWidth(m.ui.Width))
	m.viewport.syncSize(
		components.PanelInnerWidth(conversationWidth),
		components.PanelBodyHeight(conversationHeight),
	)
	m.context.syncSize(
		components.PanelInnerWidth(contextWidth),
		components.PanelBodyHeight(contextHeight),
	)
}

func (m Model) desiredComposerPanelHeight() int {
	contentLines := maxInt(1, m.textarea.Height())
	metaLines := 3
	totalHeight := contentLines + metaLines + components.PanelVerticalFrameSize() + 1
	return clampInt(totalHeight, 7, 14)
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
