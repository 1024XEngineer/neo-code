package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/tools"
)

type RuntimeMsg struct{ Event agentruntime.RuntimeEvent }
type RuntimeClosedMsg struct{}
type runFinishedMsg struct{ err error }
type permissionResolveResultMsg struct {
	requestID string
	decision  agentruntime.PermissionResolutionDecision
	err       error
}
type modelCatalogRefreshMsg struct {
	providerID string
	models     []config.ModelDescriptor
	err        error
}

type compactFinishedMsg struct {
	err error
}

type localCommandResultMsg struct {
	notice          string
	err             error
	providerChanged bool
	modelChanged    bool
}
type sessionWorkdirResultMsg struct {
	notice  string
	workdir string
	err     error
}

const (
	composerMinHeight   = 1
	composerMaxHeight   = 5
	composerPromptWidth = 2
	mouseWheelStepLines = 3
	pasteBurstWindow    = 120 * time.Millisecond
	pasteEnterGuard     = 180 * time.Millisecond
	pasteSessionGuard   = 5 * time.Second
	pasteBurstThreshold = 12
)

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var spinCmd tea.Cmd
	a.spinner, spinCmd = a.spinner.Update(msg)
	if a.isBusy() {
		cmds = append(cmds, spinCmd)
	}

	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = typed.Width
		a.height = typed.Height
		a.applyComponentLayout(true)
		return a, tea.Batch(cmds...)
	case RuntimeMsg:
		transcriptDirty := a.handleRuntimeEvent(typed.Event)
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		if transcriptDirty {
			a.rebuildTranscript()
		}
		cmds = append(cmds, ListenForRuntimeEvent(a.runtime.Events()))
		return a, tea.Batch(cmds...)
	case RuntimeClosedMsg:
		a.state.IsAgentRunning = false
		a.clearRunProgress()
		a.state.IsCompacting = false
		if strings.TrimSpace(a.state.StatusText) == "" {
			a.state.StatusText = statusRuntimeClosed
		}
		return a, tea.Batch(cmds...)
	case runFinishedMsg:
		if typed.err != nil {
			a.state.IsAgentRunning = false
			a.pendingPermission = nil
			a.clearRunProgress()
			a.state.StreamingReply = false
			a.state.CurrentTool = ""
			if errors.Is(typed.err, context.Canceled) {
				a.state.ExecutionError = ""
				a.state.StatusText = statusCanceled
			} else {
				a.state.ExecutionError = typed.err.Error()
				a.state.StatusText = typed.err.Error()
			}
		}
		if !a.state.IsAgentRunning {
			a.clearRunProgress()
		}
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		return a, tea.Batch(cmds...)
	case permissionResolveResultMsg:
		if typed.err != nil {
			if a.pendingPermission != nil && strings.EqualFold(strings.TrimSpace(a.pendingPermission.RequestID), strings.TrimSpace(typed.requestID)) {
				a.pendingPermission.Submitted = false
			}
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendActivity("permission", "Submit permission failed", typed.err.Error(), true)
			return a, tea.Batch(cmds...)
		}
		a.state.ExecutionError = ""
		a.state.StatusText = "Permission decision submitted"
		a.appendActivity("permission", "Submitted permission decision", string(typed.decision), false)
		return a, tea.Batch(cmds...)
	case modelCatalogRefreshMsg:
		if strings.EqualFold(a.modelRefreshID, typed.providerID) {
			a.modelRefreshID = ""
		}
		if !strings.EqualFold(strings.TrimSpace(a.state.CurrentProvider), strings.TrimSpace(typed.providerID)) {
			return a, tea.Batch(cmds...)
		}
		if typed.err != nil {
			a.appendActivity("provider", "Failed to refresh models", typed.err.Error(), true)
			return a, tea.Batch(cmds...)
		}

		replacePickerItems(&a.modelPicker, mapModelItems(typed.models))
		cfg := a.configManager.Get()
		a.syncConfigState(cfg)
		selectPickerItemByID(&a.modelPicker, cfg.CurrentModel)
		return a, tea.Batch(cmds...)
	case compactFinishedMsg:
		a.state.IsCompacting = false
		if typed.err != nil && strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
		}
		if err := a.refreshSessions(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendInlineMessage(roleError, err.Error())
		}
		if err := a.refreshMessages(); err != nil && strings.TrimSpace(a.state.ActiveSessionID) != "" {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendInlineMessage(roleError, err.Error())
		}
		a.syncActiveSessionTitle()
		a.rebuildTranscript()
		a.transcript.GotoBottom()
		return a, tea.Batch(cmds...)
	case localCommandResultMsg:
		if typed.err != nil {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendActivity("command", "Local command failed", typed.err.Error(), true)
		} else {
			a.state.ExecutionError = ""
			a.state.StatusText = typed.notice
			cfg := a.configManager.Get()
			a.syncConfigState(cfg)
			if typed.providerChanged {
				if err := a.refreshProviderPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				if err := a.refreshModelPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh models", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				a.selectCurrentProvider(cfg.SelectedProvider)
				a.selectCurrentModel(cfg.CurrentModel)
				if cmd := a.requestModelCatalogRefresh(cfg.SelectedProvider); cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if typed.modelChanged {
				a.selectCurrentModel(cfg.CurrentModel)
			}
			a.appendActivity("command", typed.notice, "", false)
		}
		return a, tea.Batch(cmds...)
	case sessionWorkdirResultMsg:
		if typed.err != nil {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendActivity("workspace", "Workspace command failed", typed.err.Error(), true)
			return a, tea.Batch(cmds...)
		}

		a.state.ExecutionError = ""
		a.state.StatusText = typed.notice
		a.state.CurrentWorkdir = strings.TrimSpace(typed.workdir)
		if err := a.refreshFileCandidates(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendActivity("workspace", "Failed to refresh workspace files", err.Error(), true)
			return a, tea.Batch(cmds...)
		}
		a.appendActivity("workspace", typed.notice, "", false)
		return a, tea.Batch(cmds...)
	case workspaceCommandResultMsg:
		if typed.command == "" && typed.err != nil {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = typed.err.Error()
			a.appendActivity("command", "Workspace command failed", typed.err.Error(), true)
			return a, tea.Batch(cmds...)
		}
		result := formatWorkspaceCommandResult(typed.command, typed.output, typed.err)
		if typed.err != nil {
			a.state.ExecutionError = typed.err.Error()
			a.state.StatusText = fmt.Sprintf("Command failed: %s", typed.command)
			a.appendActivity("command", "Command failed", result, true)
		} else {
			a.state.ExecutionError = ""
			a.state.StatusText = statusCommandDone
			a.appendActivity("command", "Command finished", result, false)
		}
		return a, tea.Batch(cmds...)
	case tea.MouseMsg:
		if a.handleTranscriptMouse(typed) {
			return a, tea.Batch(cmds...)
		}
		if a.handleActivityMouse(typed) {
			return a, tea.Batch(cmds...)
		}
		if a.handleInputMouse(typed) {
			return a, tea.Batch(cmds...)
		}
	case tea.KeyMsg:
		if key.Matches(typed, a.keys.Quit) {
			return a, tea.Quit
		}
		if key.Matches(typed, a.keys.ToggleHelp) {
			a.state.ShowHelp = !a.state.ShowHelp
			a.help.ShowAll = a.state.ShowHelp
			a.applyComponentLayout(true)
			return a, tea.Batch(cmds...)
		}
		if a.pendingPermission != nil {
			if permissionCmd, handled := a.handlePermissionDecisionKey(typed); handled {
				if permissionCmd != nil {
					cmds = append(cmds, permissionCmd)
				}
				return a, tea.Batch(cmds...)
			}
		}
		if a.state.IsAgentRunning && key.Matches(typed, a.keys.CancelAgent) {
			if a.runtime.CancelActiveRun() {
				a.state.StatusText = statusCanceling
			}
			return a, tea.Batch(cmds...)
		}
		if a.state.ActivePicker != pickerNone {
			return a.updatePicker(typed)
		}
		if a.focus == panelInput {
			if cmd, handled := a.updateCommandMenuSelection(typed); handled {
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, tea.Batch(cmds...)
			}
		}
		if a.focus == panelInput && key.Matches(typed, a.keys.NextPanel) {
			if a.applySelectedCommandSuggestion() {
				return a, tea.Batch(cmds...)
			}
			if a.shouldHandleTabAsInput(typed) {
				tabMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\t'}, Paste: typed.Paste}
				return a.updateInputPanel(tabMsg, tabMsg, cmds)
			}
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
		if key.Matches(typed, a.keys.NewSession) && !a.isBusy() {
			a.startDraftSession()
			return a, tea.Batch(cmds...)
		}

		switch a.focus {
		case panelSessions:
			if key.Matches(typed, a.keys.OpenSession) && !a.sessions.SettingFilter() {
				if err := a.activateSelectedSession(); err != nil {
					a.state.StatusText = err.Error()
					a.state.ExecutionError = err.Error()
					a.appendActivity("system", "Failed to open session", err.Error(), true)
				}
				a.focus = panelInput
				a.applyFocus()
				return a, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			a.sessions, cmd = a.sessions.Update(msg)
			a.sessions.SetShowFilter(a.sessions.FilterState() != list.Unfiltered)
			cmds = append(cmds, cmd)
			return a, tea.Batch(cmds...)
		case panelTranscript:
			a.handleViewportKeys(&a.transcript, typed)
			return a, tea.Batch(cmds...)
		case panelActivity:
			a.handleViewportKeys(&a.activity, typed)
			return a, tea.Batch(cmds...)
		case panelInput:
			return a.updateInputPanel(msg, typed, cmds)
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) updateInputPanel(msg tea.Msg, typed tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	now := a.now()
	effectiveTyped := typed

	if key.Matches(typed, a.keys.Send) {
		if a.shouldTreatEnterAsNewline(typed, now) {
			a.growComposerForNewline()
			msg = tea.KeyMsg{Type: tea.KeyEnter}
			effectiveTyped = tea.KeyMsg{Type: tea.KeyEnter, Paste: true}
		} else {
			input := strings.TrimSpace(a.input.Value())
			if input == "" || a.isBusy() {
				return a, tea.Batch(cmds...)
			}

			a.input.Reset()
			a.state.InputText = ""
			a.applyComponentLayout(true)
			a.refreshCommandMenu()
			a.resetPasteHeuristics()

			if handled, cmd := a.handleImmediateSlashCommand(input); handled {
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, tea.Batch(cmds...)
			}

			switch strings.ToLower(input) {
			case slashCommandProvider:
				if err := a.refreshProviderPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				a.openProviderPicker()
				return a, tea.Batch(cmds...)
			case slashCommandModelPick:
				if err := a.refreshModelPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh models", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				a.openModelPicker()
				if cmd := a.requestModelCatalogRefresh(a.state.CurrentProvider); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, tea.Batch(cmds...)
			}

			if strings.HasPrefix(input, slashPrefix) {
				if isWorkspaceSlashCommand(input) {
					a.state.StatusText = statusApplyingCommand
					a.state.ExecutionError = ""
					cmds = append(cmds, runSessionWorkdirCommand(a.runtime, a.state.ActiveSessionID, a.state.CurrentWorkdir, input))
					return a, tea.Batch(cmds...)
				}
				a.state.StatusText = statusApplyingCommand
				cmds = append(cmds, runLocalCommand(a.configManager, a.providerSvc, a.currentStatusSnapshot(), input))
				return a, tea.Batch(cmds...)
			}
			if isWorkspaceCommandInput(input) {
				command, err := extractWorkspaceCommand(input)
				if err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("command", "Invalid workspace command", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				a.clearActivities()
				a.state.StatusText = statusRunningCommand
				a.state.ExecutionError = ""
				a.appendActivity("command", "Running command", command, false)
				cmds = append(cmds, runWorkspaceCommand(a.configManager, a.state.CurrentWorkdir, input))
				return a, tea.Batch(cmds...)
			}

			a.clearActivities()
			a.clearRunProgress()
			a.state.IsAgentRunning = true
			a.state.IsCompacting = false
			a.state.StreamingReply = false
			a.state.ExecutionError = ""
			a.state.StatusText = statusThinking
			a.state.CurrentTool = ""
			a.activeMessages = append(a.activeMessages, provider.Message{Role: roleUser, Content: input})
			a.rebuildTranscript()
			requestedWorkdir := ""
			if strings.TrimSpace(a.state.ActiveSessionID) == "" {
				requestedWorkdir = a.state.CurrentWorkdir
			}
			cmds = append(cmds, runAgent(a.runtime, a.state.ActiveSessionID, requestedWorkdir, input))
			return a, tea.Batch(cmds...)
		}
	}

	if key.Matches(typed, a.keys.Newline) {
		a.growComposerForNewline()
		msg = tea.KeyMsg{Type: tea.KeyEnter}
		effectiveTyped = tea.KeyMsg{Type: tea.KeyEnter}
	}

	before := a.input.Value()
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.state.InputText = a.input.Value()
	a.noteInputEdit(before, a.state.InputText, effectiveTyped, now)
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

func (a App) now() time.Time {
	if a.nowFn == nil {
		return time.Now()
	}
	return a.nowFn()
}

func (a *App) shouldTreatEnterAsNewline(typed tea.KeyMsg, now time.Time) bool {
	if !key.Matches(typed, a.keys.Send) || a.state.IsAgentRunning {
		return false
	}
	if typed.Paste {
		a.pasteMode = true
		a.lastPasteLikeAt = now
		return true
	}
	if a.pasteMode &&
		!a.lastPasteLikeAt.IsZero() &&
		!a.lastInputEditAt.IsZero() &&
		now.Sub(a.lastPasteLikeAt) <= pasteSessionGuard &&
		now.Sub(a.lastInputEditAt) <= pasteEnterGuard {
		return true
	}
	if a.pasteMode && !a.lastPasteLikeAt.IsZero() && now.Sub(a.lastPasteLikeAt) > pasteSessionGuard {
		a.pasteMode = false
	}
	if a.lastPasteLikeAt.IsZero() {
		return false
	}
	return now.Sub(a.lastPasteLikeAt) <= pasteEnterGuard
}

func (a *App) noteInputEdit(before string, after string, typed tea.KeyMsg, now time.Time) {
	if before == after {
		return
	}

	prevEditAt := a.lastInputEditAt
	a.lastInputEditAt = now

	if key.Matches(typed, a.keys.Newline) {
		a.inputBurstStart = time.Time{}
		a.inputBurstCount = 0
		return
	}

	pasteLike := typed.Paste

	switch typed.Type {
	case tea.KeyRunes:
		runeCount := len(typed.Runes)
		if runeCount > 1 {
			pasteLike = true
		}
		if strings.ContainsRune(string(typed.Runes), '\n') || strings.ContainsRune(string(typed.Runes), '\r') {
			pasteLike = true
		}
		if runeCount > 0 {
			if prevEditAt.IsZero() || now.Sub(prevEditAt) > pasteBurstWindow || a.inputBurstCount == 0 {
				a.inputBurstStart = now
				a.inputBurstCount = runeCount
			} else {
				a.inputBurstCount += runeCount
			}
			if a.inputBurstCount >= pasteBurstThreshold {
				pasteLike = true
			}
		}
	case tea.KeyEnter:
		if typed.Paste && strings.Count(after, "\n") > strings.Count(before, "\n") {
			pasteLike = true
		}
		a.inputBurstStart = time.Time{}
		a.inputBurstCount = 0
	default:
		a.inputBurstStart = time.Time{}
		a.inputBurstCount = 0
	}

	if pasteLike {
		a.lastPasteLikeAt = now
		a.pasteMode = true
	}
}

func (a *App) resetPasteHeuristics() {
	a.lastInputEditAt = time.Time{}
	a.lastPasteLikeAt = time.Time{}
	a.inputBurstStart = time.Time{}
	a.inputBurstCount = 0
	a.pasteMode = false
}

func (a App) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.FocusInput):
		a.closePicker()
		return a, nil
	case msg.String() == "enter":
		switch a.state.ActivePicker {
		case pickerProvider:
			item, ok := a.providerPicker.SelectedItem().(selectionItem)
			a.closePicker()
			if !ok {
				return a, nil
			}
			return a, runProviderSelection(a.providerSvc, item.name)
		case pickerModel:
			item, ok := a.modelPicker.SelectedItem().(selectionItem)
			a.closePicker()
			if !ok {
				return a, nil
			}
			return a, runModelSelection(a.providerSvc, item.id)
		}
	}

	var cmd tea.Cmd
	switch a.state.ActivePicker {
	case pickerProvider:
		a.providerPicker, cmd = a.providerPicker.Update(msg)
	case pickerModel:
		a.modelPicker, cmd = a.modelPicker.Update(msg)
	case pickerFile:
		a.fileBrowser, cmd = a.fileBrowser.Update(msg)
		if didSelect, path := a.fileBrowser.DidSelectFile(msg); didSelect {
			a.closePicker()
			if err := a.applyFileReference(path); err != nil {
				a.state.ExecutionError = err.Error()
				a.state.StatusText = err.Error()
				a.appendActivity("workspace", "Failed to apply file reference", err.Error(), true)
				return a, cmd
			}
			return a, cmd
		}
		if disabled, path := a.fileBrowser.DidSelectDisabledFile(msg); disabled {
			a.state.StatusText = fmt.Sprintf("[System] %s is not selectable.", filepath.Base(path))
		}
	}
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
		a.clearActivities()
		return nil
	}

	session, err := a.runtime.LoadSession(context.Background(), a.state.ActiveSessionID)
	if err != nil {
		return err
	}

	a.activeMessages = session.Messages
	a.clearActivities()
	a.state.ActiveSessionTitle = session.Title
	a.state.CurrentWorkdir = selectSessionWorkdir(session.Workdir, a.configManager.Get().Workdir)
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
	if strings.TrimSpace(a.state.CurrentWorkdir) == "" {
		a.state.CurrentWorkdir = cfg.Workdir
	}
}

func (a *App) handleRuntimeEvent(event agentruntime.RuntimeEvent) bool {
	if a.state.ActiveSessionID == "" {
		a.state.ActiveSessionID = event.SessionID
	}

	transcriptDirty := false

	switch event.Type {
	case agentruntime.EventUserMessage:
		a.state.StatusText = statusThinking
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.state.ExecutionError = ""
		a.setRunProgress(0.15, "Queued")
	case agentruntime.EventToolCallThinking:
		if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
			a.state.CurrentTool = payload
			a.setRunProgress(0.35, "Planning")
			a.appendActivity("tool", "Planning tool call", payload, false)
		}
	case agentruntime.EventToolStart:
		a.state.StatusText = statusRunningTool
		a.state.StreamingReply = false
		if payload, ok := event.Payload.(provider.ToolCall); ok {
			a.state.CurrentTool = payload.Name
			a.setRunProgress(0.6, "Running tool")
			a.appendActivity("tool", "Running tool", payload.Name, false)
		}
	case agentruntime.EventToolResult:
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.setRunProgress(0.8, "Integrating result")
		if payload, ok := event.Payload.(tools.ToolResult); ok {
			a.activeMessages = append(a.activeMessages, provider.Message{
				Role:    roleTool,
				Content: payload.Content,
				IsError: payload.IsError,
			})
			transcriptDirty = true
			if payload.IsError {
				a.state.ExecutionError = payload.Content
				a.state.StatusText = statusToolError
				a.appendActivity("tool", "Tool error", preview(payload.Content, 88, 4), true)
			} else if strings.TrimSpace(a.state.ExecutionError) == "" {
				a.state.StatusText = statusToolFinished
				a.appendActivity("tool", "Completed tool", payload.Name, false)
			}
		}
	case agentruntime.EventAgentChunk:
		if payload, ok := event.Payload.(string); ok {
			a.appendAssistantChunk(payload)
			if !a.runProgressKnown {
				a.setRunProgress(0.72, "Generating")
			}
			transcriptDirty = true
		}
	case agentruntime.EventToolChunk:
		if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
			a.state.StatusText = statusRunningTool
			a.appendActivity("tool", "Tool output", preview(payload, 88, 4), false)
		}
	case agentruntime.EventAgentDone:
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.pendingPermission = nil
		a.clearRunProgress()
		if strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.StatusText = statusReady
		}
		if payload, ok := event.Payload.(provider.Message); ok && strings.TrimSpace(payload.Content) != "" && !a.lastAssistantMatches(payload.Content) {
			a.activeMessages = append(a.activeMessages, provider.Message{Role: roleAssistant, Content: payload.Content})
			transcriptDirty = true
		}
	case agentruntime.EventRunCanceled:
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.pendingPermission = nil
		a.state.ExecutionError = ""
		a.state.StatusText = statusCanceled
		a.clearRunProgress()
		a.appendActivity("run", "Canceled current run", "", false)
	case agentruntime.EventError:
		a.state.StatusText = statusError
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.pendingPermission = nil
		a.clearRunProgress()
		if payload, ok := event.Payload.(string); ok {
			a.state.ExecutionError = payload
			a.state.StatusText = payload
			a.appendActivity("run", "Runtime error", payload, true)
		}
	case agentruntime.EventProviderRetry:
		if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
			a.state.StatusText = statusThinking
			a.runProgressKnown = false
			a.appendActivity("provider", "Retrying provider call", payload, false)
		}
	case agentruntime.EventPermissionRequest:
		payload, ok := event.Payload.(agentruntime.PermissionRequestPayload)
		if !ok {
			return transcriptDirty
		}
		a.pendingPermission = &pendingPermissionPrompt{
			RequestID:    strings.TrimSpace(payload.RequestID),
			ToolCallID:   strings.TrimSpace(payload.ToolCallID),
			ToolName:     strings.TrimSpace(payload.ToolName),
			ToolCategory: strings.TrimSpace(payload.ToolCategory),
			Target:       strings.TrimSpace(payload.Target),
		}
		prompt := fmt.Sprintf(
			"[Permission] Tool=%s Category=%s Target=%s | y=once, a=always(session), n=reject(session)",
			fallback(payload.ToolName, "-"),
			fallback(payload.ToolCategory, "-"),
			fallback(payload.Target, "-"),
		)
		a.state.StatusText = "Permission required (y/a/n)"
		a.appendActivity("permission", "Awaiting permission decision", prompt, false)
		a.appendInlineMessage(roleSystem, prompt)
		transcriptDirty = true
	case agentruntime.EventPermissionResolved:
		payload, ok := event.Payload.(agentruntime.PermissionResolvedPayload)
		if !ok {
			return transcriptDirty
		}
		if a.pendingPermission != nil && strings.TrimSpace(a.pendingPermission.RequestID) != "" &&
			strings.EqualFold(a.pendingPermission.RequestID, strings.TrimSpace(payload.RequestID)) {
			a.pendingPermission = nil
		}
		resolved := fmt.Sprintf(
			"[Permission] %s %s (%s)",
			fallback(payload.ToolName, "-"),
			fallback(payload.ResolvedAs, "-"),
			fallback(payload.RememberScope, "-"),
		)
		a.appendActivity("permission", "Permission resolved", resolved, strings.EqualFold(payload.ResolvedAs, "rejected"))
		if strings.EqualFold(payload.ResolvedAs, "approved") {
			a.state.StatusText = "Permission approved, executing tool..."
		}
	case agentruntime.EventCompactDone:
		payload, ok := event.Payload.(agentruntime.CompactDonePayload)
		if !ok {
			return transcriptDirty
		}
		a.state.ExecutionError = ""
		a.state.StatusText = fmt.Sprintf("Compact(%s) saved %.1f%% context", payload.TriggerMode, payload.SavedRatio*100)
		a.appendInlineMessage(
			roleSystem,
			fmt.Sprintf(
				"[System] Compact(%s) %s (before=%d, after=%d, saved=%.1f%%, transcript=%s)",
				payload.TriggerMode,
				map[bool]string{true: "applied", false: "checked"}[payload.Applied],
				payload.BeforeChars,
				payload.AfterChars,
				payload.SavedRatio*100,
				payload.TranscriptPath,
			),
		)
		transcriptDirty = true
	case agentruntime.EventCompactError:
		payload, ok := event.Payload.(agentruntime.CompactErrorPayload)
		if !ok {
			return transcriptDirty
		}
		message := fmt.Sprintf("Compact(%s) failed: %s", payload.TriggerMode, payload.Message)
		a.state.ExecutionError = message
		a.state.StatusText = message
		a.appendInlineMessage(roleError, message)
		transcriptDirty = true
	}

	return transcriptDirty
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

