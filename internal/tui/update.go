package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	agentruntime "github.com/dust/neo-code/internal/runtime"
	"github.com/dust/neo-code/internal/tools"
)

type RuntimeMsg struct{ Event agentruntime.RuntimeEvent }
type RuntimeClosedMsg struct{}
type runFinishedMsg struct{ err error }
type localCommandResultMsg struct {
	notice string
	err    error
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var spinCmd tea.Cmd
	a.spinner, spinCmd = a.spinner.Update(msg)
	if a.state.IsAgentRunning {
		cmds = append(cmds, spinCmd)
	}

	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = typed.Width
		a.height = typed.Height
		a.resizeComponents()
		return a, tea.Batch(cmds...)
	case RuntimeMsg:
		a.handleRuntimeEvent(typed.Event)
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		a.rebuildTranscript()
		cmds = append(cmds, ListenForRuntimeEvent(a.runtime.Events()))
		return a, tea.Batch(cmds...)
	case RuntimeClosedMsg:
		a.state.IsAgentRunning = false
		if strings.TrimSpace(a.state.StatusText) == "" {
			a.state.StatusText = statusRuntimeClosed
		}
		a.rebuildTranscript()
		return a, tea.Batch(cmds...)
	case runFinishedMsg:
		if typed.err != nil {
			a.state.IsAgentRunning = false
			a.state.StreamingReply = false
			a.state.CurrentTool = ""
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendInlineMessage(roleError, typed.err.Error())
		}
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		a.rebuildTranscript()
		return a, tea.Batch(cmds...)
	case localCommandResultMsg:
		if typed.err != nil {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendInlineMessage(roleError, typed.err.Error())
		} else {
			a.state.ExecutionError = ""
			a.state.StatusText = typed.notice
			cfg := a.configManager.Get()
			a.syncConfigState(cfg)
			a.selectCurrentModel(cfg.CurrentModel)
			a.appendInlineMessage(roleSystem, typed.notice)
		}
		a.rebuildTranscript()
		return a, tea.Batch(cmds...)
	case tea.KeyMsg:
		if key.Matches(typed, a.keys.Quit) {
			return a, tea.Quit
		}
		if key.Matches(typed, a.keys.ToggleHelp) {
			a.state.ShowHelp = !a.state.ShowHelp
			a.help.ShowAll = a.state.ShowHelp
			a.resizeComponents()
			return a, tea.Batch(cmds...)
		}
		if a.state.ShowModelPicker {
			return a.updateModelPicker(typed)
		}
		if key.Matches(typed, a.keys.NextPanel) {
			a.focusNext()
			return a, tea.Batch(cmds...)
		}
		if key.Matches(typed, a.keys.PrevPanel) {
			a.focusPrev()
			return a, tea.Batch(cmds...)
		}
		if key.Matches(typed, a.keys.FocusInput) {
			a.focus = panelInput
			a.applyFocus()
			return a, tea.Batch(cmds...)
		}
		if key.Matches(typed, a.keys.NewSession) && !a.state.IsAgentRunning {
			a.state.ActiveSessionID = ""
			a.state.ActiveSessionTitle = draftSessionTitle
			a.activeMessages = nil
			a.state.StatusText = statusDraft
			a.state.ExecutionError = ""
			a.state.CurrentTool = ""
			a.input.Reset()
			a.state.InputText = ""
			a.focus = panelInput
			a.applyFocus()
			a.rebuildTranscript()
			return a, tea.Batch(cmds...)
		}

		switch a.focus {
		case panelSessions:
			if key.Matches(typed, a.keys.OpenSession) && !a.isFilteringSessions() {
				if err := a.activateSelectedSession(); err != nil {
					a.state.StatusText = err.Error()
					a.state.ExecutionError = err.Error()
					a.appendInlineMessage(roleError, err.Error())
				}
				a.focus = panelInput
				a.applyFocus()
				a.rebuildTranscript()
				return a, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			a.sessions, cmd = a.sessions.Update(msg)
			cmds = append(cmds, cmd)
			return a, tea.Batch(cmds...)
		case panelTranscript:
			a.handleViewportKeys(&a.transcript, typed)
			return a, tea.Batch(cmds...)
		case panelInput:
			return a.updateInputPanel(msg, typed, cmds)
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) updateInputPanel(msg tea.Msg, typed tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if key.Matches(typed, a.keys.Send) {
		input := strings.TrimSpace(a.input.Value())
		if input == "" || a.state.IsAgentRunning {
			return a, tea.Batch(cmds...)
		}

		a.input.Reset()
		a.state.InputText = ""

		switch strings.ToLower(input) {
		case slashCommandModelPicker:
			a.openModelPicker()
			return a, tea.Batch(cmds...)
		}

		if strings.HasPrefix(input, slashPrefix) {
			a.state.StatusText = statusApplyingCommand
			cmds = append(cmds, runLocalCommand(a.configManager, input))
			return a, tea.Batch(cmds...)
		}

		a.state.IsAgentRunning = true
		a.state.StreamingReply = false
		a.state.ExecutionError = ""
		a.state.StatusText = statusThinking
		a.state.CurrentTool = ""
		a.activeMessages = append(a.activeMessages, provider.Message{Role: roleUser, Content: input})
		a.rebuildTranscript()
		cmds = append(cmds, runAgent(a.runtime, a.state.ActiveSessionID, input))
		return a, tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.state.InputText = a.input.Value()
	a.resizeComponents()
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

func (a App) updateModelPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.FocusInput):
		a.closeModelPicker()
		return a, nil
	case msg.String() == "enter":
		item, ok := a.modelPicker.SelectedItem().(modelItem)
		a.closeModelPicker()
		if !ok {
			return a, nil
		}
		return a, runModelSelection(a.configManager, item.name)
	}

	var cmd tea.Cmd
	a.modelPicker, cmd = a.modelPicker.Update(msg)
	return a, cmd
}

func (a *App) refreshSessions() error {
	sessions, err := a.runtime.ListSessions(context.Background())
	if err != nil {
		return err
	}

	a.state.Sessions = sessions

	var selectedID string
	if item, ok := a.sessions.SelectedItem().(sessionItem); ok {
		selectedID = item.Summary.ID
	}

	items := make([]list.Item, 0, len(sessions))
	cursor := 0
	for i, summary := range sessions {
		items = append(items, sessionItem{Summary: summary, Active: summary.ID == a.state.ActiveSessionID})
		if summary.ID == selectedID || summary.ID == a.state.ActiveSessionID {
			cursor = i
		}
	}

	a.sessions.SetItems(items)
	if len(items) > 0 {
		a.sessions.Select(cursor)
	}

	return nil
}

func (a *App) refreshMessages() error {
	if strings.TrimSpace(a.state.ActiveSessionID) == "" {
		a.activeMessages = nil
		return nil
	}

	session, err := a.runtime.LoadSession(context.Background(), a.state.ActiveSessionID)
	if err != nil {
		return err
	}

	a.activeMessages = session.Messages
	a.state.ActiveSessionTitle = session.Title
	return nil
}

func (a *App) activateSelectedSession() error {
	item, ok := a.sessions.SelectedItem().(sessionItem)
	if !ok {
		return nil
	}

	a.state.ActiveSessionID = item.Summary.ID
	a.state.ActiveSessionTitle = item.Summary.Title
	a.state.ExecutionError = ""
	a.state.CurrentTool = ""

	if err := a.refreshSessions(); err != nil {
		return err
	}

	return a.refreshMessages()
}

func (a *App) syncActiveSessionTitle() {
	if strings.TrimSpace(a.state.ActiveSessionID) == "" {
		if strings.TrimSpace(a.state.ActiveSessionTitle) == "" {
			a.state.ActiveSessionTitle = draftSessionTitle
		}
		return
	}

	for _, item := range a.state.Sessions {
		if item.ID == a.state.ActiveSessionID {
			a.state.ActiveSessionTitle = item.Title
			return
		}
	}
}

func (a *App) syncConfigState(cfg config.Config) {
	a.state.CurrentProvider = cfg.SelectedProvider
	a.state.CurrentModel = cfg.CurrentModel
	a.state.CurrentWorkdir = cfg.Workdir
}

func (a *App) handleRuntimeEvent(event agentruntime.RuntimeEvent) {
	if a.state.ActiveSessionID == "" {
		a.state.ActiveSessionID = event.SessionID
	}

	switch event.Type {
	case agentruntime.EventUserMessage:
		a.state.StatusText = statusThinking
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.state.ExecutionError = ""
	case agentruntime.EventToolStart:
		a.state.StatusText = statusRunningTool
		a.state.StreamingReply = false
		if payload, ok := event.Payload.(provider.ToolCall); ok {
			a.state.CurrentTool = payload.Name
			a.appendInlineMessage(roleEvent, "Running tool: "+payload.Name+"...")
		}
	case agentruntime.EventToolResult:
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		if payload, ok := event.Payload.(tools.ToolResult); ok {
			if payload.IsError {
				a.state.ExecutionError = payload.Content
				a.state.StatusText = statusToolError
				a.appendInlineMessage(roleError, preview(payload.Content, 88, 4))
			} else if strings.TrimSpace(a.state.ExecutionError) == "" {
				a.state.StatusText = statusToolFinished
				a.appendInlineMessage(roleEvent, "Completed tool: "+payload.Name)
			}
		}
	case agentruntime.EventAgentChunk:
		if payload, ok := event.Payload.(string); ok {
			a.appendAssistantChunk(payload)
		}
	case agentruntime.EventAgentDone:
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		if strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.StatusText = statusReady
		}
		if payload, ok := event.Payload.(provider.Message); ok && strings.TrimSpace(payload.Content) != "" && !a.lastAssistantMatches(payload.Content) {
			a.activeMessages = append(a.activeMessages, provider.Message{Role: roleAssistant, Content: payload.Content})
		}
	case agentruntime.EventError:
		a.state.StatusText = statusError
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		if payload, ok := event.Payload.(string); ok {
			a.state.ExecutionError = payload
			a.state.StatusText = payload
			a.appendInlineMessage(roleError, payload)
		}
	}
}

