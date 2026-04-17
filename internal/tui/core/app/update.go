package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	approvalflow "neo-code/internal/runtime/approval"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuistatus "neo-code/internal/tui/core/status"
	tuiutils "neo-code/internal/tui/core/utils"
	tuiinfra "neo-code/internal/tui/infra"
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

const providerAddSelectTimeout = 10 * time.Second
const providerAddNonPersistentEnvWarning = "API key is applied to the current process only on this platform; persist it in your shell profile for future sessions."

const sessionSwitchBusyMessage = "cannot switch sessions while run or compact is active"

var panelOrder = []panel{panelTranscript, panelActivity, panelInput}
var supportsUserEnvPersistence = config.SupportsUserEnvPersistence

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
		a.layoutCached = false
		a.applyComponentLayout(true)
		return a, tea.Batch(cmds...)
	case providerAddResultMsg:
		a.handleProviderAddResultMsg(typed)
		return a, nil
	case RuntimeMsg:
		transcriptDirty := a.handleRuntimeEvent(typed.Event)
		if a.deferredEventCmd != nil {
			cmds = append(cmds, a.deferredEventCmd)
			a.deferredEventCmd = nil
		}
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
		a.syncActiveSessionTitle()
		a.syncTodosFromRun()
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
		if a.handleTodoMouse(typed) {
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
				tabMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'	'}, Paste: typed.Paste}
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

		if key.Matches(typed, a.keys.PasteImage) {
			if err := a.addImageFromClipboard(); err != nil {
				a.state.StatusText = err.Error()
				a.appendActivity("multimodal", "Failed to paste image", err.Error(), true)
			}
			return a, tea.Batch(cmds...)
		}

		switch a.focus {
		case panelTranscript:
			a.handleViewportKeys(&a.transcript, typed)
			return a, tea.Batch(cmds...)
		case panelActivity:
			a.handleViewportKeys(&a.activity, typed)
			return a, tea.Batch(cmds...)
		case panelTodo:
			switch {
			case key.Matches(typed, a.keys.ScrollUp):
				a.moveTodoSelection(-1)
			case key.Matches(typed, a.keys.ScrollDown):
				a.moveTodoSelection(1)
			case key.Matches(typed, a.keys.PageUp):
				a.moveTodoSelection(-5)
			case key.Matches(typed, a.keys.PageDown):
				a.moveTodoSelection(5)
			case key.Matches(typed, a.keys.Top):
				if !a.todoCollapsed {
					a.todoSelectedIndex = 0
					a.rebuildTodo()
				}
			case key.Matches(typed, a.keys.Bottom):
				if !a.todoCollapsed {
					a.todoSelectedIndex = len(a.visibleTodoItems()) - 1
					a.rebuildTodo()
				}
			case key.Matches(typed, a.keys.Send):
				if a.todoCollapsed {
					a.setTodoCollapsed(false)
					a.state.StatusText = statusTodoExpanded
					a.applyComponentLayout(false)
				} else {
					a.openSelectedTodoDetail()
				}
			case typed.Type == tea.KeyRunes && len(typed.Runes) == 1 && (typed.Runes[0] == 'c' || typed.Runes[0] == 'C'):
				if a.toggleTodoCollapsed() {
					a.state.StatusText = statusTodoCollapsed
				} else {
					a.state.StatusText = statusTodoExpanded
				}
				a.applyComponentLayout(false)
			}
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
			hasImages := a.hasImageAttachments()
			if (input == "" && !hasImages) || a.isBusy() {
				return a, tea.Batch(cmds...)
			}

			if handled, cmd := a.handleImmediateSlashCommand(input); handled {
				a.input.Reset()
				a.state.InputText = ""
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, tea.Batch(cmds...)
			}

			if isImageReferenceInput(input) {
				if err := a.applyImageReference(input); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("multimodal", "Failed to add image reference", err.Error(), true)
				}
				a.input.Reset()
				a.state.InputText = ""
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()
				return a, tea.Batch(cmds...)
			}

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
			case slashCommandSession:
				if err := a.ensureSessionSwitchAllowed(""); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("session", "Failed to open session picker", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				if err := a.refreshSessionPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh sessions", err.Error(), true)
					return a, tea.Batch(cmds...)
				}
				a.openPicker(pickerSession, statusChooseSession, &a.sessionPicker, a.state.ActiveSessionID)
				return a, tea.Batch(cmds...)
			case slashCommandProviderAdd:
				a.startProviderAddForm()
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

			normalizedInput, absorbedImages, err := a.absorbInlineImageReferences(input)
			if err != nil {
				a.state.ExecutionError = err.Error()
				a.state.StatusText = err.Error()
				a.appendActivity("multimodal", "Failed to absorb inline image reference", err.Error(), true)
				return a, tea.Batch(cmds...)
			}
			if absorbedImages > 0 {
				input = normalizedInput
			}

			// image capability precheck is intentionally disabled.
			// 保持与 CLI 一致，先允许输入提交流转，再由后续链路统一处理能力兜底。
			a.input.Reset()
			a.state.InputText = ""
			a.applyComponentLayout(true)
			a.refreshCommandMenu()
			a.resetPasteHeuristics()

			a.clearActivities()
			a.clearRunProgress()
			a.state.IsAgentRunning = true
			a.state.IsCompacting = false
			a.state.StreamingReply = false
			a.state.ExecutionError = ""
			a.state.StatusText = statusThinking
			a.state.CurrentTool = ""

			runID := fmt.Sprintf("run-%d", a.now().UnixNano())
			a.state.ActiveRunID = runID
			requestedWorkdir := tuiutils.RequestedWorkdirForRun(a.state.CurrentWorkdir)
			images := make([]agentruntime.UserImageInput, 0, len(a.pendingImageAttachments))
			for _, attachment := range a.pendingImageAttachments {
				images = append(images, agentruntime.UserImageInput{
					Path:     attachment.Path,
					MimeType: attachment.MimeType,
				})
			}
			cmds = append(cmds, runAgent(a.runtime, agentruntime.PrepareInput{
				SessionID: a.state.ActiveSessionID,
				RunID:     runID,
				Workdir:   requestedWorkdir,
				Text:      input,
				Images:    images,
			}))
			a.clearImageAttachments()
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

