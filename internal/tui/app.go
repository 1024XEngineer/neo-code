package tui

import (
	"context"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
	"neocode/internal/runtime"
	"neocode/internal/tools"
)

type runtimeEventMsg struct {
	event runtime.Event
}

type submitFinishedMsg struct {
	err error
}

type clearNoticeMsg struct {
	id int
}

// App wraps the Bubble Tea program and runtime wiring.
type App struct {
	runtime *runtime.Service
}

// New constructs a new TUI app.
func New(runtimeSvc *runtime.Service) *App {
	return &App{runtime: runtimeSvc}
}

// Run starts the Bubble Tea program.
func (a *App) Run(ctx context.Context) error {
	model := newModel(ctx, a.runtime)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := program.Run()
	return err
}

type model struct {
	ctx             context.Context
	runtime         *runtime.Service
	events          <-chan runtime.Event
	state           uiState
	keys            KeyMap
	help            help.Model
	composer        textarea.Model
	sidebarFilter   textinput.Model
	viewport        viewport.Model
	runtimeViewport viewport.Model
	width           int
	height          int
	layout          layoutMetrics
	rendered        renderedConversation
	lastRuntime     string
	initCmd         tea.Cmd
}

func newModel(ctx context.Context, runtimeSvc *runtime.Service) model {
	composer := textarea.New()
	composer.Placeholder = "Describe a code change, ask for an investigation, or request a tool-assisted task..."
	composer.Prompt = " > "
	composer.CharLimit = 4000
	composer.ShowLineNumbers = false
	composer.EndOfBufferCharacter = ' '
	composer.SetHeight(4)
	composer.SetWidth(80)
	composer.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"))
	composer.FocusedStyle.Prompt = composer.FocusedStyle.Prompt.Foreground(themeAccent).Bold(true)
	composer.FocusedStyle.Placeholder = composer.FocusedStyle.Placeholder.Foreground(themeMuted)
	composer.FocusedStyle.Text = composer.FocusedStyle.Text.Foreground(themeText)
	composer.BlurredStyle.Prompt = composer.BlurredStyle.Prompt.Foreground(themeDim)
	composer.BlurredStyle.Placeholder = composer.BlurredStyle.Placeholder.Foreground(themeDim)
	composer.BlurredStyle.Text = composer.BlurredStyle.Text.Foreground(themeText)
	composerFocusCmd := composer.Focus()

	sidebarFilter := textinput.New()
	sidebarFilter.Placeholder = "Filter sessions..."
	sidebarFilter.Prompt = " / "
	sidebarFilter.CharLimit = 120
	sidebarFilter.Width = 24
	sidebarFilter.PromptStyle = sidebarFilter.PromptStyle.Foreground(themeAccent).Bold(true)
	sidebarFilter.TextStyle = sidebarFilter.TextStyle.Foreground(themeText)
	sidebarFilter.PlaceholderStyle = sidebarFilter.PlaceholderStyle.Foreground(themeDim)
	sidebarFilter.Blur()

	conversationViewport := viewport.New(0, 0)
	conversationViewport.MouseWheelEnabled = true
	conversationViewport.SetContent("Loading NeoCode workspace...")

	runtimeViewport := viewport.New(0, 0)
	runtimeViewport.MouseWheelEnabled = true
	runtimeViewport.SetContent("Loading runtime activity...")

	helpModel := help.New()
	helpModel.ShowAll = false

	m := model{
		ctx:             ctx,
		runtime:         runtimeSvc,
		events:          runtimeSvc.Subscribe(128),
		keys:            defaultKeyMap(),
		help:            helpModel,
		composer:        composer,
		sidebarFilter:   sidebarFilter,
		viewport:        conversationViewport,
		runtimeViewport: runtimeViewport,
		state: uiState{
			streaming:         make(map[string]string),
			activeTools:       make(map[string]activeTool),
			pane:              paneCompose,
			selectedMessage:   -1,
			selectedCodeBlock: -1,
		},
		initCmd: tea.Batch(composerFocusCmd, textarea.Blink, textinput.Blink),
	}
	m.sync(true)
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.initCmd, listenRuntime(m.events))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case runtimeEventMsg:
		cmd := m.handleRuntimeEvent(msg.event)
		return m, tea.Batch(cmd, listenRuntime(m.events))
	case submitFinishedMsg:
		if msg.err != nil {
			m.state.lastError = msg.err.Error()
			delete(m.state.streaming, m.state.activeSessionID)
			m.state.clearTools(m.state.activeSessionID)
			cmd := m.setNotice("Run failed: "+truncateVisual(msg.err.Error(), 72), toneError)
			m.sync(false)
			return m, cmd
		}
		m.sync(false)
		return m, nil
	case clearNoticeMsg:
		if msg.id == m.state.noticeID {
			m.state.notice = ""
			m.state.noticeTone = toneInfo
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading NeoCode workspace..."
	}

	if m.state.showHelp {
		return m.renderHelpOverlay()
	}

	return renderRoot(m)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}

	if m.state.showHelp {
		if msg.String() == "esc" || key.Matches(msg, m.keys.ToggleHelp) {
			m.state.showHelp = false
		}
		return m, nil
	}

	if msg.Paste || key.Matches(msg, m.keys.Paste) {
		return m.handlePasteKey(msg)
	}

	if m.state.sidebarOpen {
		return m.handleSidebarKey(msg)
	}

	if m.state.pane == paneCompose {
		return m.handleComposerKey(msg)
	}

	if key.Matches(msg, m.keys.ToggleHelp) {
		m.state.showHelp = true
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.NewSession):
		cmd := m.createSession()
		return m, cmd
	case key.Matches(msg, m.keys.ToggleSidebar):
		return m, m.toggleSidebar()
	case key.Matches(msg, m.keys.SearchSessions):
		return m, m.openSidebar(true)
	case key.Matches(msg, m.keys.SwitchProvider):
		m.selectProvider(1)
		return m, nil
	default:
		return m.handleBrowseKey(msg)
	}
}