func (a *App) appendActivity(kind string, title string, detail string, isError bool) {
	previousCount := len(a.activities)
	title = strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	if title == "" && detail == "" {
		return
	}
	if title == "" {
		title = detail
		detail = ""
	}

	a.activities = append(a.activities, activityEntry{
		Time:    time.Now(),
		Kind:    strings.TrimSpace(kind),
		Title:   title,
		Detail:  detail,
		IsError: isError,
	})
	if len(a.activities) > maxActivityEntries {
		a.activities = append([]activityEntry(nil), a.activities[len(a.activities)-maxActivityEntries:]...)
	}
	a.syncActivityViewport(previousCount)
}

func (a *App) clearActivities() {
	previousCount := len(a.activities)
	if previousCount == 0 {
		return
	}
	a.activities = nil
	a.syncActivityViewport(previousCount)
}

func (a *App) syncActivityViewport(previousCount int) {
	visibleBefore := previousCount > 0
	visibleNow := len(a.activities) > 0
	if visibleBefore != visibleNow {
		a.applyComponentLayout(true)
	}
	a.rebuildActivity()
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
		vp.ViewUp()
	case key.Matches(msg, a.keys.PageDown):
		vp.ViewDown()
	case key.Matches(msg, a.keys.Top):
		vp.GotoTop()
	case key.Matches(msg, a.keys.Bottom):
		vp.GotoBottom()
	}
}