// updatePendingPermissionInput handles keyboard interaction in the permission prompt.
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
		case pickerSession:
			a.closePicker()
			if err := a.activateSelectedSession(); err != nil {
				a.state.ExecutionError = err.Error()
				a.state.StatusText = err.Error()
				a.appendActivity("session", "Failed to activate session", err.Error(), true)
				return a, nil
			}
			a.rebuildTranscript()
			a.state.StatusText = statusReady
			return a, nil
		}
	}

	var cmd tea.Cmd
	switch a.state.ActivePicker {
	case pickerProvider:
		a.providerPicker, cmd = a.providerPicker.Update(msg)
	case pickerModel:
		a.modelPicker, cmd = a.modelPicker.Update(msg)
	case pickerSession:
		a.sessionPicker, cmd = a.sessionPicker.Update(msg)
	case pickerHelp:
		a.helpPicker, cmd = a.helpPicker.Update(msg)
	case pickerFile:
		a.fileBrowser, cmd = a.fileBrowser.Update(msg)
		if didSelect, path := a.fileBrowser.DidSelectFile(msg); didSelect {
			a.closePicker()
			if tuiinfra.IsSupportedImageFormat(path) {
				if err := a.addImageAttachment(path); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("multimodal", "Failed to add image", err.Error(), true)
					return a, cmd
				}
				return a, cmd
			}
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
	case pickerProviderAdd:
		return a.handleProviderAddFormInput(msg)
	}
	return a, cmd
}

func (a *App) refreshSessionPicker() error {
	sessions, err := a.runtime.ListSessions(context.Background())
	if err != nil {
		return err
	}

	a.state.Sessions = sessions

	items := make([]list.Item, 0, len(sessions))
	selectedIndex := 0
	hasSelection := false
	for i, summary := range sessions {
		items = append(items, sessionItem{Summary: summary, Active: summary.ID == a.state.ActiveSessionID})
		if summary.ID == a.state.ActiveSessionID {
			selectedIndex = i
			hasSelection = true
		}
	}

	a.sessionPicker.SetItems(items)
	if len(items) > 0 {
		if hasSelection {
			a.sessionPicker.Select(selectedIndex)
		} else {
			a.sessionPicker.Select(0)
		}
	}
	return nil
}

func (a *App) refreshMessages() error {
	a.resetSessionRuntimeState()
	if strings.TrimSpace(a.state.ActiveSessionID) == "" {
		a.activeMessages = nil
		a.clearActivities()
		a.clearTodos()
		return nil
	}

	session, err := a.runtime.LoadSession(context.Background(), a.state.ActiveSessionID)
	if err != nil {
		return err
	}

	a.activeMessages = session.Messages
	a.clearActivities()
	a.syncTodos(session.Todos)
	a.state.ActiveSessionTitle = session.Title
	a.setCurrentWorkdir(agentsession.EffectiveWorkdir(session.Workdir, a.configManager.Get().Workdir))
	a.refreshRuntimeSourceSnapshot()
	return nil
}

func (a *App) resetSessionRuntimeState() {
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.lastUserMessageRunID = ""
	a.state.ToolStates = nil
	a.state.RunContext = tuistate.ContextWindowState{}
	a.state.TokenUsage = tuistate.TokenUsageState{}
	a.pendingPermission = nil
	a.clearRunProgress()
}

func (a *App) refreshTodosFromSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is empty")
	}
	session, err := a.runtime.LoadSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	a.syncTodos(session.Todos)
	a.applyComponentLayout(false)
	return nil
}

func (a *App) syncTodosFromRun() {
	sessionID := a.state.ActiveSessionID
	if sessionID == "" {
		return
	}
	session, err := a.runtime.LoadSession(context.Background(), sessionID)
	if err != nil {
		return
	}
	a.todoItems = nil
	a.todoPanelVisible = false
	a.todoSelectedIndex = 0
	if len(session.Todos) > 0 {
		a.syncTodos(session.Todos)
	}
	a.rebuildTodo()
}

func (a *App) activateSelectedSession() error {
	item, ok := a.sessionPicker.SelectedItem().(sessionItem)
	if !ok {
		return nil
	}
	if err := a.ensureSessionSwitchAllowed(item.Summary.ID); err != nil {
		return err
	}

	a.state.ActiveSessionID = item.Summary.ID
	a.state.ActiveSessionTitle = item.Summary.Title
	a.state.ExecutionError = ""
	a.state.CurrentTool = ""

	return a.refreshMessages()
}

func (a *App) activateSessionByID(sessionID string) error {
	if err := a.ensureSessionSwitchAllowed(sessionID); err != nil {
		return err
	}
	for _, s := range a.state.Sessions {
		if s.ID == sessionID {
			a.state.ActiveSessionID = s.ID
			a.state.ActiveSessionTitle = s.Title
			a.state.ExecutionError = ""
			a.state.CurrentTool = ""
			return a.refreshMessages()
		}
	}
	return fmt.Errorf("session not found: %s", sessionID)
}

func (a *App) ensureSessionSwitchAllowed(targetSessionID string) error {
	targetSessionID = strings.TrimSpace(targetSessionID)
	activeSessionID := strings.TrimSpace(a.state.ActiveSessionID)
	if !a.isBusy() || (targetSessionID != "" && strings.EqualFold(targetSessionID, activeSessionID)) {
		return nil
	}
	return fmt.Errorf(sessionSwitchBusyMessage)
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

// runtimeSessionContextSource 缂備焦鎷濋梽鍕焽椤愶箑鐭楁い鏍亹閸嬫挻寰勭仦鍓ф殸婵炴潙鍚嬫穱娲儊閼恒儳鈻斿┑鐘辫兌閻熸捇鏌￠崒姘闁绘搫绱曢幏鐘诲閿濆懎骞嬮梺鍛婃⒐缁嬪繘鍩€
type runtimeSessionContextSource interface {
	GetSessionContext(ctx context.Context, sessionID string) (any, error)
}

type runtimeSessionUsageSource interface {
	GetSessionUsage(ctx context.Context, sessionID string) (any, error)
}

type runtimeRunSnapshotSource interface {
	GetRunSnapshot(ctx context.Context, runID string) (any, error)
}

var runtimeEventHandlerRegistry = map[agentruntime.EventType]func(*App, agentruntime.RuntimeEvent) bool{
	agentruntime.EventUserMessage:                              runtimeEventUserMessageHandler,
	agentruntime.EventInputNormalized:                          runtimeEventInputNormalizedHandler,
	agentruntime.EventAssetSaved:                               runtimeEventAssetSavedHandler,
	agentruntime.EventAssetSaveFailed:                          runtimeEventAssetSaveFailedHandler,
	agentruntime.EventType(tuiservices.RuntimeEventRunContext): runtimeEventRunContextHandler,
	agentruntime.EventType(tuiservices.RuntimeEventToolStatus): runtimeEventToolStatusHandler,
	agentruntime.EventType(tuiservices.RuntimeEventUsage):      runtimeEventUsageHandler,
	agentruntime.EventToolCallThinking:                         runtimeEventToolCallThinkingHandler,
	agentruntime.EventToolStart:                                runtimeEventToolStartHandler,
	agentruntime.EventToolResult:                               runtimeEventToolResultHandler,
	agentruntime.EventAgentChunk:                               runtimeEventAgentChunkHandler,
	agentruntime.EventToolChunk:                                runtimeEventToolChunkHandler,
	agentruntime.EventAgentDone:                                runtimeEventAgentDoneHandler,
	agentruntime.EventProviderRetry:                            runtimeEventProviderRetryHandler,
	agentruntime.EventPermissionRequested:                      runtimeEventPermissionRequestHandler,
	agentruntime.EventPermissionResolved:                       runtimeEventPermissionResolvedHandler,
	agentruntime.EventCompactApplied:                           runtimeEventCompactDoneHandler,
	agentruntime.EventCompactError:                             runtimeEventCompactErrorHandler,
	agentruntime.EventPhaseChanged:                             runtimeEventPhaseChangedHandler,
	agentruntime.EventStopReasonDecided:                        runtimeEventStopReasonDecidedHandler,
	agentruntime.EventTodoUpdated:                              runtimeEventTodoUpdatedHandler,
	agentruntime.EventTodoConflict:                             runtimeEventTodoConflictHandler,
}

func runtimeEventPhaseChangedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.PhaseChangedPayload)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(payload.To)) {
	case "plan":
		a.setRunProgress(0.3, "Planning")
	case "execute":
		a.setRunProgress(0.6, "Running tools")
	case "verify":
		a.setRunProgress(0.82, "Verifying")
	}
	return false
}