func (m *model) handleComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.BrowseMode) {
		return m, m.setPane(paneBrowse)
	}

	if key.Matches(msg, m.keys.ClearComposer) {
		if strings.TrimSpace(m.composer.Value()) == "" {
			return m, nil
		}
		m.composer.Reset()
		return m, m.setNotice("Composer cleared.", toneInfo)
	}

	if key.Matches(msg, m.keys.Submit) {
		if m.state.status.IsRunning {
			return m, nil
		}

		content := strings.TrimSpace(m.composer.Value())
		if content == "" {
			return m, nil
		}

		sessionID := m.state.activeSessionID
		m.composer.Reset()
		m.state.lastError = ""
		return m, submitCmd(m.ctx, m.runtime, runtime.UserInput{
			SessionID: sessionID,
			Content:   content,
		})
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

func (m *model) handlePasteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.state.sidebarOpen {
		previous := m.sidebarFilter.Value()
		var cmd tea.Cmd
		m.sidebarFilter, cmd = m.sidebarFilter.Update(msg)
		if previous != m.sidebarFilter.Value() {
			m.state.sidebarSelection = 0
			m.state.sidebarScroll = 0
		}
		m.ensureSidebarSelection()
		return m, cmd
	}

	focusCmd := tea.Cmd(nil)
	if m.state.pane != paneCompose {
		focusCmd = m.setPane(paneCompose)
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, tea.Batch(focusCmd, cmd)
}

func (m *model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.ComposeMode):
		return m, m.setPane(paneCompose)
	case key.Matches(msg, m.keys.PrevCodeBlock):
		return m, m.moveCodeSelection(-1)
	case key.Matches(msg, m.keys.NextCodeBlock):
		return m, m.moveCodeSelection(1)
	case key.Matches(msg, m.keys.CopyCodeBlock):
		return m, m.copySelectedCodeBlock()
	case key.Matches(msg, m.keys.CopyMessage):
		return m, m.copySelectedMessage()
	case key.Matches(msg, m.keys.JumpLatest):
		m.viewport.GotoBottom()
		return m, m.setNotice("Jumped to latest output.", toneInfo)
	case key.Matches(msg, m.keys.GoTop):
		if msg.String() == "home" || msg.String() == "g" {
			m.viewport.GotoTop()
		}
		return m, nil
	case key.Matches(msg, m.keys.GoBottom):
		if msg.String() == "end" || msg.String() == "G" {
			m.viewport.GotoBottom()
		}
		return m, nil
	}

	switch msg.String() {
	case "up":
		m.viewport.LineUp(1)
	case "down":
		m.viewport.LineDown(1)
	case "pgup":
		m.viewport.PageUp()
	case "pgdown":
		m.viewport.PageDown()
	case "home":
		m.viewport.GotoTop()
	case "end":
		m.viewport.GotoBottom()
	}

	return m, nil
}

