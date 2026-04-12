package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	"neo-code/internal/memo"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuistatus "neo-code/internal/tui/core/status"
	tuiutils "neo-code/internal/tui/core/utils"
	tuiservices "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

const (
	composerMinHeight   = tuistate.ComposerMinHeight
	composerMaxHeight   = tuistate.ComposerMaxHeight
	composerPromptWidth = tuistate.ComposerPromptWidth
	mouseWheelStepLines = tuistate.MouseWheelStepLines
	pasteBurstWindow    = tuistate.PasteBurstWindow
	pasteEnterGuard     = tuistate.PasteEnterGuard
	pasteSessionGuard   = tuistate.PasteSessionGuard
	pasteBurstThreshold = tuistate.PasteBurstThreshold
)

var panelOrder = []panel{panelSessions, panelTranscript, panelActivity, panelInput}

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
		if a.deferredEventCmd != nil {
			cmds = append(cmds, a.deferredEventCmd)
			a.deferredEventCmd = nil
		}
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		if transcriptDirty {
			a.rebuildTranscript()
		}
		cmds = append(cmds, ListenForRuntimeEvent(a.runtime.Events()))
		return a, tea.Batch(cmds...)
	case RuntimeClosedMsg:
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.state.ActiveRunID = ""
		a.pendingPermission = nil
		a.clearRunProgress()
		a.state.IsCompacting = false
		if strings.TrimSpace(a.state.StatusText) == "" {
			a.state.StatusText = statusRuntimeClosed
		}
		return a, tea.Batch(cmds...)
	case runFinishedMsg:
		if typed.Err != nil {
			a.state.IsAgentRunning = false
			a.state.ActiveRunID = ""
			a.pendingPermission = nil
			a.clearRunProgress()
			a.state.StreamingReply = false
			a.state.CurrentTool = ""
			if errors.Is(typed.Err, context.Canceled) {
				a.state.ExecutionError = ""
				a.state.StatusText = statusCanceled
			} else {
				a.state.ExecutionError = typed.Err.Error()
				a.state.StatusText = typed.Err.Error()
			}
		}
		if !a.state.IsAgentRunning {
			a.clearRunProgress()
		}
		_ = a.refreshSessions()
		a.syncActiveSessionTitle()
		return a, tea.Batch(cmds...)
	case permissionResolutionFinishedMsg:
		if a.pendingPermission != nil && a.pendingPermission.Request.RequestID == typed.RequestID {
			if typed.Err != nil {
				a.pendingPermission.Submitting = false
				a.state.ExecutionError = typed.Err.Error()
				a.state.StatusText = typed.Err.Error()
				a.appendActivity("permission", "Permission decision submit failed", typed.Err.Error(), true)
			} else {
				a.pendingPermission = nil
				a.state.ExecutionError = ""
				a.state.StatusText = statusPermissionSubmitted
				a.appendActivity("permission", "Permission decision submitted", string(typed.Decision), false)
				a.refreshPermissionPromptLayout()
			}
		}
		return a, tea.Batch(cmds...)
	case modelCatalogRefreshMsg:
		if strings.EqualFold(a.modelRefreshID, typed.ProviderID) {
			a.modelRefreshID = ""
		}
		if !strings.EqualFold(strings.TrimSpace(a.state.CurrentProvider), strings.TrimSpace(typed.ProviderID)) {
			return a, tea.Batch(cmds...)
		}
		if typed.Err != nil {
			a.appendActivity("provider", "Failed to refresh models", typed.Err.Error(), true)
			return a, tea.Batch(cmds...)
		}

		replacePickerItems(&a.modelPicker, mapModelItems(typed.Models))
		cfg := a.configManager.Get()
		a.syncConfigState(cfg)
		selectPickerItemByID(&a.modelPicker, cfg.CurrentModel)
		return a, tea.Batch(cmds...)
	case compactFinishedMsg:
		a.state.IsCompacting = false
		if typed.Err != nil && strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = typed.Err.Error()
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
		if typed.Err != nil {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = typed.Err.Error()
			a.appendActivity("command", "Local command failed", typed.Err.Error(), true)
		} else {
			a.state.ExecutionError = ""
			a.state.StatusText = typed.Notice
			cfg := a.configManager.Get()
			a.syncConfigState(cfg)
			if typed.ProviderChanged {
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
			} else if typed.ModelChanged {
				a.selectCurrentModel(cfg.CurrentModel)
			}
			a.appendActivity("command", typed.Notice, "", false)
		}
		return a, tea.Batch(cmds...)
	case workspaceCommandResultMsg:
		if typed.Command == "" && typed.Err != nil {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = typed.Err.Error()
			a.appendActivity("command", "Workspace command failed", typed.Err.Error(), true)
			return a, tea.Batch(cmds...)
		}
		result := formatWorkspaceCommandResult(typed.Command, typed.Output, typed.Err)
		if typed.Err != nil {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = fmt.Sprintf("Command failed: %s", typed.Command)
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

	if a.pendingPermission != nil {
		if cmd, handled := a.updatePendingPermissionInput(typed); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
	}

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

			// 先检查是否是立即执行的命令，如果处理了，就直接返回
			if handled, cmd := a.handleImmediateSlashCommand(input); handled {
				a.input.Reset() // 只有在命令被处理后才清空输入
				a.state.InputText = ""
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, tea.Batch(cmds...)
			}

			// 如果不是立即执行的命令，再执行常规的输入重置
			a.input.Reset()
			a.state.InputText = ""
			a.applyComponentLayout(true)
			a.refreshCommandMenu()
			a.resetPasteHeuristics()

			switch strings.ToLower(input) {
			case slashCommandHelp:
				a.refreshHelpPicker()
				a.openHelpPicker()
				return a, tea.Batch(cmds...)
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
			a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleUser, Content: input})
			a.rebuildTranscript()
			runID := fmt.Sprintf("run-%d", a.now().UnixNano())
			a.state.ActiveRunID = runID
			requestedWorkdir := tuiutils.RequestedWorkdirForRun(a.state.CurrentWorkdir)
			cmds = append(cmds, runAgent(a.runtime, runID, a.state.ActiveSessionID, requestedWorkdir, input))
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