func (a *App) handleTranscriptMouse(msg tea.MouseMsg) bool {
	switch {
	case msg.Button == tea.MouseButtonWheelUp && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelUp):
		if !a.isMouseWithinTranscript(msg) {
			return false
		}
		a.transcript.LineUp(mouseWheelStepLines)
		return true
	case msg.Button == tea.MouseButtonWheelDown && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelDown):
		if !a.isMouseWithinTranscript(msg) {
			return false
		}
		a.transcript.LineDown(mouseWheelStepLines)
		return true
	}

	if !a.isMouseWithinTranscript(msg) {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			a.pendingCopyID = 0
		}
		return false
	}

	switch {
	case msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion:
		return false
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if buttonID, ok := a.copyButtonIDAtMouse(msg); ok {
			a.pendingCopyID = buttonID
			return true
		}
		a.pendingCopyID = 0
		return false
	case msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease:
		defer func() { a.pendingCopyID = 0 }()

		buttonID, ok := a.copyButtonIDAtMouse(msg)
		if !ok {
			return false
		}

		if a.pendingCopyID != 0 && a.pendingCopyID != buttonID {
			return false
		}
		return a.copyCodeBlockByID(buttonID)
	default:
		return false
	}
}

func (a App) isMouseWithinTranscript(msg tea.MouseMsg) bool {
	x, y, width, height := a.transcriptBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) transcriptBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := lipgloss.Height(a.renderHeader(lay.contentWidth))
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY
	if lay.stacked {
		streamY += lay.sidebarHeight
	} else {
		streamX += lay.sidebarWidth + lay.bodyGap
	}

	return streamX, streamY, lay.rightWidth, a.transcript.Height
}