func (m *model) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.BrowseMode):
		return m, m.closeSidebar()
	case key.Matches(msg, m.keys.Submit):
		sessions := m.filteredSessions()
		if len(sessions) == 0 {
			return m, nil
		}
		target := sessions[m.state.sidebarSelection]
		m.state.activeSessionID = target.Summary.ID
		m.sync(true)
		return m, m.closeSidebar()
	case key.Matches(msg, m.keys.SidebarUp):
		m.moveSidebarSelection(-1)
		return m, nil
	case key.Matches(msg, m.keys.SidebarDown):
		m.moveSidebarSelection(1)
		return m, nil
	}

	previous := m.sidebarFilter.Value()
	var cmd tea.Cmd
	m.sidebarFilter, cmd = m.sidebarFilter.Update(msg)
	if previous != m.sidebarFilter.Value() {
		m.state.sidebarSelection = 0
		m.state.sidebarScroll = 0
	}
	m.ensureSidebarSelection()
	return m, cmd
}

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.state.showHelp {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.state.showHelp = false
		}
		return m, nil
	}

	if tea.MouseEvent(msg).IsWheel() {
		return m, m.handleMouseWheel(msg)
	}

	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}

	for _, zone := range m.state.headerZones {
		if zone.Rect.contains(msg.X, msg.Y) {
			return m, m.performScreenAction(zone)
		}
	}

	if m.state.sidebarOpen {
		if m.layout.sidebarRect.contains(msg.X, msg.Y) {
			for _, zone := range m.state.sidebarZones {
				if zone.Rect.contains(msg.X, msg.Y) {
					return m, m.performScreenAction(zone)
				}
			}
			return m, nil
		}
		return m, m.closeSidebar()
	}

	if m.layout.composerRect.contains(msg.X, msg.Y) {
		return m, m.setPane(paneCompose)
	}

	if m.layout.runtimeRect.inner().contains(msg.X, msg.Y) {
		m.state.pane = paneBrowse
		return m, nil
	}

	if m.layout.conversationRect.inner().contains(msg.X, msg.Y) {
		m.state.pane = paneBrowse
		if cmd := m.handleConversationClick(msg.X, msg.Y); cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m *model) handleMouseWheel(msg tea.MouseMsg) tea.Cmd {
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -3
	case tea.MouseButtonWheelDown:
		delta = 3
	default:
		return nil
	}

	if m.state.sidebarOpen && m.layout.sidebarRect.contains(msg.X, msg.Y) {
		m.moveSidebarSelection(delta)
		return nil
	}

	if m.layout.runtimeRect.inner().contains(msg.X, msg.Y) {
		if delta < 0 {
			m.runtimeViewport.LineUp(-delta)
		} else {
			m.runtimeViewport.LineDown(delta)
		}
		return nil
	}

	if m.layout.conversationRect.inner().contains(msg.X, msg.Y) {
		m.state.pane = paneBrowse
		if delta < 0 {
			m.viewport.LineUp(-delta)
		} else {
			m.viewport.LineDown(delta)
		}
	}
	return nil
}