// runtimeEventStopReasonDecidedHandler 婵犮垼娉涚€氼噣骞冩繝鍥ц埞妞ゆ牗鐟ч杈╃磽娴ｅ摜澧涙い鎺撶⊕缁傚秶鈧綆浜為弶钘壝瑰鍐惧剮婵炲棎鍨芥俊
func runtimeEventStopReasonDecidedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.StopReasonDecidedPayload)
	if !ok {
		return false
	}
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.clearRunProgress()

	reason := strings.ToLower(strings.TrimSpace(string(payload.Reason)))
	switch reason {
	case "success":
		if strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.StatusText = statusReady
		}
	case "canceled":
		a.state.ExecutionError = ""
		a.state.StatusText = statusCanceled
		a.appendActivity("run", "Canceled current run", "", false)
	default:
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "runtime stopped"
		}
		a.state.ExecutionError = detail
		a.state.StatusText = detail
		a.appendActivity("run", "Runtime stopped", detail, true)
	}
	return false
}

func runtimeEventTodoUpdatedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(a.state.ActiveSessionID)
	}
	if strings.TrimSpace(sessionID) == "" || !strings.EqualFold(sessionID, strings.TrimSpace(a.state.ActiveSessionID)) {
		return false
	}

	if err := a.refreshTodosFromSession(sessionID); err != nil {
		a.appendActivity("todo", "Failed to refresh todo panel", err.Error(), true)
		return false
	}

	payload, _ := parseTodoEventPayload(event.Payload)
	action := strings.TrimSpace(payload.Action)
	if action == "" {
		action = "update"
	}
	a.appendActivity("todo", "Todo updated", action, false)
	return false
}

func runtimeEventTodoConflictHandler(a *App, event agentruntime.RuntimeEvent) bool {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(a.state.ActiveSessionID)
	}
	if strings.TrimSpace(sessionID) == "" || !strings.EqualFold(sessionID, strings.TrimSpace(a.state.ActiveSessionID)) {
		return false
	}

	if err := a.refreshTodosFromSession(sessionID); err != nil {
		a.appendActivity("todo", "Failed to refresh todo panel", err.Error(), true)
		return false
	}

	payload, _ := parseTodoEventPayload(event.Payload)
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "todo conflict"
	}
	a.appendActivity("todo", "Todo conflict", reason, true)
	return false
}

func parseTodoEventPayload(payload any) (agentruntime.TodoEventPayload, bool) {
	switch typed := payload.(type) {
	case agentruntime.TodoEventPayload:
		return typed, true
	case *agentruntime.TodoEventPayload:
		if typed == nil {
			return agentruntime.TodoEventPayload{}, false
		}
		return *typed, true
	case map[string]any:
		action := ""
		reason := ""
		if raw, ok := typed["Action"]; ok && raw != nil {
			action = strings.TrimSpace(fmt.Sprintf("%v", raw))
		}
		if raw, ok := typed["Reason"]; ok && raw != nil {
			reason = strings.TrimSpace(fmt.Sprintf("%v", raw))
		}
		if action == "" {
			if raw, ok := typed["action"]; ok && raw != nil {
				action = strings.TrimSpace(fmt.Sprintf("%v", raw))
			}
		}
		if reason == "" {
			if raw, ok := typed["reason"]; ok && raw != nil {
				reason = strings.TrimSpace(fmt.Sprintf("%v", raw))
			}
		}
		return agentruntime.TodoEventPayload{Action: action, Reason: reason}, true
	default:
		return agentruntime.TodoEventPayload{}, false
	}
}

func (a *App) handleRuntimeEvent(event agentruntime.RuntimeEvent) bool {
	if !a.shouldHandleRuntimeEvent(event) {
		return false
	}
	handler, ok := runtimeEventHandlerRegistry[event.Type]
	if !ok {
		return false
	}
	return handler(a, event)
}

func (a *App) shouldHandleRuntimeEvent(event agentruntime.RuntimeEvent) bool {
	activeSessionID := strings.TrimSpace(a.state.ActiveSessionID)
	eventSessionID := strings.TrimSpace(event.SessionID)
	if activeSessionID != "" && eventSessionID != "" && !strings.EqualFold(activeSessionID, eventSessionID) {
		return false
	}

	activeRunID := strings.TrimSpace(a.state.ActiveRunID)
	eventRunID := strings.TrimSpace(event.RunID)
	if activeRunID != "" && eventRunID != "" && !strings.EqualFold(activeRunID, eventRunID) {
		return false
	}
	return true
}

func runtimeEventInputNormalizedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if strings.TrimSpace(event.RunID) != "" {
		a.state.ActiveRunID = strings.TrimSpace(event.RunID)
	}
	payload, ok := event.Payload.(agentruntime.InputNormalizedPayload)
	if !ok {
		return false
	}
	if payload.ImageCount > 0 {
		a.appendActivity(
			"multimodal",
			"Input normalized",
			fmt.Sprintf("text=%d chars, images=%d", payload.TextLength, payload.ImageCount),
			false,
		)
	}
	return false
}

func runtimeEventAssetSavedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.AssetSavedPayload)
	if !ok {
		return false
	}
	detail := strings.TrimSpace(payload.AssetID)
	if detail == "" {
		detail = "asset saved"
	}
	if strings.TrimSpace(payload.Path) != "" {
		detail = fmt.Sprintf("%s (%s)", detail, filepath.Base(payload.Path))
	}
	a.appendActivity("multimodal", "Saved attachment", detail, false)
	return false
}

func runtimeEventAssetSaveFailedHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.AssetSaveFailedPayload)
	if !ok {
		return false
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = "failed to save attachment"
	}
	a.state.ExecutionError = message
	a.state.StatusText = message
	a.appendActivity("multimodal", "Failed to save attachment", message, true)
	return false
}

func runtimeEventUserMessageHandler(a *App, event agentruntime.RuntimeEvent) bool {
	runID := strings.TrimSpace(event.RunID)
	if runID != "" {
		a.state.ActiveRunID = runID
	}
	if sessionID := strings.TrimSpace(event.SessionID); sessionID != "" {
		a.state.ActiveSessionID = sessionID
	}
	a.state.StatusText = statusThinking
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ExecutionError = ""
	a.setRunProgress(0.15, "Queued")
	payload, ok := event.Payload.(providertypes.Message)
	if !ok {
		return false
	}
	content := renderMessagePartsForDisplay(payload.Parts)
	if strings.TrimSpace(content) == "" {
		return false
	}
	if runID != "" && strings.EqualFold(a.lastUserMessageRunID, runID) {
		return false
	}
	a.activeMessages = append(a.activeMessages, providertypes.Message{
		Role:  roleUser,
		Parts: providertypes.CloneParts(payload.Parts),
	})
	if runID != "" {
		a.lastUserMessageRunID = runID
	}
	return true
}

func runtimeEventRunContextHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseRunContextPayload(event.Payload)
	if !ok {
		return false
	}
	mapped := tuiservices.MapRunContextPayload(event.RunID, event.SessionID, payload)
	a.state.RunContext = mapped
	if strings.TrimSpace(mapped.SessionID) != "" {
		a.state.ActiveSessionID = strings.TrimSpace(mapped.SessionID)
	}
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

func runtimeEventUsageHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseUsagePayload(event.Payload)
	if !ok {
		return false
	}
	a.state.TokenUsage = tuiservices.MapUsagePayload(payload)
	return false
}

// runtimeEventToolCallThinkingHandler 婵犮垼娉涚€氼噣骞冩繝鍌ゅ晠闁靛鍎卞鏃堟偡濞嗗繐顏╅柛銊︾箞濮婂ジ鎳滃▓鍨杸婵炲瓨绮岄鍕枎閵忋倕违
func runtimeEventToolCallThinkingHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.CurrentTool = payload
		a.setRunProgress(0.35, "Planning")
		a.appendActivity("tool", "Planning tool call", payload, false)
	}
	return false
}

// runtimeEventToolStartHandler 婵犮垼娉涚€氼噣骞冩繝鍌ゅ晠闁靛鍎卞鏃傗偓娈垮枓閸嬫挸鈹戦纰卞剱濠⒀呮櫕閹壆浠﹂懖鈺冩婵炲濮剧紙浼村焵
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
		Parts:   []providertypes.ContentPart{providertypes.NewTextPart(payload.Content)},
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

// runtimeEventAgentChunkHandler 婵犮垼娉涚€氼噣骞冩繝鍋界喖鍨惧畷鍥ｅ亾瀹勬噴瑙勬媴閸濄儳顢呮繝鈷€鍛槐闁革絿鍎ゅ蹇涘箻閸愬弶鐦旈梺
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

func runtimeEventToolChunkHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.StatusText = statusRunningTool
		a.appendActivity("tool", "Tool output", preview(payload, 88, 4), false)
	}
	return false
}

// runtimeEventAgentDoneHandler 婵犮垼娉涚€氼噣骞冩繝鍐╀氦闁归偊鍨奸弨浠嬫倵閻熺増婀伴柛銊︾缁傚秶鈧絺鏅滈浠嬫煏
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
	if payload, ok := event.Payload.(providertypes.Message); ok {
		content := renderMessagePartsForDisplay(payload.Parts)
		if strings.TrimSpace(content) != "" && !a.lastAssistantMatches(content) {
			a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart(content)}})
		}
		return true
	}
	return false
}

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

// runtimeEventErrorHandler 婵犮垼娉涚€氼噣骞冩繝鍐╀氦闁归偊鍨奸弨浠嬫煛閸愩劎鍩ｉ柡浣革功閹风娀顢涘▎鎴犳婵炲濮剧紙浼村焵
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

func runtimeEventProviderRetryHandler(a *App, event agentruntime.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.StatusText = statusThinking
		a.runProgressKnown = false
		a.appendActivity("provider", "Retrying provider call", payload, false)
	}
	return false
}

func runtimeEventPermissionRequestHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := parsePermissionRequestPayload(event.Payload)
	if !ok {
		return false
	}

	if a.pendingPermission != nil {
		currentRequestID := strings.TrimSpace(a.pendingPermission.Request.RequestID)
		nextRequestID := strings.TrimSpace(payload.RequestID)
		if currentRequestID != "" && currentRequestID != nextRequestID && !a.pendingPermission.Submitting {
			a.deferredEventCmd = runResolvePermission(a.runtime, currentRequestID, approvalflow.DecisionReject)
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

// refreshPermissionPromptLayout 闂侀潻璐熼崝宀€绮╂搴濇勃闁逞屽墮椤斿繘骞撻幒鎴犱淮婵犳鍠栭鍛偓鍨叀瀵喚鎹勯崫鍕幈闂佸搫鍊绘晶妤€顭囬崼銉︹挃闁规壆澧楀銊╂煛婢跺孩纭舵繛鏉戭樀瀹曟鎼归銏㈢懇闂佺粯顨呴悧鍕焵
func (a *App) refreshPermissionPromptLayout() {
	if a.width <= 0 || a.height <= 0 {
		return
	}
	a.applyComponentLayout(false)
}

func runtimeEventCompactDoneHandler(a *App, event agentruntime.RuntimeEvent) bool {
	payload, ok := event.Payload.(agentruntime.CompactResult)
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
		a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart(chunk)}})
		a.state.StreamingReply = true
		return
	}

	content := renderMessagePartsForDisplay(a.activeMessages[len(a.activeMessages)-1].Parts)
	a.activeMessages[len(a.activeMessages)-1].Parts = []providertypes.ContentPart{providertypes.NewTextPart(content + chunk)}
}