func (a App) isMouseWithinInput(msg tea.MouseMsg) bool {
	x, y, width, height := a.inputBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) inputBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := lipgloss.Height(a.renderHeader(lay.contentWidth))
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY
	if lay.stacked {
		streamY += lay.sidebarHeight
	} else {
		streamX += lay.sidebarWidth + lay.bodyGap
	}

	inputY := streamY + a.transcript.Height + a.activityPreviewHeight() + a.commandMenuHeight(max(24, lay.rightWidth))
	inputHeight := lipgloss.Height(a.renderPrompt(max(24, lay.rightWidth)))
	return streamX, inputY, lay.rightWidth, inputHeight
}

func (a App) activityBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := lipgloss.Height(a.renderHeader(lay.contentWidth))
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY
	if lay.stacked {
		streamY += lay.sidebarHeight
	} else {
		streamX += lay.sidebarWidth + lay.bodyGap
	}

	activityHeight := a.activityPreviewHeight()
	if activityHeight <= 0 {
		return streamX, streamY + a.transcript.Height, lay.rightWidth, 0
	}
	return streamX, streamY + a.transcript.Height, lay.rightWidth, activityHeight
}

func (a App) isMouseWithinActivity(msg tea.MouseMsg) bool {
	x, y, width, height := a.activityBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a *App) handleActivityMouse(msg tea.MouseMsg) bool {
	if len(a.activities) == 0 || !a.isMouseWithinActivity(msg) {
		return false
	}
	if a.state.ActivePicker != pickerNone {
		return false
	}

	switch {
	case msg.Button == tea.MouseButtonWheelUp && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelUp):
		if a.focus != panelActivity {
			a.focus = panelActivity
			a.applyFocus()
		}
		a.activity.LineUp(mouseWheelStepLines)
		return true
	case msg.Button == tea.MouseButtonWheelDown && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelDown):
		if a.focus != panelActivity {
			a.focus = panelActivity
			a.applyFocus()
		}
		a.activity.LineDown(mouseWheelStepLines)
		return true
	default:
		return false
	}
}