// updatePendingPermissionInput 处理权限审批面板上的键盘交互（上下选择与回车确认）。
func (a *App) updatePendingPermissionInput(typed tea.KeyMsg) (tea.Cmd, bool) {
	if a.pendingPermission == nil {
		return nil, false
	}
	if a.pendingPermission.Submitting {
		return nil, true
	}

	switch {
	case key.Matches(typed, a.keys.ScrollUp):
		a.pendingPermission.Selected = normalizePermissionPromptSelection(a.pendingPermission.Selected - 1)
		a.state.StatusText = statusPermissionRequired
		return nil, true
	case key.Matches(typed, a.keys.ScrollDown):
		a.pendingPermission.Selected = normalizePermissionPromptSelection(a.pendingPermission.Selected + 1)
		a.state.StatusText = statusPermissionRequired
		return nil, true
	case key.Matches(typed, a.keys.Send):
		option := permissionPromptOptionAt(a.pendingPermission.Selected)
		return a.submitPermissionDecision(option.Decision), true
	}

	if typed.Type == tea.KeyRunes && len(typed.Runes) > 0 {
		if decision, ok := parsePermissionShortcut(string(typed.Runes)); ok {
			return a.submitPermissionDecision(decision), true
		}
	}
	return nil, true
}

// submitPermissionDecision 触发一次权限审批提交命令。
func (a *App) submitPermissionDecision(decision agentruntime.PermissionResolutionDecision) tea.Cmd {
	if a.pendingPermission == nil {
		return nil
	}

	requestID := strings.TrimSpace(a.pendingPermission.Request.RequestID)
	if requestID == "" {
		return nil
	}

	a.pendingPermission.Submitting = true
	a.state.StatusText = statusPermissionSubmitting
	a.appendActivity("permission", "Submitting permission decision", string(decision), false)

	return runResolvePermission(a.runtime, requestID, decision)
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
		case pickerHelp:
			item, ok := a.helpPicker.SelectedItem().(selectionItem)
			a.closePicker()
			if !ok {
				return a, nil
			}
			return a, a.runSlashCommandSelection(item.id)
		}
	}

	var cmd tea.Cmd
	switch a.state.ActivePicker {
	case pickerProvider:
		a.providerPicker, cmd = a.providerPicker.Update(msg)
	case pickerModel:
		a.modelPicker, cmd = a.modelPicker.Update(msg)
	case pickerHelp:
		a.helpPicker, cmd = a.helpPicker.Update(msg)
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
	a.setCurrentWorkdir(agentsession.EffectiveWorkdir(session.Workdir, a.configManager.Get().Workdir))
	a.refreshRuntimeSourceSnapshot()
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
		a.setCurrentWorkdir(cfg.Workdir)
	}
}