func (a *App) appendInlineMessage(role string, message string) {
	content := strings.TrimSpace(message)
	if content == "" {
		return
	}

	a.activeMessages = append(a.activeMessages, providertypes.Message{Role: role, Parts: []providertypes.ContentPart{providertypes.NewTextPart(content)}})
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
		a.activities = a.activities[len(a.activities)-maxActivityEntries:]
	}
	a.syncActivityViewport(previousCount)
	a.viewDirty = true
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
	return last.Role == roleAssistant && strings.TrimSpace(renderMessagePartsForDisplay(last.Parts)) == strings.TrimSpace(content)
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

	return streamX, streamY, lay.contentWidth, a.transcript.Height
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

	inputY := streamY + a.transcript.Height + a.activityPreviewHeight() + a.todoPreviewHeight() + a.commandMenuHeight(lay.contentWidth)
	inputHeight := lipgloss.Height(a.renderPrompt(lay.contentWidth))
	return streamX, inputY, lay.contentWidth, inputHeight
}

func (a App) activityBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := headerBarHeight
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY

	activityHeight := a.activityPreviewHeight()
	if activityHeight <= 0 {
		return streamX, streamY + a.transcript.Height, lay.contentWidth, 0
	}
	return streamX, streamY + a.transcript.Height, lay.contentWidth, activityHeight
}

func (a App) todoBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := headerBarHeight
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY

	todoHeight := a.todoPreviewHeight()
	if todoHeight <= 0 {
		return streamX, streamY + a.transcript.Height + a.activityPreviewHeight(), lay.contentWidth, 0
	}
	return streamX, streamY + a.transcript.Height + a.activityPreviewHeight(), lay.contentWidth, todoHeight
}

func (a App) isMouseWithinActivity(msg tea.MouseMsg) bool {
	x, y, width, height := a.activityBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) isMouseWithinTodo(msg tea.MouseMsg) bool {
	x, y, width, height := a.todoBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) isMouseWithinTodoHeader(msg tea.MouseMsg) bool {
	if !a.isMouseWithinTodo(msg) {
		return false
	}
	_, y, _, _ := a.todoBounds()
	// top border + one-line panel header
	return msg.Y <= y+1
}

func (a App) todoItemIndexAtMouse(msg tea.MouseMsg) (int, bool) {
	if a.todoCollapsed || a.todo.Height <= 0 {
		return 0, false
	}
	if !a.isMouseWithinTodo(msg) {
		return 0, false
	}

	_, y, _, _ := a.todoBounds()
	// one top border row + one panel header row
	bodyRow := msg.Y - (y + 2)
	if bodyRow < 0 || bodyRow >= a.todo.Height {
		return 0, false
	}

	contentLine := a.todo.YOffset + bodyRow
	// line 0 is table header
	index := contentLine - 1
	visibleCount := len(a.visibleTodoItems())
	if index < 0 || index >= visibleCount {
		return 0, false
	}
	return index, true
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

func (a *App) handleTodoMouse(msg tea.MouseMsg) bool {
	if !a.todoPanelVisible || !a.isMouseWithinTodo(msg) {
		return false
	}
	if a.state.ActivePicker != pickerNone {
		return false
	}

	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if a.focus != panelTodo {
			a.focus = panelTodo
			a.applyFocus()
		}
		if a.isMouseWithinTodoHeader(msg) {
			if a.toggleTodoCollapsed() {
				a.state.StatusText = statusTodoCollapsed
			} else {
				a.state.StatusText = statusTodoExpanded
			}
			a.applyComponentLayout(false)
			return true
		}
		if a.todoCollapsed {
			a.setTodoCollapsed(false)
			a.state.StatusText = statusTodoExpanded
			a.applyComponentLayout(false)
			return true
		}
		if index, ok := a.todoItemIndexAtMouse(msg); ok {
			a.todoSelectedIndex = index
			a.rebuildTodo()
			return true
		}
		return false
	case msg.Button == tea.MouseButtonWheelUp && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelUp):
		if a.focus != panelTodo {
			a.focus = panelTodo
			a.applyFocus()
		}
		if a.todoCollapsed {
			return true
		}
		a.moveTodoSelection(-mouseWheelStepLines)
		return true
	case msg.Button == tea.MouseButtonWheelDown && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelDown):
		if a.focus != panelTodo {
			a.focus = panelTodo
			a.applyFocus()
		}
		if a.todoCollapsed {
			return true
		}
		a.moveTodoSelection(mouseWheelStepLines)
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
	a.layoutCached = true
	a.cachedWidth = a.width
	a.cachedHeight = a.height

	lay := a.computeLayout()
	prevTranscriptWidth := a.transcript.Width
	prevActivityWidth := a.activity.Width
	prevActivityHeight := a.activity.Height
	prevTodoWidth := a.todo.Width
	prevTodoHeight := a.todo.Height
	a.help.ShowAll = a.state.ShowHelp
	a.transcript.Width = lay.contentWidth
	a.resizeCommandMenu()
	a.input.SetWidth(a.composerInnerWidth(lay.contentWidth))
	a.input.SetHeight(a.composerHeight())
	transcriptHeight, activityHeight, _, todoHeight := a.waterfallMetrics(a.transcript.Width, lay.contentHeight)
	a.transcript.Height = transcriptHeight

	if activityHeight > 0 {
		panelStyle := a.styles.panelFocused
		frameHeight := panelStyle.GetVerticalFrameSize()
		borderWidth := 2
		paddingWidth := panelStyle.GetHorizontalFrameSize() - borderWidth
		panelWidth := max(1, lay.contentWidth-borderWidth)
		bodyWidth := max(10, panelWidth-paddingWidth)
		bodyHeight := max(1, activityHeight-frameHeight-1)
		a.activity.Width = bodyWidth
		a.activity.Height = bodyHeight
	} else {
		a.activity.Width = max(10, lay.contentWidth-4)
		a.activity.Height = 0
	}

	if todoHeight > 0 {
		panelStyle := a.styles.panelFocused
		frameHeight := panelStyle.GetVerticalFrameSize()
		borderWidth := 2
		paddingWidth := panelStyle.GetHorizontalFrameSize() - borderWidth
		panelWidth := max(1, lay.contentWidth-borderWidth)
		bodyWidth := max(10, panelWidth-paddingWidth)
		bodyHeight := max(1, todoHeight-frameHeight-1)
		a.todo.Width = bodyWidth
		a.todo.Height = bodyHeight
	} else {
		a.todo.Width = max(10, lay.contentWidth-4)
		a.todo.Height = 0
	}

	pickerLayout := a.buildPickerLayout(lay.contentWidth, lay.contentHeight)
	a.providerPicker.SetSize(pickerLayout.listWidth, pickerLayout.listHeight)
	a.modelPicker.SetSize(pickerLayout.listWidth, pickerLayout.listHeight)
	a.sessionPicker.SetSize(pickerLayout.listWidth, pickerLayout.listHeight)
	helpPickerDesiredHeight := (len(a.helpPicker.Items()) * 3) + 1
	a.helpPicker.SetSize(
		pickerLayout.listWidth,
		tuiutils.Clamp(helpPickerDesiredHeight, pickerListMinHeight, pickerLayout.listHeight),
	)
	a.fileBrowser.SetHeight(max(pickerListMinHeight, pickerLayout.listHeight))
	if rebuildTranscript || prevTranscriptWidth != a.transcript.Width {
		a.rebuildTranscript()
	} else if a.transcript.AtBottom() || a.isBusy() {
		a.transcript.GotoBottom()
	}
	if prevActivityWidth != a.activity.Width || prevActivityHeight != a.activity.Height {
		a.rebuildActivity()
	}
	if prevTodoWidth != a.todo.Width || prevTodoHeight != a.todo.Height {
		a.rebuildTodo()
	}
}

