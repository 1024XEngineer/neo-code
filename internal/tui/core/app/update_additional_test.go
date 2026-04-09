package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
)

func TestShouldHandleRuntimeEventForegroundRules(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
	app.state.ActiveRunID = "run-current"
	app.state.ActiveSessionID = "session-current"

	if !app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		RunID:     "run-current",
		SessionID: "session-current",
	}) {
		t.Fatalf("expected matching foreground event to be accepted")
	}
	if !app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		RunID:     "",
		SessionID: "session-current",
	}) {
		t.Fatalf("expected event without run id to reuse active session")
	}
	if app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		RunID:     "run-other",
		SessionID: "session-current",
	}) {
		t.Fatalf("expected different run id to be rejected")
	}
	if app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		RunID:     "run-current",
		SessionID: "session-other",
	}) {
		t.Fatalf("expected different session id to be rejected")
	}
}

func TestRuntimeEventSupplementalHandlers(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
	app.state.IsAgentRunning = true

	if dirty := runtimeEventUserMessageHandler(&app, agentruntime.RuntimeEvent{RunID: "run-queued"}); dirty {
		t.Fatalf("expected user message handler not to dirty transcript")
	}
	if app.state.ActiveRunID != "run-queued" {
		t.Fatalf("expected user message handler to sync run id, got %q", app.state.ActiveRunID)
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected user message handler to set thinking status, got %q", app.state.StatusText)
	}
	if !app.runProgressKnown || app.runProgressLabel != "Queued" {
		t.Fatalf("expected user message handler to set queued progress, got known=%v label=%q", app.runProgressKnown, app.runProgressLabel)
	}

	if dirty := runtimeEventToolChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "  chunk output  "}); dirty {
		t.Fatalf("expected tool chunk handler not to dirty transcript")
	}
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Tool output" {
		t.Fatalf("expected tool chunk handler to append activity, got %+v", app.activities)
	}

	activityCount := len(app.activities)
	if dirty := runtimeEventToolChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "   "}); dirty {
		t.Fatalf("expected blank tool chunk handler not to dirty transcript")
	}
	if len(app.activities) != activityCount {
		t.Fatalf("expected blank tool chunk to be ignored, got %+v", app.activities)
	}
}

func TestAppAppendHelpers(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())

	app.appendInlineMessage(roleAssistant, "  hello world  ")
	app.appendInlineMessage(roleAssistant, "   ")
	if len(app.activeMessages) != 1 || app.activeMessages[0].Content != "hello world" {
		t.Fatalf("expected inline message helper to trim and skip blanks, got %+v", app.activeMessages)
	}

	app.appendActivity("tool", "", "detail only", false)
	if len(app.activities) != 1 || app.activities[0].Title != "detail only" || app.activities[0].Detail != "" {
		t.Fatalf("expected blank-title activity to promote detail into title, got %+v", app.activities)
	}

	for i := 0; i < maxActivityEntries+2; i++ {
		app.appendActivity("tool", "entry", "detail", false)
	}
	if len(app.activities) != maxActivityEntries {
		t.Fatalf("expected activity helper to cap entries at %d, got %d", maxActivityEntries, len(app.activities))
	}

	if !app.lastAssistantMatches("hello world") {
		t.Fatalf("expected assistant content match")
	}
	if app.lastAssistantMatches("other") {
		t.Fatalf("expected mismatched assistant content to return false")
	}
}