// refreshRuntimeSourceSnapshot 浠?runtime 鏌ヨ context/token/tool 蹇収锛岀敤浜庝細璇濆垏鎹㈡垨鎭㈠鏃跺洖濉?UI銆
func (a *App) refreshRuntimeSourceSnapshot() {
	sessionID := strings.TrimSpace(a.state.ActiveSessionID)
	if sessionID != "" {
		if source, ok := a.runtime.(runtimeSessionContextSource); ok {
			raw, err := source.GetSessionContext(context.Background(), sessionID)
			if err == nil {
				contextSnapshot, parsed := tuiservices.ParseSessionContextSnapshot(raw)
				if parsed {
					mapped := tuiservices.MapSessionContextSnapshot(contextSnapshot)
					a.state.RunContext.Provider = mapped.Provider
					a.state.RunContext.Model = mapped.Model
					a.state.RunContext.Workdir = mapped.Workdir
					a.state.RunContext.Mode = mapped.Mode
					a.state.RunContext.SessionID = mapped.SessionID
				}
			}
		}
		if source, ok := a.runtime.(runtimeSessionUsageSource); ok {
			raw, err := source.GetSessionUsage(context.Background(), sessionID)
			if err == nil {
				usageSnapshot, parsed := tuiservices.ParseUsageSnapshot(raw)
				if parsed {
					a.state.TokenUsage = tuiservices.MapUsageSnapshot(usageSnapshot, a.state.TokenUsage)
				}
			}
		}
	}

	runID := strings.TrimSpace(a.state.ActiveRunID)
	if runID == "" {
		return
	}
	if source, ok := a.runtime.(runtimeRunSnapshotSource); ok {
		raw, err := source.GetRunSnapshot(context.Background(), runID)
		if err == nil {
			runSnapshot, parsed := tuiservices.ParseRunSnapshot(raw)
			if parsed {
				contextVM, toolVM, usageVM := tuiservices.MapRunSnapshot(runSnapshot)
				if strings.TrimSpace(contextVM.Provider) != "" {
					a.state.RunContext = contextVM
				}
				if len(toolVM) > 0 {
					a.state.ToolStates = append([]tuistate.ToolState(nil), toolVM...)
				}
				a.state.TokenUsage = usageVM
			}
		}
	}
}

// runtimeSessionContextSource 约束可选的会话上下文查询能力。
type runtimeSessionContextSource interface {
	GetSessionContext(ctx context.Context, sessionID string) (any, error)
}

// runtimeSessionUsageSource 约束可选的会话 token 使用量查询能力。
type runtimeSessionUsageSource interface {
	GetSessionUsage(ctx context.Context, sessionID string) (any, error)
}

// runtimeRunSnapshotSource 约束可选的运行快照查询能力。
type runtimeRunSnapshotSource interface {
	GetRunSnapshot(ctx context.Context, runID string) (any, error)
}