func (a App) composerBoxWidth(totalWidth int) int {
	return totalWidth
}

func (a App) composerInnerWidth(totalWidth int) int {
	return max(4, totalWidth-a.styles.inputBoxFocused.GetHorizontalFrameSize())
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
		if message.Role == roleTool {
			continue
		}
		rendered, bindings := a.renderMessageBlockWithCopy(message, width, nextCopyID)
		if rendered == "" {
			continue
		}
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
	command, _ := splitFirstWord(strings.ToLower(strings.TrimSpace(input)))
	switch command {
	case slashCommandExit:
		return true, tea.Quit
	case slashCommandClear:
		a.startDraftSession()
		a.state.StatusText = "[System] Cleared current draft/history."
		return true, nil
	case slashCommandSession:
		if err := a.ensureSessionSwitchAllowed(""); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendActivity("session", "Failed to open session picker", err.Error(), true)
			return true, nil
		}
		if err := a.refreshSessionPicker(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendActivity("system", "Failed to refresh sessions", err.Error(), true)
			return true, nil
		}
		a.openPicker(pickerSession, statusChooseSession, &a.sessionPicker, a.state.ActiveSessionID)
		return true, nil
	case slashCommandProviderAdd:
		a.startProviderAddForm()
		return true, nil
	default:
		return false, nil
	}
}

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
	a.clearTodos()
	a.state.IsCompacting = false
	a.state.StatusText = statusDraft
	a.state.ExecutionError = ""
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.lastUserMessageRunID = ""
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

func runAgent(runtime agentruntime.Runtime, input agentruntime.PrepareInput) tea.Cmd {
	return tuiservices.RunSubmitCmd(
		runtime,
		input,
		func(err error) tea.Msg { return runFinishedMsg{Err: err} },
	)
}

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

// isBusy reports whether an agent run or compact operation is in progress.
func (a App) isBusy() bool {
	return tuiutils.IsBusy(a.state.IsAgentRunning, a.state.IsCompacting)
}

// setCurrentWorkdir updates the current workdir only when the value is non-empty and absolute.
func (a *App) setCurrentWorkdir(workdir string) {
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" || !filepath.IsAbs(trimmed) {
		return
	}
	a.state.CurrentWorkdir = trimmed
}

type providerAddFieldID int

const (
	providerAddFieldName providerAddFieldID = iota
	providerAddFieldDriver
	providerAddFieldBaseURL
	providerAddFieldAPIStyle
	providerAddFieldDeploymentMode
	providerAddFieldAPIVersion
	providerAddFieldDiscoveryEndpointPath
	providerAddFieldDiscoveryResponseProfile
	providerAddFieldAPIKeyEnv
	providerAddFieldAPIKey
)

func providerAddVisibleFields(driver string) []providerAddFieldID {
	fields := []providerAddFieldID{
		providerAddFieldName,
		providerAddFieldDriver,
		providerAddFieldBaseURL,
	}

	switch provider.NormalizeProviderDriver(driver) {
	case provider.DriverOpenAICompat:
		fields = append(fields, providerAddFieldAPIStyle)
	case provider.DriverGemini:
		fields = append(fields, providerAddFieldDeploymentMode)
	case provider.DriverAnthropic:
		fields = append(fields, providerAddFieldAPIVersion)
	}

	fields = append(fields, providerAddFieldDiscoveryEndpointPath, providerAddFieldDiscoveryResponseProfile)
	fields = append(fields, providerAddFieldAPIKeyEnv, providerAddFieldAPIKey)
	return fields
}

func clampProviderAddStep(form *providerAddFormState) {
	if form == nil {
		return
	}
	fields := providerAddVisibleFields(form.Driver)
	if len(fields) == 0 {
		form.Step = 0
		return
	}
	if form.Step < 0 {
		form.Step = 0
	}
	if form.Step >= len(fields) {
		form.Step = len(fields) - 1
	}
}

func currentProviderAddField(form *providerAddFormState) providerAddFieldID {
	if form == nil {
		return providerAddFieldName
	}
	clampProviderAddStep(form)
	fields := providerAddVisibleFields(form.Driver)
	if len(fields) == 0 {
		return providerAddFieldName
	}
	return fields[form.Step]
}

func (a *App) startProviderAddForm() {
	a.providerAddForm = &providerAddFormState{
		Step:                     0,
		Name:                     "",
		Driver:                   provider.DriverOpenAICompat,
		BaseURL:                  "",
		APIStyle:                 provider.OpenAICompatibleAPIStyleChatCompletions,
		DeploymentMode:           "",
		APIVersion:               "",
		DiscoveryEndpointPath:    provider.DiscoveryEndpointPathModels,
		DiscoveryResponseProfile: provider.DiscoveryResponseProfileOpenAI,
		APIKeyEnv:                "",
		APIKey:                   "",
		Error:                    "",
		ErrorIsHard:              false,
		Drivers:                  []string{provider.DriverOpenAICompat, provider.DriverGemini, provider.DriverAnthropic},
	}
	a.state.ActivePicker = pickerProviderAdd
	a.state.StatusText = "Add new provider"
	a.state.ExecutionError = ""
}