func (m *model) handleConversationClick(x, y int) tea.Cmd {
	inner := m.layout.conversationRect.inner()
	relX := x - inner.X
	relY := y - inner.Y
	if relX < 0 || relY < 0 {
		return nil
	}

	contentLine := m.viewport.YOffset + relY
	for _, block := range m.rendered.CodeBlocks {
		if block.HeaderLine == contentLine && relX >= block.CopyX1 && relX < block.CopyX2 {
			m.state.selectedCodeBlock = block.Index
			m.state.selectedMessage = block.MessageIndex
			m.refreshConversation(false)
			return m.copySelectedCodeBlock()
		}
	}

	for _, block := range m.rendered.CodeBlocks {
		if contentLine >= block.StartLine && contentLine <= block.EndLine {
			m.state.selectedCodeBlock = block.Index
			m.state.selectedMessage = block.MessageIndex
			m.refreshConversation(false)
			return nil
		}
	}

	for _, message := range m.rendered.Messages {
		if contentLine >= message.StartLine && contentLine <= message.EndLine {
			m.state.selectedMessage = message.MessageIndex
			m.state.selectedCodeBlock = -1
			m.refreshConversation(false)
			break
		}
	}
	return nil
}

func (m *model) performScreenAction(zone screenActionZone) tea.Cmd {
	switch zone.Kind {
	case actionToggleSidebar:
		return m.toggleSidebar()
	case actionNewSession:
		return m.createSession()
	case actionOpenSession:
		m.state.activeSessionID = zone.SessionID
		m.sync(true)
		return m.closeSidebar()
	case actionJumpLatest:
		m.viewport.GotoBottom()
		return m.setNotice("Jumped to latest output.", toneInfo)
	default:
		return nil
	}
}

func (m *model) moveCodeSelection(delta int) tea.Cmd {
	next := nextCodeBlockIndex(m.rendered.CodeBlocks, m.state.selectedCodeBlock, delta)
	if next < 0 {
		return m.setNotice("No code block is available in this conversation.", toneInfo)
	}

	m.state.selectedCodeBlock = next
	m.state.selectedMessage = m.rendered.CodeBlocks[next].MessageIndex
	m.refreshConversation(false)
	m.scrollConversationToLine(m.rendered.CodeBlocks[next].HeaderLine)
	return nil
}

func (m *model) scrollConversationToLine(line int) {
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(max(0, line))
		return
	}
	if line >= m.viewport.YOffset+m.viewport.Height {
		target := line - max(0, m.viewport.Height/2)
		m.viewport.SetYOffset(max(0, target))
	}
}

func (m *model) copySelectedCodeBlock() tea.Cmd {
	_, codeText, _, ok := copyTargets(m.rendered, m.state.selectedMessage, m.state.selectedCodeBlock)
	if !ok || strings.TrimSpace(codeText) == "" {
		return m.setNotice("No code block is selected.", toneInfo)
	}
	if err := clipboard.WriteAll(codeText); err != nil {
		return m.setNotice("Copy failed: "+truncateVisual(err.Error(), 72), toneError)
	}
	return m.setNotice("Code block copied to clipboard.", toneSuccess)
}

func (m *model) copySelectedMessage() tea.Cmd {
	messageText, _, okMessage, _ := copyTargets(m.rendered, m.state.selectedMessage, m.state.selectedCodeBlock)
	if !okMessage || strings.TrimSpace(messageText) == "" {
		return m.setNotice("No message is selected.", toneInfo)
	}
	if err := clipboard.WriteAll(messageText); err != nil {
		return m.setNotice("Copy failed: "+truncateVisual(err.Error(), 72), toneError)
	}
	return m.setNotice("Message copied to clipboard.", toneSuccess)
}

func (m *model) createSession() tea.Cmd {
	session, err := m.runtime.CreateSession("")
	if err != nil {
		m.state.lastError = err.Error()
		return m.setNotice("Create session failed: "+truncateVisual(err.Error(), 64), toneError)
	}
	m.state.activeSessionID = session.ID
	m.state.selectedMessage = -1
	m.state.selectedCodeBlock = -1
	m.sidebarFilter.Reset()
	m.sync(true)
	return tea.Batch(m.closeSidebar(), m.setPane(paneCompose))
}