var runtimeEventHandlerRegistry = map[agentruntime.EventType]func(*App, agentruntime.RuntimeEvent) bool{
	agentruntime.EventUserMessage:                              runtimeEventUserMessageHandler,
	agentruntime.EventType(tuiservices.RuntimeEventRunContext): runtimeEventRunContextHandler,
	agentruntime.EventType(tuiservices.RuntimeEventToolStatus): runtimeEventToolStatusHandler,
	agentruntime.EventType(tuiservices.RuntimeEventUsage):      runtimeEventUsageHandler,
	agentruntime.EventToolCallThinking:                         runtimeEventToolCallThinkingHandler,
	agentruntime.EventToolStart:                                runtimeEventToolStartHandler,
	agentruntime.EventToolResult:                               runtimeEventToolResultHandler,
	agentruntime.EventAgentChunk:                               runtimeEventAgentChunkHandler,
	agentruntime.EventToolChunk:                                runtimeEventToolChunkHandler,
	agentruntime.EventAgentDone:                                runtimeEventAgentDoneHandler,
	agentruntime.EventRunCanceled:                              runtimeEventRunCanceledHandler,
	agentruntime.EventError:                                    runtimeEventErrorHandler,
	agentruntime.EventProviderRetry:                            runtimeEventProviderRetryHandler,
	agentruntime.EventPermissionRequest:                        runtimeEventPermissionRequestHandler,
	agentruntime.EventPermissionResolved:                       runtimeEventPermissionResolvedHandler,
	agentruntime.EventCompactDone:                              runtimeEventCompactDoneHandler,
	agentruntime.EventCompactError:                             runtimeEventCompactErrorHandler,
}

// handleRuntimeEvent 通过注册表分发 runtime 事件，避免巨型 switch 膨胀。
func (a *App) handleRuntimeEvent(event agentruntime.RuntimeEvent) bool {
	if a.state.ActiveSessionID == "" {
		a.state.ActiveSessionID = event.SessionID
	}
	handler, ok := runtimeEventHandlerRegistry[event.Type]
	if !ok {
		return false
	}
	return handler(a, event)
}

// runtimeEventUserMessageHandler 处理用户消息进入运行队列后的状态同步。
func runtimeEventUserMessageHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if strings.TrimSpace(event.RunID) != "" {
		a.state.ActiveRunID = strings.TrimSpace(event.RunID)
	}
	a.state.StatusText = statusThinking
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ExecutionError = ""
	a.setRunProgress(0.15, "Queued")
	return false
}

// runtimeEventRunContextHandler 处理 runtime 上下文事件并回填界面状态。
func runtimeEventRunContextHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseRunContextPayload(event.Payload)
	if !ok {
		return false
	}
	mapped := tuiservices.MapRunContextPayload(event.RunID, event.SessionID, payload)
	a.state.RunContext = mapped
	if strings.TrimSpace(mapped.RunID) != "" {
		a.state.ActiveRunID = mapped.RunID
	}
	if strings.TrimSpace(mapped.Provider) != "" {
		a.state.CurrentProvider = mapped.Provider
	}
	if strings.TrimSpace(mapped.Model) != "" {
		a.state.CurrentModel = mapped.Model
	}
	if strings.TrimSpace(mapped.Workdir) != "" {
		a.setCurrentWorkdir(mapped.Workdir)
	}
	return false
}

// runtimeEventToolStatusHandler 处理工具状态流转并更新当前工具展示。
func runtimeEventToolStatusHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseToolStatusPayload(event.Payload)
	if !ok {
		return false
	}
	toolVM := tuiservices.MapToolStatusPayload(payload)
	a.state.ToolStates = tuiservices.MergeToolStates(a.state.ToolStates, toolVM, 16)
	switch toolVM.Status {
	case tuistate.ToolLifecyclePlanned, tuistate.ToolLifecycleRunning:
		if strings.TrimSpace(toolVM.ToolName) != "" {
			a.state.CurrentTool = toolVM.ToolName
		}
	case tuistate.ToolLifecycleSucceeded, tuistate.ToolLifecycleFailed:
		a.state.CurrentTool = ""
	}
	return false
}

// runtimeEventUsageHandler 处理 token 使用量更新。
func runtimeEventUsageHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseUsagePayload(event.Payload)
	if !ok {
		return false
	}
	a.state.TokenUsage = tuiservices.MapUsagePayload(payload)
	return false
}

// runtimeEventToolCallThinkingHandler 处理工具规划阶段事件。
func runtimeEventToolCallThinkingHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.CurrentTool = payload
		a.setRunProgress(0.35, "Planning")
		a.appendActivity("tool", "Planning tool call", payload, false)
	}
	return false
}

