package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	"github.com/charmbracelet/x/ansi"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
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
const providerAddManualModelsJSONTemplate = "[\n  {\n    \"id\": \"model-id\",\n    \"name\": \"Model Name\"\n  }\n]"
const modelScopeGuideSelectTimeout = 12 * time.Second
const modelScopeGuideRollbackTimeout = 8 * time.Second
const modelScopeLoginURL = "https://www.modelscope.cn/"
const modelScopeAuthURL = "https://www.modelscope.cn/my/settings/account"
const modelScopeTokenURL = "https://www.modelscope.cn/my/access/token"

const sessionSwitchBusyMessage = "cannot switch sessions while run or compact is active"
const logViewerEntryLimit = 500
const logViewerPersistDebounce = 300 * time.Millisecond
const footerErrorFlashDuration = 8 * time.Second
const pasteTxnFlushDebounce = 140 * time.Millisecond
const pastedTextLoadDebounce = 180 * time.Millisecond
const duplicatePasteSuppressWindow = 1200 * time.Millisecond
const pasteSessionMinGuard = 2 * time.Second
const pasteSessionPerLineGuard = 8 * time.Millisecond
const inlineLogMarker = "[[neo-log]] "
const sessionWorkdirMissingWarning = "Session workspace not found, keeping current workspace."

type sessionLogPersistenceRuntime interface {
	LoadSessionLogEntries(ctx context.Context, sessionID string) ([]tuiservices.SessionLogEntry, error)
	SaveSessionLogEntries(ctx context.Context, sessionID string, entries []tuiservices.SessionLogEntry) error
}

var panelOrder = []panel{panelTranscript, panelInput}
var supportsUserEnvPersistence = config.SupportsUserEnvPersistence
var persistProviderUserEnvVar = config.PersistUserEnvVar
var deleteProviderUserEnvVar = config.DeleteUserEnvVar
var lookupProviderUserEnvVar = config.LookupUserEnvVar
var openExternalResource = tuiinfra.OpenExternalResource
var savePastedTextToTempFile = tuiinfra.SaveTextToTempFile

type startupWakeSubmitMsg struct {
	Input startupWakeSubmitInput
}

// emitStartupWakeSubmitCmd dispatches one startup wake submission message.
func emitStartupWakeSubmitCmd(input startupWakeSubmitInput) tea.Cmd {
	return func() tea.Msg {
		return startupWakeSubmitMsg{Input: input}
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var spinCmd tea.Cmd
	batchUpdateCmds := func() tea.Cmd {
		if a.deferredFooterTick != nil {
			cmds = append(cmds, a.deferredFooterTick)
			a.deferredFooterTick = nil
		}
		if a.deferredPastedTextLoadCmd != nil {
			cmds = append(cmds, a.deferredPastedTextLoadCmd)
			a.deferredPastedTextLoadCmd = nil
		}
		return tea.Batch(cmds...)
	}
	a.syncFooterErrorToast()
	a.spinner, spinCmd = a.spinner.Update(msg)
	if a.isBusy() {
		cmds = append(cmds, spinCmd)
	}
	if a.deferredLogPersistCmd != nil {
		cmds = append(cmds, a.deferredLogPersistCmd)
		a.deferredLogPersistCmd = nil
	}
	if a.deferredFooterTick != nil {
		cmds = append(cmds, a.deferredFooterTick)
		a.deferredFooterTick = nil
	}

	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = typed.Width
		a.height = typed.Height
		a.layoutCached = false
		a.applyComponentLayout(true)
		return a, batchUpdateCmds()
	case tickMsg:
		now := time.Time(typed)
		needNextTick := false

		if !a.footerErrorUntil.IsZero() && now.Before(a.footerErrorUntil) {
			needNextTick = true
		}
		if needNextTick {
			cmds = append(cmds, appTickCmd())
		}
		return a, batchUpdateCmds()
	case pasteTxnFlushMsg:
		if typed.Version != a.pasteTxnVersion || !a.pasteTxnActive {
			return a, batchUpdateCmds()
		}
		a.flushPasteTransaction()
		return a, batchUpdateCmds()
	case pastedTextLoadReadyMsg:
		if !a.loadingPastedText {
			return a, batchUpdateCmds()
		}
		now := a.now()
		if !a.shouldCompletePastedTextLoading(now) {
			a.deferredPastedTextLoadCmd = schedulePastedTextLoadReady()
			return a, batchUpdateCmds()
		}
		a.loadingPastedText = false
		if a.state.StatusText == statusLoadingPastedText {
			a.state.StatusText = statusReady
		}
		if current := a.input.Value(); strings.TrimSpace(current) != "" && !strings.HasSuffix(current, " ") {
			a.input.SetValue(current + " ")
			a.state.InputText = a.input.Value()
			a.normalizeComposerHeight()
			a.applyComponentLayout(false)
			a.refreshCommandMenu()
		}
		if !a.pendingSendAfterPasteLoad {
			return a, batchUpdateCmds()
		}
		a.pendingSendAfterPasteLoad = false
		a.skipNextSendPasteLoadWait = true
		if strings.TrimSpace(a.input.Value()) == "" && !a.hasImageAttachments() {
			a.skipNextSendPasteLoadWait = false
			return a, batchUpdateCmds()
		}
		enter := tea.KeyMsg{Type: tea.KeyEnter}
		return a.updateInputPanel(enter, enter, cmds)
	case startupWakeSubmitMsg:
		if cmd := a.handleStartupWakeSubmitMsg(typed); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, batchUpdateCmds()
	case providerAddResultMsg:
		a.handleProviderAddResultMsg(typed)
		return a, batchUpdateCmds()
	case modelScopeGuideOpenResultMsg:
		a.handleModelScopeGuideOpenResultMsg(typed)
		return a, batchUpdateCmds()
	case modelScopeGuideSubmitResultMsg:
		if cmd := a.handleModelScopeGuideSubmitResultMsg(typed); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, batchUpdateCmds()
	case RuntimeMsg:
		runtimeEvent, ok := typed.Event.(tuiservices.RuntimeEvent)
		if !ok {
			cmds = append(cmds, ListenForRuntimeEvent(a.runtime.Events()))
			return a, batchUpdateCmds()
		}
		transcriptDirty := a.handleRuntimeEvent(runtimeEvent)
		if a.deferredEventCmd != nil {
			cmds = append(cmds, a.deferredEventCmd)
			a.deferredEventCmd = nil
		}
		a.syncActiveSessionTitle()
		if transcriptDirty {
			a.rebuildTranscript()
		}
		if cmd := a.dispatchQueuedInterventionIfIdle(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, ListenForRuntimeEvent(a.runtime.Events()))
		return a, batchUpdateCmds()
	case logPersistFlushMsg:
		if typed.Version != a.logPersistVersion || !a.logPersistDirty {
			return a, batchUpdateCmds()
		}
		a.persistLogEntriesForActiveSession()
		return a, batchUpdateCmds()
	case RuntimeClosedMsg:
		a.state.IsAgentRunning = false
		a.state.StreamingReply = false
		a.state.CurrentTool = ""
		a.state.ActiveRunID = ""
		a.pendingPermission = nil
		a.pendingAutoPermission = nil
		a.clearRunProgress()
		a.state.IsCompacting = false
		if strings.TrimSpace(a.state.StatusText) == "" {
			a.state.StatusText = statusRuntimeClosed
		}
		if cmd := a.dispatchQueuedInterventionIfIdle(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, batchUpdateCmds()
	case runFinishedMsg:
		if typed.Err != nil {
			a.state.IsAgentRunning = false
			a.state.ActiveRunID = ""
			a.pendingPermission = nil
			a.pendingAutoPermission = nil
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
		if cmd := a.dispatchQueuedInterventionIfIdle(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, batchUpdateCmds()
	case permissionResolutionFinishedMsg:
		if a.handleAutoPermissionResolutionFinished(typed) {
			return a, batchUpdateCmds()
		}
		a.handlePermissionResolutionFinished(typed)
		return a, batchUpdateCmds()
	case modelCatalogRefreshMsg:
		if strings.EqualFold(a.modelRefreshID, typed.ProviderID) {
			a.modelRefreshID = ""
		}
		if !strings.EqualFold(strings.TrimSpace(a.state.CurrentProvider), strings.TrimSpace(typed.ProviderID)) {
			return a, batchUpdateCmds()
		}
		if typed.Err != nil {
			a.appendActivity("provider", "Failed to refresh models", typed.Err.Error(), true)
			return a, batchUpdateCmds()
		}

		replacePickerItems(&a.modelPicker, mapModelItems(typed.Models))
		cfg := a.configManager.Get()
		a.syncConfigState(cfg)
		selectPickerItemByID(&a.modelPicker, cfg.CurrentModel)
		return a, batchUpdateCmds()
	case compactFinishedMsg:
		a.state.IsCompacting = false
		if typed.Err != nil && strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = typed.Err.Error()
		}
		if err := a.refreshMessages(); err != nil && strings.TrimSpace(a.state.ActiveSessionID) != "" {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			a.appendInlineMessage(roleError, err.Error())
		}
		a.syncActiveSessionTitle()
		a.rebuildTranscript()
		a.transcript.GotoBottom()
		if cmd := a.dispatchQueuedInterventionIfIdle(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, batchUpdateCmds()
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
					return a, batchUpdateCmds()
				}
				if err := a.refreshModelPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh models", err.Error(), true)
					return a, batchUpdateCmds()
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
		return a, batchUpdateCmds()
	case skillCommandResultMsg:
		requestSessionID := strings.TrimSpace(typed.RequestSessionID)
		activeSessionID := strings.TrimSpace(a.state.ActiveSessionID)
		if requestSessionID != "" && !strings.EqualFold(requestSessionID, activeSessionID) {
			a.recordStaleSkillCommandResult(requestSessionID, activeSessionID, typed.Err)
			return a, batchUpdateCmds()
		}
		if typed.Err != nil {
			a.state.ExecutionError = typed.Err.Error()
			a.state.StatusText = typed.Err.Error()
			a.appendActivity("skills", "Skill command failed", typed.Err.Error(), true)
		} else {
			notice := strings.TrimSpace(typed.Notice)
			if notice == "" {
				notice = "Skill command completed."
			}
			a.state.ExecutionError = ""
			a.state.StatusText = notice
			a.appendInlineMessage(roleSystem, notice)
			a.appendActivity("skills", "Skill command completed", notice, false)
		}
		a.rebuildTranscript()
		return a, tea.Batch(cmds...)
	case tea.MouseMsg:
		if a.logViewerVisible && a.handleLogViewerMouse(typed) {
			return a, batchUpdateCmds()
		}
		if a.handleTranscriptMouse(typed) {
			return a, batchUpdateCmds()
		}
		if a.handleActivityMouse(typed) {
			return a, batchUpdateCmds()
		}
		if a.handleTodoMouse(typed) {
			return a, batchUpdateCmds()
		}
		if a.handleInputMouse(typed) {
			return a, batchUpdateCmds()
		}
	case tea.KeyMsg:
		if typed.Type == tea.KeyCtrlC && a.hasTextSelection() {
			a.copySelectionToClipboard()
			return a, batchUpdateCmds()
		}
		if key.Matches(typed, a.keys.Quit) {
			if a.hasTextSelection() {
				a.copySelectionToClipboard()
				return a, batchUpdateCmds()
			}
			return a, tea.Quit
		}
		if key.Matches(typed, a.keys.ToggleHelp) {
			a.refreshHelpPicker()
			a.openHelpPicker()
			return a, batchUpdateCmds()
		}
		if a.pendingFullAccessPrompt != nil {
			if cmd, handled := a.updatePendingFullAccessPromptInput(typed); handled {
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, batchUpdateCmds()
			}
		}
		if key.Matches(typed, a.keys.ToggleFullAccess) {
			a.toggleFullAccessMode()
			return a, batchUpdateCmds()
		}
		if a.state.IsAgentRunning && key.Matches(typed, a.keys.CancelAgent) {
			if a.runtime.CancelActiveRun() {
				a.state.StatusText = statusCanceling
			}
			return a, batchUpdateCmds()
		}
		if a.state.ActivePicker != pickerNone {
			return a.updatePicker(typed)
		}
		if a.logViewerVisible {
			if handled := a.handleLogViewerKey(typed); handled {
				return a, batchUpdateCmds()
			}
		}

		if a.focus == panelInput {
			if cmd, handled := a.updateCommandMenuSelection(typed); handled {
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, batchUpdateCmds()
			}
		}
		if a.focus == panelInput && key.Matches(typed, a.keys.NextPanel) {
			if a.applySelectedCommandSuggestion() {
				return a, batchUpdateCmds()
			}
			if a.shouldHandleTabAsInput(typed) {
				tabMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'	'}, Paste: typed.Paste}
				return a.updateInputPanel(tabMsg, tabMsg, cmds)
			}
		}
		if key.Matches(typed, a.keys.NextPanel) {
			a.focusNext()
			return a, batchUpdateCmds()
		}
		if key.Matches(typed, a.keys.PrevPanel) {
			a.focusPrev()
			return a, batchUpdateCmds()
		}
		if key.Matches(typed, a.keys.FocusInput) {
			a.clearTextSelection()
			a.focus = panelInput
			a.applyFocus()
			return a, batchUpdateCmds()
		}
		if key.Matches(typed, a.keys.NewSession) && !a.isBusy() {
			a.startDraftSession()
			a.startupScreenLocked = true
			return a, batchUpdateCmds()
		}
		if key.Matches(typed, a.keys.OpenWorkspace) {
			a.openFileBrowser()
			return a, batchUpdateCmds()
		}

		if key.Matches(typed, a.keys.LogViewer) {
			a.logViewerVisible = true
			a.logViewerOffset = 0
			a.viewDirty = true
			a.logViewerPrevStatus = strings.TrimSpace(a.state.StatusText)
			a.state.StatusText = "Log viewer"
			a.applyComponentLayout(false)
			return a, batchUpdateCmds()
		}

		switch a.focus {
		case panelTranscript:
			a.handleViewportKeys(&a.transcript, typed)
			return a, batchUpdateCmds()
		case panelActivity:
			a.handleViewportKeys(&a.activity, typed)
			return a, batchUpdateCmds()
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
			return a, batchUpdateCmds()
		case panelInput:
			return a.updateInputPanel(msg, typed, cmds)
		}
	}

	return a, batchUpdateCmds()
}

func (a App) updateInputPanel(msg tea.Msg, typed tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	now := a.now()
	effectiveTyped := typed
	batchUpdateCmds := func() tea.Cmd {
		if a.deferredFooterTick != nil {
			cmds = append(cmds, a.deferredFooterTick)
			a.deferredFooterTick = nil
		}
		if a.deferredPastedTextLoadCmd != nil {
			cmds = append(cmds, a.deferredPastedTextLoadCmd)
			a.deferredPastedTextLoadCmd = nil
		}
		return tea.Batch(cmds...)
	}

	if a.pendingPermission != nil {
		if cmd, handled := a.updatePendingPermissionInput(typed); handled {
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, batchUpdateCmds()
		}
	}
	if a.consumePendingCtrlVPasteEcho(typed, now) {
		return a, batchUpdateCmds()
	}
	if typed.Type == tea.KeyRunes && len(typed.Runes) > 0 && a.hasPendingCtrlVPasteEcho(now) {
		if strings.TrimSpace(a.pendingCtrlVPasteEcho) != "" || isPotentialCtrlVPasteEchoChunk(typed) {
			return a, batchUpdateCmds()
		}
	}
	if !typed.Paste && !a.pasteTxnActive && typed.Type == tea.KeyRunes && len(typed.Runes) > 0 && a.tryPrimePasteFromClipboard(typed, now) {
		return a, batchUpdateCmds()
	}
	if a.pasteTxnActive && !a.shouldCapturePasteTxnChunk(typed) {
		a.flushPasteTransaction()
	}
	if a.shouldCapturePasteTxnChunk(typed) {
		a.appendPasteTxnChunk(string(typed.Runes))
		cmds = append(cmds, schedulePasteTxnFlush(a.pasteTxnVersion))
		return a, batchUpdateCmds()
	}
	if key.Matches(typed, a.keys.Send) && a.pasteTxnActive {
		a.flushPasteTransaction()
	}

	if key.Matches(typed, a.keys.PasteImage) {
		if err := a.addImageFromClipboard(); err == nil {
			a.applyComponentLayout(false)
			a.refreshCommandMenu()
			return a, batchUpdateCmds()
		}
		// No image payload: proactively process clipboard text to avoid raw echo rendering.
		text, err := readClipboardText()
		if err != nil || strings.TrimSpace(text) == "" {
			return a, batchUpdateCmds()
		}
		pastedText := normalizeClipboardText(text)
		trimmed := strings.TrimSpace(pastedText)
		if handled, applyErr := a.applyPastedFileReferences(trimmed); handled {
			if applyErr != nil {
				a.state.StatusText = applyErr.Error()
				a.appendActivity("multimodal", "Failed to parse pasted file references", applyErr.Error(), true)
			}
			a.pendingCtrlVPasteEcho = pastedText
			a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
			return a, batchUpdateCmds()
		}
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pastedText), Paste: true}
		typed = msg.(tea.KeyMsg)
		effectiveTyped = typed
		a.pendingCtrlVPasteEcho = pastedText
		a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
	}

	if typed.Paste {
		pastedText := normalizeClipboardText(string(typed.Runes))
		clipboardText := ""
		if clipText, err := readClipboardText(); err == nil {
			clipboardText = normalizeClipboardText(clipText)
		}
		if strings.TrimSpace(clipboardText) != "" &&
			(strings.TrimSpace(pastedText) == "" || strings.Contains(clipboardText, pastedText) || strings.Contains(pastedText, clipboardText)) {
			pastedText = clipboardText
			a.pendingCtrlVPasteEcho = clipboardText
			a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
		}
		trimmed := strings.TrimSpace(pastedText)
		if trimmed == "" {
			// Some terminals emit an empty paste event for image-only clipboard payloads.
			if err := a.addImageFromClipboard(); err == nil {
				a.applyComponentLayout(false)
				a.refreshCommandMenu()
				return a, batchUpdateCmds()
			}
			// Empty paste with no resolvable clipboard payload: ignore silently.
			return a, batchUpdateCmds()
		}
		if handled, err := a.applyPastedFileReferences(trimmed); handled {
			if err != nil {
				a.state.StatusText = err.Error()
				a.appendActivity("multimodal", "Failed to parse pasted file references", err.Error(), true)
			}
			return a, batchUpdateCmds()
		}
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pastedText), Paste: true}
		typed = msg.(tea.KeyMsg)
		effectiveTyped = typed
	}
	if key.Matches(typed, a.keys.Send) {
		currentInput := a.input.Value()
		needsPasteLoad := a.shouldDeferSendForPastedTextLoad(currentInput)
		if a.pendingSendAfterPasteLoad && !a.loadingPastedText {
			a.pendingSendAfterPasteLoad = false
		}
		if a.pendingSendAfterPasteLoad {
			return a, batchUpdateCmds()
		}
		if !a.skipNextSendPasteLoadWait && a.loadingPastedText && needsPasteLoad {
			a.pendingSendAfterPasteLoad = true
			a.state.StatusText = statusLoadingPastedText
			return a, batchUpdateCmds()
		}
		if a.loadingPastedText && !needsPasteLoad {
			a.loadingPastedText = false
			if a.state.StatusText == statusLoadingPastedText {
				a.state.StatusText = statusReady
			}
		}
		a.skipNextSendPasteLoadWait = false
		if a.shouldTreatEnterAsNewline(typed, now) {
			a.growComposerForNewline()
			msg = tea.KeyMsg{Type: tea.KeyEnter}
			effectiveTyped = tea.KeyMsg{Type: tea.KeyEnter, Paste: true}
		} else {
			rawInput := currentInput
			resolvedInput, resolveErr := a.resolvePendingTextPastes(rawInput)
			if resolveErr != nil {
				a.state.ExecutionError = resolveErr.Error()
				a.state.StatusText = resolveErr.Error()
				a.appendActivity("multimodal", "Failed to resolve pasted content", resolveErr.Error(), true)
				return a, batchUpdateCmds()
			}
			rawInput = resolvedInput
			hasImages := a.hasImageAttachments()
			if strings.TrimSpace(rawInput) == "" && !hasImages {
				return a, batchUpdateCmds()
			}
			input := strings.TrimSpace(rawInput)
			images := a.collectPendingImageInputs()

			if a.isBusy() {
				a.queueInterventionInput(input, images)
				a.rebuildTranscript()
				a.input.Reset()
				a.state.InputText = ""
				a.clearComposerAttachments()
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()

				if a.state.IsAgentRunning && a.runtime.CancelActiveRun() {
					a.state.StatusText = statusInterventionCanceling
					a.appendActivity("run", "Queued intervention", "Canceling current run", false)
				} else {
					a.state.StatusText = statusInterventionQueued
					a.appendActivity("run", "Queued intervention", "Will run after current task", false)
				}
				return a, batchUpdateCmds()
			}

			if handled, cmd := a.handleImmediateSlashCommand(input); handled {
				a.input.Reset()
				a.state.InputText = ""
				a.clearPendingTextPastes()
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, batchUpdateCmds()
			}

			if isImageReferenceInput(input) {
				if err := a.applyImageReference(input); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("multimodal", "Failed to add image reference", err.Error(), true)
				}
				a.input.Reset()
				a.state.InputText = ""
				a.clearPendingTextPastes()
				a.applyComponentLayout(true)
				a.refreshCommandMenu()
				a.resetPasteHeuristics()
				return a, batchUpdateCmds()
			}

			a.input.Reset()
			a.state.InputText = ""
			a.clearPendingTextPastes()
			a.applyComponentLayout(true)
			a.refreshCommandMenu()
			a.resetPasteHeuristics()
			switch strings.ToLower(input) {
			case slashCommandHelp:
				a.refreshHelpPicker()
				a.openHelpPicker()
				return a, batchUpdateCmds()
			case slashCommandProvider:
				if err := a.refreshProviderPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
					return a, batchUpdateCmds()
				}
				a.openProviderPicker()
				return a, batchUpdateCmds()
			case slashCommandModelPick:
				if err := a.refreshModelPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh models", err.Error(), true)
					return a, batchUpdateCmds()
				}
				a.openModelPicker()
				if cmd := a.requestModelCatalogRefresh(a.state.CurrentProvider); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return a, batchUpdateCmds()
			case slashCommandSession:
				if err := a.ensureSessionSwitchAllowed(""); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("session", "Failed to open session picker", err.Error(), true)
					return a, batchUpdateCmds()
				}
				if err := a.refreshSessionPicker(); err != nil {
					a.state.ExecutionError = err.Error()
					a.state.StatusText = err.Error()
					a.appendActivity("system", "Failed to refresh sessions", err.Error(), true)
					return a, batchUpdateCmds()
				}
				a.openPicker(pickerSession, statusChooseSession, &a.sessionPicker, a.state.ActiveSessionID)
				return a, batchUpdateCmds()
			case slashCommandProviderAdd:
				a.startProviderAddForm()
				return a, batchUpdateCmds()
			}

			if strings.HasPrefix(input, slashPrefix) {
				a.state.StatusText = statusApplyingCommand
				cmds = append(cmds, runLocalCommand(a.configManager, a.providerSvc, a.currentStatusSnapshot(), input))
				return a, tea.Batch(cmds...)
			}
			normalizedInput, absorbedImages, err := a.absorbInlineImageReferences(input)
			if err != nil {
				a.state.ExecutionError = err.Error()
				a.state.StatusText = err.Error()
				a.appendActivity("multimodal", "Failed to absorb inline image reference", err.Error(), true)
				return a, batchUpdateCmds()
			}
			if absorbedImages > 0 {
				input = normalizedInput
			}
			images = a.collectPendingImageInputs()

			// image capability precheck is intentionally disabled.
			// Keep CLI behavior consistent and let runtime own capability validation.
			if cmd := a.beginAgentRun(input, images); cmd != nil {
				cmds = append(cmds, cmd)
			}
			a.clearComposerAttachments()
			return a, batchUpdateCmds()
		}
	}

	if key.Matches(typed, a.keys.Newline) {
		a.growComposerForNewline()
		msg = tea.KeyMsg{Type: tea.KeyEnter}
		effectiveTyped = tea.KeyMsg{Type: tea.KeyEnter}
	}
	var skipInputUpdate bool
	msg, effectiveTyped, skipInputUpdate = a.maybeCollapseLongPaste(msg, effectiveTyped, now)
	if skipInputUpdate {
		return a, batchUpdateCmds()
	}
	before := a.input.Value()
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.state.InputText = a.input.Value()
	a.noteInputEdit(before, a.state.InputText, effectiveTyped, now)
	a.maybeCollapseLongPasteAfterInput(before, effectiveTyped)
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	cmds = append(cmds, cmd)
	return a, batchUpdateCmds()
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