func TestHandleImmediateSlashCommandPaths(t *testing.T) {
	t.Run("clear resets draft state", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
		app.state.ActiveSessionID = "session-1"
		app.state.ActiveSessionTitle = "Session One"
		app.state.ActiveRunID = "run-1"
		app.activeMessages = []providertypes.Message{{Role: roleAssistant, Content: "history"}}

		handled, cmd := app.handleImmediateSlashCommand("/clear")
		if !handled || cmd != nil {
			t.Fatalf("expected /clear to be handled locally")
		}
		if app.state.ActiveSessionID != "" || len(app.activeMessages) != 0 {
			t.Fatalf("expected /clear to reset draft state, got state=%+v messages=%+v", app.state, app.activeMessages)
		}
		if app.state.StatusText != "[System] Cleared current draft/history." {
			t.Fatalf("unexpected /clear status: %q", app.state.StatusText)
		}
	})

	t.Run("compact rejects extra arguments", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())

		handled, cmd := app.handleImmediateSlashCommand("/compact now")
		if !handled || cmd != nil {
			t.Fatalf("expected /compact with args to be handled locally")
		}
		if app.state.ExecutionError == "" || len(app.activeMessages) != 1 || app.activeMessages[0].Role != roleError {
			t.Fatalf("expected usage error inline message, got state=%+v messages=%+v", app.state, app.activeMessages)
		}
	})

	t.Run("compact requires existing session", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())

		handled, cmd := app.handleImmediateSlashCommand("/compact")
		if !handled || cmd != nil {
			t.Fatalf("expected /compact without session to be handled locally")
		}
		if app.state.ExecutionError != "compact requires an existing session" {
			t.Fatalf("unexpected /compact empty session error: %q", app.state.ExecutionError)
		}
	})

	t.Run("compact rejects while busy", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
		app.state.ActiveSessionID = "session-1"
		app.state.IsAgentRunning = true

		handled, cmd := app.handleImmediateSlashCommand("/compact")
		if !handled || cmd != nil {
			t.Fatalf("expected busy /compact to be handled locally")
		}
		if app.state.ExecutionError != "compact is already running, please wait" {
			t.Fatalf("unexpected busy compact error: %q", app.state.ExecutionError)
		}
	})

	t.Run("compact dispatches runtime command", func(t *testing.T) {
		runtime := &slashCommandRuntimeStub{}
		app := newWorkspaceResultTestApp(t, runtime, t.TempDir())
		app.state.ActiveSessionID = "session-compact"
		app.state.CurrentTool = "filesystem_read_file"
		app.state.StreamingReply = true

		handled, cmd := app.handleImmediateSlashCommand("/compact")
		if !handled || cmd == nil {
			t.Fatalf("expected /compact to return runtime command")
		}
		if !app.state.IsCompacting || app.state.CurrentTool != "" || app.state.StatusText != statusCompacting {
			t.Fatalf("expected compact state to switch into compacting mode, got %+v", app.state)
		}

		msg := cmd()
		finished, ok := msg.(compactFinishedMsg)
		if !ok {
			t.Fatalf("expected compactFinishedMsg, got %T", msg)
		}
		if finished.Err != nil {
			t.Fatalf("expected compact runtime command to succeed, got %v", finished.Err)
		}
		if runtime.compactCalls != 1 || runtime.lastCompactInput.SessionID != "session-compact" {
			t.Fatalf("expected runtime compact command to execute once, got calls=%d input=%+v", runtime.compactCalls, runtime.lastCompactInput)
		}
	})

	t.Run("exit returns quit command", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())

		handled, cmd := app.handleImmediateSlashCommand("/exit")
		if !handled || cmd == nil {
			t.Fatalf("expected /exit to return quit command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("expected quit command message, got %T", cmd())
		}
	})

	t.Run("unknown slash command falls through", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())

		handled, cmd := app.handleImmediateSlashCommand("/unknown")
		if handled || cmd != nil {
			t.Fatalf("expected unknown slash command to fall through")
		}
	})
}