// runtimeEventToolStartHandler 处理工具开始执行事件。
func runtimeEventToolStartHandler(a *App, event agentruntime.RuntimeEvent) bool {
	a.state.StatusText = statusRunningTool
	a.state.StreamingReply = false
	if payload, ok := event.Payload.(providertypes.ToolCall); ok {
		a.state.CurrentTool = payload.Name
		a.setRunProgress(0.6, "Running tool")
		a.appendActivity("tool", "Running tool", payload.Name, false)
	}
	return false
}

// runtimeEventToolResultHandler 处理工具执行结果并决定是否刷新对话区。
func runtimeEventToolResultHandler(a *App, event agentruntime.RuntimeEvent) bool {
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.setRunProgress(0.8, "Integrating result")
	payload, ok := event.Payload.(tools.ToolResult)
	if !ok {
		return false
	}
	a.activeMessages = append(a.activeMessages, providertypes.Message{
		Role:    roleTool,
		Content: payload.Content,
		IsError: payload.IsError,
	})
	if payload.IsError {
		a.state.ExecutionError = payload.Content
		a.state.StatusText = statusToolError
		a.appendActivity("tool", "Tool error", preview(payload.Content, 88, 4), true)
	} else if strings.TrimSpace(a.state.ExecutionError) == "" {
		a.state.StatusText = statusToolFinished
		a.appendActivity("tool", "Completed tool", payload.Name, false)
	}
	return true
}

// runtimeEventAgentChunkHandler 处理模型流式增量输出。
func runtimeEventAgentChunkHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(string)
	if !ok {
		return false
	}
	a.appendAssistantChunk(payload)
	if !a.runProgressKnown {
		a.setRunProgress(0.72, "Generating")
	}
	return true
}

// runtimeEventToolChunkHandler 处理工具流式输出片段。
func runtimeEventToolChunkHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.StatusText = statusRunningTool
		a.appendActivity("tool", "Tool output", preview(payload, 88, 4), false)
	}
	return false
}

// runtimeEventAgentDoneHandler 处理运行完成事件。
func runtimeEventAgentDoneHandler(a *App, event agentruntime.RuntimeEvent) bool {
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.clearRunProgress()
	if strings.TrimSpace(a.state.ExecutionError) == "" {
		a.state.StatusText = statusReady
	}
	if payload, ok := event.Payload.(providertypes.Message); ok && strings.TrimSpace(payload.Content) != "" && !a.lastAssistantMatches(payload.Content) {
		a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleAssistant, Content: payload.Content})
		return true
	}
	return false
}

// runtimeEventRunCanceledHandler 处理运行取消事件。
func runtimeEventRunCanceledHandler(a *App, event agentruntime.RuntimeEvent) bool {
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.state.ExecutionError = ""
	a.state.StatusText = statusCanceled
	a.clearRunProgress()
	a.appendActivity("run", "Canceled current run", "", false)
	return false
}

// runtimeEventErrorHandler 处理运行时错误事件。
func runtimeEventErrorHandler(a *App, event agentruntime.RuntimeEvent) bool {
	a.state.StatusText = statusError
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.clearRunProgress()
	if payload, ok := event.Payload.(string); ok {
		a.state.ExecutionError = payload
		a.state.StatusText = payload
		a.appendActivity("run", "Runtime error", payload, true)
	}
	return false
}

// runtimeEventProviderRetryHandler 处理 provider 重试提示事件。
func runtimeEventProviderRetryHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.StatusText = statusThinking
		a.runProgressKnown = false
		a.appendActivity("provider", "Retrying provider call", payload, false)
	}
	return false
}

// runtimeEventPermissionRequestHandler 处理 permission_request 事件并激活审批面板。
func runtimeEventPermissionRequestHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := parsePermissionRequestPayload(event.Payload)
	if !ok {
		return false
	}

	if a.pendingPermission != nil {
		currentRequestID := strings.TrimSpace(a.pendingPermission.Request.RequestID)
		nextRequestID := strings.TrimSpace(payload.RequestID)
		if currentRequestID != "" && currentRequestID != nextRequestID && !a.pendingPermission.Submitting {
			a.deferredEventCmd = runResolvePermission(a.runtime, currentRequestID, agentruntime.PermissionResolutionReject)
			a.appendActivity(
				"permission",
				"Auto-rejected superseded permission request",
				currentRequestID,
				false,
			)
		}
	}

	a.pendingPermission = &permissionPromptState{
		Request:    payload,
		Selected:   0,
		Submitting: false,
	}
	a.focus = panelInput
	a.applyFocus()
	a.state.StatusText = statusPermissionRequired
	a.state.ExecutionError = ""
	a.appendActivity(
		"permission",
		"Permission request",
		fmt.Sprintf("%s -> %s", fallbackText(payload.ToolName, "tool"), fallbackText(payload.Target, "(empty target)")),
		false,
	)
	a.refreshPermissionPromptLayout()
	return false
}