func (a *App) handleInputMouse(msg tea.MouseMsg) bool {
	if !a.isMouseWithinInput(msg) {
		return false
	}
	if a.state.ActivePicker != pickerNone {
		return false
	}

	switch {
	case msg.Button == tea.MouseButtonWheelUp && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelUp):
		a.scrollInputPage(-1)
		return true
	case msg.Button == tea.MouseButtonWheelDown && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelDown):
		a.scrollInputPage(1)
		return true
	default:
		return false
	}
}

func (a *App) scrollInputPage(direction int) {
	if direction == 0 {
		return
	}
	if a.focus != panelInput {
		a.focus = panelInput
		a.applyFocus()
	}

	step := max(1, a.input.Height()-1)
	keyType := tea.KeyUp
	if direction > 0 {
		keyType = tea.KeyDown
	}

	for i := 0; i < step; i++ {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(tea.KeyMsg{Type: keyType})
		_ = cmd
	}
	a.state.InputText = a.input.Value()
}

func (a App) shouldHandleTabAsInput(typed tea.KeyMsg) bool {
	if a.focus != panelInput || a.state.ActivePicker != pickerNone || typed.Type != tea.KeyTab {
		return false
	}
	if typed.Paste || a.pasteMode {
		return true
	}
	return strings.TrimSpace(a.input.Value()) != ""
}