func TestUpdateHelperFunctions(t *testing.T) {
	t.Run("tab focus and composer helpers", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
		app.keys = newKeyMap()
		app.focus = panelInput
		app.input.SetValue("hello")
		app.state.InputText = "hello"

		if !app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
			t.Fatalf("expected tab to be treated as input when composer has text")
		}
		app.state.ActivePicker = pickerFile
		if app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
			t.Fatalf("expected active picker to disable tab-as-input")
		}
		app.state.ActivePicker = pickerNone

		app.focus = panelSessions
		app.focusNext()
		if app.focus != panelTranscript {
			t.Fatalf("expected focusNext to advance to transcript, got %v", app.focus)
		}
		app.focusPrev()
		if app.focus != panelSessions {
			t.Fatalf("expected focusPrev to move back to sessions, got %v", app.focus)
		}

		app.focus = panelInput
		app.applyFocus()
		if !app.input.Focused() {
			t.Fatalf("expected input panel focus to focus textarea")
		}
		app.focus = panelActivity
		app.applyFocus()
		if app.input.Focused() {
			t.Fatalf("expected non-input focus to blur textarea")
		}

		app.input.SetValue("line1")
		app.normalizeComposerHeight()
		beforeHeight := app.input.Height()
		app.growComposerForNewline()
		if app.input.Height() < beforeHeight {
			t.Fatalf("expected growComposerForNewline not to shrink composer")
		}
		app.input.SetValue("line1\nline2\nline3")
		app.normalizeComposerHeight()
		if app.input.Height() != 3 {
			t.Fatalf("expected normalizeComposerHeight to follow line count, got %d", app.input.Height())
		}
	})

	t.Run("paste heuristics and viewport helpers", func(t *testing.T) {
		app := newWorkspaceResultTestApp(t, &slashCommandRuntimeStub{}, t.TempDir())
		app.keys = newKeyMap()
		now := time.Now()
		if !app.shouldTreatEnterAsNewline(tea.KeyMsg{Type: tea.KeyEnter, Paste: true}, now) {
			t.Fatalf("expected pasted enter to become newline")
		}

		app.noteInputEdit("", "hello", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}, now)
		if !app.pasteMode {
			t.Fatalf("expected multi-rune edit to enable paste mode")
		}
		app.resetPasteHeuristics()
		if app.pasteMode || !app.lastInputEditAt.IsZero() || !app.lastPasteLikeAt.IsZero() {
			t.Fatalf("expected resetPasteHeuristics to clear paste tracking")
		}

		app.transcript.SetContent("1\n2\n3\n4\n5\n6")
		app.transcript.Height = 2
		app.transcript.GotoBottom()
		before := app.transcript.YOffset
		app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		if app.transcript.YOffset >= before {
			t.Fatalf("expected scroll up key to move viewport upward")
		}
	})

	t.Run("runtime bridge helpers", func(t *testing.T) {
		runtime := &slashCommandRuntimeStub{
			switchResult: agentruntime.WorkspaceSwitchResult{
				Workdir:          "/repo/pkg",
				WorkspaceRoot:    "/repo",
				WorkspaceChanged: true,
				ResetToDraft:     true,
			},
		}
		app := newWorkspaceResultTestApp(t, runtime, t.TempDir())

		runCmd := runAgent(runtime, "run-1", "session-1", "/repo", "hello")
		msg := runCmd()
		finished, ok := msg.(runFinishedMsg)
		if !ok || finished.Err != nil {
			t.Fatalf("expected run command to return successful runFinishedMsg, got %T %+v", msg, msg)
		}
		if runtime.runCalls != 1 || runtime.lastRunInput.RunID != "run-1" || runtime.lastRunInput.Workdir != "/repo" {
			t.Fatalf("expected run command to forward runtime input, got calls=%d input=%+v", runtime.runCalls, runtime.lastRunInput)
		}

		workdirCmd := runSessionWorkdirCommand(runtime, "session-1", "/repo", "/cwd ./pkg")
		workdirMsg := workdirCmd()
		workdirResult, ok := workdirMsg.(sessionWorkdirResultMsg)
		if !ok {
			t.Fatalf("expected sessionWorkdirResultMsg, got %T", workdirMsg)
		}
		if !workdirResult.ResetToDraft || workdirResult.Workdir != "/repo/pkg" || runtime.switchCalls != 1 {
			t.Fatalf("expected workdir command to forward runtime switch result, got result=%+v calls=%d input=%+v", workdirResult, runtime.switchCalls, runtime.lastSwitchInput)
		}

		events := make(chan agentruntime.RuntimeEvent, 1)
		events <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentChunk, Payload: "hello"}
		cmd := ListenForRuntimeEvent(events)
		if _, ok := cmd().(RuntimeMsg); !ok {
			t.Fatalf("expected runtime event listener to map event into RuntimeMsg")
		}
		close(events)
		if _, ok := ListenForRuntimeEvent(events)().(RuntimeClosedMsg); !ok {
			t.Fatalf("expected closed listener to map into RuntimeClosedMsg")
		}

		if cmd := app.requestModelCatalogRefresh(""); cmd != nil {
			t.Fatalf("expected empty provider refresh request to be skipped")
		}
		cmd = app.requestModelCatalogRefresh("openai")
		if app.modelRefreshID != "openai" {
			t.Fatalf("expected provider refresh id to be recorded once, got %q", app.modelRefreshID)
		}
		if cmd := app.requestModelCatalogRefresh("openai"); cmd != nil {
			t.Fatalf("expected duplicate provider refresh request to be suppressed")
		}
	})
}

type slashCommandRuntimeStub struct {
	compactCalls     int
	lastCompactInput agentruntime.CompactInput
	compactErr       error
	runCalls         int
	lastRunInput     agentruntime.UserInput
	runErr           error
	switchCalls      int
	lastSwitchInput  agentruntime.WorkspaceSwitchInput
	switchResult     agentruntime.WorkspaceSwitchResult
	switchErr        error
}

func (s *slashCommandRuntimeStub) Run(_ context.Context, input agentruntime.UserInput) error {
	s.runCalls++
	s.lastRunInput = input
	return s.runErr
}

func (s *slashCommandRuntimeStub) Compact(_ context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	s.compactCalls++
	s.lastCompactInput = input
	return agentruntime.CompactResult{}, s.compactErr
}

func (s *slashCommandRuntimeStub) SwitchWorkspace(_ context.Context, input agentruntime.WorkspaceSwitchInput) (agentruntime.WorkspaceSwitchResult, error) {
	s.switchCalls++
	s.lastSwitchInput = input
	return s.switchResult, s.switchErr
}

func (s *slashCommandRuntimeStub) ResolvePermission(context.Context, agentruntime.PermissionResolutionInput) error {
	return nil
}

func (s *slashCommandRuntimeStub) CancelActiveRun() bool {
	return false
}

func (s *slashCommandRuntimeStub) Events() <-chan agentruntime.RuntimeEvent {
	return make(chan agentruntime.RuntimeEvent)
}

func (s *slashCommandRuntimeStub) ListSessions(context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (s *slashCommandRuntimeStub) LoadSession(context.Context, string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (s *slashCommandRuntimeStub) GetSessionContext(context.Context, string) (any, error) {
	return nil, errors.New("not implemented")
}

func (s *slashCommandRuntimeStub) GetSessionUsage(context.Context, string) (any, error) {
	return nil, errors.New("not implemented")
}

func (s *slashCommandRuntimeStub) GetRunSnapshot(context.Context, string) (any, error) {
	return nil, errors.New("not implemented")
}