func (a *App) handleProviderAddFormInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.providerAddForm == nil || a.providerAddForm.Submitting {
		return a, nil
	}

	typed := msg
	prevStep := a.providerAddForm.Step
	fields := providerAddVisibleFields(a.providerAddForm.Driver)
	fieldCount := len(fields)
	if fieldCount == 0 {
		fieldCount = 1
	}

	switch {
	case key.Matches(typed, a.keys.PrevPanel):
		a.providerAddForm.Step = (a.providerAddForm.Step + fieldCount - 1) % fieldCount
	case key.Matches(typed, a.keys.NextPanel):
		a.providerAddForm.Step = (a.providerAddForm.Step + 1) % fieldCount
	case key.Matches(typed, a.keys.Send):
		return a, a.submitProviderAddForm()
	case key.Matches(typed, a.keys.FocusInput):
		a.providerAddForm = nil
		a.state.ActivePicker = pickerNone
		a.state.StatusText = statusReady
	case typed.Type == tea.KeyBackspace:
		switch currentProviderAddField(a.providerAddForm) {
		case providerAddFieldName:
			a.providerAddForm.Name = trimLastRune(a.providerAddForm.Name)
		case providerAddFieldDriver:
			a.providerAddForm.Driver = trimLastRune(a.providerAddForm.Driver)
		case providerAddFieldBaseURL:
			a.providerAddForm.BaseURL = trimLastRune(a.providerAddForm.BaseURL)
		case providerAddFieldAPIStyle:
			a.providerAddForm.APIStyle = trimLastRune(a.providerAddForm.APIStyle)
		case providerAddFieldDeploymentMode:
			a.providerAddForm.DeploymentMode = trimLastRune(a.providerAddForm.DeploymentMode)
		case providerAddFieldAPIVersion:
			a.providerAddForm.APIVersion = trimLastRune(a.providerAddForm.APIVersion)
		case providerAddFieldDiscoveryEndpointPath:
			a.providerAddForm.DiscoveryEndpointPath = trimLastRune(a.providerAddForm.DiscoveryEndpointPath)
		case providerAddFieldDiscoveryResponseProfile:
			a.providerAddForm.DiscoveryResponseProfile = trimLastRune(a.providerAddForm.DiscoveryResponseProfile)
		case providerAddFieldAPIKeyEnv:
			a.providerAddForm.APIKeyEnv = trimLastRune(a.providerAddForm.APIKeyEnv)
		case providerAddFieldAPIKey:
			a.providerAddForm.APIKey = trimLastRune(a.providerAddForm.APIKey)
		}
		return a, nil
	case typed.Type == tea.KeyUp:
		if currentProviderAddField(a.providerAddForm) == providerAddFieldDriver {
			currentIdx := -1
			for i, d := range a.providerAddForm.Drivers {
				if d == a.providerAddForm.Driver {
					currentIdx = i
					break
				}
			}
			if currentIdx >= 0 {
				currentIdx = (currentIdx - 1 + len(a.providerAddForm.Drivers)) % len(a.providerAddForm.Drivers)
				a.providerAddForm.Driver = a.providerAddForm.Drivers[currentIdx]
				clampProviderAddStep(a.providerAddForm)
			}
		}
		return a, nil
	case typed.Type == tea.KeyDown:
		if currentProviderAddField(a.providerAddForm) == providerAddFieldDriver {
			currentIdx := -1
			for i, d := range a.providerAddForm.Drivers {
				if d == a.providerAddForm.Driver {
					currentIdx = i
					break
				}
			}
			if currentIdx >= 0 {
				currentIdx = (currentIdx + 1) % len(a.providerAddForm.Drivers)
				a.providerAddForm.Driver = a.providerAddForm.Drivers[currentIdx]
				clampProviderAddStep(a.providerAddForm)
			}
		}
		return a, nil
	default:
		if len(typed.Runes) > 0 {
			if cleanInput := sanitizeProviderAddInputRunes(typed.Runes); cleanInput != "" {
				switch currentProviderAddField(a.providerAddForm) {
				case providerAddFieldName:
					a.providerAddForm.Name += cleanInput
				case providerAddFieldBaseURL:
					a.providerAddForm.BaseURL += cleanInput
				case providerAddFieldAPIStyle:
					a.providerAddForm.APIStyle += cleanInput
				case providerAddFieldDeploymentMode:
					a.providerAddForm.DeploymentMode += cleanInput
				case providerAddFieldAPIVersion:
					a.providerAddForm.APIVersion += cleanInput
				case providerAddFieldDiscoveryEndpointPath:
					a.providerAddForm.DiscoveryEndpointPath += cleanInput
				case providerAddFieldDiscoveryResponseProfile:
					a.providerAddForm.DiscoveryResponseProfile += cleanInput
				case providerAddFieldAPIKeyEnv:
					a.providerAddForm.APIKeyEnv += cleanInput
				case providerAddFieldAPIKey:
					a.providerAddForm.APIKey += cleanInput
				}
			}
		}
	}

	if prevStep != a.providerAddForm.Step {
		a.providerAddForm.Error = ""
		a.providerAddForm.ErrorIsHard = false
	}

	return a, nil
}

func (a *App) submitProviderAddForm() tea.Cmd {
	if a.providerAddForm == nil {
		return nil
	}

	request, validationErr := buildProviderAddRequest(*a.providerAddForm)
	if validationErr != "" {
		a.providerAddForm.Error = "Please update the form: " + validationErr
		a.providerAddForm.ErrorIsHard = false
		return nil
	}

	a.providerAddForm.Submitting = true
	a.providerAddForm.Error = ""
	a.providerAddForm.ErrorIsHard = false
	a.state.StatusText = "Adding provider..."
	a.appendActivity("provider", "Adding provider", request.Name, false)

	return a.runProviderAddFlow(request)
}

type providerAddRequest struct {
	Name                     string
	Driver                   string
	BaseURL                  string
	APIStyle                 string
	DeploymentMode           string
	APIVersion               string
	DiscoveryEndpointPath    string
	DiscoveryResponseProfile string
	APIKeyEnv                string
	APIKey                   string
}

type providerAddResultMsg struct {
	Name    string
	Model   string
	Error   string
	Warning string
}