func (a *App) focusNext() {
	order := []panel{panelSessions, panelTranscript, panelActivity, panelInput}
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
	order := []panel{panelSessions, panelTranscript, panelActivity, panelInput}
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
	if a.focus == panelInput && a.state.ActivePicker == pickerNone {
		a.input.Focus()
		return
	}
	a.input.Blur()
}

func (a *App) applyComponentLayout(rebuildTranscript bool) {
	lay := a.computeLayout()
	prevTranscriptWidth := a.transcript.Width
	prevActivityWidth := a.activity.Width
	prevActivityHeight := a.activity.Height
	a.help.ShowAll = a.state.ShowHelp
	sidebarFrameWidth := a.styles.panelFocused.GetHorizontalFrameSize()
	sidebarFrameHeight := a.styles.panelFocused.GetVerticalFrameSize()
	sidebarBodyWidth := max(14, lay.sidebarWidth-sidebarFrameWidth)
	sidebarBodyHeight := max(4, lay.sidebarHeight-sidebarFrameHeight-lipgloss.Height(a.renderSidebarHeader(sidebarBodyWidth)))
	a.sessions.SetSize(sidebarBodyWidth, sidebarBodyHeight)
	a.transcript.Width = max(24, lay.rightWidth)
	a.resizeCommandMenu()
	menuHeight := a.commandMenuHeight(max(24, lay.rightWidth))
	activityHeight := a.activityPreviewHeight()
	a.input.SetWidth(a.composerInnerWidth(lay.rightWidth))
	a.input.SetHeight(a.composerHeight())
	promptHeight := lipgloss.Height(a.renderPrompt(a.transcript.Width))
	a.transcript.Height = max(6, lay.rightHeight-activityHeight-menuHeight-promptHeight)

	if activityHeight > 0 {
		panelStyle := a.styles.panelFocused
		frameHeight := panelStyle.GetVerticalFrameSize()
		borderWidth := 2
		paddingWidth := panelStyle.GetHorizontalFrameSize() - borderWidth
		panelWidth := max(1, lay.rightWidth-borderWidth)
		bodyWidth := max(10, panelWidth-paddingWidth)
		bodyHeight := max(1, activityHeight-frameHeight-1)
		a.activity.Width = bodyWidth
		a.activity.Height = bodyHeight
	} else {
		a.activity.Width = max(10, lay.rightWidth-4)
		a.activity.Height = 0
	}

	a.providerPicker.SetSize(max(24, clamp(lay.rightWidth-14, 28, 52)), max(4, clamp(lay.rightHeight-10, 6, 10)))
	a.modelPicker.SetSize(max(24, clamp(lay.rightWidth-14, 28, 52)), max(4, clamp(lay.rightHeight-10, 6, 10)))
	a.fileBrowser.SetHeight(max(6, clamp(lay.rightHeight-8, 8, 16)))
	if rebuildTranscript || prevTranscriptWidth != a.transcript.Width {
		a.rebuildTranscript()
	} else if a.transcript.AtBottom() || a.isBusy() {
		a.transcript.GotoBottom()
	}
	if prevActivityWidth != a.activity.Width || prevActivityHeight != a.activity.Height {
		a.rebuildActivity()
	}
}