func (m *model) setPane(next paneMode) tea.Cmd {
	m.state.pane = next
	switch next {
	case paneCompose:
		m.sidebarFilter.Blur()
		return m.composer.Focus()
	case paneSidebar:
		m.composer.Blur()
		return m.sidebarFilter.Focus()
	default:
		m.composer.Blur()
		m.sidebarFilter.Blur()
		return nil
	}
}

func (m *model) openSidebar(focusFilter bool) tea.Cmd {
	m.state.sidebarOpen = true
	m.ensureSidebarSelection()
	m.refreshZones()
	if focusFilter {
		return m.setPane(paneSidebar)
	}
	m.state.pane = paneSidebar
	return m.sidebarFilter.Focus()
}

func (m *model) closeSidebar() tea.Cmd {
	m.state.sidebarOpen = false
	m.sidebarFilter.Blur()
	m.refreshZones()
	return m.setPane(paneBrowse)
}

func (m *model) toggleSidebar() tea.Cmd {
	if m.state.sidebarOpen {
		return m.closeSidebar()
	}
	return m.openSidebar(false)
}

func (m *model) moveSidebarSelection(delta int) {
	sessions := m.filteredSessions()
	if len(sessions) == 0 {
		m.state.sidebarSelection = 0
		m.state.sidebarScroll = 0
		return
	}

	next := m.state.sidebarSelection + delta
	if next < 0 {
		next = 0
	}
	if next >= len(sessions) {
		next = len(sessions) - 1
	}
	m.state.sidebarSelection = next
	m.ensureSidebarSelection()
}

func (m *model) ensureSidebarSelection() {
	sessions := m.filteredSessions()
	if len(sessions) == 0 {
		m.state.sidebarSelection = 0
		m.state.sidebarScroll = 0
		m.refreshZones()
		return
	}

	if m.state.sidebarSelection < 0 {
		m.state.sidebarSelection = 0
	}
	if m.state.sidebarSelection >= len(sessions) {
		m.state.sidebarSelection = len(sessions) - 1
	}

	visible := max(1, m.layout.sidebarRect.inner().Height-4)
	if m.state.sidebarSelection < m.state.sidebarScroll {
		m.state.sidebarScroll = m.state.sidebarSelection
	}
	if m.state.sidebarSelection >= m.state.sidebarScroll+visible {
		m.state.sidebarScroll = m.state.sidebarSelection - visible + 1
	}
	if m.state.sidebarScroll < 0 {
		m.state.sidebarScroll = 0
	}
	m.refreshZones()
}

func (m model) filteredSessions() []filteredSession {
	return filterSessions(
		m.state.sessions,
		m.state.activeSessionID,
		m.state.streaming,
		m.state.activeTools,
		m.sidebarFilter.Value(),
	)
}

func (m *model) sync(forceBottom bool) {
	if m.state.streaming == nil {
		m.state.streaming = make(map[string]string)
	}
	if m.state.activeTools == nil {
		m.state.activeTools = make(map[string]activeTool)
	}

	previousSessionID := m.state.activeSessionID
	m.state.sessions = m.runtime.Sessions()
	m.state.providers = m.runtime.ProviderSummaries()

	if len(m.state.sessions) == 0 {
		session, err := m.runtime.CreateSession("")
		if err == nil {
			m.state.sessions = m.runtime.Sessions()
			m.state.activeSessionID = session.ID
		} else {
			m.state.lastError = err.Error()
		}
	}

	if len(m.state.sessions) > 0 && (m.state.activeSessionID == "" || !m.hasSession(m.state.activeSessionID)) {
		m.state.activeSessionID = m.state.sessions[0].ID
	}

	if previousSessionID != "" && previousSessionID != m.state.activeSessionID {
		m.state.selectedMessage = -1
		m.state.selectedCodeBlock = -1
		for idx, session := range m.filteredSessions() {
			if session.Summary.ID == m.state.activeSessionID {
				m.state.sidebarSelection = idx
				break
			}
		}
	}

	m.state.status = m.runtime.Status()
	m.state.lastUpdatedAt = time.Now()

	m.refreshConversation(forceBottom)
	m.refreshRuntime(forceBottom)
	m.ensureSidebarSelection()
	m.refreshZones()
}