func (a *App) submitPermissionDecision(decision tuiservices.PermissionResolutionDecision) tea.Cmd {
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

// toggleFullAccessMode 处理 Full Access 模式的启停切换；启用前必须经过风险确认?
func (a *App) toggleFullAccessMode() {
	if a.fullAccessModeEnabled {
		a.disableFullAccessMode()
		return
	}

	a.openFullAccessPrompt()
}

// updatePendingFullAccessPromptInput 处理 Full Access 风险确认弹窗的键盘交互?
func (a *App) updatePendingFullAccessPromptInput(typed tea.KeyMsg) (tea.Cmd, bool) {
	if a.pendingFullAccessPrompt == nil {
		return nil, false
	}

	switch {
	case key.Matches(typed, a.keys.ScrollUp):
		a.pendingFullAccessPrompt.Selected = normalizeFullAccessPromptSelection(a.pendingFullAccessPrompt.Selected - 1)
		a.state.StatusText = statusFullAccessPrompt
		return nil, true
	case key.Matches(typed, a.keys.ScrollDown):
		a.pendingFullAccessPrompt.Selected = normalizeFullAccessPromptSelection(a.pendingFullAccessPrompt.Selected + 1)
		a.state.StatusText = statusFullAccessPrompt
		return nil, true
	case key.Matches(typed, a.keys.Send):
		option := fullAccessPromptOptionAt(a.pendingFullAccessPrompt.Selected)
		return a.applyFullAccessPromptSelection(option.Enable), true
	case key.Matches(typed, a.keys.FocusInput):
		return a.applyFullAccessPromptSelection(false), true
	}

	if typed.Type == tea.KeyRunes && len(typed.Runes) > 0 {
		if enable, ok := parseFullAccessPromptShortcut(string(typed.Runes)); ok {
			return a.applyFullAccessPromptSelection(enable), true
		}
	}
	return nil, true
}

// applyFullAccessPromptSelection 根据风险确认结果更新 Full Access 模式，并按需自动处理待审批请求?
func (a *App) applyFullAccessPromptSelection(enable bool) tea.Cmd {
	a.pendingFullAccessPrompt = nil
	if !enable {
		a.state.StatusText = statusFullAccessCanceled
		a.state.ExecutionError = ""
		a.appendActivity("permission", "Full access enable canceled", "", false)
		a.refreshPermissionPromptLayout()
		return nil
	}

	a.fullAccessModeEnabled = true
	a.state.StatusText = statusFullAccessEnabled
	a.state.ExecutionError = ""
	a.appendActivity("permission", "Full access mode enabled", "All upcoming tool requests will be auto-approved", false)
	a.refreshPermissionPromptLayout()

	if a.pendingPermission != nil && !a.pendingPermission.Submitting {
		return a.submitPermissionDecision(tuiservices.DecisionAllowSession)
	}
	return nil
}

// openFullAccessPrompt 打开 Full Access 风险确认弹窗，并将输入焦点收回输入区。
func (a *App) openFullAccessPrompt() {
	a.pendingFullAccessPrompt = &fullAccessPromptState{Selected: 0}
	a.focus = panelInput
	a.applyFocus()
	a.state.StatusText = statusFullAccessPrompt
	a.state.ExecutionError = ""
	a.appendActivity("permission", "Full access risk prompt opened", "Press Y to enable, N to cancel", false)
	a.refreshPermissionPromptLayout()
}

// disableFullAccessMode 关闭 Full Access 模式并刷新提示区布局?
func (a *App) disableFullAccessMode() {
	a.fullAccessModeEnabled = false
	a.pendingFullAccessPrompt = nil
	a.state.StatusText = statusFullAccessDisabled
	a.state.ExecutionError = ""
	a.appendActivity("permission", "Full access mode disabled", "", false)
	a.refreshPermissionPromptLayout()
}

// handleAutoPermissionResolutionFinished 处理 Full Access 自动审批回执，并在失败时回退到手动审批?
func (a *App) handleAutoPermissionResolutionFinished(msg permissionResolutionFinishedMsg) bool {
	if a.pendingAutoPermission == nil || a.pendingAutoPermission.Request.RequestID != msg.RequestID {
		return false
	}

	request := a.pendingAutoPermission.Request
	a.pendingAutoPermission = nil
	if msg.Err != nil {
		a.pendingPermission = &permissionPromptState{
			Request:  request,
			Selected: normalizePermissionPromptSelection(1),
		}
		a.focus = panelInput
		a.applyFocus()
		a.state.ExecutionError = msg.Err.Error()
		a.state.StatusText = statusPermissionRequired
		a.appendActivity("permission", "Full access auto-approval failed", msg.Err.Error(), true)
		a.refreshPermissionPromptLayout()
		return true
	}

	a.state.ExecutionError = ""
	a.state.StatusText = statusPermissionSubmitted
	a.appendActivity("permission", "Full access auto-approval submitted", string(msg.Decision), false)
	return true
}

// handlePermissionResolutionFinished 更新手动审批提交流程的成功或失败状态?
func (a *App) handlePermissionResolutionFinished(msg permissionResolutionFinishedMsg) {
	if a.pendingPermission == nil || a.pendingPermission.Request.RequestID != msg.RequestID {
		return
	}

	if msg.Err != nil {
		a.pendingPermission.Submitting = false
		a.state.ExecutionError = msg.Err.Error()
		a.state.StatusText = msg.Err.Error()
		a.appendActivity("permission", "Permission decision submit failed", msg.Err.Error(), true)
		return
	}

	a.pendingPermission = nil
	a.state.ExecutionError = ""
	a.state.StatusText = statusPermissionSubmitted
	a.appendActivity("permission", "Permission decision submitted", string(msg.Decision), false)
	a.refreshPermissionPromptLayout()
}

func (a App) now() time.Time {
	if a.nowFn == nil {
		return time.Now()
	}
	return a.nowFn()
}

type logPersistFlushMsg struct {
	Version int
}

type pasteTxnFlushMsg struct {
	Version int
}

type pastedTextLoadReadyMsg struct{}

// scheduleLogPersistFlush triggers log persistence with debounce.
func scheduleLogPersistFlush(version int) tea.Cmd {
	return tea.Tick(logViewerPersistDebounce, func(time.Time) tea.Msg {
		return logPersistFlushMsg{Version: version}
	})
}

func schedulePasteTxnFlush(version int) tea.Cmd {
	return tea.Tick(pasteTxnFlushDebounce, func(time.Time) tea.Msg {
		return pasteTxnFlushMsg{Version: version}
	})
}

func schedulePastedTextLoadReady() tea.Cmd {
	return tea.Tick(pastedTextLoadDebounce, func(time.Time) tea.Msg {
		return pastedTextLoadReadyMsg{}
	})
}

func (a *App) beginPastedTextLoading() {
	if !a.loadingPastedText {
		a.deferredPastedTextLoadCmd = schedulePastedTextLoadReady()
	}
	a.loadingPastedText = true
	a.state.StatusText = statusLoadingPastedText
	a.pendingSendAfterPasteLoad = false
	a.skipNextSendPasteLoadWait = false
}

func (a *App) shouldCompletePastedTextLoading(now time.Time) bool {
	if a.pasteTxnActive {
		return false
	}
	if a.hasPendingCtrlVPasteEcho(now) {
		return false
	}
	// Once paste stream and echo settle, pending tokens become resolvable.
	a.markPendingTextPastesLoaded()
	// Keep loading if there are still unresolved tokens in the current input.
	if a.shouldDeferSendForPastedTextLoad(a.input.Value()) {
		return false
	}
	return true
}

func (a *App) markPendingTextPastesLoaded() {
	if len(a.pendingTextPastes) == 0 {
		return
	}
	for i := range a.pendingTextPastes {
		a.pendingTextPastes[i].Loaded = true
	}
}

func (a *App) extendPasteSession(now time.Time, lineCount int) {
	if lineCount < 1 {
		lineCount = 1
	}
	window := pasteSessionMinGuard + time.Duration(lineCount)*pasteSessionPerLineGuard
	if a.pasteSessionStartedAt.IsZero() || now.After(a.pasteSessionUntil) {
		a.pasteSessionStartedAt = now
	}
	until := a.pasteSessionStartedAt.Add(window)
	if until.After(a.pasteSessionUntil) {
		a.pasteSessionUntil = until
	}
	a.lastPasteLikeAt = now
}

func (a App) inPasteSessionWindow(now time.Time) bool {
	if a.pasteTxnActive || a.hasPendingCtrlVPasteEcho(now) {
		return true
	}
	return !a.pasteSessionUntil.IsZero() && now.Before(a.pasteSessionUntil)
}

func (a *App) shouldTreatEnterAsNewline(typed tea.KeyMsg, now time.Time) bool {
	if !key.Matches(typed, a.keys.Send) {
		return false
	}
	if typed.Paste {
		a.pasteMode = true
		a.extendPasteSession(now, countPasteLines(normalizeClipboardText(string(typed.Runes))))
		return true
	}
	if a.pasteMode &&
		a.inPasteSessionWindow(now) &&
		!a.lastInputEditAt.IsZero() &&
		now.Sub(a.lastInputEditAt) <= pasteEnterGuard {
		return true
	}
	if a.pasteMode && !a.inPasteSessionWindow(now) {
		a.pasteMode = false
		a.pasteSessionBase = ""
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
		if a.pasteSessionBase == "" {
			a.pasteSessionBase = before
		}
		lineHint := countPasteLines(normalizeClipboardText(string(typed.Runes)))
		if lineHint < 1 {
			lineHint = countPasteLines(normalizeClipboardText(after))
		}
		a.extendPasteSession(now, lineHint)
		a.pasteMode = true
	} else if !a.pasteMode {
		a.pasteSessionBase = ""
	}
}

func (a *App) maybeCollapseLongPaste(msg tea.Msg, typed tea.KeyMsg, now time.Time) (tea.Msg, tea.KeyMsg, bool) {
	if typed.Type != tea.KeyRunes || !typed.Paste || len(typed.Runes) == 0 {
		return msg, typed, false
	}

	content := normalizeClipboardText(string(typed.Runes))
	if token, ok := parsePasteSummaryToken(content); ok && strings.HasSuffix(a.input.Value(), token) {
		return msg, typed, true
	}
	if a.shouldSuppressDuplicatePasteContent(content, now) {
		return msg, typed, true
	}
	summarized, _, err := a.summarizePastedText(content)
	if err != nil {
		a.state.StatusText = err.Error()
		a.appendActivity("multimodal", "Failed to summarize pasted content", err.Error(), true)
		return msg, typed, false
	}
	if summarized == content {
		a.recordRecentPastedContent(content, now)
		return msg, typed, false
	}
	if token, ok := parsePasteSummaryToken(summarized); ok && strings.HasSuffix(a.input.Value(), token) {
		return msg, typed, true
	}
	next := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(summarized), Paste: true}
	return next, next, false
}

func (a *App) maybeCollapseLongPasteAfterInput(before string, typed tea.KeyMsg) {
	after := a.input.Value()
	if strings.TrimSpace(after) == "" {
		return
	}

	if a.pasteSessionBase != "" {
		if a.tryCollapseInsertedSegment(a.pasteSessionBase, after) {
			return
		}
	}

	if !shouldTreatInputDeltaAsPaste(typed, a.pasteMode) {
		return
	}
	a.tryCollapseInsertedSegment(before, after)
}

func (a *App) tryCollapseInsertedSegment(before string, after string) bool {
	prefix, inserted, suffix, ok := extractInsertedSegment(before, after)
	if !ok {
		return false
	}

	summarized, summarizedOK, err := a.summarizePastedText(inserted)
	if err != nil {
		a.state.StatusText = err.Error()
		a.appendActivity("multimodal", "Failed to summarize pasted content", err.Error(), true)
		return false
	}
	if !summarizedOK {
		return false
	}

	collapsed := prefix + summarized + suffix
	a.input.SetValue(collapsed)
	a.state.InputText = collapsed
	a.pasteSessionBase = collapsed
	return true
}

func (a *App) summarizePastedText(content string) (text string, summarized bool, err error) {
	normalized := normalizeClipboardText(content)
	if !shouldSummarizePastedText(normalized) {
		return normalized, false, nil
	}
	lineCount := countPasteLines(normalized)
	token := formatPasteSummaryToken(lineCount)
	now := a.now()
	if pinned, ok := a.reuseSinglePasteSessionToken(now); ok {
		a.beginPastedTextLoading()
		a.extendPasteSession(now, lineCount)
		a.recordRecentPastedContent(normalized, now)
		a.lastSummarizedPasteText = normalized
		a.lastSummarizedPasteAt = now
		a.lastSummarizedPasteToken = pinned
		return pinned, true, nil
	}
	if a.shouldReusePasteSummaryToken(token, normalized, now) {
		a.beginPastedTextLoading()
		a.extendPasteSession(now, lineCount)
		a.recordRecentPastedContent(normalized, now)
		a.lastSummarizedPasteText = normalized
		a.lastSummarizedPasteAt = now
		a.lastSummarizedPasteToken = token
		a.markPasteSessionToken(token)
		return token, true, nil
	}
	path, saveErr := savePastedTextToTempFile(normalized, "paste")
	if saveErr != nil {
		return "", false, fmt.Errorf("failed to persist pasted text: %w", saveErr)
	}
	a.pendingTextPastes = append(a.pendingTextPastes, pendingTextPaste{
		Token:     token,
		FilePath:  path,
		LineCount: lineCount,
	})
	a.beginPastedTextLoading()
	a.extendPasteSession(now, lineCount)
	a.recordRecentPastedContent(normalized, now)
	a.lastSummarizedPasteText = normalized
	a.lastSummarizedPasteAt = now
	a.lastSummarizedPasteToken = token
	a.markPasteSessionToken(token)
	return token, true, nil
}

func (a *App) reuseSinglePasteSessionToken(now time.Time) (string, bool) {
	token := strings.TrimSpace(a.pasteTxnInjectedToken)
	if !a.pasteTxnTokenInjected || token == "" {
		return "", false
	}
	if !a.isInPinnedPasteSession(now) {
		return "", false
	}
	if !a.hasPendingTextPasteToken(token) {
		return "", false
	}
	return token, true
}

func (a *App) markPasteSessionToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	a.pasteTxnTokenInjected = true
	a.pasteTxnInjectedToken = token
}

func (a App) isInPinnedPasteSession(now time.Time) bool {
	return a.inPasteSessionWindow(now)
}

func (a App) shouldSuppressDuplicatePasteContent(content string, now time.Time) bool {
	normalized := normalizeClipboardText(content)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	if !shouldSummarizePastedText(normalized) {
		if a.lastPastedContentAt.IsZero() || now.Sub(a.lastPastedContentAt) > duplicatePasteSuppressWindow {
			return false
		}
		if normalized != a.lastPastedContent {
			return false
		}
		return strings.HasSuffix(a.input.Value(), normalized)
	}
	token := formatPasteSummaryToken(countPasteLines(normalized))
	if token == "" {
		return false
	}
	if strings.HasSuffix(a.input.Value(), token) {
		// In one paste transaction, terminals may emit multiple chunks with slightly different
		// payload boundaries (e.g. trailing newline differences). Once the token is already
		// visible at the cursor, suppress additional injections in the same paste session.
		if a.pasteTxnActive || a.hasPendingCtrlVPasteEcho(now) {
			return true
		}
		if a.inPasteSessionWindow(now) {
			return true
		}
		if token == a.lastSummarizedPasteToken &&
			!a.lastSummarizedPasteAt.IsZero() &&
			now.Sub(a.lastSummarizedPasteAt) <= duplicatePasteSuppressWindow {
			return true
		}
	}
	if a.lastSummarizedPasteAt.IsZero() || now.Sub(a.lastSummarizedPasteAt) > duplicatePasteSuppressWindow {
		return false
	}
	if normalized != a.lastSummarizedPasteText {
		return false
	}
	return token == a.lastSummarizedPasteToken && strings.HasSuffix(a.input.Value(), token)
}

func formatPasteSummaryToken(lineCount int) string {
	return fmt.Sprintf("[paste %d LINE]", lineCount)
}