// runtimeEventPermissionResolvedHandler 处理 permission_resolved 事件并清理审批面板状态。
func runtimeEventPermissionResolvedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := parsePermissionResolvedPayload(event.Payload)
	if !ok {
		return false
	}

	if a.pendingPermission != nil && a.pendingPermission.Request.RequestID == payload.RequestID {
		a.pendingPermission = nil
	}
	a.state.StatusText = fmt.Sprintf("Permission %s", fallbackText(payload.ResolvedAs, "resolved"))
	a.appendActivity(
		"permission",
		"Permission resolved",
		fmt.Sprintf("%s (%s)", fallbackText(payload.Decision, "unknown"), fallbackText(payload.RememberScope, "once")),
		false,
	)
	a.refreshPermissionPromptLayout()
	return false
}

// refreshPermissionPromptLayout 在布局已初始化时刷新权限面板相关排版。
func (a *App) refreshPermissionPromptLayout() {
	if a.width <= 0 || a.height <= 0 {
		return
	}
	a.applyComponentLayout(false)
}

// runtimeEventCompactDoneHandler 处理 compact 完成事件。
func runtimeEventCompactDoneHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.CompactDonePayload)
	if !ok {
		return false
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
	return true
}

// runtimeEventCompactErrorHandler 处理 compact 异常事件。
func runtimeEventCompactErrorHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.CompactErrorPayload)
	if !ok {
		return false
	}
	message := fmt.Sprintf("Compact(%s) failed: %s", payload.TriggerMode, payload.Message)
	a.state.ExecutionError = message
	a.state.StatusText = message
	a.appendInlineMessage(roleError, message)
	return true
}
func (a *App) appendAssistantChunk(chunk string) {
	if chunk == "" {
		return
	}

	if !a.state.StreamingReply || len(a.activeMessages) == 0 || a.activeMessages[len(a.activeMessages)-1].Role != roleAssistant {
		a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleAssistant, Content: chunk})
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

	a.activeMessages = append(a.activeMessages, providertypes.Message{Role: role, Content: content})
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

	a.activities = append(a.activities, tuistate.ActivityEntry{
		Time:    time.Now(),
		Kind:    strings.TrimSpace(kind),
		Title:   title,
		Detail:  detail,
		IsError: isError,
	})
	if len(a.activities) > maxActivityEntries {
		a.activities = append([]tuistate.ActivityEntry(nil), a.activities[len(a.activities)-maxActivityEntries:]...)
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
	headerHeight := headerBarHeight
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
	headerHeight := headerBarHeight
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
	headerHeight := headerBarHeight
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
	order := panelOrder
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
	order := panelOrder
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
	a.input.SetWidth(a.composerInnerWidth(lay.rightWidth))
	a.input.SetHeight(a.composerHeight())
	transcriptHeight, activityHeight, _, _ := a.waterfallMetrics(a.transcript.Width, lay.rightHeight)
	a.transcript.Height = transcriptHeight

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

	a.providerPicker.SetSize(max(24, tuiutils.Clamp(lay.rightWidth-14, 28, 52)), max(4, tuiutils.Clamp(lay.rightHeight-10, 6, 10)))
	a.modelPicker.SetSize(max(24, tuiutils.Clamp(lay.rightWidth-14, 28, 52)), max(4, tuiutils.Clamp(lay.rightHeight-10, 6, 10)))
	helpPickerMaxHeight := max(8, lay.rightHeight-6)
	helpPickerDesiredHeight := (len(a.helpPicker.Items()) * 3) + 1
	a.helpPicker.SetSize(
		max(24, tuiutils.Clamp(lay.rightWidth-14, 28, 52)),
		max(6, tuiutils.Clamp(helpPickerDesiredHeight, 6, helpPickerMaxHeight)),
	)
	a.fileBrowser.SetHeight(max(6, tuiutils.Clamp(lay.rightHeight-8, 8, 16)))
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
	return tuiutils.Clamp(a.input.LineCount(), composerMinHeight, composerMaxHeight)
}