func (m *model) resize() {
	if m.width == 0 || m.height == 0 {
		return
	}

	m.layout = computeLayout(m.width, m.height)
	m.help.Width = max(24, m.width-4)
	m.composer.SetWidth(max(24, m.layout.composerRect.inner().Width))
	m.composer.SetHeight(max(3, m.layout.composerRect.inner().Height))
	m.sidebarFilter.Width = max(18, m.layout.sidebarRect.inner().Width-4)
	m.viewport.Width = max(24, m.layout.conversationRect.inner().Width)
	m.viewport.Height = max(6, m.layout.conversationRect.inner().Height)
	m.runtimeViewport.Width = max(20, m.layout.runtimeRect.inner().Width)
	m.runtimeViewport.Height = max(6, m.layout.runtimeRect.inner().Height)
	m.refreshConversation(false)
	m.refreshRuntime(false)
	m.ensureSidebarSelection()
	m.refreshZones()
}

func (m *model) refreshZones() {
	m.state.headerZones = m.state.headerZones[:0]
	m.state.sidebarZones = m.state.sidebarZones[:0]

	if m.width <= 0 || m.height <= 0 {
		return
	}

	statusLabel := "IDLE"
	statusColor := themeSuccess
	if m.state.status.IsRunning {
		statusLabel = "RUNNING"
		statusColor = themeAccent2
	}

	x := 1
	x += ansi.StringWidth(renderBadge("NEOCODE", themeAccent, themeCanvas)) + 1
	x += ansi.StringWidth(renderBadge(statusLabel, statusColor, themeCanvas)) + 1

	sessionsAction := renderAction("Sessions", m.state.sidebarOpen, themeAccent)
	newAction := renderAction("New", false, themeAccent2)

	m.state.headerZones = append(m.state.headerZones,
		screenActionZone{
			Kind: actionToggleSidebar,
			Rect: rect{
				X:      x,
				Y:      0,
				Width:  ansi.StringWidth(sessionsAction),
				Height: 1,
			},
		},
		screenActionZone{
			Kind: actionNewSession,
			Rect: rect{
				X:      x + ansi.StringWidth(sessionsAction) + 1,
				Y:      0,
				Width:  ansi.StringWidth(newAction),
				Height: 1,
			},
		},
	)

	if !m.state.sidebarOpen {
		return
	}

	inner := m.layout.sidebarRect.inner()
	sessions := m.filteredSessions()
	line := 4
	visible := max(1, inner.Height-line-3)
	start := min(m.state.sidebarScroll, max(0, len(sessions)-visible))
	end := min(len(sessions), start+visible)

	for idx := start; idx < end; idx++ {
		entry := sessions[idx]
		height := 3
		if entry.IsBusy || entry.IsActive {
			height = 4
		}
		m.state.sidebarZones = append(m.state.sidebarZones, screenActionZone{
			Kind:      actionOpenSession,
			SessionID: entry.Summary.ID,
			Rect: rect{
				X:      inner.X,
				Y:      inner.Y + line,
				Width:  inner.Width,
				Height: height,
			},
		})
		line += height
	}
}