func parsePasteSummaryToken(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "[paste ") || !strings.HasSuffix(trimmed, " LINE]") {
		return "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "[paste "), " LINE]")
	if body == "" {
		return "", false
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return trimmed, true
}

func (a App) shouldReusePasteSummaryToken(token string, normalized string, now time.Time) bool {
	if token == "" || !strings.HasSuffix(a.input.Value(), token) || !a.hasPendingTextPasteToken(token) {
		return false
	}
	if normalized == a.lastSummarizedPasteText &&
		token == a.lastSummarizedPasteToken &&
		!a.lastSummarizedPasteAt.IsZero() &&
		now.Sub(a.lastSummarizedPasteAt) <= pasteSessionGuard {
		return true
	}
	if a.pasteTxnActive || a.hasPendingCtrlVPasteEcho(now) {
		return true
	}
	return a.inPasteSessionWindow(now)
}

func (a App) hasPendingTextPasteToken(token string) bool {
	for _, pending := range a.pendingTextPastes {
		if pending.Token == token {
			return true
		}
	}
	return false
}

func shouldSummarizePastedText(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	return countPasteLines(trimmed) > 1
}

func countPasteLines(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func (a *App) resolvePendingTextPastes(input string) (string, error) {
	if strings.TrimSpace(input) == "" || len(a.pendingTextPastes) == 0 {
		return input, nil
	}

	expanded := input
	for _, pending := range a.pendingTextPastes {
		if pending.Token == "" || pending.FilePath == "" {
			continue
		}
		if !strings.Contains(expanded, pending.Token) {
			continue
		}
		data, err := os.ReadFile(pending.FilePath)
		if err != nil {
			return "", fmt.Errorf("failed to read pasted content: %w", err)
		}
		expanded = strings.Replace(expanded, pending.Token, string(data), 1)
	}
	return expanded, nil
}

func (a *App) clearPendingTextPastes() {
	if len(a.pendingTextPastes) == 0 {
		return
	}
	for _, pending := range a.pendingTextPastes {
		if strings.TrimSpace(pending.FilePath) == "" {
			continue
		}
		_ = os.Remove(pending.FilePath)
	}
	a.pendingTextPastes = nil
}

func (a *App) clearComposerAttachments() {
	a.clearImageAttachments()
	a.clearPendingTextPastes()
}

func (a App) shouldDeferSendForPastedTextLoad(input string) bool {
	if strings.TrimSpace(input) == "" || len(a.pendingTextPastes) == 0 {
		return false
	}
	for _, pending := range a.pendingTextPastes {
		if pending.Token == "" || pending.Loaded {
			continue
		}
		if strings.Contains(input, pending.Token) {
			return true
		}
	}
	return false
}

func shouldTreatInputDeltaAsPaste(typed tea.KeyMsg, pasteMode bool) bool {
	if typed.Paste || pasteMode {
		return true
	}
	return typed.Type == tea.KeyRunes && len(typed.Runes) > 1
}

func extractInsertedSegment(before string, after string) (prefix string, inserted string, suffix string, ok bool) {
	beforeRunes := []rune(before)
	afterRunes := []rune(after)
	if len(afterRunes) <= len(beforeRunes) {
		return "", "", "", false
	}

	prefixEnd := 0
	for prefixEnd < len(beforeRunes) && prefixEnd < len(afterRunes) && beforeRunes[prefixEnd] == afterRunes[prefixEnd] {
		prefixEnd++
	}

	beforeTail := len(beforeRunes) - 1
	afterTail := len(afterRunes) - 1
	for beforeTail >= prefixEnd && afterTail >= prefixEnd && beforeRunes[beforeTail] == afterRunes[afterTail] {
		beforeTail--
		afterTail--
	}
	if afterTail < prefixEnd {
		return "", "", "", false
	}

	return string(afterRunes[:prefixEnd]),
		string(afterRunes[prefixEnd : afterTail+1]),
		string(afterRunes[afterTail+1:]),
		true
}

func normalizeClipboardText(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(normalized, "\r", "\n")
}

func (a *App) resetPasteHeuristics() {
	a.lastInputEditAt = time.Time{}
	a.lastPasteLikeAt = time.Time{}
	a.pasteSessionStartedAt = time.Time{}
	a.pasteSessionUntil = time.Time{}
	a.pendingCtrlVPasteEcho = ""
	a.pendingCtrlVEchoUntil = time.Time{}
	a.deferredPastedTextLoadCmd = nil
	a.loadingPastedText = false
	a.pendingSendAfterPasteLoad = false
	a.skipNextSendPasteLoadWait = false
	a.lastPastedContent = ""
	a.lastPastedContentAt = time.Time{}
	a.pasteTxnActive = false
	a.pasteTxnBuffer = ""
	a.pasteTxnVersion = 0
	a.pasteTxnTokenInjected = false
	a.pasteTxnInjectedToken = ""
	a.inputBurstStart = time.Time{}
	a.inputBurstCount = 0
	a.pasteMode = false
	a.pasteSessionBase = ""
}

func (a *App) consumePendingCtrlVPasteEcho(typed tea.KeyMsg, now time.Time) bool {
	if a.pendingCtrlVPasteEcho == "" && (a.pendingCtrlVEchoUntil.IsZero() || now.After(a.pendingCtrlVEchoUntil)) {
		return false
	}
	if !a.pendingCtrlVEchoUntil.IsZero() && now.After(a.pendingCtrlVEchoUntil) {
		a.pendingCtrlVPasteEcho = ""
		a.pendingCtrlVEchoUntil = time.Time{}
		return false
	}
	if typed.Type == tea.KeyEnter {
		if strings.TrimSpace(a.pendingCtrlVPasteEcho) == "" {
			return false
		}
		return true
	}
	if typed.Type != tea.KeyRunes || len(typed.Runes) == 0 {
		return false
	}
	chunk := normalizeClipboardText(string(typed.Runes))
	if chunk == "" {
		return false
	}

	// During the immediate post-Ctrl+V window, aggressively swallow multi-rune echoes.
	likelyEchoChunk := len(typed.Runes) > 1 || strings.ContainsRune(chunk, '\n') || strings.ContainsRune(chunk, '\t')
	if a.pendingCtrlVPasteEcho == "" {
		if likelyEchoChunk {
			return true
		}
		return false
	}

	remaining := a.pendingCtrlVPasteEcho
	if strings.HasPrefix(remaining, chunk) {
		a.pendingCtrlVPasteEcho = strings.TrimPrefix(remaining, chunk)
		if a.pendingCtrlVPasteEcho == "" {
			a.pendingCtrlVEchoUntil = now.Add(300 * time.Millisecond)
		}
		return true
	}
	if matchPasteEchoLoosely(remaining, chunk) {
		a.pendingCtrlVPasteEcho = trimPrefixByRuneCount(remaining, len([]rune(chunk)))
		if a.pendingCtrlVPasteEcho == "" {
			a.pendingCtrlVEchoUntil = now.Add(300 * time.Millisecond)
		}
		return true
	}
	if likelyEchoChunk {
		a.pendingCtrlVPasteEcho = trimPrefixByRuneCount(remaining, len([]rune(chunk)))
		if a.pendingCtrlVPasteEcho == "" {
			a.pendingCtrlVEchoUntil = now.Add(300 * time.Millisecond)
		}
		return true
	}
	return false
}

func (a App) hasPendingCtrlVPasteEcho(now time.Time) bool {
	if strings.TrimSpace(a.pendingCtrlVPasteEcho) != "" {
		return true
	}
	return !a.pendingCtrlVEchoUntil.IsZero() && now.Before(a.pendingCtrlVEchoUntil)
}

func (a *App) recordRecentPastedContent(content string, now time.Time) {
	normalized := normalizeClipboardText(content)
	if strings.TrimSpace(normalized) == "" {
		return
	}
	a.lastPastedContent = normalized
	a.lastPastedContentAt = now
}

func trimPrefixByRuneCount(content string, runes int) string {
	if runes <= 0 || content == "" {
		return content
	}
	rs := []rune(content)
	if runes >= len(rs) {
		return ""
	}
	return string(rs[runes:])
}

func normalizeEchoWhitespace(content string) string {
	normalized := normalizeClipboardText(content)
	normalized = strings.ReplaceAll(normalized, "\t", " ")
	return strings.Join(strings.Fields(normalized), " ")
}

func matchPasteEchoLoosely(expected string, chunk string) bool {
	if strings.HasPrefix(chunk, expected) {
		return true
	}
	expectedNorm := normalizeEchoWhitespace(expected)
	chunkNorm := normalizeEchoWhitespace(chunk)
	if expectedNorm == "" || chunkNorm == "" {
		return false
	}
	return strings.HasPrefix(expectedNorm, chunkNorm) || strings.HasPrefix(chunkNorm, expectedNorm)
}

func isPotentialCtrlVPasteEchoChunk(typed tea.KeyMsg) bool {
	if typed.Type != tea.KeyRunes || len(typed.Runes) == 0 {
		return false
	}
	chunk := normalizeClipboardText(string(typed.Runes))
	if chunk == "" {
		return false
	}
	return len(typed.Runes) > 1 ||
		strings.ContainsRune(chunk, '\n') ||
		strings.ContainsRune(chunk, '\r') ||
		strings.ContainsRune(chunk, '\t')
}

func (a *App) tryPrimePasteFromClipboard(typed tea.KeyMsg, now time.Time) bool {
	if typed.Type != tea.KeyRunes || len(typed.Runes) == 0 {
		return false
	}
	if !isPotentialCtrlVPasteEchoChunk(typed) {
		return false
	}
	chunk := normalizeClipboardText(string(typed.Runes))
	if chunk == "" {
		return false
	}
	clip, err := readClipboardText()
	if err != nil {
		return false
	}
	clip = normalizeClipboardText(clip)
	if strings.TrimSpace(clip) == "" {
		return false
	}
	if !(strings.HasPrefix(clip, chunk) || matchPasteEchoLoosely(clip, chunk)) {
		return false
	}

	trimmed := strings.TrimSpace(clip)
	if handled, err := a.applyPastedFileReferences(trimmed); handled {
		if err != nil {
			a.state.StatusText = err.Error()
			a.appendActivity("multimodal", "Failed to parse pasted file references", err.Error(), true)
		}
		a.pendingCtrlVPasteEcho = clip
		a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
		return true
	}
	if a.shouldSuppressDuplicatePasteContent(clip, now) {
		a.pendingCtrlVPasteEcho = clip
		a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
		return true
	}

	insert := clip
	if summarized, ok, summarizeErr := a.summarizePastedText(insert); summarizeErr == nil {
		if ok {
			insert = summarized
		}
	} else {
		a.state.StatusText = summarizeErr.Error()
		a.appendActivity("multimodal", "Failed to summarize pasted content", summarizeErr.Error(), true)
	}
	before := a.input.Value()
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(insert), Paste: true})
	_ = cmd
	a.state.InputText = a.input.Value()
	a.noteInputEdit(before, a.state.InputText, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(insert), Paste: true}, now)
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	a.recordRecentPastedContent(insert, now)

	a.pendingCtrlVPasteEcho = clip
	a.pendingCtrlVEchoUntil = now.Add(2 * time.Second)
	return true
}

func (a App) shouldCapturePasteTxnChunk(typed tea.KeyMsg) bool {
	if typed.Type != tea.KeyRunes || len(typed.Runes) == 0 {
		return false
	}
	if typed.Paste {
		return false
	}
	chunk := string(typed.Runes)
	if a.pasteTxnActive {
		// Do not lock the input while a paste transaction is open:
		// only keep capturing obviously paste-like chunks.
		if len(typed.Runes) > 1 {
			return true
		}
		return strings.ContainsRune(chunk, '\n') || strings.ContainsRune(chunk, '\r') || strings.ContainsRune(chunk, '\t')
	}
	if len(typed.Runes) > 1 {
		return true
	}
	if a.shouldCaptureSingleRunePasteChunk(typed) {
		return true
	}
	return strings.ContainsRune(chunk, '\n') || strings.ContainsRune(chunk, '\r') || strings.ContainsRune(chunk, '\t')
}

func (a App) shouldCaptureSingleRunePasteChunk(typed tea.KeyMsg) bool {
	if typed.Type != tea.KeyRunes || len(typed.Runes) != 1 || typed.Paste {
		return false
	}
	if strings.TrimSpace(a.input.Value()) != "" {
		return false
	}
	clip, err := readClipboardText()
	if err != nil {
		return false
	}
	clip = normalizeClipboardText(clip)
	if strings.TrimSpace(clip) == "" {
		return false
	}
	chunk := normalizeClipboardText(string(typed.Runes))
	return strings.HasPrefix(clip, chunk) || matchPasteEchoLoosely(clip, chunk)
}

func (a *App) appendPasteTxnChunk(chunk string) {
	if chunk == "" {
		return
	}
	chunk = normalizeClipboardText(chunk)
	lineHint := countPasteLines(chunk)
	if !a.pasteTxnActive {
		a.pasteTxnActive = true
		a.pasteTxnBuffer = chunk
		a.pasteTxnVersion++
		a.pasteTxnTokenInjected = false
		a.pasteTxnInjectedToken = ""
		a.extendPasteSession(a.now(), lineHint)
		return
	}
	a.pasteTxnBuffer += chunk
	a.pasteTxnVersion++
	a.extendPasteSession(a.now(), lineHint)
}

func (a *App) flushPasteTransaction() {
	if !a.pasteTxnActive {
		return
	}
	content := normalizeClipboardText(a.pasteTxnBuffer)
	a.pasteTxnActive = false
	a.pasteTxnBuffer = ""
	a.pasteTxnVersion = 0

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return
	}
	if clip, err := readClipboardText(); err == nil {
		clip = normalizeClipboardText(clip)
		clipTrimmed := strings.TrimSpace(clip)
		if clipTrimmed != "" && (strings.HasPrefix(clip, content) || strings.Contains(clip, content) || strings.Contains(content, clip)) {
			content = clip
			trimmed = clipTrimmed
			a.pendingCtrlVPasteEcho = clip
			a.pendingCtrlVEchoUntil = a.now().Add(2 * time.Second)
		}
	}
	if handled, err := a.applyPastedFileReferences(trimmed); handled {
		if err != nil {
			a.state.StatusText = err.Error()
			a.appendActivity("multimodal", "Failed to parse pasted file references", err.Error(), true)
		}
		return
	}
	if a.shouldSuppressDuplicatePasteContent(content, a.now()) {
		return
	}

	insert := content
	if summarized, ok, summarizeErr := a.summarizePastedText(insert); summarizeErr == nil {
		if ok {
			insert = summarized
		}
	} else {
		a.state.StatusText = summarizeErr.Error()
		a.appendActivity("multimodal", "Failed to summarize pasted content", summarizeErr.Error(), true)
	}
	before := a.input.Value()
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(insert), Paste: true})
	_ = cmd
	a.state.InputText = a.input.Value()
	a.noteInputEdit(before, a.state.InputText, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(insert), Paste: true}, a.now())
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	a.recordRecentPastedContent(insert, a.now())
}

func (a *App) handleClipboardPasteShortcut() (string, bool, error) {
	imageErr := a.addImageFromClipboard()
	if imageErr == nil {
		return "", true, nil
	}

	text, textErr := readClipboardText()
	if textErr != nil {
		return "", true, imageErr
	}

	if paths, ok := clipboardFileReferencePathsFromText(text, a.state.CurrentWorkdir); ok {
		references := make([]string, 0, len(paths))
		for _, path := range paths {
			if tuiinfra.IsSupportedImageFormat(path) || looksLikeImagePath(path) {
				if err := a.addImageAttachment(path); err != nil {
					return "", true, err
				}
				continue
			}
			reference, err := a.fileReferenceForPath(path)
			if err != nil {
				return "", true, err
			}
			references = append(references, reference)
		}

		if len(references) > 0 {
			combined := strings.Join(references, " ")
			current := strings.TrimSpace(a.input.Value())
			if current == "" {
				current = combined
			} else {
				current = current + " " + combined
			}
			a.input.SetValue(current)
			a.state.InputText = current
			a.normalizeComposerHeight()
			a.applyComponentLayout(false)
			a.refreshCommandMenu()
			a.state.StatusText = fmt.Sprintf("[System] Added %d file reference(s) from clipboard.", len(references))
		}
		return "", true, nil
	}

	if text == "" {
		return "", true, imageErr
	}
	return text, true, nil
}

func (a *App) applyPastedFileReferences(text string) (bool, error) {
	paths, ok := clipboardFileReferencePathsFromText(text, a.state.CurrentWorkdir)
	if !ok {
		return false, nil
	}

	references := make([]string, 0, len(paths))
	for _, path := range paths {
		if tuiinfra.IsSupportedImageFormat(path) || looksLikeImagePath(path) {
			if err := a.addImageAttachment(path); err != nil {
				return true, err
			}
			continue
		}
		reference, err := a.fileReferenceForPath(path)
		if err != nil {
			return true, err
		}
		references = append(references, reference)
	}

	if len(references) > 0 {
		combined := strings.Join(references, " ")
		current := strings.TrimSpace(a.input.Value())
		if current == "" {
			current = combined
		} else {
			current = current + " " + combined
		}
		a.input.SetValue(current)
		a.state.InputText = current
		a.normalizeComposerHeight()
		a.applyComponentLayout(false)
		a.refreshCommandMenu()
		a.state.StatusText = fmt.Sprintf("[System] Added %d file reference(s) from paste.", len(references))
	}
	return true, nil
}

func (a App) collectPendingImageInputs() []tuiservices.UserImageInput {
	images := make([]tuiservices.UserImageInput, 0, len(a.pendingImageAttachments))
	for _, attachment := range a.pendingImageAttachments {
		images = append(images, tuiservices.UserImageInput{
			Path:     attachment.Path,
			MimeType: attachment.MimeType,
		})
	}
	return images
}

func (a *App) queueInterventionInput(input string, images []tuiservices.UserImageInput) {
	text := strings.TrimSpace(input)
	if text == "" && len(images) == 0 {
		a.queuedIntervention = nil
		return
	}
	clonedImages := append([]tuiservices.UserImageInput(nil), images...)
	a.queuedIntervention = &queuedInterventionInput{
		Text:   text,
		Images: clonedImages,
	}
}

func (a *App) dispatchQueuedInterventionIfIdle() tea.Cmd {
	if a.isBusy() || a.queuedIntervention == nil {
		return nil
	}

	queued := a.queuedIntervention
	a.queuedIntervention = nil
	a.rebuildTranscript()
	if queued == nil {
		return nil
	}
	if strings.TrimSpace(queued.Text) == "" && len(queued.Images) == 0 {
		return nil
	}
	a.appendActivity("run", "Applying queued intervention", "", false)
	return a.beginAgentRun(queued.Text, queued.Images)
}

func (a *App) beginAgentRun(input string, images []tuiservices.UserImageInput) tea.Cmd {
	normalizedInput := strings.TrimSpace(input)
	if normalizedInput == "" {
		return nil
	}
	a.input.Reset()
	a.state.InputText = ""
	a.applyComponentLayout(true)
	a.refreshCommandMenu()
	a.resetPasteHeuristics()

	a.clearActivities()
	a.clearRunProgress()
	a.startupScreenLocked = false
	a.state.IsAgentRunning = true
	a.state.IsCompacting = false
	a.state.StreamingReply = false
	a.state.ExecutionError = ""
	a.state.StatusText = statusThinking
	a.state.CurrentTool = ""

	runID := fmt.Sprintf("run-%d", a.now().UnixNano())
	a.state.ActiveRunID = runID
	requestedWorkdir := tuiutils.RequestedWorkdirForRun(a.state.CurrentWorkdir)
	clonedImages := append([]tuiservices.UserImageInput(nil), images...)
	return runAgent(a.runtime, tuiservices.PrepareInput{
		SessionID: a.state.ActiveSessionID,
		RunID:     runID,
		Workdir:   requestedWorkdir,
		Text:      normalizedInput,
		Images:    clonedImages,
	})
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
			if cmd, started := a.maybeStartModelScopeGuideFromProvider(item.id); started {
				return a, cmd
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
		a.providerPicker, cmd = updateListPickerModel(a.providerPicker, msg)
	case pickerModel:
		a.modelPicker, cmd = updateListPickerModel(a.modelPicker, msg)
	case pickerSession:
		a.sessionPicker, cmd = updateListPickerModel(a.sessionPicker, msg)
	case pickerHelp:
		a.helpPicker, cmd = updateListPickerModel(a.helpPicker, msg)
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
	case pickerModelScope:
		return a.handleModelScopeGuideInput(msg)
	}
	return a, cmd
}

func updateListPickerModel(picker list.Model, msg tea.KeyMsg) (list.Model, tea.Cmd) {
	if picker.SettingFilter() {
		switch msg.Type {
		case tea.KeyUp:
			picker.CursorUp()
			return picker, nil
		case tea.KeyDown:
			picker.CursorDown()
			return picker, nil
		case tea.KeyPgUp:
			picker.PrevPage()
			return picker, nil
		case tea.KeyPgDown:
			picker.NextPage()
			return picker, nil
		case tea.KeyHome:
			picker.GoToStart()
			return picker, nil
		case tea.KeyEnd:
			picker.GoToEnd()
			return picker, nil
		}
	}

	next, cmd := picker.Update(msg)
	if !next.SettingFilter() || !isPickerFilterEditKey(msg) {
		return next, cmd
	}

	filterValue := next.FilterValue()
	next.SetFilterText(filterValue)
	next.SetFilterState(list.Filtering)
	return next, nil
}

func isPickerFilterEditKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeyBackspace, tea.KeyDelete:
		return true
	default:
		return false
	}
}