func (a App) composerBoxWidth(totalWidth int) int {
	return max(8, totalWidth-2)
}

func (a App) composerInnerWidth(totalWidth int) int {
	return max(4, a.composerBoxWidth(totalWidth)-a.styles.inputBoxFocused.GetHorizontalFrameSize())
}

func (a App) composerHeight() int {
	return clamp(a.input.LineCount(), composerMinHeight, composerMaxHeight)
}

func (a *App) growComposerForNewline() {
	nextHeight := clamp(a.input.LineCount()+1, composerMinHeight, composerMaxHeight)
	if nextHeight > a.input.Height() {
		a.input.SetHeight(nextHeight)
	}
}

func (a *App) normalizeComposerHeight() {
	targetHeight := clamp(a.input.LineCount(), composerMinHeight, composerMaxHeight)
	if targetHeight != a.input.Height() {
		a.input.SetHeight(targetHeight)
	}
}

func (a *App) rebuildTranscript() {
	width := max(24, a.transcript.Width)
	if len(a.activeMessages) == 0 {
		a.setCodeCopyBlocks(nil)
		a.transcript.SetContent(a.styles.empty.Width(width).Render(emptyConversationText))
		a.transcript.GotoTop()
		return
	}

	atBottom := a.transcript.AtBottom()
	blocks := make([]string, 0, len(a.activeMessages))
	copyButtons := make([]copyCodeButtonBinding, 0, 4)
	nextCopyID := 1
	for _, message := range a.activeMessages {
		rendered, bindings := a.renderMessageBlockWithCopy(message, width, nextCopyID)
		blocks = append(blocks, rendered)
		copyButtons = append(copyButtons, bindings...)
		nextCopyID += len(bindings)
	}
	a.setCodeCopyBlocks(copyButtons)

	a.transcript.SetContent(strings.Join(blocks, "\n\n"))
	if atBottom || a.state.IsAgentRunning {
		a.transcript.GotoBottom()
	}
}

func (a *App) rebuildActivity() {
	if len(a.activities) == 0 || a.activity.Height <= 0 {
		a.activity.SetContent("")
		a.activity.GotoTop()
		return
	}

	atBottom := a.activity.AtBottom()
	width := max(12, a.activity.Width)
	lines := make([]string, 0, len(a.activities))
	for _, entry := range a.activities {
		lines = append(lines, a.renderActivityLine(entry, width))
	}
	a.activity.SetContent(strings.Join(lines, "\n"))
	if atBottom || a.focus != panelActivity {
		a.activity.GotoBottom()
	}
}

func (a *App) setRunProgress(value float64, label string) {
	a.runProgressKnown = true
	switch {
	case value < 0:
		a.runProgressValue = 0
	case value > 1:
		a.runProgressValue = 1
	default:
		a.runProgressValue = value
	}
	a.runProgressLabel = strings.TrimSpace(label)
}

func (a *App) clearRunProgress() {
	a.runProgressKnown = false
	a.runProgressValue = 0
	a.runProgressLabel = ""
}

func (a *App) handleImmediateSlashCommand(input string) (bool, tea.Cmd) {
	command, rest := splitFirstWord(strings.ToLower(strings.TrimSpace(input)))
	switch command {
	case slashCommandExit:
		return true, tea.Quit
	case slashCommandClear:
		a.startDraftSession()
		a.state.StatusText = "[System] Cleared current draft/history."
		return true, nil
	case slashCommandCompact:
		if strings.TrimSpace(rest) != "" {
			errText := fmt.Sprintf("usage: %s", slashUsageCompact)
			a.state.ExecutionError = errText
			a.state.StatusText = errText
			a.appendInlineMessage(roleError, errText)
			a.rebuildTranscript()
			return true, nil
		}
		if strings.TrimSpace(a.state.ActiveSessionID) == "" {
			errText := "compact requires an existing session"
			a.state.ExecutionError = errText
			a.state.StatusText = errText
			a.appendInlineMessage(roleError, errText)
			a.rebuildTranscript()
			return true, nil
		}
		if a.isBusy() {
			errText := "compact is already running, please wait"
			a.state.ExecutionError = errText
			a.state.StatusText = errText
			a.appendInlineMessage(roleError, errText)
			a.rebuildTranscript()
			return true, nil
		}
		a.state.IsCompacting = true
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.state.StatusText = statusCompacting
		a.state.ExecutionError = ""
		return true, runCompact(a.runtime, a.state.ActiveSessionID)
	default:
		return false, nil
	}
}

func (a App) currentStatusSnapshot() statusSnapshot {
	picker := "none"
	switch a.state.ActivePicker {
	case pickerProvider:
		picker = "provider"
	case pickerModel:
		picker = "model"
	case pickerFile:
		picker = "file"
	}

	return statusSnapshot{
		ActiveSessionID:    a.state.ActiveSessionID,
		ActiveSessionTitle: a.state.ActiveSessionTitle,
		IsAgentRunning:     a.state.IsAgentRunning,
		IsCompacting:       a.state.IsCompacting,
		CurrentProvider:    a.state.CurrentProvider,
		CurrentModel:       a.state.CurrentModel,
		CurrentWorkdir:     a.state.CurrentWorkdir,
		CurrentTool:        a.state.CurrentTool,
		ExecutionError:     a.state.ExecutionError,
		FocusLabel:         a.focusLabel(),
		PickerLabel:        picker,
		MessageCount:       len(a.activeMessages),
	}
}