func (m *model) refreshConversation(forceBottom bool) {
	if m.layout.conversationRect.Width == 0 || m.layout.conversationRect.Height == 0 {
		return
	}

	atBottom := forceBottom || m.viewport.AtBottom()
	contentWidth := max(24, m.layout.conversationRect.inner().Width)
	rendered := renderedConversation{}
	if session, ok := m.state.activeSession(m.runtime); ok {
		rendered = buildConversation(
			session,
			m.state.streaming[m.state.activeSessionID],
			contentWidth,
			m.state.selectedMessage,
			m.state.selectedCodeBlock,
		)
	} else {
		rendered = buildEmptyConversation(contentWidth)
	}

	if len(rendered.Messages) > 0 && (m.state.selectedMessage < 0 || m.state.selectedMessage >= len(rendered.Messages)) {
		m.state.selectedMessage = len(rendered.Messages) - 1
	}
	if len(rendered.CodeBlocks) > 0 && (m.state.selectedCodeBlock < 0 || m.state.selectedCodeBlock >= len(rendered.CodeBlocks)) {
		m.state.selectedCodeBlock = len(rendered.CodeBlocks) - 1
		m.state.selectedMessage = rendered.CodeBlocks[m.state.selectedCodeBlock].MessageIndex
		rendered = buildConversation(
			m.mustActiveSession(),
			m.state.streaming[m.state.activeSessionID],
			contentWidth,
			m.state.selectedMessage,
			m.state.selectedCodeBlock,
		)
	}
	if len(rendered.CodeBlocks) == 0 {
		m.state.selectedCodeBlock = -1
	}

	m.rendered = rendered
	m.viewport.SetContent(rendered.Content)
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *model) refreshRuntime(forceBottom bool) {
	if m.layout.runtimeRect.Width == 0 || m.layout.runtimeRect.Height == 0 {
		return
	}

	atBottom := forceBottom || m.runtimeViewport.AtBottom()
	content := buildRuntimeContent(m)
	if content != m.lastRuntime {
		m.lastRuntime = content
		m.runtimeViewport.SetContent(content)
		if atBottom {
			m.runtimeViewport.GotoBottom()
		}
	}
}

func (m model) mustActiveSession() runtime.Session {
	session, _ := m.state.activeSession(m.runtime)
	return session
}

func (m *model) selectProvider(delta int) {
	if len(m.state.providers) == 0 {
		return
	}

	current := 0
	for idx, providerSummary := range m.state.providers {
		if providerSummary.Name == m.state.status.Provider {
			current = idx
			break
		}
	}

	next := (current + delta + len(m.state.providers)) % len(m.state.providers)
	if err := m.runtime.SwitchProvider(m.state.providers[next].Name); err != nil {
		m.state.lastError = err.Error()
		return
	}

	m.state.lastError = ""
	m.sync(false)
}

func (m model) hasSession(id string) bool {
	for _, session := range m.state.sessions {
		if session.ID == id {
			return true
		}
	}
	return false
}

func listenRuntime(events <-chan runtime.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		return runtimeEventMsg{event: event}
	}
}

func submitCmd(ctx context.Context, runtimeSvc *runtime.Service, input runtime.UserInput) tea.Cmd {
	return func() tea.Msg {
		err := runtimeSvc.Run(ctx, input)
		return submitFinishedMsg{err: err}
	}
}

func noticeCmd(id int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return clearNoticeMsg{id: id}
	})
}

func (m *model) setNotice(text string, tone activityTone) tea.Cmd {
	m.state.noticeID++
	m.state.notice = text
	m.state.noticeTone = tone
	return noticeCmd(m.state.noticeID)
}