// maybeStartModelScopeGuideFromProvider 在选择 modelscope 且未配置 token 时进入半引导流程?
func (a *App) maybeStartModelScopeGuideFromProvider(providerID string) (tea.Cmd, bool) {
	if !strings.EqualFold(strings.TrimSpace(providerID), config.ModelScopeName) {
		return nil, false
	}

	currentConfig := a.configManager.Get()
	providerConfig, err := currentConfig.ProviderByName(providerID)
	if err != nil {
		return nil, false
	}

	apiKeyEnv := strings.TrimSpace(providerConfig.APIKeyEnv)
	if apiKeyEnv == "" {
		return nil, false
	}
	if strings.TrimSpace(os.Getenv(apiKeyEnv)) != "" {
		return nil, false
	}
	if userValue, exists, lookupErr := lookupProviderUserEnvVar(apiKeyEnv); lookupErr == nil && exists &&
		strings.TrimSpace(userValue) != "" {
		return nil, false
	}

	guidePath := a.resolveModelScopeGuidePath()
	a.modelScopeGuide = &modelScopeGuideState{
		ProviderID: strings.TrimSpace(providerID),
		APIKeyEnv:  apiKeyEnv,
		GuidePath:  guidePath,
		Step:       modelScopeGuideStepGuide,
	}
	a.state.ActivePicker = pickerModelScope
	a.state.StatusText = "ModelScope setup guide"
	a.state.ExecutionError = ""
	if strings.TrimSpace(guidePath) == "" {
		a.modelScopeGuide.Step = modelScopeGuideStepLogin
		a.modelScopeGuide.Notice = "Guide HTML not found, fallback to ModelScope login page."
		return a.runModelScopeGuideOpen(modelScopeLoginURL), true
	}
	return a.runModelScopeGuideOpen(guidePath), true
}

// resolveModelScopeGuidePath 解析 ModelScope 指导页的本地路径；文件不存在时返回空字符串?
func (a *App) resolveModelScopeGuidePath() string {
	baseDir := strings.TrimSpace(a.configManager.BaseDir())
	if baseDir == "" {
		return ""
	}
	candidate := filepath.Join(baseDir, "modelscope-guide.html")
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	return candidate
}

// handleModelScopeGuideInput 处理 ModelScope 半引导流程中的键盘交互。
func (a *App) handleModelScopeGuideInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.modelScopeGuide == nil {
		return a, nil
	}
	if key.Matches(msg, a.keys.FocusInput) {
		a.modelScopeGuide = nil
		a.closePicker()
		a.state.StatusText = "ModelScope setup canceled"
		return a, nil
	}
	if a.modelScopeGuide.Submitting {
		return a, nil
	}

	guide := a.modelScopeGuide
	switch {
	case msg.String() == "enter":
		if guide.Step == modelScopeGuideStepPasteToken {
			return a, a.submitModelScopeGuideToken(guide)
		}
		if target, ok := modelScopeGuideOpenTarget(guide.Step, guide.GuidePath); ok {
			return a, a.runModelScopeGuideOpen(target)
		}
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH:
		if guide.Step == modelScopeGuideStepPasteToken {
			guide.Token = trimLastRune(guide.Token)
			clearModelScopeGuideFeedback(guide)
		}
	case msg.Type == tea.KeyRunes && len(msg.Runes) > 0:
		if guide.Step == modelScopeGuideStepPasteToken {
			guide.Token += sanitizeProviderAddInputRunes(msg.Runes)
			clearModelScopeGuideFeedback(guide)
		}
	}

	return a, nil
}

// modelScopeGuideOpenTarget 返回当前引导步骤对应的外部资源目标。
func modelScopeGuideOpenTarget(step modelScopeGuideStep, guidePath string) (string, bool) {
	switch step {
	case modelScopeGuideStepGuide:
		target := strings.TrimSpace(guidePath)
		if target == "" {
			return modelScopeLoginURL, true
		}
		return target, true
	case modelScopeGuideStepLogin:
		return modelScopeLoginURL, true
	case modelScopeGuideStepToken:
		return modelScopeTokenURL, true
	default:
		return "", false
	}
}

// submitModelScopeGuideToken 校验并提交用户粘贴的 token。
func (a *App) submitModelScopeGuideToken(guide *modelScopeGuideState) tea.Cmd {
	token := strings.TrimSpace(guide.Token)
	if token == "" {
		guide.Error = "Token is required."
		return nil
	}
	guide.Submitting = true
	clearModelScopeGuideFeedback(guide)
	a.state.StatusText = "Validating ModelScope token..."
	return a.runModelScopeGuideSubmit(guide.ProviderID, guide.APIKeyEnv, token)
}

// runModelScopeGuideOpen 异步打开 ModelScope 引导资源，本地 HTML 和网页 URL 共用这一入口。
func (a *App) runModelScopeGuideOpen(target string) tea.Cmd {
	openTarget := strings.TrimSpace(target)
	if openTarget == "" {
		return nil
	}
	return func() tea.Msg {
		if err := openExternalResource(openTarget); err != nil {
			return modelScopeGuideOpenResultMsg{
				Target: openTarget,
				Error:  sanitizeProviderAddError(err),
			}
		}
		return modelScopeGuideOpenResultMsg{Target: openTarget}
	}
}

// handleModelScopeGuideOpenResultMsg 处理引导资源打开结果，并推进引导步骤。
func (a *App) handleModelScopeGuideOpenResultMsg(msg modelScopeGuideOpenResultMsg) {
	if a.modelScopeGuide == nil {
		return
	}

	guide := a.modelScopeGuide
	if strings.TrimSpace(msg.Error) != "" {
		guide.Error = msg.Error
		guide.Notice = ""
		a.state.ExecutionError = msg.Error
		a.state.StatusText = "ModelScope guide open failed"
		a.appendActivity("provider", "ModelScope guide open failed", msg.Error, true)
		return
	}

	guide.Error = ""
	guide.Notice = "Opened: " + strings.TrimSpace(msg.Target)
	a.state.ExecutionError = ""
	a.state.StatusText = "ModelScope guide opened"
	if nextStep, advanced := advanceModelScopeGuideStep(guide.Step, msg.Target); advanced {
		guide.Step = nextStep
	}
}

// advanceModelScopeGuideStep 根据资源打开结果推进 ModelScope 引导步骤。
func advanceModelScopeGuideStep(current modelScopeGuideStep, target string) (modelScopeGuideStep, bool) {
	switch current {
	case modelScopeGuideStepGuide:
		return modelScopeGuideStepLogin, true
	case modelScopeGuideStepLogin:
		return modelScopeGuideStepToken, true
	case modelScopeGuideStepToken:
		if strings.TrimSpace(target) == modelScopeTokenURL {
			return modelScopeGuideStepPasteToken, true
		}
	}
	return current, false
}

// clearModelScopeGuideFeedback 清空引导面板上的错误与提示信息。
func clearModelScopeGuideFeedback(guide *modelScopeGuideState) {
	if guide == nil {
		return
	}
	guide.Error = ""
	guide.Notice = ""
}

// runModelScopeGuideSubmit 设置 token 后完成 provider 选择与最小可用校验。
func (a *App) runModelScopeGuideSubmit(providerID string, apiKeyEnv string, token string) tea.Cmd {
	providerSvc := a.providerSvc
	baseDir := a.configManager.BaseDir()
	previousSelection := a.configManager.Get()
	previousProviderID := strings.TrimSpace(previousSelection.SelectedProvider)
	previousModelID := strings.TrimSpace(previousSelection.CurrentModel)

	return func() tea.Msg {
		trimmedToken := strings.TrimSpace(token)
		trimmedEnvName := strings.TrimSpace(apiKeyEnv)
		if trimmedToken == "" {
			return modelScopeGuideSubmitResultMsg{Error: "ModelScope token is empty"}
		}
		if err := config.ValidateEnvVarName(trimmedEnvName); err != nil {
			return modelScopeGuideSubmitResultMsg{Error: sanitizeProviderAddError(err)}
		}
		if config.IsProtectedEnvVarName(trimmedEnvName) {
			return modelScopeGuideSubmitResultMsg{
				Error: fmt.Sprintf("ModelScope token env %q is protected", trimmedEnvName),
			}
		}

		previousValue, hadPreviousValue := os.LookupEnv(trimmedEnvName)
		restoreEnv := func() {
			if hadPreviousValue {
				_ = os.Setenv(trimmedEnvName, previousValue)
				return
			}
			_ = os.Unsetenv(trimmedEnvName)
		}
		if setErr := os.Setenv(trimmedEnvName, trimmedToken); setErr != nil {
			return modelScopeGuideSubmitResultMsg{Error: sanitizeProviderAddError(setErr)}
		}
		failWithRollback := func(rawErr error, stage string) modelScopeGuideSubmitResultMsg {
			restoreEnv()
			wrappedErr := fmt.Errorf("%s: %w", stage, rawErr)
			if rollbackErr := rollbackModelScopeGuideSelection(providerSvc, previousProviderID, previousModelID); rollbackErr != nil {
				wrappedErr = fmt.Errorf("%w; rollback selection: %v", wrappedErr, rollbackErr)
			}
			return modelScopeGuideSubmitResultMsg{
				Error: sanitizeProviderAddError(wrappedErr, trimmedToken, baseDir),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), modelScopeGuideSelectTimeout)
		defer cancel()

		selection, selectErr := providerSvc.SelectProvider(ctx, providerID)
		if selectErr != nil {
			return failWithRollback(selectErr, "select provider")
		}
		if _, listErr := providerSvc.ListModels(ctx); listErr != nil {
			return failWithRollback(listErr, "verify token")
		}

		if persistErr := persistProviderUserEnvVar(trimmedEnvName, trimmedToken); persistErr != nil {
			return failWithRollback(persistErr, "persist token env")
		}

		return modelScopeGuideSubmitResultMsg{
			Selection: selection,
			Warning:   providerAddPersistenceWarning(),
		}
	}
}

// rollbackModelScopeGuideSelection 在引导流程失败时回滚 provider 和 model 选择，避免状态漂移。
func rollbackModelScopeGuideSelection(providerSvc ProviderController, providerID string, modelID string) error {
	normalizedProviderID := strings.TrimSpace(providerID)
	normalizedModelID := strings.TrimSpace(modelID)
	if normalizedProviderID == "" || providerSvc == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), modelScopeGuideRollbackTimeout)
	defer cancel()

	if _, err := providerSvc.SelectProvider(ctx, normalizedProviderID); err != nil {
		return fmt.Errorf("rollback provider selection: %w", err)
	}
	if normalizedModelID == "" {
		return nil
	}
	if _, err := providerSvc.SetCurrentModel(ctx, normalizedModelID); err != nil {
		return fmt.Errorf("rollback model selection: %w", err)
	}
	return nil
}

// handleModelScopeGuideSubmitResultMsg 处理 token 校验结果；成功后关闭引导，失败时回退并提示下一步。
func (a *App) handleModelScopeGuideSubmitResultMsg(msg modelScopeGuideSubmitResultMsg) tea.Cmd {
	if a.modelScopeGuide == nil {
		return nil
	}

	guide := a.modelScopeGuide
	guide.Submitting = false
	if strings.TrimSpace(msg.Error) != "" {
		guide.Error = msg.Error
		guide.Notice = ""
		guide.Step = modelScopeGuideStepPasteToken
		a.state.ExecutionError = msg.Error
		a.state.StatusText = "ModelScope token validation failed"
		a.appendActivity("provider", "ModelScope token validation failed", msg.Error, true)

		if isModelScopeAuthOrPermissionError(msg.Error) {
			guide.Notice = "Detected auth/permission issue, opening Aliyun account binding page."
			guide.Step = modelScopeGuideStepToken
			return a.runModelScopeGuideOpen(modelScopeAuthURL)
		}
		return nil
	}

	guideProviderID := strings.TrimSpace(guide.ProviderID)
	a.modelScopeGuide = nil
	a.state.ActivePicker = pickerNone
	a.state.ExecutionError = ""
	if strings.TrimSpace(msg.Selection.ProviderID) != "" {
		a.state.CurrentProvider = strings.TrimSpace(msg.Selection.ProviderID)
	} else {
		a.state.CurrentProvider = guideProviderID
	}
	if strings.TrimSpace(msg.Selection.ModelID) != "" {
		a.state.CurrentModel = strings.TrimSpace(msg.Selection.ModelID)
	}
	a.state.StatusText = "ModelScope provider selected"
	a.appendActivity("provider", "ModelScope setup completed", a.state.CurrentProvider, false)
	if strings.TrimSpace(msg.Warning) != "" {
		a.appendActivity("provider", "Provider key persistence", strings.TrimSpace(msg.Warning), false)
	}

	if err := a.refreshProviderPicker(); err != nil {
		a.appendActivity("system", "Failed to refresh providers", err.Error(), true)
	}
	if err := a.refreshModelPicker(); err != nil {
		a.appendActivity("system", "Failed to refresh models", err.Error(), true)
	}
	return a.requestModelCatalogRefresh(a.state.CurrentProvider)
}

// isModelScopeAuthOrPermissionError 判断错误是否指向认证或权限未完成场景。
func isModelScopeAuthOrPermissionError(raw string) bool {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	if lowered == "" {
		return false
	}
	keywords := []string{
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"permission",
		"access denied",
		"auth",
		"实名认证",
		"权限",
		"aliyun",
	}
	for _, keyword := range keywords {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
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
		a.loadLogEntriesForSession("")
		return nil
	}

	session, err := a.runtime.LoadSession(context.Background(), a.state.ActiveSessionID)
	if err != nil {
		return err
	}

	a.applySessionSnapshot(session, false)
	return nil
}

// HydrateSession 在 TUI 启动阶段加载并接管既有会话状态，用于 URL 唤醒后的无感续接。
func (a *App) HydrateSession(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	session, err := a.runtime.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.ID) == "" {
		session.ID = sessionID
	}

	a.setActiveSessionID(session.ID)
	a.state.ExecutionError = ""
	a.state.CurrentTool = ""
	a.resetSessionRuntimeState()
	a.applySessionSnapshot(session, true)
	a.rebuildTranscript()
	a.transcript.GotoBottom()
	a.applyComponentLayout(false)
	return nil
}

// ConfigureStartupWakeInput 配置启动阶段的一次性自动提交输入，不会直接触发 runtime 调用?
func (a *App) ConfigureStartupWakeInput(text string, workdir string) error {
	normalizedText := strings.TrimSpace(text)
	if normalizedText == "" {
		return fmt.Errorf("startup wake input is empty")
	}
	a.startupWakeSubmitInput = &startupWakeSubmitInput{
		Text:    normalizedText,
		Workdir: strings.TrimSpace(workdir),
	}
	return nil
}

// applySessionSnapshot 将会话快照同步到前端状态，统一复用于普通刷新与启动接管路径。
func (a *App) applySessionSnapshot(session agentsession.Session, warnOnMissingWorkdir bool) {
	a.activeMessages = session.Messages
	a.clearActivities()
	a.syncTodos(session.Todos)
	a.state.ActiveSessionTitle = session.Title
	a.syncSessionWorkdir(session.Workdir, warnOnMissingWorkdir)
	a.loadLogEntriesForSession(session.ID)
	a.refreshRuntimeSourceSnapshot()
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
	a.queuedIntervention = nil
	a.pendingAutoPermission = nil
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

	a.setActiveSessionID(item.Summary.ID)
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
			a.setActiveSessionID(s.ID)
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

// runtimeSessionContextSource 定义读取会话上下文快照的最小接口，便于 UI 侧按需刷新运行态信息。
type runtimeSessionContextSource interface {
	GetSessionContext(ctx context.Context, sessionID string) (any, error)
}

type runtimeSessionUsageSource interface {
	GetSessionUsage(ctx context.Context, sessionID string) (any, error)
}

type runtimeRunSnapshotSource interface {
	GetRunSnapshot(ctx context.Context, runID string) (any, error)
}

var runtimeEventHandlerRegistry = map[tuiservices.EventType]func(*App, tuiservices.RuntimeEvent) bool{
	tuiservices.EventUserMessage:                              runtimeEventUserMessageHandler,
	tuiservices.EventInputNormalized:                          runtimeEventInputNormalizedHandler,
	tuiservices.EventAssetSaved:                               runtimeEventAssetSavedHandler,
	tuiservices.EventAssetSaveFailed:                          runtimeEventAssetSaveFailedHandler,
	tuiservices.EventType(tuiservices.RuntimeEventRunContext): runtimeEventRunContextHandler,
	tuiservices.EventType(tuiservices.RuntimeEventToolStatus): runtimeEventToolStatusHandler,
	tuiservices.EventType(tuiservices.RuntimeEventUsage):      runtimeEventUsageHandler,
	tuiservices.EventToolCallThinking:                         runtimeEventToolCallThinkingHandler,
	tuiservices.EventToolStart:                                runtimeEventToolStartHandler,
	tuiservices.EventToolResult:                               runtimeEventToolResultHandler,
	tuiservices.EventAgentChunk:                               runtimeEventAgentChunkHandler,
	tuiservices.EventToolChunk:                                runtimeEventToolChunkHandler,
	tuiservices.EventAgentDone:                                runtimeEventAgentDoneHandler,
	tuiservices.EventRunCanceled:                              runtimeEventRunCanceledHandler,
	tuiservices.EventError:                                    runtimeEventErrorHandler,
	tuiservices.EventPermissionRequested:                      runtimeEventPermissionRequestHandler,
	tuiservices.EventPermissionResolved:                       runtimeEventPermissionResolvedHandler,
	tuiservices.EventCompactApplied:                           runtimeEventCompactDoneHandler,
	tuiservices.EventCompactError:                             runtimeEventCompactErrorHandler,
	tuiservices.EventTokenUsage:                               runtimeEventTokenUsageHandler,
	tuiservices.EventPhaseChanged:                             runtimeEventPhaseChangedHandler,
	tuiservices.EventVerificationStarted:                      runtimeEventVerificationStartedHandler,
	tuiservices.EventVerificationStageFinished:                runtimeEventVerificationStageFinishedHandler,
	tuiservices.EventVerificationFinished:                     runtimeEventVerificationFinishedHandler,
	tuiservices.EventVerificationCompleted:                    runtimeEventVerificationCompletedHandler,
	tuiservices.EventVerificationFailed:                       runtimeEventVerificationFailedHandler,
	tuiservices.EventAcceptanceDecided:                        runtimeEventAcceptanceDecidedHandler,
	tuiservices.EventStopReasonDecided:                        runtimeEventStopReasonDecidedHandler,
	tuiservices.EventTodoUpdated:                              runtimeEventTodoUpdatedHandler,
	tuiservices.EventTodoConflict:                             runtimeEventTodoConflictHandler,
	tuiservices.EventTodoSnapshotUpdated:                      runtimeEventTodoSnapshotUpdatedHandler,
	tuiservices.EventSkillActivated:                           runtimeEventSkillActivatedHandler,
	tuiservices.EventSkillDeactivated:                         runtimeEventSkillDeactivatedHandler,
	tuiservices.EventSkillMissing:                             runtimeEventSkillMissingHandler,
	tuiservices.EventHookStarted:                              runtimeEventHookStartedHandler,
	tuiservices.EventHookFinished:                             runtimeEventHookFinishedHandler,
	tuiservices.EventHookFailed:                               runtimeEventHookFailedHandler,
	tuiservices.EventHookBlocked:                              runtimeEventHookBlockedHandler,
	tuiservices.EventRepoHooksDiscovered:                      runtimeEventRepoHooksDiscoveredHandler,
	tuiservices.EventRepoHooksLoaded:                          runtimeEventRepoHooksLoadedHandler,
	tuiservices.EventRepoHooksSkippedUntrusted:                runtimeEventRepoHooksSkippedUntrustedHandler,
	tuiservices.EventRepoHooksTrustStoreInvalid:               runtimeEventRepoHooksTrustStoreInvalidHandler,
	tuiservices.EventSubAgentStarted:                          runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentProgress:                         runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentRetried:                          runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentBlocked:                          runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentCompleted:                        runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentFailed:                           runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentCanceled:                         runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentFinished:                         runtimeEventSubAgentLifecycleHandler,
	tuiservices.EventSubAgentToolCallStarted:                  runtimeEventSubAgentToolCallHandler,
	tuiservices.EventSubAgentToolCallResult:                   runtimeEventSubAgentToolCallHandler,
	tuiservices.EventSubAgentToolCallDenied:                   runtimeEventSubAgentToolCallHandler,
	tuiservices.EventRuntimeSnapshotUpdated:                   runtimeEventRuntimeSnapshotUpdatedHandler,
	tuiservices.EventFactsUpdated:                             runtimeEventFactsUpdatedHandler,
	tuiservices.EventDecisionMade:                             runtimeEventDecisionMadeHandler,
	tuiservices.EventSubAgentSnapshotUpdated:                  runtimeEventSubAgentSnapshotUpdatedHandler,
}

func hookActivityLabel(source string, hookID string) string {
	normalizedID := strings.TrimSpace(hookID)
	if normalizedID == "" {
		normalizedID = "unknown_hook"
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		return normalizedID
	}
	return normalizedSource + ":" + normalizedID
}

func runtimeEventHookStartedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.HookEventPayload)
	if !ok {
		return false
	}
	hookLabel := hookActivityLabel(payload.Source, payload.HookID)
	point := strings.TrimSpace(payload.Point)
	if point == "" {
		point = "unknown_point"
	}
	a.appendActivity("hook", "Hook started", hookLabel+" @ "+point, false)
	return false
}

func runtimeEventHookFinishedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.HookEventPayload)
	if !ok {
		return false
	}
	hookLabel := hookActivityLabel(payload.Source, payload.HookID)
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "pass"
	}
	detail := fmt.Sprintf("%s (%dms)", status, payload.DurationMS)
	if message := strings.TrimSpace(payload.Message); message != "" {
		detail = detail + " · " + message
	}
	a.appendActivity("hook", "Hook finished: "+hookLabel, detail, false)
	return false
}

func runtimeEventHookFailedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.HookEventPayload)
	if !ok {
		return false
	}
	hookLabel := hookActivityLabel(payload.Source, payload.HookID)
	detail := strings.TrimSpace(payload.Error)
	if detail == "" {
		detail = "hook execution failed"
	}
	if message := strings.TrimSpace(payload.Message); message != "" {
		detail = message + " · " + detail
	}
	a.appendActivity("hook", "Hook failed: "+hookLabel, detail, true)
	return false
}

func runtimeEventHookBlockedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.HookBlockedPayload)
	if !ok {
		return false
	}
	hookLabel := hookActivityLabel(payload.Source, payload.HookID)
	point := strings.TrimSpace(payload.Point)
	if point == "" {
		point = "unknown_point"
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "hook returned block"
	}
	title := "Hook blocked: " + hookLabel + " @ " + point
	if !payload.Enforced {
		title = "Hook block observed: " + hookLabel + " @ " + point
	}
	a.appendActivity("hook", title, reason, payload.Enforced)
	return false
}

func runtimeEventRepoHooksDiscoveredHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.RepoHooksLifecyclePayload)
	if !ok {
		return false
	}
	detail := strings.TrimSpace(payload.HooksPath)
	if detail == "" {
		detail = strings.TrimSpace(payload.Workspace)
	}
	a.appendActivity("hook", "Repo hooks discovered", detail, false)
	return false
}

func runtimeEventRepoHooksLoadedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.RepoHooksLifecyclePayload)
	if !ok {
		return false
	}
	detail := fmt.Sprintf("workspace=%s, hooks=%d", strings.TrimSpace(payload.Workspace), payload.HookCount)
	a.appendActivity("hook", "Repo hooks loaded", detail, false)
	return false
}

func runtimeEventRepoHooksSkippedUntrustedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.RepoHooksLifecyclePayload)
	if !ok {
		return false
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "workspace is not trusted"
	}
	a.appendActivity("hook", "Repo hooks skipped (untrusted)", reason, false)
	return false
}

func runtimeEventRepoHooksTrustStoreInvalidHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.RepoHooksTrustStoreInvalidPayload)
	if !ok {
		return false
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "trust store is invalid"
	}
	a.appendActivity("hook", "Repo hooks trust store invalid", reason, false)
	return false
}

// runtimeEventSubAgentLifecycleHandler 统一处理 subagent 生命周期事件并写入活动区/日志。
func runtimeEventSubAgentLifecycleHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.SubAgentEventPayload)
	if !ok {
		return false
	}
	eventType := strings.TrimSpace(string(event.Type))
	title := "SubAgent event"
	switch event.Type {
	case tuiservices.EventSubAgentStarted:
		title = "SubAgent started"
	case tuiservices.EventSubAgentProgress:
		title = "SubAgent progress"
	case tuiservices.EventSubAgentRetried:
		title = "SubAgent retried"
	case tuiservices.EventSubAgentBlocked:
		title = "SubAgent blocked"
	case tuiservices.EventSubAgentCompleted:
		title = "SubAgent completed"
	case tuiservices.EventSubAgentFailed:
		title = "SubAgent failed"
	case tuiservices.EventSubAgentCanceled:
		title = "SubAgent canceled"
	case tuiservices.EventSubAgentFinished:
		title = "SubAgent finished"
	default:
		title = "SubAgent event: " + fallbackText(eventType, "unknown")
	}

	details := make([]string, 0, 6)
	if role := strings.TrimSpace(payload.Role); role != "" {
		details = append(details, "role="+role)
	}
	if taskID := strings.TrimSpace(payload.TaskID); taskID != "" {
		details = append(details, "task="+taskID)
	}
	if state := strings.TrimSpace(payload.State); state != "" {
		details = append(details, "state="+state)
	}
	if reason := strings.TrimSpace(payload.StopReason); reason != "" {
		details = append(details, "stop="+reason)
	}
	if payload.Step > 0 {
		details = append(details, fmt.Sprintf("step=%d", payload.Step))
	}
	if message := strings.TrimSpace(payload.Reason); message != "" {
		details = append(details, message)
	} else if message := strings.TrimSpace(payload.Delta); message != "" {
		details = append(details, message)
	}
	if errText := strings.TrimSpace(payload.Error); errText != "" {
		details = append(details, "error="+errText)
	}

	isError := event.Type == tuiservices.EventSubAgentFailed
	a.appendActivity("subagent", title, strings.Join(details, " | "), isError)
	return false
}

// runtimeEventSubAgentToolCallHandler 处理 subagent 工具调用相关事件并写入活动区/日志。
func runtimeEventSubAgentToolCallHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.SubAgentToolCallEventPayload)
	if !ok {
		return false
	}
	title := "SubAgent tool call"
	switch event.Type {
	case tuiservices.EventSubAgentToolCallStarted:
		title = "SubAgent tool call started"
	case tuiservices.EventSubAgentToolCallResult:
		title = "SubAgent tool call result"
	case tuiservices.EventSubAgentToolCallDenied:
		title = "SubAgent tool call denied"
	}

	details := make([]string, 0, 6)
	if role := strings.TrimSpace(payload.Role); role != "" {
		details = append(details, "role="+role)
	}
	if taskID := strings.TrimSpace(payload.TaskID); taskID != "" {
		details = append(details, "task="+taskID)
	}
	details = append(details, "tool="+fallbackText(strings.TrimSpace(payload.ToolName), "unknown"))
	if decision := strings.TrimSpace(payload.Decision); decision != "" {
		details = append(details, "decision="+decision)
	}
	if payload.ElapsedMS > 0 {
		details = append(details, fmt.Sprintf("elapsed=%dms", payload.ElapsedMS))
	}
	if payload.Truncated {
		details = append(details, "truncated=true")
	}
	if errText := strings.TrimSpace(payload.Error); errText != "" {
		details = append(details, "error="+errText)
	}

	isError := event.Type == tuiservices.EventSubAgentToolCallDenied || strings.TrimSpace(payload.Error) != ""
	a.appendActivity("subagent", title, strings.Join(details, " | "), isError)
	return false
}

func runtimeEventPhaseChangedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.PhaseChangedPayload)
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

// runtimeEventVerificationStartedHandler 处理验证流程开始事件，并更新运行进度提示。
func runtimeEventVerificationStartedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.VerificationStartedPayload)
	if !ok {
		return false
	}
	progress := 0.84
	if payload.CompletionPassed {
		progress = 0.88
	}
	a.setRunProgress(progress, "Verifying acceptance")
	detail := fmt.Sprintf("completion_passed=%t", payload.CompletionPassed)
	if reason := strings.TrimSpace(payload.CompletionBlockedReason); reason != "" {
		detail = detail + " reason=" + reason
	}
	a.appendActivity("verify", "Verification started", detail, false)
	return false
}

// runtimeEventVerificationStageFinishedHandler 处理单个 verifier 阶段完成事件。
func runtimeEventVerificationStageFinishedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.VerificationStageFinishedPayload)
	if !ok {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	detail := strings.TrimSpace(payload.Summary)
	if detail == "" {
		detail = strings.TrimSpace(payload.Reason)
	}
	if detail == "" {
		detail = "no summary"
	}
	title := "Verifier stage finished"
	if name := strings.TrimSpace(payload.Name); name != "" {
		title = "Verifier stage: " + name
	}
	isError := status == "fail" || status == "hard_block"
	a.appendActivity("verify", title, detail, isError)
	return false
}

// runtimeEventVerificationFinishedHandler 处理验证总流程结束事件。
func runtimeEventVerificationFinishedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.VerificationFinishedPayload)
	if !ok {
		return false
	}
	a.setRunProgress(0.92, "Verification finished")
	detail := fmt.Sprintf("acceptance=%s stop_reason=%s", payload.AcceptanceStatus, payload.StopReason)
	a.appendActivity("verify", "Verification finished", detail, false)
	return false
}

// runtimeEventVerificationCompletedHandler 处理验证通过事件。
func runtimeEventVerificationCompletedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.VerificationCompletedPayload)
	if !ok {
		return false
	}
	detail := strings.TrimSpace(string(payload.StopReason))
	if detail == "" {
		detail = "accepted"
	}
	a.appendActivity("verify", "Verification completed", detail, false)
	return false
}

// runtimeEventVerificationFailedHandler 处理验证失败事件。
func runtimeEventVerificationFailedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.VerificationFailedPayload)
	if !ok {
		return false
	}
	detail := strings.TrimSpace(string(payload.StopReason))
	if detail == "" {
		detail = "verification_failed"
	}
	if class := strings.TrimSpace(string(payload.ErrorClass)); class != "" {
		detail = detail + " (" + class + ")"
	}
	a.appendActivity("verify", "Verification failed", detail, true)
	return false
}

// runtimeEventAcceptanceDecidedHandler 处理 acceptance 决策事件，并记录可观测活动日志。
func runtimeEventAcceptanceDecidedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.AcceptanceDecidedPayload)
	if !ok {
		return false
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "unknown"
	}
	detail := strings.TrimSpace(payload.UserVisibleSummary)
	if detail == "" {
		detail = strings.TrimSpace(payload.InternalSummary)
	}
	if detail == "" {
		detail = strings.TrimSpace(payload.ContinueHint)
	}
	if detail == "" {
		detail = "acceptance decision generated"
	}
	if reason := strings.TrimSpace(payload.CompletionBlockedReason); reason != "" {
		detail = detail + " (reason=" + reason + ")"
	}
	isError := strings.EqualFold(status, "failed")
	a.appendActivity("acceptance", "Acceptance decided ("+status+")", detail, isError)
	return false
}

// runtimeEventStopReasonDecidedHandler 处理运行终止原因事件，统一收尾状态与活动日志。
func runtimeEventStopReasonDecidedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.StopReasonDecidedPayload)
	if !ok {
		return false
	}
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.pendingAutoPermission = nil
	a.clearRunProgress()

	reason := strings.ToLower(strings.TrimSpace(string(payload.Reason)))
	switch reason {
	case "stop_completed":
		reason = strings.ToLower(string(tuiservices.StopReasonAccepted))
	case "stop_user_interrupt":
		reason = strings.ToLower(string(tuiservices.StopReasonUserInterrupt))
	case "stop_fatal_error":
		reason = strings.ToLower(string(tuiservices.StopReasonFatalError))
	case "stop_max_turns_reached":
		reason = strings.ToLower(string(tuiservices.StopReasonMaxTurnExceeded))
	case "stop_budget_exceeded":
		reason = strings.ToLower(string(tuiservices.StopReasonBudgetExceeded))
	}
	switch reason {
	case strings.ToLower(string(tuiservices.StopReasonAccepted)):
		if strings.TrimSpace(a.state.ExecutionError) == "" {
			a.state.StatusText = statusReady
		}
	case strings.ToLower(string(tuiservices.StopReasonTodoNotConverged)),
		strings.ToLower(string(tuiservices.StopReasonTodoWaitingExternal)),
		strings.ToLower(string(tuiservices.StopReasonNoProgressAfterFinalIntercept)),
		strings.ToLower(string(tuiservices.StopReasonMaxTurnExceededWithUnconvergedTodos)),
		strings.ToLower(string(tuiservices.StopReasonMaxTurnExceededWithFailedVerification)):
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "Task is incomplete"
		}
		a.state.ExecutionError = ""
		a.state.StatusText = detail
		a.appendActivity("run", "Run incomplete", detail, false)
	case strings.ToLower(string(tuiservices.StopReasonUserInterrupt)):
		a.state.ExecutionError = ""
		a.state.StatusText = statusCanceled
		a.appendActivity("run", "Canceled current run", "", false)
	case strings.ToLower(string(tuiservices.StopReasonVerificationFailed)),
		strings.ToLower(string(tuiservices.StopReasonRequiredTodoFailed)),
		strings.ToLower(string(tuiservices.StopReasonVerificationExecutionDenied)),
		strings.ToLower(string(tuiservices.StopReasonVerificationExecutionError)):
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "Verification failed"
		}
		a.state.ExecutionError = detail
		a.state.StatusText = detail
		a.appendActivity("run", "Verification failed", detail, true)
	case strings.ToLower(string(tuiservices.StopReasonBudgetExceeded)):
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "Context budget exceeded"
		}
		a.state.ExecutionError = ""
		a.state.StatusText = detail
		a.appendActivity("run", "Context budget exceeded", detail, false)
	case strings.ToLower(string(tuiservices.StopReasonMaxTurnExceeded)):
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "Max turns reached"
		}
		a.state.ExecutionError = ""
		a.state.StatusText = detail
		a.appendActivity("run", "Max turn limit reached", detail, false)
	case strings.ToLower(string(tuiservices.StopReasonFatalError)):
		detail := strings.TrimSpace(payload.Detail)
		if detail == "" {
			detail = "runtime stopped"
		}
		a.state.ExecutionError = detail
		a.state.StatusText = detail
		a.appendActivity("run", "Runtime stopped", detail, true)
	default:
		detail := "unknown stop reason: " + strings.TrimSpace(string(payload.Reason))
		a.state.ExecutionError = detail
		a.state.StatusText = detail
		a.appendActivity("run", "Runtime stopped", detail, true)
	}
	return false
}

func runtimeEventTodoUpdatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(a.state.ActiveSessionID)
	}
	if strings.TrimSpace(sessionID) == "" || !strings.EqualFold(sessionID, strings.TrimSpace(a.state.ActiveSessionID)) {
		return false
	}

	payload, _ := parseTodoEventPayload(event.Payload)
	if len(payload.Items) > 0 {
		a.syncTodosFromEventItems(payload.Items)
	} else if err := a.refreshTodosFromSession(sessionID); err != nil {
		a.appendActivity("todo", "Failed to refresh todo panel", err.Error(), true)
		return false
	}
	a.state.StatusText = formatTodoSummaryStatus(payload.Summary)
	action := strings.TrimSpace(payload.Action)
	if action == "" {
		action = "update"
	}
	detail := action
	if payload.Summary.RequiredTotal > 0 {
		detail = fmt.Sprintf(
			"%s (required %d/%d open=%d failed=%d)",
			action,
			payload.Summary.RequiredCompleted,
			payload.Summary.RequiredTotal,
			payload.Summary.RequiredOpen,
			payload.Summary.RequiredFailed,
		)
	}
	a.appendActivity("todo", "Todo updated", detail, false)
	return false
}

func runtimeEventTodoConflictHandler(a *App, event tuiservices.RuntimeEvent) bool {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(a.state.ActiveSessionID)
	}
	if strings.TrimSpace(sessionID) == "" || !strings.EqualFold(sessionID, strings.TrimSpace(a.state.ActiveSessionID)) {
		return false
	}

	payload, _ := parseTodoEventPayload(event.Payload)
	if len(payload.Items) > 0 {
		a.syncTodosFromEventItems(payload.Items)
	} else if err := a.refreshTodosFromSession(sessionID); err != nil {
		a.appendActivity("todo", "Failed to refresh todo panel", err.Error(), true)
		return false
	}
	a.state.StatusText = formatTodoSummaryStatus(payload.Summary)
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "todo conflict"
	}
	if payload.Summary.RequiredTotal > 0 {
		reason = fmt.Sprintf(
			"%s (required %d/%d open=%d failed=%d)",
			reason,
			payload.Summary.RequiredCompleted,
			payload.Summary.RequiredTotal,
			payload.Summary.RequiredOpen,
			payload.Summary.RequiredFailed,
		)
	}
	a.appendActivity("todo", "Todo conflict", reason, true)
	return false
}

// runtimeEventTodoSnapshotUpdatedHandler 处理 todo_snapshot_updated 事件并实时同步 Todo 面板。
func runtimeEventTodoSnapshotUpdatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	return runtimeEventTodoUpdatedHandler(a, event)
}

// runtimeEventRuntimeSnapshotUpdatedHandler 处理 runtime_snapshot_updated 事件并同步 Todo 摘要。
func runtimeEventRuntimeSnapshotUpdatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.RuntimeSnapshotUpdatedPayload)
	if !ok {
		return false
	}
	snapshot := payload.Snapshot
	if len(snapshot.Todos.Items) > 0 {
		a.syncTodosFromEventItems(snapshot.Todos.Items)
	}
	a.state.StatusText = formatTodoSummaryStatus(snapshot.Todos.Summary)
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "snapshot updated"
	}
	a.appendActivity("runtime", "Runtime snapshot updated", reason, false)
	return false
}

// runtimeEventFactsUpdatedHandler 处理 facts_updated 事件，输出简洁事实更新日志。
func runtimeEventFactsUpdatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.FactsUpdatedPayload)
	if !ok {
		return false
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "facts updated"
	}
	detail := reason
	if runtimeFacts, ok := payload.Facts.RuntimeFacts["progress"].(map[string]any); ok {
		if observed := coerceInt(runtimeFacts["observed_fact_count"]); observed > 0 {
			detail = fmt.Sprintf("%s (observed_facts=%d)", reason, observed)
		}
	}
	a.appendActivity("facts", "Runtime facts updated", detail, false)
	return false
}

// runtimeEventDecisionMadeHandler 处理 decision_made 事件，输出最终裁决摘要。
func runtimeEventDecisionMadeHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.DecisionMadePayload)
	if !ok {
		return false
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = "unknown"
	}
	statusLower := strings.ToLower(status)
	shouldRenderDecisionBlock := statusLower == "continue" || statusLower == "incomplete"
	if statusLower == "continue" || statusLower == "incomplete" {
		discardTrailingAssistantMessage(a)
		a.state.StreamingReply = false
		a.suppressAssistantForRun = strings.TrimSpace(event.RunID)
	}
	detail := strings.TrimSpace(payload.UserVisibleSummary)
	if detail == "" {
		detail = strings.TrimSpace(payload.InternalSummary)
	}
	if detail == "" {
		detail = "decision generated"
	}
	if statusLower == "continue" || statusLower == "incomplete" {
		if debugDetail := formatDecisionDebugDetail(payload); debugDetail != "" {
			detail = detail + " | " + debugDetail
		}
	}
	a.appendActivity("decision", "Final decision ("+status+")", detail, false)
	if shouldRenderDecisionBlock {
		a.appendInlineMessage(roleSystem, formatDecisionBlockMessage(payload))
	}
	return false
}

// runtimeEventSubAgentSnapshotUpdatedHandler 处理 subagent_snapshot_updated 事件，输出聚合计数。
func runtimeEventSubAgentSnapshotUpdatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.SubAgentSnapshotUpdatedPayload)
	if !ok {
		return false
	}
	detail := fmt.Sprintf(
		"started=%d completed=%d failed=%d",
		payload.SubAgent.StartedCount,
		payload.SubAgent.CompletedCount,
		payload.SubAgent.FailedCount,
	)
	a.appendActivity("subagent", "SubAgent snapshot updated", detail, false)
	return false
}

// runtimeEventSkillActivatedHandler 在 runtime 激活 skill 后同步活动日志。
func runtimeEventSkillActivatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := parseSessionSkillEventPayload(event.Payload)
	if !ok {
		return false
	}
	skillID := sanitizeSkillDisplayText(payload.SkillID, "(unknown)")
	a.appendActivity("skills", "Skill activated", skillID, false)
	return false
}

// runtimeEventSkillDeactivatedHandler 在 runtime 停用 skill 后同步活动日志。
func runtimeEventSkillDeactivatedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := parseSessionSkillEventPayload(event.Payload)
	if !ok {
		return false
	}
	skillID := sanitizeSkillDisplayText(payload.SkillID, "(unknown)")
	a.appendActivity("skills", "Skill deactivated", skillID, false)
	return false
}

// runtimeEventSkillMissingHandler 在会话 skill 缺失时输出显式错误反馈，便于排查恢复问题。
func runtimeEventSkillMissingHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := parseSessionSkillEventPayload(event.Payload)
	if !ok {
		return false
	}
	skillID := sanitizeSkillDisplayText(payload.SkillID, "(unknown)")
	a.appendActivity("skills", "Skill missing in registry", skillID, true)
	return false
}

// parseSessionSkillEventPayload 解析 runtime skill 事件负载，并兼容 map 结构。
func parseSessionSkillEventPayload(payload any) (tuiservices.SessionSkillEventPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.SessionSkillEventPayload:
		return typed, true
	case *tuiservices.SessionSkillEventPayload:
		if typed == nil {
			return tuiservices.SessionSkillEventPayload{}, false
		}
		return *typed, true
	case map[string]any:
		if raw, ok := typed["skill_id"]; ok && raw != nil {
			return tuiservices.SessionSkillEventPayload{SkillID: strings.TrimSpace(fmt.Sprintf("%v", raw))}, true
		}
		if raw, ok := typed["SkillID"]; ok && raw != nil {
			return tuiservices.SessionSkillEventPayload{SkillID: strings.TrimSpace(fmt.Sprintf("%v", raw))}, true
		}
		return tuiservices.SessionSkillEventPayload{}, false
	default:
		return tuiservices.SessionSkillEventPayload{}, false
	}
}