func (a *App) appendAssistantChunk(chunk string) {
	if chunk == "" {
		return
	}

	if !a.state.StreamingReply || len(a.activeMessages) == 0 || a.activeMessages[len(a.activeMessages)-1].Role != roleAssistant {
		a.activeMessages = append(a.activeMessages, provider.Message{Role: roleAssistant, Content: chunk})
		a.state.StreamingReply = true
		return
	}

	a.activeMessages[len(a.activeMessages)-1].Content += chunk
}

func (a *App) appendInlineMessage(role string, message string) {
	content := strings.TrimSpace(message)
	if content == "" {
		return
	}

	a.activeMessages = append(a.activeMessages, provider.Message{Role: role, Content: content})
}

func (a *App) lastAssistantMatches(content string) bool {
	if len(a.activeMessages) == 0 {
		return false
	}

	last := a.activeMessages[len(a.activeMessages)-1]
	return last.Role == roleAssistant && strings.TrimSpace(last.Content) == strings.TrimSpace(content)
}

func (a *App) handleViewportKeys(vp *viewport.Model, msg tea.KeyMsg) {
	switch {
	case key.Matches(msg, a.keys.ScrollUp):
		vp.LineUp(2)
	case key.Matches(msg, a.keys.ScrollDown):
		vp.LineDown(2)
	case key.Matches(msg, a.keys.PageUp):
		vp.HalfViewUp()
	case key.Matches(msg, a.keys.PageDown):
		vp.HalfViewDown()
	case key.Matches(msg, a.keys.Top):
		vp.GotoTop()
	case key.Matches(msg, a.keys.Bottom):
		vp.GotoBottom()
	}
}