func (a *App) growComposerForNewline() {
	nextHeight := tuiutils.Clamp(a.input.LineCount()+1, composerMinHeight, composerMaxHeight)
	if nextHeight > a.input.Height() {
		a.input.SetHeight(nextHeight)
	}
}

func (a *App) normalizeComposerHeight() {
	targetHeight := tuiutils.Clamp(a.input.LineCount(), composerMinHeight, composerMaxHeight)
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
	case slashCommandMemo:
		return true, a.handleMemoCommand()
	case slashCommandRemember:
		return true, a.handleRememberCommand(rest)
	case slashCommandForget:
		return true, a.handleForgetCommand(rest)
	default:
		return false, nil
	}
}

// runSlashCommandSelection 根据 /help 弹层选中的命令执行对应 slash 行为。
func (a *App) runSlashCommandSelection(command string) tea.Cmd {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return nil
	}

	if handled, cmd := a.handleImmediateSlashCommand(command); handled {
		return cmd
	}

	switch command {
	case slashCommandHelp:
		a.refreshHelpPicker()
		a.openHelpPicker()
		return nil
	case slashCommandProvider:
		if err := a.refreshProviderPicker(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
			return nil
		}
		a.openProviderPicker()
		return nil
	case slashCommandModelPick:
		if err := a.refreshModelPicker(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendActivity("system", "Failed to refresh models", err.Error(), true)
			return nil
		}
		a.openModelPicker()
		return a.requestModelCatalogRefresh(a.state.CurrentProvider)
	default:
		a.state.StatusText = statusApplyingCommand
		a.state.ExecutionError = ""
		return runLocalCommand(a.configManager, a.providerSvc, a.currentStatusSnapshot(), command)
	}
}

func (a App) currentStatusSnapshot() tuistatus.Snapshot {
	return tuistatus.BuildFromUIState(
		a.state,
		len(a.activeMessages),
		a.focusLabel(),
		tuiutils.PickerLabelFromMode(a.state.ActivePicker),
	)
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
	a.state.ActiveRunID = ""
	a.state.ToolStates = nil
	a.state.RunContext = tuistate.ContextWindowState{}
	a.state.TokenUsage = tuistate.TokenUsageState{}
	a.pendingPermission = nil
	a.clearRunProgress()
	a.input.Reset()
	a.state.InputText = ""
	a.setCurrentWorkdir(a.configManager.Get().Workdir)
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
	return tuiservices.ListenForRuntimeEventCmd(
		sub,
		func(event agentruntime.RuntimeEvent) tea.Msg { return RuntimeMsg{Event: event} },
		func() tea.Msg { return RuntimeClosedMsg{} },
	)
}

func runAgent(runtime agentruntime.Runtime, runID string, sessionID string, workdir string, content string) tea.Cmd {
	return tuiservices.RunAgentCmd(
		runtime,
		agentruntime.UserInput{
			SessionID: sessionID,
			RunID:     strings.TrimSpace(runID),
			Content:   content,
			Workdir:   workdir,
		},
		func(err error) tea.Msg { return runFinishedMsg{Err: err} },
	)
}

// runResolvePermission 提交一次权限审批决定到 runtime。
func runResolvePermission(
	runtime agentruntime.Runtime,
	requestID string,
	decision agentruntime.PermissionResolutionDecision,
) tea.Cmd {
	return tuiservices.RunResolvePermissionCmd(
		runtime,
		agentruntime.PermissionResolutionInput{
			RequestID: strings.TrimSpace(requestID),
			Decision:  decision,
		},
		func(input agentruntime.PermissionResolutionInput, err error) tea.Msg {
			return permissionResolutionFinishedMsg{
				RequestID: input.RequestID,
				Decision:  input.Decision,
				Err:       err,
			}
		},
	)
}