func parseTodoEventPayload(payload any) (tuiservices.TodoEventPayload, bool) {
	switch typed := payload.(type) {
	case tuiservices.TodoEventPayload:
		return typed, true
	case *tuiservices.TodoEventPayload:
		if typed == nil {
			return tuiservices.TodoEventPayload{}, false
		}
		return *typed, true
	case map[string]any:
		action := ""
		reason := ""
		summary := tuiservices.TodoSummary{}
		items := make([]tuiservices.TodoViewItem, 0)
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
		if raw, ok := typed["summary"]; ok {
			summary = coerceTodoSummary(raw)
		}
		if raw, ok := typed["required_total"]; ok && summary.RequiredTotal == 0 {
			summary.RequiredTotal = coerceInt(raw)
		}
		if raw, ok := typed["required_completed"]; ok && summary.RequiredCompleted == 0 {
			summary.RequiredCompleted = coerceInt(raw)
		}
		if raw, ok := typed["required_open"]; ok && summary.RequiredOpen == 0 {
			summary.RequiredOpen = coerceInt(raw)
		}
		if raw, ok := typed["required_failed"]; ok && summary.RequiredFailed == 0 {
			summary.RequiredFailed = coerceInt(raw)
		}
		if raw, ok := typed["total"]; ok && summary.Total == 0 {
			summary.Total = coerceInt(raw)
		}
		if raw, ok := typed["items"]; ok {
			items = coerceTodoItems(raw)
		} else if raw, ok := typed["todos"]; ok {
			items = coerceTodoItems(raw)
		}
		return tuiservices.TodoEventPayload{
			Action:  action,
			Reason:  reason,
			Items:   items,
			Summary: summary,
		}, true
	default:
		return tuiservices.TodoEventPayload{}, false
	}
}

// coerceInt 将 map 反序列化后的数字值统一转换为 int，供事件兼容解析使用。
func coerceInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

// coerceTodoSnapshots 将动态 payload 中的 todos 字段转换为强类型快照列表。
func coerceTodoItems(value any) []tuiservices.TodoViewItem {
	rawList, ok := value.([]any)
	if !ok || len(rawList) == 0 {
		return nil
	}
	snapshots := make([]tuiservices.TodoViewItem, 0, len(rawList))
	for _, raw := range rawList {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		snapshot := tuiservices.TodoViewItem{
			ID:            mapStringValue(item, "id"),
			Content:       mapStringValue(item, "content"),
			Status:        mapStringValue(item, "status"),
			Required:      coerceBool(item["required"]),
			FailureReason: mapStringValue(item, "failure_reason"),
			BlockedReason: mapStringValue(item, "blocked_reason"),
			Revision:      int64(coerceInt(item["revision"])),
		}
		if rawArtifacts, exists := item["artifacts"]; exists {
			snapshot.Artifacts = coerceStringSlice(rawArtifacts)
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil
	}
	return snapshots
}

func coerceTodoSummary(value any) tuiservices.TodoSummary {
	item, ok := value.(map[string]any)
	if !ok {
		return tuiservices.TodoSummary{}
	}
	return tuiservices.TodoSummary{
		Total:             coerceInt(item["total"]),
		RequiredTotal:     coerceInt(item["required_total"]),
		RequiredCompleted: coerceInt(item["required_completed"]),
		RequiredFailed:    coerceInt(item["required_failed"]),
		RequiredOpen:      coerceInt(item["required_open"]),
	}
}

func (a *App) syncTodosFromEventItems(items []tuiservices.TodoViewItem) {
	if len(items) == 0 {
		return
	}
	mapped := make([]todoViewItem, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, todoViewItem{
			ID:       strings.TrimSpace(item.ID),
			Title:    strings.TrimSpace(item.Content),
			Status:   strings.TrimSpace(item.Status),
			Priority: 0,
			Executor: "",
			Owner:    "-",
		})
	}
	a.todoItems = mapped
	a.todoPanelVisible = len(mapped) > 0
	a.todoSelectedIndex = clampTodoSelection(a.todoSelectedIndex, len(a.visibleTodoItems()))
	a.rebuildTodo()
	a.applyComponentLayout(false)
}

func formatTodoSummaryStatus(summary tuiservices.TodoSummary) string {
	if summary.RequiredTotal > 0 {
		if summary.RequiredFailed > 0 {
			return fmt.Sprintf(
				"Todo: %d/%d completed, %d failed",
				summary.RequiredCompleted,
				summary.RequiredTotal,
				summary.RequiredFailed,
			)
		}
		return fmt.Sprintf("Todo: %d/%d completed", summary.RequiredCompleted, summary.RequiredTotal)
	}
	if summary.Total > 0 {
		return fmt.Sprintf("Todo: %d open", max(0, summary.Total-summary.RequiredCompleted-summary.RequiredFailed))
	}
	return "Todo: empty"
}

// mapStringValue 从 map 中安全读取字符串字段，避免缺失键被格式化为 "<nil>"。
func mapStringValue(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

// coerceBool 将动态 payload 中的布尔值做兼容解析。
func coerceBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// coerceStringSlice 将动态数组字段转换为去空白后的字符串列表。
func coerceStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			trimmed := strings.TrimSpace(entry)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			trimmed := strings.TrimSpace(fmt.Sprintf("%v", entry))
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func (a *App) handleRuntimeEvent(event tuiservices.RuntimeEvent) bool {
	if !a.shouldHandleRuntimeEvent(event) {
		return false
	}
	handler, ok := runtimeEventHandlerRegistry[event.Type]
	if !ok {
		return false
	}
	return handler(a, event)
}

func (a *App) shouldHandleRuntimeEvent(event tuiservices.RuntimeEvent) bool {
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

func runtimeEventInputNormalizedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	if strings.TrimSpace(event.RunID) != "" {
		a.state.ActiveRunID = strings.TrimSpace(event.RunID)
	}
	payload, ok := event.Payload.(tuiservices.InputNormalizedPayload)
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

func runtimeEventAssetSavedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.AssetSavedPayload)
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

func runtimeEventAssetSaveFailedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.AssetSaveFailedPayload)
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

func runtimeEventUserMessageHandler(a *App, event tuiservices.RuntimeEvent) bool {
	runID := strings.TrimSpace(event.RunID)
	if runID != "" {
		a.state.ActiveRunID = runID
	}
	a.suppressAssistantForRun = ""
	if sessionID := strings.TrimSpace(event.SessionID); sessionID != "" {
		a.setActiveSessionID(sessionID)
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

func runtimeEventRunContextHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseRunContextPayload(event.Payload)
	if !ok {
		return false
	}
	mapped := tuiservices.MapRunContextPayload(event.RunID, event.SessionID, payload)
	a.state.RunContext = mapped
	if strings.TrimSpace(mapped.SessionID) != "" {
		a.setActiveSessionID(mapped.SessionID)
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

func runtimeEventToolStatusHandler(a *App, event tuiservices.RuntimeEvent) bool {
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

func runtimeEventUsageHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := tuiservices.ParseUsagePayload(event.Payload)
	if !ok {
		return false
	}
	a.state.TokenUsage = tuiservices.MapUsagePayload(payload)
	return false
}

func runtimeEventTokenUsageHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.TokenUsagePayload)
	if !ok {
		return false
	}
	a.state.TokenUsage = tuiservices.MapTokenUsagePayload(payload, a.state.TokenUsage)
	return false
}

// runtimeEventToolCallThinkingHandler 在工具调用进入规划阶段时同步当前工具和进度提示。
func runtimeEventToolCallThinkingHandler(a *App, event tuiservices.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.CurrentTool = payload
		a.setRunProgress(0.35, "Planning")
		a.appendActivity("tool", "Planning tool call", payload, false)
	}
	return false
}

// runtimeEventToolStartHandler 在工具开始执行时更新状态栏和活动记录。
func runtimeEventToolStartHandler(a *App, event tuiservices.RuntimeEvent) bool {
	a.state.StatusText = statusRunningTool
	a.state.StreamingReply = false
	if payload, ok := event.Payload.(providertypes.ToolCall); ok {
		a.state.CurrentTool = payload.Name
		a.setRunProgress(0.6, "Running tool")
		a.appendActivity("tool", "Running tool", payload.Name, false)
	}
	return false
}

func runtimeEventToolResultHandler(a *App, event tuiservices.RuntimeEvent) bool {
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

// runtimeEventAgentChunkHandler 将流式回复分片持续追加到转录区，并推进运行进度。
func runtimeEventAgentChunkHandler(a *App, event tuiservices.RuntimeEvent) bool {
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

func runtimeEventToolChunkHandler(a *App, event tuiservices.RuntimeEvent) bool {
	if payload, ok := event.Payload.(string); ok && strings.TrimSpace(payload) != "" {
		a.state.StatusText = statusRunningTool
		a.appendActivity("tool", "Tool output", preview(payload, 88, 4), false)
	}
	return false
}

// runtimeEventAgentDoneHandler 在代理回复结束时收尾状态，并补齐最终 assistant 消息。
func runtimeEventAgentDoneHandler(a *App, event tuiservices.RuntimeEvent) bool {
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.pendingAutoPermission = nil
	a.clearRunProgress()
	if strings.TrimSpace(a.state.ExecutionError) == "" {
		a.state.StatusText = statusReady
	}
	if payload, ok := event.Payload.(providertypes.Message); ok {
		if shouldSuppressAssistantFinalMessage(a, strings.TrimSpace(event.RunID)) {
			return false
		}
		content := renderMessagePartsForDisplay(payload.Parts)
		if strings.TrimSpace(content) != "" && !a.lastAssistantMatches(content) {
			a.activeMessages = append(a.activeMessages, providertypes.Message{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart(content)}})
		}
		return true
	}
	return false
}

func runtimeEventRunCanceledHandler(a *App, event tuiservices.RuntimeEvent) bool {
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.pendingAutoPermission = nil
	a.state.ExecutionError = ""
	a.state.StatusText = statusCanceled
	a.clearRunProgress()
	a.appendActivity("run", "Canceled current run", "", false)
	return false
}

// runtimeEventErrorHandler 在运行报错时统一清理现场，并展示错误信息。
func runtimeEventErrorHandler(a *App, event tuiservices.RuntimeEvent) bool {
	a.state.StatusText = statusError
	a.state.IsAgentRunning = false
	a.state.StreamingReply = false
	a.state.CurrentTool = ""
	a.state.ActiveRunID = ""
	a.pendingPermission = nil
	a.pendingAutoPermission = nil
	a.clearRunProgress()
	if payload, ok := event.Payload.(string); ok {
		a.state.ExecutionError = payload
		a.state.StatusText = payload
		a.appendActivity("run", "Runtime error", payload, true)
	}
	return false
}

func runtimeEventPermissionRequestHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := parsePermissionRequestPayload(event.Payload)
	if !ok {
		return false
	}
	if a.beginAutoPermissionApproval(payload) {
		return false
	}

	if a.pendingPermission != nil {
		currentRequestID := strings.TrimSpace(a.pendingPermission.Request.RequestID)
		nextRequestID := strings.TrimSpace(payload.RequestID)
		if currentRequestID != "" && currentRequestID != nextRequestID && !a.pendingPermission.Submitting {
			a.deferredEventCmd = runResolvePermission(a.runtime, currentRequestID, tuiservices.DecisionReject)
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
		permissionRequestActivityDetail(payload),
		false,
	)
	a.refreshPermissionPromptLayout()
	return false
}

// beginAutoPermissionApproval Full Access 模式下直接提session 级审批，并记录回执所需状态
func (a *App) beginAutoPermissionApproval(payload tuiservices.PermissionRequestPayload) bool {
	if !a.fullAccessModeEnabled {
		return false
	}

	requestID := strings.TrimSpace(payload.RequestID)
	if requestID == "" {
		return false
	}

	a.pendingPermission = nil
	a.pendingAutoPermission = &autoPermissionApprovalState{Request: payload}
	a.state.StatusText = statusPermissionSubmitting
	a.state.ExecutionError = ""
	a.deferredEventCmd = runResolvePermission(a.runtime, requestID, tuiservices.DecisionAllowSession)
	a.appendActivity("permission", "Full access auto-approved permission", permissionRequestActivityDetail(payload), false)
	a.refreshPermissionPromptLayout()
	return true
}

// permissionRequestActivityDetail 统一格式化权限请求活动详情，避免各分支重复拼接。
func permissionRequestActivityDetail(payload tuiservices.PermissionRequestPayload) string {
	return fmt.Sprintf("%s -> %s", fallbackText(payload.ToolName, "tool"), fallbackText(payload.Target, "(empty target)"))
}

func runtimeEventPermissionResolvedHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := parsePermissionResolvedPayload(event.Payload)
	if !ok {
		return false
	}

	if a.pendingPermission != nil && a.pendingPermission.Request.RequestID == payload.RequestID {
		a.pendingPermission = nil
	}
	if a.pendingAutoPermission != nil && a.pendingAutoPermission.Request.RequestID == payload.RequestID {
		a.pendingAutoPermission = nil
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

// refreshPermissionPromptLayout 在权限提示出现或消失后刷新布局，避免遮挡输入区。
func (a *App) refreshPermissionPromptLayout() {
	if a.width <= 0 || a.height <= 0 {
		return
	}
	a.applyComponentLayout(false)
}

func runtimeEventCompactDoneHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.CompactResult)
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

func runtimeEventCompactErrorHandler(a *App, event tuiservices.RuntimeEvent) bool {
	payload, ok := event.Payload.(tuiservices.CompactErrorPayload)
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

// shouldSuppressAssistantFinalMessage 判断是否应抑制当前 run 的 assistant 最终消息，防止 continue/incomplete 误展示“已完成”。
func shouldSuppressAssistantFinalMessage(a *App, runID string) bool {
	if a == nil {
		return false
	}
	targetRunID := strings.TrimSpace(a.suppressAssistantForRun)
	if targetRunID == "" {
		return false
	}
	if runID != "" && !strings.EqualFold(runID, targetRunID) {
		return false
	}
	a.suppressAssistantForRun = ""
	return true
}

// discardTrailingAssistantMessage 在继续/未完成裁决时移除尾部 assistant 文本，避免用户看到伪最终回复。
func discardTrailingAssistantMessage(a *App) {
	if a == nil || len(a.activeMessages) == 0 {
		return
	}
	last := a.activeMessages[len(a.activeMessages)-1]
	if last.Role != roleAssistant {
		return
	}
	a.activeMessages = a.activeMessages[:len(a.activeMessages)-1]
}

// formatDecisionBlockMessage 把 continue/incomplete 裁决渲染为用户友好的验收提示。
func formatDecisionBlockMessage(payload tuiservices.DecisionMadePayload) string {
	status := firstNonBlank(strings.TrimSpace(payload.Status), "unknown")
	reason := firstNonBlank(strings.TrimSpace(payload.StopReason), "n/a")

	lines := []string{
		"正在验收中...",
		"状态: " + status,
		"原因: " + humanizeDecisionReason(reason, payload.MissingFacts),
	}
	if summary := strings.TrimSpace(payload.UserVisibleSummary); summary != "" {
		lines = append(lines, "说明: "+summary)
	}
	lines = append(lines, "建议: "+humanizeDecisionSuggestion(payload))
	return strings.Join(lines, "\n")
}

// humanizeDecisionReason 将机器 stop reason 映射为用户可读原因，避免主界面暴露内部字段。
func humanizeDecisionReason(reason string, missingFacts []map[string]any) string {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch normalized {
	case "todo_not_converged":
		for _, fact := range missingFacts {
			kind := strings.ToLower(strings.TrimSpace(readStringFromMap(fact, "kind")))
			switch kind {
			case "verification_passed":
				return "文件写入尚未完成验证。"
			case "file_exists":
				return "尚未确认目标文件存在。"
			case "required_todo_terminal":
				return "仍有 required todo 未收敛。"
			case "post_execute_closure":
				return "需要先基于工具结果完成闭环。"
			}
		}
		return "任务仍缺少关键事实。"
	case "no_progress_after_final_intercept":
		return "连续多轮未产生新事实。"
	case "required_todo_failed":
		return "存在 required todo 失败。"
	default:
		if normalized == "" || normalized == "n/a" {
			return "系统正在校验任务事实。"
		}
		return reason
	}
}

// humanizeDecisionSuggestion 生成用户可执行的自然语言建议，不直接泄露工具参数 JSON。
func humanizeDecisionSuggestion(payload tuiservices.DecisionMadePayload) string {
	for _, fact := range payload.MissingFacts {
		kind := strings.ToLower(strings.TrimSpace(readStringFromMap(fact, "kind")))
		switch kind {
		case "verification_passed":
			return "请读取目标文件并确认内容符合预期。"
		case "file_exists":
			return "请确认目标文件已创建并可访问。"
		case "required_todo_terminal":
			return "请继续推进待办状态直至 required 项收敛。"
		case "post_execute_closure":
			return "请先基于最近工具结果补充闭环，再尝试完成。"
		}
	}
	if len(payload.RequiredNextActions) > 0 {
		return "请继续调用合适工具补充可验证事实。"
	}
	return "请继续推进任务并补充可验证事实。"
}

// readStringFromMap 读取 map 中的字符串字段，字段缺失或类型不匹配时返回空串。
func readStringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// formatDecisionDebugDetail 生成供活动日志/调试视图使用的结构化详情。
func formatDecisionDebugDetail(payload tuiservices.DecisionMadePayload) string {
	parts := make([]string, 0, 2)
	if len(payload.MissingFacts) > 0 {
		parts = append(parts, "missing_facts="+jsonCompactOrFallback(payload.MissingFacts))
	}
	if len(payload.RequiredNextActions) > 0 {
		parts = append(parts, "required_next_actions="+jsonCompactOrFallback(payload.RequiredNextActions))
	}
	return strings.Join(parts, " ")
}

// firstNonBlank 返回第一个非空候选值，保证展示字段稳定可读。
func firstNonBlank(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// jsonCompactOrFallback 将结构压缩成单行 JSON，序列化失败时回退为字符串表示。
func jsonCompactOrFallback(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "")
	err := encoder.Encode(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return strings.TrimSpace(buf.String())
}

// applyInlineCommandError 统一写入内联命令错误，并立即刷新转录区显示。
func (a *App) applyInlineCommandError(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	a.state.ExecutionError = message
	a.state.StatusText = message
	a.appendInlineMessage(roleError, message)
	a.rebuildTranscript()
}

// recordStaleSkillCommandResult 记录来自旧会话的 skill 命令结果，避免切会话后被静默丢弃。
func (a *App) recordStaleSkillCommandResult(requestSessionID, activeSessionID string, runErr error) {
	detail := fmt.Sprintf("result from session %q ignored after switching to %q", requestSessionID, activeSessionID)
	if runErr != nil {
		detail = fmt.Sprintf("%s; original error: %s", detail, runErr.Error())
	}
	a.appendActivity("skills", "Ignored stale skill command result", detail, runErr != nil)
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
	if isError {
		a.showFooterError(fallbackText(detail, title))
	}
	a.syncActivityViewport(previousCount)
	a.viewDirty = true
	a.addLogEntry(kind, title, detail)
	a.appendInlineMessage(roleSystem, formatActivityInlineLog(kind, title, detail))
	a.rebuildTranscript()
}

func formatActivityInlineLog(kind string, title string, detail string) string {
	category := strings.TrimSpace(kind)
	if category == "" {
		category = "log"
	}
	content := strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	if content == "" {
		content = detail
		detail = ""
	}
	if detail != "" {
		content = content + " | " + detail
	}
	return inlineLogMarker + category + ": " + strings.TrimSpace(content)
}

func isInlineLogMessage(message providertypes.Message) bool {
	if message.Role != roleSystem {
		return false
	}
	content := strings.TrimSpace(renderMessagePartsForDisplay(message.Parts))
	return strings.HasPrefix(content, inlineLogMarker)
}

func (a *App) syncFooterErrorToast() {
	current := strings.TrimSpace(a.state.ExecutionError)
	if current == "" {
		a.footerErrorLast = ""
		return
	}
	if strings.EqualFold(current, a.footerErrorLast) {
		return
	}
	a.footerErrorLast = current
	a.showFooterError(current)
}

func (a *App) showFooterError(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if !strings.HasPrefix(strings.ToLower(message), "error:") {
		message = "Error: " + message
	}
	a.footerErrorText = message
	a.footerErrorUntil = a.now().Add(footerErrorFlashDuration)
	// Schedule one tick immediately so the footer toast can expire even while idle.
	a.deferredFooterTick = appTickCmd()
}

func (a *App) clearActivities() {
	previousCount := len(a.activities)
	if previousCount == 0 {
		return
	}
	a.activities = nil
	a.syncActivityViewport(previousCount)
}

func (a *App) addLogEntry(kind string, title string, detail string) {
	level := "info"
	if strings.Contains(title, "error") || strings.Contains(title, "Error") || strings.Contains(title, "failed") {
		level = "error"
	} else if strings.Contains(title, "warn") || strings.Contains(title, "Warn") {
		level = "warn"
	}

	a.logEntries = append(a.logEntries, logEntry{
		Timestamp: time.Now(),
		Level:     level,
		Source:    kind,
		Message:   title + ": " + detail,
	})

	a.logEntries = clampLogEntries(a.logEntries)
	_, _, _, height := a.logViewerBounds()
	maxOffset := a.logViewerMaxOffset(height)
	if a.logViewerOffset > maxOffset {
		a.logViewerOffset = maxOffset
	}
	if strings.TrimSpace(a.state.ActiveSessionID) == "" {
		return
	}
	a.logPersistDirty = true
	a.logPersistVersion++
	a.deferredLogPersistCmd = scheduleLogPersistFlush(a.logPersistVersion)
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

func (a *App) handleLogViewerKey(msg tea.KeyMsg) bool {
	_, _, _, height := a.logViewerBounds()
	rows := a.logViewerRows(height)

	switch {
	case key.Matches(msg, a.keys.LogViewer), key.Matches(msg, a.keys.FocusInput):
		a.logViewerVisible = false
		a.restoreStatusAfterLogViewer()
		a.applyComponentLayout(false)
		a.viewDirty = true
	case key.Matches(msg, a.keys.ScrollUp):
		a.scrollLogViewer(-1, height)
	case key.Matches(msg, a.keys.ScrollDown):
		a.scrollLogViewer(1, height)
	case key.Matches(msg, a.keys.PageUp):
		a.scrollLogViewer(-rows, height)
	case key.Matches(msg, a.keys.PageDown):
		a.scrollLogViewer(rows, height)
	case key.Matches(msg, a.keys.Top):
		if a.logViewerOffset != 0 {
			a.logViewerOffset = 0
			a.viewDirty = true
		}
	case key.Matches(msg, a.keys.Bottom):
		maxOffset := a.logViewerMaxOffset(height)
		if a.logViewerOffset != maxOffset {
			a.logViewerOffset = maxOffset
			a.viewDirty = true
		}
	}
	return true
}

func (a *App) handleLogViewerMouse(msg tea.MouseMsg) bool {
	if !a.isMouseWithinLogViewer(msg) {
		return true
	}

	_, _, _, height := a.logViewerBounds()
	switch {
	case msg.Button == tea.MouseButtonWheelUp && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelUp):
		a.scrollLogViewer(-1, height)
	case msg.Button == tea.MouseButtonWheelDown && (msg.Action == tea.MouseActionPress || msg.Type == tea.MouseWheelDown):
		a.scrollLogViewer(1, height)
	}
	return true
}

func (a *App) scrollLogViewer(delta int, height int) {
	if delta == 0 {
		return
	}
	next := a.logViewerOffset + delta
	if next < 0 {
		next = 0
	}
	maxOffset := a.logViewerMaxOffset(height)
	if next > maxOffset {
		next = maxOffset
	}
	if next != a.logViewerOffset {
		a.logViewerOffset = next
		a.viewDirty = true
	}
}

func (a *App) handleTranscriptMouse(msg tea.MouseMsg) bool {
	if a.transcriptScrollbarDrag {
		switch {
		case msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion:
			a.setTranscriptOffsetFromScrollbarY(msg.Y)
			return true
		case msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease:
			a.transcriptScrollbarDrag = false
			a.setTranscriptOffsetFromScrollbarY(msg.Y)
			return true
		}
	}

	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && a.isMouseWithinTranscriptScrollbar(msg) {
		a.transcriptScrollbarDrag = true
		a.setTranscriptOffsetFromScrollbarY(msg.Y)
		return true
	}

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
			a.transcriptScrollbarDrag = false
			a.finishTextSelection()
		}
		return false
	}

	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return a.beginTextSelection(msg)
	case (msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion) && a.textSelection.dragging:
		return a.updateTextSelection(msg)
	case msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease:
		return a.finishTextSelection()
	case msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress:
		if a.hasTextSelection() {
			a.copySelectionToClipboard()
			return true
		}
		return false
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

func (a App) isMouseWithinTranscriptScrollbar(msg tea.MouseMsg) bool {
	x, y, width, height := a.transcriptScrollbarBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) isMouseWithinLogViewer(msg tea.MouseMsg) bool {
	x, y, width, height := a.logViewerBounds()
	if width <= 0 || height <= 0 {
		return false
	}
	return msg.X >= x && msg.X < x+width && msg.Y >= y && msg.Y < y+height
}

func (a App) logViewerBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	return contentX, contentY + headerBarHeight, lay.contentWidth, lay.contentHeight
}

func (a App) logViewerRows(height int) int {
	return max(1, height-5)
}

func (a App) logViewerMaxOffset(height int) int {
	return max(0, len(a.logEntries)-a.logViewerRows(height))
}

func (a App) transcriptBounds() (int, int, int, int) {
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	headerHeight := headerBarHeight
	bodyY := contentY + headerHeight

	streamX := contentX
	streamY := bodyY

	return streamX, streamY, a.transcript.Width, a.transcript.Height
}

func (a App) transcriptScrollbarBounds() (int, int, int, int) {
	lay := a.computeLayout()
	contentX := a.styles.doc.GetPaddingLeft()
	contentY := a.styles.doc.GetPaddingTop()
	bodyY := contentY + headerBarHeight
	scrollbarWidth := max(0, lay.contentWidth-a.transcript.Width)
	return contentX + a.transcript.Width, bodyY, scrollbarWidth, a.transcript.Height
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

	inputY := streamY + a.transcript.Height + a.activityPreviewHeight() + a.todoPreviewHeight() + a.commandMenuHeight(lay.contentWidth, lay.contentHeight)
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
	a.transcript.Width = max(1, lay.contentWidth-a.transcriptScrollbarWidth(lay.contentWidth))
	a.resizeCommandMenu()
	promptWidth := a.startupPanelWidth(lay.contentWidth)
	a.input.SetWidth(a.composerInnerWidth(promptWidth))
	a.input.SetHeight(a.composerHeight())
	transcriptHeight, activityHeight, _, todoHeight := a.waterfallMetrics(lay.contentWidth, lay.contentHeight)
	a.transcript.Height = transcriptHeight

	_ = activityHeight
	a.activity.Width = max(10, lay.contentWidth-4)
	a.activity.Height = 0

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
	} else if a.transcript.AtBottom() {
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
	if a.input.Value() == "" {
		return composerMinHeight
	}
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
		queued := a.renderQueuedInterventionBlock(width)
		if strings.TrimSpace(queued) == "" {
			a.setTranscriptContent(a.styles.empty.Width(width).Render(emptyConversationText))
			a.transcript.GotoTop()
			return
		}
		a.setTranscriptContent(queued)
		a.transcript.GotoTop()
		return
	}

	atBottom := a.transcript.AtBottom()
	var builder strings.Builder
	hasBlock := false
	lastRenderedRole := ""
	for _, message := range a.activeMessages {
		inlineLog := isInlineLogMessage(message)
		continuation := message.Role == roleAssistant && lastRenderedRole == roleAssistant
		if inlineLog && lastRenderedRole == roleAssistant {
			continuation = true
		}
		rendered, _ := a.renderMessageBlockWithCopy(message, width, 0, !continuation)
		if rendered == "" {
			continue
		}

		if hasBlock {
			separator := "\n\n"
			if continuation {
				separator = "\n"
			}
			builder.WriteString(separator)
		}
		builder.WriteString(rendered)
		hasBlock = true
		if !inlineLog {
			lastRenderedRole = message.Role
		}
	}

	if queued := a.renderQueuedInterventionBlock(width); strings.TrimSpace(queued) != "" {
		if hasBlock {
			builder.WriteString("\n\n")
		}
		builder.WriteString(queued)
		hasBlock = true
	}

	if !hasBlock {
		a.setTranscriptContent(a.styles.empty.Width(width).Render(emptyConversationText))
		a.transcript.GotoTop()
		return
	}

	a.setTranscriptContent(builder.String())
	if atBottom {
		a.transcript.GotoBottom()
	}
}