func buildProviderAddRequest(form providerAddFormState) (providerAddRequest, string) {
	request := providerAddRequest{
		Name:                     normalizeProviderAddFieldValue(form.Name),
		Driver:                   provider.NormalizeProviderDriver(normalizeProviderAddFieldValue(form.Driver)),
		BaseURL:                  normalizeProviderAddFieldValue(form.BaseURL),
		APIStyle:                 normalizeProviderAddFieldValue(form.APIStyle),
		DeploymentMode:           normalizeProviderAddFieldValue(form.DeploymentMode),
		APIVersion:               normalizeProviderAddFieldValue(form.APIVersion),
		DiscoveryEndpointPath:    normalizeProviderAddFieldValue(form.DiscoveryEndpointPath),
		DiscoveryResponseProfile: normalizeProviderAddFieldValue(form.DiscoveryResponseProfile),
		APIKeyEnv:                normalizeProviderAddFieldValue(form.APIKeyEnv),
		APIKey:                   normalizeProviderAddFieldValue(form.APIKey),
	}

	if request.Name == "" {
		return providerAddRequest{}, "Name is required"
	}
	if request.Driver == "" {
		return providerAddRequest{}, "Driver is required"
	}
	if request.APIKey == "" {
		return providerAddRequest{}, "API Key is required"
	}
	if request.APIKeyEnv == "" {
		return providerAddRequest{}, "API Key Env is required"
	}
	if err := config.ValidateEnvVarName(request.APIKeyEnv); err != nil {
		return providerAddRequest{}, err.Error()
	}
	if config.IsProtectedEnvVarName(request.APIKeyEnv) {
		return providerAddRequest{}, fmt.Sprintf("API Key Env %q is protected", request.APIKeyEnv)
	}

	switch request.Driver {
	case provider.DriverOpenAICompat:
		if request.BaseURL == "" {
			request.BaseURL = config.OpenAIDefaultBaseURL
		}
		if request.APIStyle == "" {
			request.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions
		}
		request.DeploymentMode = ""
		request.APIVersion = ""
	case provider.DriverGemini:
		if request.BaseURL == "" {
			request.BaseURL = config.GeminiDefaultBaseURL
		}
		request.APIStyle = ""
		request.APIVersion = ""
	case provider.DriverAnthropic:
		if request.BaseURL == "" {
			return providerAddRequest{}, "Base URL is required for anthropic provider"
		}
		request.APIStyle = ""
		request.DeploymentMode = ""
	default:
		if request.BaseURL == "" {
			return providerAddRequest{}, "Base URL is required for custom driver"
		}
		request.APIStyle = ""
		request.DeploymentMode = ""
		request.APIVersion = ""
	}
	normalizedProtocols, err := provider.NormalizeProviderProtocolSettings(
		request.Driver,
		"",
		"",
		"",
		request.DiscoveryEndpointPath,
		"",
		"",
		request.APIStyle,
		request.DiscoveryResponseProfile,
	)
	if err != nil {
		return providerAddRequest{}, err.Error()
	}
	if request.Driver == provider.DriverOpenAICompat {
		request.APIStyle = normalizedProtocols.LegacyAPIStyle
	} else {
		request.APIStyle = ""
	}
	request.DiscoveryEndpointPath = normalizedProtocols.DiscoveryEndpointPath
	request.DiscoveryResponseProfile = normalizedProtocols.ResponseProfile

	return request, ""
}

// sanitizeProviderAddInputRunes 过滤 provider 表单输入中的控制字符，避免不可见字符污染配置字段。
func sanitizeProviderAddInputRunes(runes []rune) string {
	if len(runes) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(runes))
	for _, r := range runes {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// normalizeProviderAddFieldValue 对 provider 表单字段做统一清理，去除控制字符并裁剪首尾空白。
func normalizeProviderAddFieldValue(value string) string {
	return strings.TrimSpace(sanitizeProviderAddInputRunes([]rune(value)))
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 || size > len(value) {
		return ""
	}
	return value[:len(value)-size]
}

func sanitizeProviderAddError(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "unknown error"
	}

	for _, secret := range secrets {
		if trimmed := strings.TrimSpace(secret); trimmed != "" {
			text = strings.ReplaceAll(text, trimmed, "[REDACTED]")
			text = strings.ReplaceAll(text, filepath.ToSlash(trimmed), "[REDACTED]")
		}
	}
	return text
}

func (a *App) runProviderAddFlow(request providerAddRequest) tea.Cmd {
	baseDir := a.configManager.BaseDir()
	providerSvc := a.providerSvc

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), providerAddSelectTimeout)
		defer cancel()

		selection, err := providerSvc.CreateCustomProvider(ctx, configstate.CreateCustomProviderInput{
			Name:                     request.Name,
			Driver:                   request.Driver,
			BaseURL:                  request.BaseURL,
			APIStyle:                 request.APIStyle,
			DeploymentMode:           request.DeploymentMode,
			APIVersion:               request.APIVersion,
			DiscoveryEndpointPath:    request.DiscoveryEndpointPath,
			DiscoveryResponseProfile: request.DiscoveryResponseProfile,
			APIKeyEnv:                request.APIKeyEnv,
			APIKey:                   request.APIKey,
		})
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf(
					"model discovery timed out after %s; check base URL, API key, and network connectivity",
					providerAddSelectTimeout,
				)
			}
			return providerAddResultMsg{
				Name:  request.Name,
				Error: sanitizeProviderAddError(fmt.Errorf("create provider: %w", err), request.APIKey, baseDir),
			}
		}

		return providerAddResultMsg{
			Name:    request.Name,
			Model:   strings.TrimSpace(selection.ModelID),
			Warning: providerAddPersistenceWarning(),
		}
	}
}

func providerAddPersistenceWarning() string {
	if supportsUserEnvPersistence() {
		return ""
	}
	return providerAddNonPersistentEnvWarning
}

func (a *App) handleProviderAddResultMsg(msg providerAddResultMsg) {
	if a.providerAddForm == nil {
		return
	}

	if msg.Error != "" {
		a.providerAddForm.Error = msg.Error
		a.providerAddForm.ErrorIsHard = true
		a.providerAddForm.Submitting = false
		a.state.ExecutionError = msg.Error
		a.state.StatusText = "Failed to add provider"
		a.appendActivity("provider", "Failed to add provider", msg.Error, true)
		return
	}

	a.providerAddForm = nil
	a.state.ActivePicker = pickerNone
	a.state.ExecutionError = ""
	a.state.StatusText = "Provider added: " + msg.Name
	a.state.CurrentProvider = msg.Name
	if msg.Model != "" {
		a.state.CurrentModel = msg.Model
	}
	a.appendActivity("provider", "Provider added", msg.Name, false)
	if msg.Warning != "" {
		a.appendActivity("provider", "Provider key persistence", msg.Warning, false)
	}

	if err := a.refreshProviderPicker(); err != nil {
		a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
	}
	if err := a.refreshModelPicker(); err != nil {
		a.appendActivity("system", "Failed to refresh models", err.Error(), true)
	}
}