func (m *model) handleRuntimeEvent(event runtime.Event) tea.Cmd {
	cmds := make([]tea.Cmd, 0, 2)

	switch event.Type {
	case runtime.EventSessionCreated:
		m.state.appendActivity(activityEntry{
			At:        event.At,
			SessionID: event.SessionID,
			Title:     "Session created",
			Detail:    "Workspace ready for a new thread",
			Tone:      toneInfo,
		})
		m.sync(event.SessionID == m.state.activeSessionID)
	case runtime.EventUserMessage:
		if message, ok := event.Payload.(provider.Message); ok {
			m.state.appendActivity(activityEntry{
				At:        event.At,
				SessionID: event.SessionID,
				Title:     "Prompt queued",
				Detail:    truncateVisual(trimMultiline(message.Content), 60),
				Tone:      toneInfo,
			})
		}
		m.sync(event.SessionID == m.state.activeSessionID)
	case runtime.EventAgentChunk:
		if delta, ok := event.Payload.(string); ok {
			m.state.streaming[event.SessionID] += delta
		}
		m.state.status = m.runtime.Status()
		if event.SessionID == m.state.activeSessionID {
			m.refreshConversation(false)
		}
	case runtime.EventAgentMessage:
		if message, ok := event.Payload.(provider.Message); ok {
			if message.Role == provider.RoleAssistant && len(message.ToolCalls) == 0 {
				delete(m.state.streaming, event.SessionID)
				m.state.appendActivity(activityEntry{
					At:        event.At,
					SessionID: event.SessionID,
					Title:     "Assistant replied",
					Detail:    truncateVisual(trimMultiline(message.Content), 60),
					Tone:      toneSuccess,
				})
			}
			if len(message.ToolCalls) > 0 {
				m.state.appendActivity(activityEntry{
					At:        event.At,
					SessionID: event.SessionID,
					Title:     "Agent requested tools",
					Detail:    truncateVisual(toolNames(message.ToolCalls), 60),
					Tone:      toneRunning,
				})
			}
		}
		m.sync(event.SessionID == m.state.activeSessionID)
	case runtime.EventToolStarted:
		if call, ok := event.Payload.(provider.ToolCall); ok {
			m.state.rememberTool(event.SessionID, call, event.At)
			m.state.appendActivity(activityEntry{
				At:        event.At,
				SessionID: event.SessionID,
				Title:     "Tool started",
				Detail:    call.Name,
				Tone:      toneRunning,
			})
		}
		m.sync(event.SessionID == m.state.activeSessionID)
	case runtime.EventToolFinished:
		detail := "tool completed"
		tone := toneSuccess
		if payload, ok := event.Payload.(map[string]any); ok {
			if call, ok := payload["call"].(provider.ToolCall); ok {
				m.state.forgetTool(event.SessionID, call.ID)
				detail = call.Name
			}
			if result, ok := payload["result"].(tools.Result); ok {
				if result.IsError {
					tone = toneError
				}
				if summary := summarizeToolResult(result); summary != "" {
					detail = detail + " - " + summary
				}
			}
		}
		m.state.appendActivity(activityEntry{
			At:        event.At,
			SessionID: event.SessionID,
			Title:     "Tool finished",
			Detail:    truncateVisual(detail, 72),
			Tone:      tone,
		})
		m.sync(event.SessionID == m.state.activeSessionID)
	case runtime.EventCompleted:
		delete(m.state.streaming, event.SessionID)
		m.state.clearTools(event.SessionID)
		m.state.appendActivity(activityEntry{
			At:        event.At,
			SessionID: event.SessionID,
			Title:     "Run completed",
			Detail:    "Assistant is ready for the next prompt",
			Tone:      toneSuccess,
		})
		m.sync(event.SessionID == m.state.activeSessionID)
		cmds = append(cmds, m.setPane(paneBrowse))
	case runtime.EventError:
		delete(m.state.streaming, event.SessionID)
		m.state.clearTools(event.SessionID)
		if errMsg, ok := event.Payload.(string); ok {
			m.state.lastError = errMsg
			m.state.appendActivity(activityEntry{
				At:        event.At,
				SessionID: event.SessionID,
				Title:     "Runtime error",
				Detail:    truncateVisual(trimMultiline(errMsg), 72),
				Tone:      toneError,
			})
			cmds = append(cmds, m.setNotice("Runtime error: "+truncateVisual(errMsg, 72), toneError))
		}
		m.sync(event.SessionID == m.state.activeSessionID)
		cmds = append(cmds, m.setPane(paneBrowse))
	case runtime.EventStatus:
		if status, ok := event.Payload.(runtime.Status); ok {
			m.state.status = status
		} else {
			m.state.status = m.runtime.Status()
		}
		m.state.lastUpdatedAt = time.Now()
	default:
		m.sync(false)
	}

	return tea.Batch(cmds...)
}