func (a *App) setTranscriptContent(content string) {
	normalized := normalizeTranscriptForDisplay(content)
	contentChanged := a.transcriptContent != normalized
	if contentChanged && a.textSelection.active && !a.textSelection.dragging {
		a.textSelection.active = false
		a.textSelection.dragging = false
		a.textSelection.startLine = 0
		a.textSelection.startCol = 0
		a.textSelection.endLine = 0
		a.textSelection.endCol = 0
	}
	a.transcriptContent = normalized
	if a.hasTextSelection() {
		a.transcript.SetContent(a.highlightTranscriptContent(normalized))
		return
	}
	a.transcript.SetContent(normalized)
}

func (a *App) highlightTranscriptContent(content string) string {
	lines := strings.Split(content, "\n")
	startLine, startCol, endLine, endCol, ok := a.textSelectionRange(lines)
	if !ok {
		return content
	}

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(startupMetaColor))

	for i := startLine; i <= endLine && i < len(lines); i++ {
		lineWidth := ansi.StringWidth(lines[i])
		selStart := 0
		selEnd := lineWidth
		if i == startLine {
			selStart = startCol
		}
		if i == endLine {
			selEnd = endCol
		}
		selStart = max(0, min(selStart, lineWidth))
		selEnd = max(selStart, min(selEnd, lineWidth))
		if selEnd <= selStart {
			continue
		}
		prefix := ansi.Cut(lines[i], 0, selStart)
		selected := ansi.Cut(lines[i], selStart, selEnd)
		suffix := ansi.Cut(lines[i], selEnd, lineWidth)
		lines[i] = prefix + highlightStyle.Render(selected) + suffix
	}
	return strings.Join(lines, "\n")
}

func normalizeTranscriptForDisplay(content string) string {
	return strings.ReplaceAll(content, "\t", "    ")
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
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageCompact))
			return true, nil
		}
		if strings.TrimSpace(a.state.ActiveSessionID) == "" {
			a.applyInlineCommandError("compact requires an existing session")
			return true, nil
		}
		if a.isBusy() {
			a.applyInlineCommandError("compact is already running, please wait")
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
	case slashCommandSkills:
		if strings.TrimSpace(rest) != "" {
			a.applyInlineCommandError(fmt.Sprintf("usage: %s", slashUsageSkills))
			return true, nil
		}
		return true, a.handleSkillsCommand()
	case slashCommandSkill:
		return true, a.handleSkillCommand(rest)
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
	a.setActiveSessionID("")
	a.startupScreenLocked = false
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
	a.queuedIntervention = nil
	a.pendingAutoPermission = nil
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

// handleStartupWakeSubmitMsg 处理启动期的一次性唤醒输入，并沿用普通发送链路触Submit
func (a *App) handleStartupWakeSubmitMsg(msg startupWakeSubmitMsg) tea.Cmd {
	a.startupWakeSubmitInput = nil
	text := strings.TrimSpace(msg.Input.Text)
	if text == "" {
		return nil
	}
	if workdir := strings.TrimSpace(msg.Input.Workdir); workdir != "" {
		a.setCurrentWorkdir(workdir)
		if err := a.refreshFileCandidates(); err != nil {
			a.state.ExecutionError = err.Error()
			a.state.StatusText = err.Error()
			return nil
		}
	}
	a.input.SetValue(text)
	a.state.InputText = text
	return a.beginAgentRun(text, nil)
}

func ListenForRuntimeEvent(sub <-chan tuiservices.RuntimeEvent) tea.Cmd {
	return tuiservices.ListenForRuntimeEventCmd(
		sub,
		func(event tuiservices.RuntimeEvent) tea.Msg { return RuntimeMsg{Event: event} },
		func() tea.Msg { return RuntimeClosedMsg{} },
	)
}

func runAgent(runtime tuiservices.Runtime, input tuiservices.PrepareInput) tea.Cmd {
	return tuiservices.RunSubmitCmd(
		runtime,
		input,
		func(err error) tea.Msg { return runFinishedMsg{Err: err} },
	)
}

func runResolvePermission(
	runtime tuiservices.Runtime,
	requestID string,
	decision tuiservices.PermissionResolutionDecision,
) tea.Cmd {
	return tuiservices.RunResolvePermissionCmd(
		runtime,
		tuiservices.PermissionResolutionInput{
			RequestID: strings.TrimSpace(requestID),
			Decision:  decision,
		},
		func(input tuiservices.PermissionResolutionInput, err error) tea.Msg {
			return permissionResolutionFinishedMsg{
				RequestID: input.RequestID,
				Decision:  string(input.Decision),
				Err:       err,
			}
		},
	)
}

func runCompact(runtime tuiservices.Runtime, sessionID string) tea.Cmd {
	return tuiservices.RunCompactCmd(
		runtime,
		tuiservices.CompactInput{SessionID: sessionID},
		func(err error) tea.Msg { return compactFinishedMsg{Err: err} },
	)
}

func (a *App) setActiveSessionID(sessionID string) {
	next := strings.TrimSpace(sessionID)
	current := strings.TrimSpace(a.state.ActiveSessionID)
	if next != "" {
		a.startupScreenLocked = false
	}
	if strings.EqualFold(current, next) {
		a.state.ActiveSessionID = next
		return
	}
	if current != "" && a.logPersistDirty {
		a.persistLogEntriesForActiveSession()
	}

	previousEntries := a.logEntries
	a.state.ActiveSessionID = next
	if next == "" {
		a.loadLogEntriesForSession("")
		return
	}

	loaded := a.readLogEntriesForSession(next)
	if current == "" && len(previousEntries) > 0 {
		loaded = append(loaded, previousEntries...)
		loaded = clampLogEntries(loaded)
	}
	a.logEntries = loaded
	a.logViewerOffset = 0
	a.clampLogViewerOffset()
	if current == "" && len(previousEntries) > 0 {
		a.persistLogEntriesForActiveSession()
	}
}

func (a *App) loadLogEntriesForSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		a.logEntries = nil
		a.logViewerOffset = 0
		return
	}
	a.logEntries = a.readLogEntriesForSession(sessionID)
	a.logViewerOffset = 0
	a.clampLogViewerOffset()
}

func (a *App) readLogEntriesForSession(sessionID string) []logEntry {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	runtimeWithPersistence := a.sessionLogRuntime()
	if runtimeWithPersistence == nil {
		return nil
	}
	entries, err := runtimeWithPersistence.LoadSessionLogEntries(context.Background(), sessionID)
	if err != nil {
		a.reportLogPersistenceError("load", err)
		return nil
	}
	return clampLogEntries(fromRuntimeSessionLogEntries(entries))
}

func (a *App) persistLogEntriesForActiveSession() {
	sessionID := strings.TrimSpace(a.state.ActiveSessionID)
	if sessionID == "" {
		a.logPersistDirty = false
		return
	}

	runtimeWithPersistence := a.sessionLogRuntime()
	if runtimeWithPersistence == nil {
		a.logPersistDirty = false
		return
	}
	if err := runtimeWithPersistence.SaveSessionLogEntries(
		context.Background(),
		sessionID,
		toRuntimeSessionLogEntries(clampLogEntries(a.logEntries)),
	); err != nil {
		a.reportLogPersistenceError("save", err)
		a.logPersistVersion++
		a.deferredLogPersistCmd = scheduleLogPersistFlush(a.logPersistVersion)
		return
	}
	a.logPersistDirty = false
}

// sessionLogRuntime 返回支持会话日志读写的 runtime 适配能力。
func (a *App) sessionLogRuntime() sessionLogPersistenceRuntime {
	runtimeWithPersistence, ok := a.runtime.(sessionLogPersistenceRuntime)
	if !ok {
		return nil
	}
	return runtimeWithPersistence
}

// reportLogPersistenceError 统一处理日志持久化失败提示，避免错误被静默吞掉。
func (a *App) reportLogPersistenceError(action string, err error) {
	if err == nil {
		return
	}
	message := fmt.Sprintf("Failed to %s log entries: %v", strings.TrimSpace(action), err)
	a.state.StatusText = message
	a.showFooterError(message)
}

// restoreStatusAfterLogViewer 关闭日志视图后恢复可读状态，避免覆盖真实运行态。
func (a *App) restoreStatusAfterLogViewer() {
	defer func() { a.logViewerPrevStatus = "" }()
	if executionError := strings.TrimSpace(a.state.ExecutionError); executionError != "" {
		a.state.StatusText = executionError
		return
	}
	if a.state.IsCompacting {
		a.state.StatusText = statusCompacting
		return
	}
	if a.state.IsAgentRunning {
		if strings.TrimSpace(a.state.CurrentTool) != "" {
			a.state.StatusText = statusRunningTool
		} else {
			a.state.StatusText = statusThinking
		}
		return
	}
	if prev := strings.TrimSpace(a.logViewerPrevStatus); prev != "" {
		a.state.StatusText = prev
		return
	}
	a.state.StatusText = statusReady
}

// toRuntimeSessionLogEntries 将日志条目转换为 runtime 持久化模型。
func toRuntimeSessionLogEntries(entries []logEntry) []tuiservices.SessionLogEntry {
	converted := make([]tuiservices.SessionLogEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, tuiservices.SessionLogEntry{
			Timestamp: entry.Timestamp,
			Level:     entry.Level,
			Source:    entry.Source,
			Message:   entry.Message,
		})
	}
	return converted
}

// fromRuntimeSessionLogEntries 将 runtime 持久化模型还原为 TUI 展示模型。
func fromRuntimeSessionLogEntries(entries []tuiservices.SessionLogEntry) []logEntry {
	converted := make([]logEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, logEntry{
			Timestamp: entry.Timestamp,
			Level:     entry.Level,
			Source:    entry.Source,
			Message:   entry.Message,
		})
	}
	return converted
}

func clampLogEntries(entries []logEntry) []logEntry {
	if len(entries) <= logViewerEntryLimit {
		return entries
	}
	return append([]logEntry(nil), entries[len(entries)-logViewerEntryLimit:]...)
}

func (a *App) clampLogViewerOffset() {
	_, _, _, height := a.logViewerBounds()
	maxOffset := a.logViewerMaxOffset(height)
	if a.logViewerOffset > maxOffset {
		a.logViewerOffset = maxOffset
	}
}

func (a App) transcriptMaxOffset() int {
	return max(0, a.transcript.TotalLineCount()-a.transcript.VisibleLineCount())
}

func (a *App) setTranscriptOffsetFromScrollbarY(mouseY int) {
	_, y, _, height := a.transcriptScrollbarBounds()
	if height <= 0 {
		return
	}
	maxOffset := a.transcriptMaxOffset()
	if maxOffset <= 0 {
		a.transcript.SetYOffset(0)
		return
	}
	relative := mouseY - y
	if relative < 0 {
		relative = 0
	}
	if relative >= height {
		relative = height - 1
	}

	denominator := max(1, height-1)
	target := (relative*maxOffset + denominator/2) / denominator
	target = max(0, min(target, maxOffset))
	if target != a.transcript.YOffset {
		a.transcript.SetYOffset(target)
	}
}

// isBusy reports whether a runtime operation is in progress.
// Pasted-text loading is intentionally excluded so typing and navigation remain available.
func (a App) isBusy() bool {
	return tuiutils.IsBusy(a.state.IsAgentRunning, a.state.IsCompacting)
}

func (a *App) handleMemoCommand() tea.Cmd {
	return a.runMemoSystemTool(tools.ToolNameMemoList, map[string]any{})
}

func (a *App) handleRememberCommand(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Usage: %s", slashUsageRemember))
		a.rebuildTranscript()
		return nil
	}
	return a.runMemoSystemTool(tools.ToolNameMemoRemember, map[string]any{
		"type":    "user",
		"title":   text,
		"content": text,
	})
}

func (a *App) handleForgetCommand(keyword string) tea.Cmd {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Usage: %s", slashUsageForget))
		a.rebuildTranscript()
		return nil
	}
	return a.runMemoSystemTool(tools.ToolNameMemoRemove, map[string]any{
		"keyword": keyword,
		"scope":   "all",
	})
}

func (a *App) runMemoSystemTool(toolName string, arguments map[string]any) tea.Cmd {
	payload, err := json.Marshal(arguments)
	if err != nil {
		a.appendInlineMessage(roleError, fmt.Sprintf("[System] Failed to encode memo command: %s", err))
		a.rebuildTranscript()
		return nil
	}

	return tuiservices.RunSystemToolCmd(
		a.runtime,
		tuiservices.SystemToolInput{
			SessionID: a.state.ActiveSessionID,
			Workdir:   a.state.CurrentWorkdir,
			ToolName:  toolName,
			Arguments: payload,
		},
		func(result tools.ToolResult, err error) tea.Msg {
			if err != nil {
				message := strings.TrimSpace(result.Content)
				if message == "" {
					message = normalizeMemoCommandErrorMessage(err)
				}
				return localCommandResultMsg{Err: errors.New(message)}
			}
			notice := strings.TrimSpace(result.Content)
			if notice == "" {
				notice = "Memo command completed."
			}
			return localCommandResultMsg{Notice: notice}
		},
	)
}

// normalizeMemoCommandErrorMessage memo 命令的底层错误映射为用户可读提示，避免暴露内sentinel 文本
func normalizeMemoCommandErrorMessage(err error) string {
	if err == nil {
		return "memo command failed"
	}
	if isGatewayUnsupportedActionError(err) {
		return "gateway does not support memo commands; please upgrade gateway and client to the latest version"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "memo command failed"
	}
	return message
}