func (a *App) focusNext() {
	order := []panel{panelSessions, panelTranscript, panelInput}
	current := 0
	for i, item := range order {
		if item == a.focus {
			current = i
			break
		}
	}

	a.focus = order[(current+1)%len(order)]
	a.applyFocus()
}

func (a *App) focusPrev() {
	order := []panel{panelSessions, panelTranscript, panelInput}
	current := 0
	for i, item := range order {
		if item == a.focus {
			current = i
			break
		}
	}

	if current == 0 {
		a.focus = order[len(order)-1]
	} else {
		a.focus = order[current-1]
	}

	a.applyFocus()
}

func (a *App) applyFocus() {
	a.state.Focus = a.focus
	if a.focus == panelInput && !a.state.ShowModelPicker {
		a.input.Focus()
		return
	}
	a.input.Blur()
}

func (a *App) resizeComponents() {
	lay := a.computeLayout()
	a.help.ShowAll = a.state.ShowHelp
	a.sessions.SetSize(max(20, lay.sidebarWidth-4), max(4, lay.sidebarHeight-4))
	menuHeight := a.commandMenuHeight(max(24, lay.rightWidth))
	a.transcript.Width = max(24, lay.rightWidth)
	a.transcript.Height = max(6, lay.rightHeight-2-menuHeight-1)
	a.input.SetWidth(max(24, lay.rightWidth-2))
	a.input.SetHeight(1)
	a.modelPicker.SetSize(max(24, clamp(lay.rightWidth-14, 28, 52)), max(4, clamp(lay.rightHeight-10, 6, 10)))
	a.rebuildTranscript()
}

func (a *App) rebuildTranscript() {
	width := max(24, a.transcript.Width)
	if len(a.activeMessages) == 0 {
		a.transcript.SetContent(a.styles.empty.Width(width).Render(emptyConversationText))
		a.transcript.GotoTop()
		return
	}

	atBottom := a.transcript.AtBottom()
	blocks := make([]string, 0, len(a.activeMessages))
	for _, message := range a.activeMessages {
		blocks = append(blocks, a.renderMessageBlock(message, width))
	}

	a.transcript.SetContent(strings.Join(blocks, "\n\n"))
	if atBottom || a.state.IsAgentRunning {
		a.transcript.GotoBottom()
	}
}

func ListenForRuntimeEvent(sub <-chan agentruntime.RuntimeEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-sub
		if !ok {
			return RuntimeClosedMsg{}
		}
		return RuntimeMsg{Event: event}
	}
}

func runAgent(runtime agentruntime.Runtime, sessionID string, content string) tea.Cmd {
	return func() tea.Msg {
		err := runtime.Run(context.Background(), agentruntime.UserInput{SessionID: sessionID, Content: content})
		return runFinishedMsg{err: err}
	}
}