// runCompact 鍦ㄧ嫭绔嬪懡浠や腑瑙﹀彂 runtime compact锛屽苟鎶婄粨鏋滃洖浼犵粰 TUI銆
func runCompact(runtime agentruntime.Runtime, sessionID string) tea.Cmd {
	return tuiservices.RunCompactCmd(
		runtime,
		agentruntime.CompactInput{SessionID: sessionID},
		func(err error) tea.Msg { return compactFinishedMsg{Err: err} },
	)
}

// isBusy 缁熶竴鍒ゆ柇褰撳墠鐣岄潰鏄惁瀛樺湪杩涜涓殑 agent 鎴?compact 鎿嶄綔銆
func (a App) isBusy() bool {
	return tuiutils.IsBusy(a.state.IsAgentRunning, a.state.IsCompacting)
}

// handleMemoCommand 处理 /memo 命令，显示记忆索引内容。
func (a *App) handleMemoCommand() tea.Cmd {
	if a.memoSvc == nil {
		a.appendInlineMessage(roleError, "[System] Memo service is not enabled.")
		a.rebuildTranscript()
		return nil
	}
	entries, err := a.memoSvc.List(context.Background())
	if err != nil {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Failed to load memo: %s", err))
		a.rebuildTranscript()
		return nil
	}
	if len(entries) == 0 {
		a.appendInlineMessage(roleSystem, "[System] No memos stored yet. Use /remember <text> to add one.")
		a.rebuildTranscript()
		return nil
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("[System] %d memo(s):", len(entries)))
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("  [%s] %s", entry.Type, entry.Title))
	}
	a.appendInlineMessage(roleSystem, strings.Join(lines, "\n"))
	a.rebuildTranscript()
	return nil
}

// handleRememberCommand 处理 /remember <text> 命令，创建新的记忆条目。
func (a *App) handleRememberCommand(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if a.memoSvc == nil {
		a.appendInlineMessage(roleError, "[System] Memo service is not enabled.")
		a.rebuildTranscript()
		return nil
	}
	if text == "" {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Usage: %s", slashUsageRemember))
		a.rebuildTranscript()
		return nil
	}
	title := memo.NormalizeTitle(text)
	entry := memo.Entry{
		Type:    memo.TypeUser,
		Title:   title,
		Content: text,
		Source:  memo.SourceUserManual,
	}
	if err := a.memoSvc.Add(context.Background(), entry); err != nil {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Failed to save memo: %s", err))
		a.rebuildTranscript()
		return nil
	}
	a.appendInlineMessage(roleSystem, fmt.Sprintf("[System] Memo saved: %s", title))
	a.rebuildTranscript()
	return nil
}

// handleForgetCommand 处理 /forget <keyword> 命令，删除匹配的记忆条目。
func (a *App) handleForgetCommand(keyword string) tea.Cmd {
	keyword = strings.TrimSpace(keyword)
	if a.memoSvc == nil {
		a.appendInlineMessage(roleError, "[System] Memo service is not enabled.")
		a.rebuildTranscript()
		return nil
	}
	if keyword == "" {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Usage: %s", slashUsageForget))
		a.rebuildTranscript()
		return nil
	}
	removed, err := a.memoSvc.Remove(context.Background(), keyword)
	if err != nil {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Failed to remove memo: %s", err))
		a.rebuildTranscript()
		return nil
	}
	if removed == 0 {
		a.appendInlineMessage(roleSystem, fmt.Sprintf("[System] No memos matching %q.", keyword))
	} else {
		a.appendInlineMessage(roleSystem, fmt.Sprintf("[System] Removed %d memo(s) matching %q.", removed, keyword))
	}
	a.rebuildTranscript()
	return nil
}

// setCurrentWorkdir 统一设置当前工作目录，仅接受非空白且为绝对路径的值。
// 非法值会被静默忽略，防止 runtime 事件或异常输入污染 UI 状态。
func (a *App) setCurrentWorkdir(workdir string) {
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" || !filepath.IsAbs(trimmed) {
		return
	}
	a.state.CurrentWorkdir = trimmed

}