// syncSessionWorkdir 依据会话快照更新当前工作区；若路径失效可选择展示告警并保留现有目录
func (a *App) syncSessionWorkdir(sessionWorkdir string, warnOnMissing bool) {
	resolved := strings.TrimSpace(agentsession.EffectiveWorkdir(sessionWorkdir, a.configManager.Get().Workdir))
	if resolved == "" || !filepath.IsAbs(resolved) {
		return
	}
	if !warnOnMissing {
		a.setCurrentWorkdir(resolved)
		return
	}

	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		if warnOnMissing {
			a.showFooterError(sessionWorkdirMissingWarning)
		}
		return
	}

	a.setCurrentWorkdir(resolved)
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
	providerAddFieldModelSource
	providerAddFieldChatAPIMode
	providerAddFieldBaseURL
	providerAddFieldChatEndpointPath
	providerAddFieldDiscoveryEndpointPath
	providerAddFieldAPIKeyEnv
	providerAddFieldAPIKey
)

func providerAddVisibleFields(driver string, modelSource string) []providerAddFieldID {
	fields := []providerAddFieldID{
		providerAddFieldName,
		providerAddFieldDriver,
		providerAddFieldModelSource,
	}
	if provider.NormalizeProviderDriver(driver) == provider.DriverOpenAICompat {
		fields = append(fields, providerAddFieldChatAPIMode)
	}
	fields = append(fields,
		providerAddFieldBaseURL,
		providerAddFieldChatEndpointPath,
	)

	if config.NormalizeModelSource(strings.TrimSpace(modelSource)) == config.ModelSourceDiscover {
		fields = append(fields, providerAddFieldDiscoveryEndpointPath)
	}
	fields = append(fields, providerAddFieldAPIKeyEnv, providerAddFieldAPIKey)
	return fields
}

func clampProviderAddStep(form *providerAddFormState) {
	if form == nil {
		return
	}
	fields := providerAddVisibleFields(form.Driver, form.ModelSource)
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
	fields := providerAddVisibleFields(form.Driver, form.ModelSource)
	if len(fields) == 0 {
		return providerAddFieldName
	}
	return fields[form.Step]
}

// isProviderAddEnumField 判断当前新增 Provider 表单焦点是否位于枚举字段（Driver/Model Source）。
func isProviderAddEnumField(form *providerAddFormState) bool {
	switch currentProviderAddField(form) {
	case providerAddFieldDriver, providerAddFieldModelSource, providerAddFieldChatAPIMode:
		return true
	default:
		return false
	}
}

func (a *App) startProviderAddForm() {
	a.providerAddForm = &providerAddFormState{
		Stage:                 providerAddFormStageFields,
		Step:                  0,
		Name:                  "",
		Driver:                provider.DriverOpenAICompat,
		ModelSource:           config.ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		BaseURL:               "",
		ChatEndpointPath:      providerAddDefaultChatEndpointPath(provider.DriverOpenAICompat),
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		ManualModelsJSON:      "",
		APIKeyEnv:             "",
		APIKey:                "",
		Error:                 "",
		ErrorIsHard:           false,
		Drivers:               []string{provider.DriverOpenAICompat, provider.DriverGemini, provider.DriverAnthropic},
		ModelSources:          []string{config.ModelSourceDiscover, config.ModelSourceManual},
		ChatAPIModes:          []string{provider.ChatAPIModeChatCompletions, provider.ChatAPIModeResponses},
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
	if a.providerAddForm.Stage == providerAddFormStageManualModels {
		switch {
		case key.Matches(typed, a.keys.Send):
			return a, a.submitProviderAddForm()
		case key.Matches(typed, a.keys.FocusInput):
			a.providerAddForm = nil
			a.state.ActivePicker = pickerNone
			a.state.StatusText = statusReady
			return a, nil
		case key.Matches(typed, a.keys.PrevPanel):
			a.providerAddForm.Stage = providerAddFormStageFields
			a.providerAddForm.Error = ""
			a.providerAddForm.ErrorIsHard = false
			return a, nil
		case typed.Type == tea.KeyBackspace:
			a.providerAddForm.ManualModelsJSON = trimLastRune(a.providerAddForm.ManualModelsJSON)
			return a, nil
		case key.Matches(typed, a.keys.Newline):
			a.providerAddForm.ManualModelsJSON += "\n"
			return a, nil
		default:
			if len(typed.Runes) > 0 {
				a.providerAddForm.ManualModelsJSON += sanitizeProviderAddJSONInputRunes(typed.Runes)
			}
			return a, nil
		}
	}

	prevStep := a.providerAddForm.Step
	fields := providerAddVisibleFields(a.providerAddForm.Driver, a.providerAddForm.ModelSource)
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
		case providerAddFieldBaseURL:
			a.providerAddForm.BaseURL = trimLastRune(a.providerAddForm.BaseURL)
		case providerAddFieldChatEndpointPath:
			a.providerAddForm.ChatEndpointPath = trimLastRune(a.providerAddForm.ChatEndpointPath)
		case providerAddFieldDiscoveryEndpointPath:
			a.providerAddForm.DiscoveryEndpointPath = trimLastRune(a.providerAddForm.DiscoveryEndpointPath)
		case providerAddFieldAPIKeyEnv:
			a.providerAddForm.APIKeyEnv = trimLastRune(a.providerAddForm.APIKeyEnv)
		case providerAddFieldAPIKey:
			a.providerAddForm.APIKey = trimLastRune(a.providerAddForm.APIKey)
		}
		return a, nil
	case typed.Type == tea.KeyUp || (isProviderAddEnumField(a.providerAddForm) && key.Matches(typed, a.keys.ScrollUp)):
		if currentProviderAddField(a.providerAddForm) == providerAddFieldDriver {
			currentIdx := -1
			for i, d := range a.providerAddForm.Drivers {
				if d == a.providerAddForm.Driver {
					currentIdx = i
					break
				}
			}
			if currentIdx >= 0 {
				previousDriver := a.providerAddForm.Driver
				currentIdx = (currentIdx - 1 + len(a.providerAddForm.Drivers)) % len(a.providerAddForm.Drivers)
				a.providerAddForm.Driver = a.providerAddForm.Drivers[currentIdx]
				syncProviderAddDriverDefaults(a.providerAddForm, previousDriver)
				clampProviderAddStep(a.providerAddForm)
			}
		} else if currentProviderAddField(a.providerAddForm) == providerAddFieldModelSource {
			currentIdx := 0
			for i, source := range a.providerAddForm.ModelSources {
				if source == a.providerAddForm.ModelSource {
					currentIdx = i
					break
				}
			}
			currentIdx = (currentIdx - 1 + len(a.providerAddForm.ModelSources)) % len(a.providerAddForm.ModelSources)
			a.providerAddForm.ModelSource = a.providerAddForm.ModelSources[currentIdx]
			clampProviderAddStep(a.providerAddForm)
		} else if currentProviderAddField(a.providerAddForm) == providerAddFieldChatAPIMode {
			previousMode := a.providerAddForm.ChatAPIMode
			currentIdx := 0
			for i, mode := range a.providerAddForm.ChatAPIModes {
				if mode == a.providerAddForm.ChatAPIMode {
					currentIdx = i
					break
				}
			}
			currentIdx = (currentIdx - 1 + len(a.providerAddForm.ChatAPIModes)) % len(a.providerAddForm.ChatAPIModes)
			a.providerAddForm.ChatAPIMode = a.providerAddForm.ChatAPIModes[currentIdx]
			syncProviderAddOpenAICompatModeDefaults(a.providerAddForm, previousMode)
			clampProviderAddStep(a.providerAddForm)
		}
		return a, nil
	case typed.Type == tea.KeyDown || (isProviderAddEnumField(a.providerAddForm) && key.Matches(typed, a.keys.ScrollDown)):
		if currentProviderAddField(a.providerAddForm) == providerAddFieldDriver {
			currentIdx := -1
			for i, d := range a.providerAddForm.Drivers {
				if d == a.providerAddForm.Driver {
					currentIdx = i
					break
				}
			}
			if currentIdx >= 0 {
				previousDriver := a.providerAddForm.Driver
				currentIdx = (currentIdx + 1) % len(a.providerAddForm.Drivers)
				a.providerAddForm.Driver = a.providerAddForm.Drivers[currentIdx]
				syncProviderAddDriverDefaults(a.providerAddForm, previousDriver)
				clampProviderAddStep(a.providerAddForm)
			}
		} else if currentProviderAddField(a.providerAddForm) == providerAddFieldModelSource {
			currentIdx := 0
			for i, source := range a.providerAddForm.ModelSources {
				if source == a.providerAddForm.ModelSource {
					currentIdx = i
					break
				}
			}
			currentIdx = (currentIdx + 1) % len(a.providerAddForm.ModelSources)
			a.providerAddForm.ModelSource = a.providerAddForm.ModelSources[currentIdx]
			clampProviderAddStep(a.providerAddForm)
		} else if currentProviderAddField(a.providerAddForm) == providerAddFieldChatAPIMode {
			previousMode := a.providerAddForm.ChatAPIMode
			currentIdx := 0
			for i, mode := range a.providerAddForm.ChatAPIModes {
				if mode == a.providerAddForm.ChatAPIMode {
					currentIdx = i
					break
				}
			}
			currentIdx = (currentIdx + 1) % len(a.providerAddForm.ChatAPIModes)
			a.providerAddForm.ChatAPIMode = a.providerAddForm.ChatAPIModes[currentIdx]
			syncProviderAddOpenAICompatModeDefaults(a.providerAddForm, previousMode)
			clampProviderAddStep(a.providerAddForm)
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
				case providerAddFieldChatEndpointPath:
					a.providerAddForm.ChatEndpointPath += cleanInput
				case providerAddFieldDiscoveryEndpointPath:
					a.providerAddForm.DiscoveryEndpointPath += cleanInput
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

	formForValidation := *a.providerAddForm
	if formForValidation.Stage == providerAddFormStageFields &&
		config.NormalizeModelSource(normalizeProviderAddFieldValue(formForValidation.ModelSource)) == config.ModelSourceManual &&
		strings.TrimSpace(formForValidation.ManualModelsJSON) == "" {
		formForValidation.ManualModelsJSON = providerAddManualModelsJSONTemplate
	}

	request, validationErr := buildProviderAddRequest(formForValidation)
	if validationErr != "" {
		a.providerAddForm.Error = "Please update the form: " + validationErr
		a.providerAddForm.ErrorIsHard = false
		return nil
	}
	if request.ModelSource == config.ModelSourceManual && a.providerAddForm.Stage == providerAddFormStageFields {
		a.providerAddForm.Stage = providerAddFormStageManualModels
		a.providerAddForm.Error = ""
		a.providerAddForm.ErrorIsHard = false
		a.state.StatusText = "Fill manual model JSON"
		return nil
	}
	if request.ModelSource == config.ModelSourceManual && strings.TrimSpace(request.ManualModelsJSON) == "" {
		a.providerAddForm.Error = "Please update the form: Model JSON is required for manual model source"
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
	Name                  string
	Driver                string
	BaseURL               string
	ChatAPIMode           string
	ChatEndpointPath      string
	ModelSource           string
	ManualModelsJSON      string
	DiscoveryEndpointPath string
	APIKeyEnv             string
	APIKey                string
}

type providerAddResultMsg struct {
	Name    string
	Model   string
	Error   string
	Warning string
}

type modelScopeGuideOpenResultMsg struct {
	Target string
	Error  string
}

type modelScopeGuideSubmitResultMsg struct {
	Selection configstate.Selection
	Error     string
	Warning   string
}

// providerAddDefaultChatEndpointPath 返回 provider add 表单按 driver 推导的默认 chat endpoint。
func providerAddDefaultChatEndpointPath(driver string) string {
	switch provider.NormalizeProviderDriver(driver) {
	case provider.DriverGemini:
		return "/models"
	case provider.DriverAnthropic:
		return "/messages"
	default:
		return "/chat/completions"
	}
}

// providerAddDefaultOpenAICompatChatEndpointPath 根据 chat_api_mode 返回 openaicompat 的默认 chat endpoint。
func providerAddDefaultOpenAICompatChatEndpointPath(chatAPIMode string) string {
	mode, err := provider.NormalizeProviderChatAPIMode(chatAPIMode)
	if err != nil || mode == "" {
		mode = provider.DefaultProviderChatAPIMode()
	}
	if mode == provider.ChatAPIModeResponses {
		return "/responses"
	}
	return "/chat/completions"
}

// syncProviderAddOpenAICompatModeDefaults 在切换 chat_api_mode 时同步默认 chat endpoint，避免默认值错配。
func syncProviderAddOpenAICompatModeDefaults(form *providerAddFormState, previousMode string) {
	if form == nil || provider.NormalizeProviderDriver(form.Driver) != provider.DriverOpenAICompat {
		return
	}

	currentPath := strings.TrimSpace(form.ChatEndpointPath)
	previousDefaultPath := providerAddDefaultOpenAICompatChatEndpointPath(previousMode)
	if currentPath != "" && currentPath != previousDefaultPath {
		return
	}
	form.ChatEndpointPath = providerAddDefaultOpenAICompatChatEndpointPath(form.ChatAPIMode)
}

// providerAddDefaultBaseURL 返回 provider add 表单按 driver 推导的默认 base URL。
func providerAddDefaultBaseURL(driver string) string {
	switch provider.NormalizeProviderDriver(driver) {
	case provider.DriverOpenAICompat:
		return config.OpenAIDefaultBaseURL
	case provider.DriverGemini:
		return config.GeminiDefaultBaseURL
	case provider.DriverAnthropic:
		return config.AnthropicDefaultBaseURL
	default:
		return ""
	}
}

// syncProviderAddDriverDefaults 在切换 driver 时按需同步默认 base URL 与 chat endpoint。
func syncProviderAddDriverDefaults(form *providerAddFormState, previousDriver string) {
	if form == nil {
		return
	}
	oldBaseURL := providerAddDefaultBaseURL(previousDriver)
	newBaseURL := providerAddDefaultBaseURL(form.Driver)
	currentBaseURL := strings.TrimSpace(form.BaseURL)
	if newBaseURL != "" && (currentBaseURL == "" || (oldBaseURL != "" && currentBaseURL == oldBaseURL)) {
		form.BaseURL = newBaseURL
	}

	oldChatPath := providerAddDefaultChatEndpointPath(previousDriver)
	newChatPath := providerAddDefaultChatEndpointPath(form.Driver)
	currentChatPath := strings.TrimSpace(form.ChatEndpointPath)
	if currentChatPath == "" || currentChatPath == oldChatPath {
		form.ChatEndpointPath = newChatPath
	}
	if provider.NormalizeProviderDriver(form.Driver) == provider.DriverOpenAICompat {
		if _, err := provider.NormalizeProviderChatAPIMode(form.ChatAPIMode); err != nil || strings.TrimSpace(form.ChatAPIMode) == "" {
			form.ChatAPIMode = provider.DefaultProviderChatAPIMode()
		}
	} else {
		form.ChatAPIMode = ""
	}
}

func buildProviderAddRequest(form providerAddFormState) (providerAddRequest, string) {
	request := providerAddRequest{
		Name:                  normalizeProviderAddFieldValue(form.Name),
		Driver:                provider.NormalizeProviderDriver(normalizeProviderAddFieldValue(form.Driver)),
		ModelSource:           config.NormalizeModelSource(normalizeProviderAddFieldValue(form.ModelSource)),
		ChatAPIMode:           normalizeProviderAddFieldValue(form.ChatAPIMode),
		BaseURL:               normalizeProviderAddFieldValue(form.BaseURL),
		ChatEndpointPath:      normalizeProviderAddFieldValue(form.ChatEndpointPath),
		ManualModelsJSON:      strings.TrimSpace(form.ManualModelsJSON),
		DiscoveryEndpointPath: normalizeProviderAddFieldValue(form.DiscoveryEndpointPath),
		APIKeyEnv:             normalizeProviderAddFieldValue(form.APIKeyEnv),
		APIKey:                normalizeProviderAddFieldValue(form.APIKey),
	}

	if request.Name == "" {
		return providerAddRequest{}, "Name is required"
	}
	if request.Driver == "" {
		return providerAddRequest{}, "Driver is required"
	}
	if request.ModelSource == "" {
		return providerAddRequest{}, "Model Source must be discover or manual"
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
	normalizedMode, err := provider.NormalizeProviderChatAPIMode(request.ChatAPIMode)
	if err != nil {
		return providerAddRequest{}, err.Error()
	}
	if request.Driver == provider.DriverOpenAICompat {
		if normalizedMode == "" {
			normalizedMode = provider.DefaultProviderChatAPIMode()
		}
		request.ChatAPIMode = normalizedMode
	} else {
		request.ChatAPIMode = ""
	}

	if strings.TrimSpace(request.ChatEndpointPath) == "" {
		if request.Driver == provider.DriverOpenAICompat {
			request.ChatEndpointPath = providerAddDefaultOpenAICompatChatEndpointPath(request.ChatAPIMode)
		} else {
			request.ChatEndpointPath = providerAddDefaultChatEndpointPath(request.Driver)
		}
	}

	switch request.Driver {
	case provider.DriverOpenAICompat:
		if request.BaseURL == "" {
			request.BaseURL = config.OpenAIDefaultBaseURL
		}
	case provider.DriverGemini:
		if request.BaseURL == "" {
			request.BaseURL = config.GeminiDefaultBaseURL
		}
	case provider.DriverAnthropic:
		if request.BaseURL == "" {
			request.BaseURL = config.AnthropicDefaultBaseURL
		}
	default:
		if request.BaseURL == "" {
			return providerAddRequest{}, "Base URL is required for custom driver"
		}
	}

	var manualModels []providertypes.ModelDescriptor
	if request.ModelSource == config.ModelSourceManual {
		if strings.TrimSpace(request.ManualModelsJSON) == "" {
			return providerAddRequest{}, "Model JSON is required for manual model source"
		}
		models, err := parseProviderAddManualModelsJSON(request.ManualModelsJSON)
		if err != nil {
			return providerAddRequest{}, err.Error()
		}
		manualModels = models
	}

	normalizedInput, err := config.NormalizeCustomProviderInput(config.SaveCustomProviderInput{
		Name:                  request.Name,
		Driver:                request.Driver,
		BaseURL:               request.BaseURL,
		ChatAPIMode:           request.ChatAPIMode,
		ChatEndpointPath:      request.ChatEndpointPath,
		APIKeyEnv:             request.APIKeyEnv,
		DiscoveryEndpointPath: request.DiscoveryEndpointPath,
		ModelSource:           request.ModelSource,
		Models:                manualModels,
	})
	if err != nil {
		return providerAddRequest{}, err.Error()
	}

	request.Name = normalizedInput.Name
	request.Driver = normalizedInput.Driver
	request.BaseURL = normalizedInput.BaseURL
	request.ChatAPIMode = normalizedInput.ChatAPIMode
	request.ChatEndpointPath = normalizedInput.ChatEndpointPath
	request.APIKeyEnv = normalizedInput.APIKeyEnv
	request.ModelSource = normalizedInput.ModelSource
	request.DiscoveryEndpointPath = normalizedInput.DiscoveryEndpointPath
	if request.ModelSource != config.ModelSourceManual {
		request.ManualModelsJSON = ""
	}

	return request, ""
}

type providerAddManualModelJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// parseProviderAddManualModelsJSON 解析 provider add 表单中的手工模型 JSON，并复用 config 归一化校验规则。
func parseProviderAddManualModelsJSON(raw string) ([]providertypes.ModelDescriptor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("Model JSON is required for manual model source")
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()

	var models []providerAddManualModelJSON
	if err := decoder.Decode(&models); err != nil {
		return nil, fmt.Errorf("parse manual model json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("parse manual model json: unexpected trailing content")
	}
	if len(models) == 0 {
		return nil, errors.New("manual model list is empty")
	}

	descriptors := make([]providertypes.ModelDescriptor, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		descriptor := providertypes.ModelDescriptor{
			ID:   strings.TrimSpace(model.ID),
			Name: strings.TrimSpace(model.Name),
		}
		key := provider.NormalizeKey(descriptor.ID)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("parse manual model json: models.id %q is duplicated", descriptor.ID)
		}
		seen[key] = struct{}{}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}

// sanitizeProviderAddInputRunes 过滤 provider 表单输入中的控制字符，避免不可见字符污染字段。
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

// sanitizeProviderAddJSONInputRunes 过滤 JSON 输入中的不可见格式控制字符，同时保留换行与制表符。
func sanitizeProviderAddJSONInputRunes(runes []rune) string {
	if len(runes) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(runes))
	for _, r := range runes {
		if unicode.In(r, unicode.Cf) {
			continue
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			continue
		}
		if r == '\r' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// normalizeProviderAddFieldValue 统一清洗 provider 表单字段，去掉控制字符并裁剪首尾空白。
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
			Name:                  request.Name,
			Driver:                request.Driver,
			BaseURL:               request.BaseURL,
			ChatAPIMode:           request.ChatAPIMode,
			ChatEndpointPath:      request.ChatEndpointPath,
			ModelSource:           request.ModelSource,
			ManualModelsJSON:      request.ManualModelsJSON,
			DiscoveryEndpointPath: request.DiscoveryEndpointPath,
			APIKeyEnv:             request.APIKeyEnv,
			APIKey:                request.APIKey,
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