func (a *App) startDraftSession() {
	a.state.ActiveSessionID = ""
	a.state.ActiveSessionTitle = draftSessionTitle
	a.activeMessages = nil
	a.clearActivities()
	a.state.IsCompacting = false
	a.state.StatusText = statusDraft
	a.state.ExecutionError = ""
	a.state.CurrentTool = ""
	a.clearRunProgress()
	a.input.Reset()
	a.state.InputText = ""
	a.state.CurrentWorkdir = a.configManager.Get().Workdir
	if err := a.refreshFileCandidates(); err != nil {
		a.state.ExecutionError = err.Error()
		a.appendActivity("workspace", "Failed to refresh workspace files", err.Error(), true)
	}
	a.focus = panelInput
	a.applyFocus()
	a.applyComponentLayout(true)
	a.rebuildTranscript()
}

func (a *App) requestModelCatalogRefresh(providerID string) tea.Cmd {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" || strings.EqualFold(a.modelRefreshID, providerID) {
		return nil
	}

	a.modelRefreshID = providerID
	return runModelCatalogRefresh(a.providerSvc, providerID)
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

// handlePermissionDecisionKey 处理待审批状态下的快捷授权键位。
func (a *App) handlePermissionDecisionKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if a.pendingPermission == nil {
		return nil, false
	}

	keyText := strings.ToLower(strings.TrimSpace(msg.String()))
	var decision agentruntime.PermissionResolutionDecision
	switch keyText {
	case "y":
		decision = agentruntime.PermissionResolutionAllowOnce
	case "a":
		decision = agentruntime.PermissionResolutionAllowSession
	case "n":
		decision = agentruntime.PermissionResolutionReject
	default:
		return nil, false
	}
	if a.pendingPermission.Submitted {
		a.state.StatusText = "Permission decision already submitted, waiting runtime..."
		return nil, true
	}

	requestID := strings.TrimSpace(a.pendingPermission.RequestID)
	a.pendingPermission.Submitted = true
	a.state.StatusText = "Submitting permission decision..."
	a.state.ExecutionError = ""
	return runResolvePermission(a.runtime, requestID, decision), true
}

func runAgent(runtime agentruntime.Runtime, sessionID string, workdir string, content string) tea.Cmd {
	return func() tea.Msg {
		err := runtime.Run(context.Background(), agentruntime.UserInput{
			SessionID: sessionID,
			Content:   content,
			Workdir:   workdir,
		})
		return runFinishedMsg{err: err}
	}
}

// runResolvePermission 在独立命令中提交权限审批决定，避免阻塞 UI 事件循环。
func runResolvePermission(
	runtime agentruntime.Runtime,
	requestID string,
	decision agentruntime.PermissionResolutionDecision,
) tea.Cmd {
	return func() tea.Msg {
		err := runtime.ResolvePermission(context.Background(), agentruntime.PermissionResolutionInput{
			RequestID: requestID,
			Decision:  decision,
		})
		return permissionResolveResultMsg{
			requestID: requestID,
			decision:  decision,
			err:       err,
		}
	}
}

func runSessionWorkdirCommand(
	runtime agentruntime.Runtime,
	sessionID string,
	currentWorkdir string,
	raw string,
) tea.Cmd {
	return func() tea.Msg {
		requested, err := parseWorkspaceSlashCommand(raw)
		if err != nil {
			return sessionWorkdirResultMsg{err: err}
		}
		if strings.TrimSpace(requested) == "" {
			workdir := strings.TrimSpace(currentWorkdir)
			if workdir == "" {
				return sessionWorkdirResultMsg{err: fmt.Errorf("usage: /cwd <path>")}
			}
			return sessionWorkdirResultMsg{
				notice:  fmt.Sprintf("[System] Current workspace is %s.", workdir),
				workdir: workdir,
			}
		}

		if strings.TrimSpace(sessionID) == "" {
			workdir, err := resolveWorkspacePath(currentWorkdir, requested)
			if err != nil {
				return sessionWorkdirResultMsg{err: err}
			}
			return sessionWorkdirResultMsg{
				notice:  fmt.Sprintf("[System] Draft workspace switched to %s.", workdir),
				workdir: workdir,
			}
		}

		session, err := runtime.SetSessionWorkdir(context.Background(), sessionID, requested)
		if err != nil {
			return sessionWorkdirResultMsg{err: err}
		}
		workdir := strings.TrimSpace(session.Workdir)
		if workdir == "" {
			workdir = strings.TrimSpace(currentWorkdir)
		}
		return sessionWorkdirResultMsg{
			notice:  fmt.Sprintf("[System] Session workspace switched to %s.", workdir),
			workdir: workdir,
		}
	}
}

func resolveWorkspacePath(base string, requested string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace: resolve current directory: %w", err)
		}
		base = workingDir
	}

	target := strings.TrimSpace(requested)
	if target == "" {
		target = "."
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}

	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace: %q is not a directory", absolute)
	}
	return filepath.Clean(absolute), nil
}

func selectSessionWorkdir(sessionWorkdir string, defaultWorkdir string) string {
	workdir := strings.TrimSpace(sessionWorkdir)
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(defaultWorkdir)
}

// runCompact 在独立命令中触发 runtime compact，并把结果回传给 TUI。
func runCompact(runtime agentruntime.Runtime, sessionID string) tea.Cmd {
	return func() tea.Msg {
		_, err := runtime.Compact(context.Background(), agentruntime.CompactInput{SessionID: sessionID})
		return compactFinishedMsg{err: err}
	}
}

// isBusy 统一判断当前界面是否存在进行中的 agent 或 compact 操作。
func (a App) isBusy() bool {
	return a.state.IsAgentRunning || a.state.IsCompacting
}
