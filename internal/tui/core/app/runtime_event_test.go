package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuistate "neo-code/internal/tui/state"
)

func TestHandleRuntimeEventIgnoresStaleRunAndSession(t *testing.T) {
	app := App{}
	app.state.ActiveSessionID = "session-current"
	app.state.ActiveRunID = "run-current"

	dirty := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		RunID:     "run-stale",
		SessionID: "session-stale",
		Payload:   "stale chunk",
	})
	if dirty {
		t.Fatalf("expected stale runtime event to be ignored")
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected stale runtime event to leave transcript untouched, got %+v", app.activeMessages)
	}
}

func TestHandleRuntimeEventIgnoresEventsWithoutActiveForeground(t *testing.T) {
	app := App{}

	dirty := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		RunID:     "run-stale",
		SessionID: "session-stale",
		Payload:   "stale chunk",
	})
	if dirty {
		t.Fatalf("expected orphan runtime event to be ignored")
	}
	if app.state.ActiveSessionID != "" {
		t.Fatalf("expected orphan runtime event not to restore active session, got %q", app.state.ActiveSessionID)
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected orphan runtime event to leave transcript untouched, got %+v", app.activeMessages)
	}
}

func TestHandleRuntimeEventAcceptsAnonymousEventWithoutForeground(t *testing.T) {
	app := App{}

	dirty := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:    agentruntime.EventAgentChunk,
		Payload: "draft chunk",
	})
	if !dirty {
		t.Fatalf("expected anonymous runtime event to be handled")
	}
	if len(app.activeMessages) != 1 {
		t.Fatalf("expected anonymous runtime event to append one message, got %+v", app.activeMessages)
	}
	if app.activeMessages[0].Content != "draft chunk" {
		t.Fatalf("expected anonymous runtime payload to be appended, got %+v", app.activeMessages)
	}
}

func TestUpdateSessionWorkdirResultResetsDraftState(t *testing.T) {
	workdir := t.TempDir()
	other := t.TempDir()
	runtime := &workspaceResultRuntimeStub{
		summaries: []agentsession.Summary{{ID: "session-new", Title: "new session"}},
	}
	app := newWorkspaceResultTestApp(t, runtime, workdir)
	app.state.ActiveSessionID = "session-old"
	app.state.ActiveRunID = "run-old"
	app.state.ActiveSessionTitle = "old session"
	app.state.CurrentTool = "filesystem_read_file"
	app.state.ToolStates = []tuistate.ToolState{{ToolName: "filesystem_read_file"}}
	app.state.RunContext = tuistate.ContextWindowState{RunID: "run-old"}
	app.state.TokenUsage = tuistate.TokenUsageState{RunInputTokens: 3, RunOutputTokens: 5}
	app.activeMessages = []providertypes.Message{{Role: providertypes.RoleAssistant, Content: "old"}}

	model, _ := app.Update(sessionWorkdirResultMsg{
		Notice:       "workspace switched",
		Workdir:      other,
		ResetToDraft: true,
	})
	updated := model.(App)

	if updated.state.ActiveSessionID != "" {
		t.Fatalf("expected draft session after workspace reset, got %q", updated.state.ActiveSessionID)
	}
	if updated.state.ActiveRunID != "" {
		t.Fatalf("expected active run to be cleared after workspace reset, got %q", updated.state.ActiveRunID)
	}
	if updated.state.CurrentWorkdir != filepath.Clean(other) {
		t.Fatalf("expected current workdir %q, got %q", filepath.Clean(other), updated.state.CurrentWorkdir)
	}
	if updated.state.StatusText != "workspace switched" {
		t.Fatalf("expected status text to reflect workspace notice, got %q", updated.state.StatusText)
	}
	if len(updated.activeMessages) != 0 {
		t.Fatalf("expected workspace reset to clear transcript, got %+v", updated.activeMessages)
	}
	if len(updated.state.Sessions) != 1 || updated.state.Sessions[0].ID != "session-new" {
		t.Fatalf("expected refreshed workspace sessions, got %+v", updated.state.Sessions)
	}
}

func TestUpdateSessionWorkdirResultRefreshesFilesWithinWorkspace(t *testing.T) {
	workdir := t.TempDir()
	target := filepath.Join(workdir, "child")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	filePath := filepath.Join(target, "demo.txt")
	if err := os.WriteFile(filePath, []byte("demo"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	app := newWorkspaceResultTestApp(t, &workspaceResultRuntimeStub{}, workdir)
	app.state.ActiveSessionID = "session-current"

	model, _ := app.Update(sessionWorkdirResultMsg{
		Notice:  "workdir updated",
		Workdir: target,
	})
	updated := model.(App)

	if updated.state.ActiveSessionID != "session-current" {
		t.Fatalf("expected same-workspace update to keep active session, got %q", updated.state.ActiveSessionID)
	}
	if updated.state.CurrentWorkdir != filepath.Clean(target) {
		t.Fatalf("expected current workdir %q, got %q", filepath.Clean(target), updated.state.CurrentWorkdir)
	}
	if updated.state.StatusText != "workdir updated" {
		t.Fatalf("expected status text to reflect workdir notice, got %q", updated.state.StatusText)
	}
	if len(updated.fileCandidates) == 0 {
		t.Fatalf("expected file candidates to be refreshed for target workdir")
	}
}

func TestRefreshMessagesLoadsSessionState(t *testing.T) {
	workdir := t.TempDir()
	sessionWorkdir := filepath.Join(workdir, "child")
	if err := os.MkdirAll(sessionWorkdir, 0o755); err != nil {
		t.Fatalf("mkdir session workdir: %v", err)
	}

	runtime := &workspaceResultRuntimeStub{
		sessions: map[string]agentsession.Session{
			"session-1": {
				ID:      "session-1",
				Title:   "Session One",
				Workdir: sessionWorkdir,
				Messages: []providertypes.Message{
					{Role: providertypes.RoleUser, Content: "hello"},
					{Role: providertypes.RoleAssistant, Content: "world"},
				},
			},
		},
	}
	app := newWorkspaceResultTestApp(t, runtime, workdir)
	app.state.ActiveSessionID = "session-1"
	app.activities = []tuistate.ActivityEntry{{Title: "stale"}}

	if err := app.refreshMessages(); err != nil {
		t.Fatalf("refreshMessages() error = %v", err)
	}
	if len(app.activeMessages) != 2 {
		t.Fatalf("expected loaded session messages, got %+v", app.activeMessages)
	}
	if app.state.ActiveSessionTitle != "Session One" {
		t.Fatalf("expected session title to sync, got %q", app.state.ActiveSessionTitle)
	}
	if app.state.CurrentWorkdir != filepath.Clean(sessionWorkdir) {
		t.Fatalf("expected session workdir %q, got %q", filepath.Clean(sessionWorkdir), app.state.CurrentWorkdir)
	}
	if len(app.activities) != 0 {
		t.Fatalf("expected refreshMessages to clear activities, got %+v", app.activities)
	}
}

func TestActivateSelectedSessionLoadsChosenSession(t *testing.T) {
	workdir := t.TempDir()
	sessionA := agentsession.Session{ID: "session-a", Title: "Session A", Messages: []providertypes.Message{{Role: providertypes.RoleAssistant, Content: "A"}}}
	sessionB := agentsession.Session{ID: "session-b", Title: "Session B", Messages: []providertypes.Message{{Role: providertypes.RoleAssistant, Content: "B"}}}
	runtime := &workspaceResultRuntimeStub{
		summaries: []agentsession.Summary{
			{ID: sessionA.ID, Title: sessionA.Title},
			{ID: sessionB.ID, Title: sessionB.Title},
		},
		sessions: map[string]agentsession.Session{
			sessionA.ID: sessionA,
			sessionB.ID: sessionB,
		},
	}
	app := newWorkspaceResultTestApp(t, runtime, workdir)
	if err := app.refreshSessions(); err != nil {
		t.Fatalf("refreshSessions() error = %v", err)
	}
	app.sessions.Select(1)

	if err := app.activateSelectedSession(); err != nil {
		t.Fatalf("activateSelectedSession() error = %v", err)
	}
	if app.state.ActiveSessionID != sessionB.ID {
		t.Fatalf("expected selected session id %q, got %q", sessionB.ID, app.state.ActiveSessionID)
	}
	if app.state.ActiveSessionTitle != sessionB.Title {
		t.Fatalf("expected selected session title %q, got %q", sessionB.Title, app.state.ActiveSessionTitle)
	}
	if len(app.activeMessages) != 1 || app.activeMessages[0].Content != "B" {
		t.Fatalf("expected selected session messages to load, got %+v", app.activeMessages)
	}
}

func TestRuntimeEventHandlersUpdateState(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &workspaceResultRuntimeStub{}, t.TempDir())

	if dirty := runtimeEventRunContextHandler(&app, agentruntime.RuntimeEvent{
		RunID:     "run-1",
		SessionID: "session-1",
		Payload: map[string]any{
			"Provider": "openai",
			"Model":    "gpt-5.4",
			"Workdir":  "/repo",
			"Mode":     "act",
		},
	}); dirty {
		t.Fatalf("expected run context handler not to dirty transcript")
	}
	if app.state.ActiveRunID != "run-1" || app.state.CurrentProvider != "openai" || app.state.CurrentModel != "gpt-5.4" || app.state.CurrentWorkdir != "/repo" {
		t.Fatalf("unexpected run context state: %+v", app.state)
	}

	runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{
		Payload: map[string]any{
			"ToolCallID": "call-1",
			"ToolName":   "filesystem_read_file",
			"Status":     "running",
		},
	})
	if app.state.CurrentTool != "filesystem_read_file" || len(app.state.ToolStates) != 1 {
		t.Fatalf("expected running tool state, got current=%q states=%+v", app.state.CurrentTool, app.state.ToolStates)
	}

	runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{
		Payload: map[string]any{
			"ToolCallID": "call-1",
			"ToolName":   "filesystem_read_file",
			"Status":     "succeeded",
		},
	})
	if app.state.CurrentTool != "" {
		t.Fatalf("expected succeeded tool to clear current tool, got %q", app.state.CurrentTool)
	}

	runtimeEventUsageHandler(&app, agentruntime.RuntimeEvent{
		Payload: map[string]any{
			"Run": map[string]any{
				"InputTokens":  10,
				"OutputTokens": 20,
				"TotalTokens":  30,
			},
			"Session": map[string]any{
				"InputTokens":  40,
				"OutputTokens": 50,
				"TotalTokens":  90,
			},
		},
	})
	if app.state.TokenUsage.RunTotalTokens != 30 || app.state.TokenUsage.SessionTotalTokens != 90 {
		t.Fatalf("unexpected token usage state: %+v", app.state.TokenUsage)
	}
}

func TestRuntimeEventTerminalHandlers(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &workspaceResultRuntimeStub{}, t.TempDir())
	app.state.IsAgentRunning = true
	app.state.ActiveRunID = "run-1"
	app.state.CurrentTool = "tool"

	runtimeEventToolCallThinkingHandler(&app, agentruntime.RuntimeEvent{Payload: "bash"})
	if app.state.CurrentTool != "bash" || len(app.activities) == 0 {
		t.Fatalf("expected tool call thinking to update activity, got current=%q activities=%+v", app.state.CurrentTool, app.activities)
	}

	runtimeEventToolStartHandler(&app, agentruntime.RuntimeEvent{
		Payload: providertypes.ToolCall{Name: "filesystem_read_file"},
	})
	if app.state.StatusText != statusRunningTool || app.state.CurrentTool != "filesystem_read_file" {
		t.Fatalf("expected tool start state, got status=%q current=%q", app.state.StatusText, app.state.CurrentTool)
	}

	dirty := runtimeEventToolResultHandler(&app, agentruntime.RuntimeEvent{
		Payload: tools.ToolResult{Name: "filesystem_read_file", Content: "done"},
	})
	if !dirty || app.state.StatusText != statusToolFinished {
		t.Fatalf("expected successful tool result to dirty transcript and finish tool, got dirty=%v status=%q", dirty, app.state.StatusText)
	}

	app.state.IsAgentRunning = true
	app.state.ActiveRunID = "run-2"
	dirty = runtimeEventAgentDoneHandler(&app, agentruntime.RuntimeEvent{
		Payload: providertypes.Message{Role: providertypes.RoleAssistant, Content: "final answer"},
	})
	if !dirty || app.state.IsAgentRunning || app.state.ActiveRunID != "" {
		t.Fatalf("expected agent done to clear run state, got dirty=%v state=%+v", dirty, app.state)
	}

	app.state.IsAgentRunning = true
	app.state.ActiveRunID = "run-3"
	runtimeEventRunCanceledHandler(&app, agentruntime.RuntimeEvent{})
	if app.state.StatusText != statusCanceled || app.state.IsAgentRunning {
		t.Fatalf("expected canceled state, got %+v", app.state)
	}

	app.state.IsAgentRunning = true
	runtimeEventErrorHandler(&app, agentruntime.RuntimeEvent{Payload: "boom"})
	if app.state.ExecutionError != "boom" || app.state.StatusText != "boom" || app.state.IsAgentRunning {
		t.Fatalf("expected runtime error state, got %+v", app.state)
	}

	runtimeEventProviderRetryHandler(&app, agentruntime.RuntimeEvent{Payload: "retrying"})
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected provider retry to restore thinking status, got %q", app.state.StatusText)
	}

	dirty = runtimeEventCompactDoneHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.CompactDonePayload{
			Applied:        true,
			BeforeChars:    100,
			AfterChars:     40,
			SavedRatio:     0.6,
			TriggerMode:    "manual",
			TranscriptPath: "/tmp/compact.txt",
		},
	})
	if !dirty || len(app.activeMessages) == 0 {
		t.Fatalf("expected compact done to append inline message, got dirty=%v messages=%+v", dirty, app.activeMessages)
	}

	dirty = runtimeEventCompactErrorHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.CompactErrorPayload{
			TriggerMode: "manual",
			Message:     "failed",
		},
	})
	if !dirty || app.state.ExecutionError == "" {
		t.Fatalf("expected compact error to append error state, got dirty=%v state=%+v", dirty, app.state)
	}
}

func TestAppStateSyncHelpers(t *testing.T) {
	app := newWorkspaceResultTestApp(t, &workspaceResultRuntimeStub{}, t.TempDir())

	app.syncActiveSessionTitle()
	if app.state.ActiveSessionTitle != draftSessionTitle {
		t.Fatalf("expected draft title fallback, got %q", app.state.ActiveSessionTitle)
	}

	app.state.ActiveSessionID = "session-1"
	app.state.Sessions = []agentsession.Summary{{ID: "session-1", Title: "Session One"}}
	app.syncActiveSessionTitle()
	if app.state.ActiveSessionTitle != "Session One" {
		t.Fatalf("expected active session title to sync from summaries, got %q", app.state.ActiveSessionTitle)
	}

	app.state.CurrentWorkdir = ""
	app.syncConfigState(config.Config{
		SelectedProvider: "openai",
		CurrentModel:     "gpt-5.4",
		Workdir:          "/repo",
	})
	if app.state.CurrentProvider != "openai" || app.state.CurrentModel != "gpt-5.4" || app.state.CurrentWorkdir != "/repo" {
		t.Fatalf("unexpected config sync state: %+v", app.state)
	}
}

func TestRefreshRuntimeSourceSnapshot(t *testing.T) {
	workdir := t.TempDir()
	runtime := &workspaceResultRuntimeStub{
		sessionContext: map[string]any{
			"SessionID": "session-1",
			"Provider":  "openai",
			"Model":     "gpt-5.4",
			"Workdir":   "/session",
			"Mode":      "chat",
		},
		sessionUsage: map[string]any{
			"InputTokens":  11,
			"OutputTokens": 22,
			"TotalTokens":  33,
		},
		runSnapshot: map[string]any{
			"RunID":     "run-1",
			"SessionID": "session-1",
			"Context": map[string]any{
				"Provider": "openai",
				"Model":    "gpt-5.4",
				"Workdir":  "/run",
				"Mode":     "act",
			},
			"ToolStates": []any{
				map[string]any{
					"ToolCallID": "call-1",
					"ToolName":   "filesystem_read_file",
					"Status":     "running",
					"Message":    "working",
					"DurationMS": int64(12),
				},
			},
			"Usage": map[string]any{
				"InputTokens":  1,
				"OutputTokens": 2,
				"TotalTokens":  3,
			},
			"SessionUsage": map[string]any{
				"InputTokens":  10,
				"OutputTokens": 20,
				"TotalTokens":  30,
			},
		},
	}
	app := newWorkspaceResultTestApp(t, runtime, workdir)
	app.state.ActiveSessionID = "session-1"
	app.state.ActiveRunID = "run-1"

	app.refreshRuntimeSourceSnapshot()

	if app.state.RunContext.RunID != "run-1" || app.state.RunContext.Workdir != "/run" {
		t.Fatalf("expected run snapshot context to override session snapshot, got %+v", app.state.RunContext)
	}
	if len(app.state.ToolStates) != 1 || app.state.ToolStates[0].ToolName != "filesystem_read_file" {
		t.Fatalf("expected run snapshot tool states, got %+v", app.state.ToolStates)
	}
	if app.state.TokenUsage.RunTotalTokens != 3 || app.state.TokenUsage.SessionTotalTokens != 30 {
		t.Fatalf("expected run snapshot usage, got %+v", app.state.TokenUsage)
	}
}

type workspaceResultRuntimeStub struct {
	summaries      []agentsession.Summary
	sessions       map[string]agentsession.Session
	sessionContext any
	sessionUsage   any
	runSnapshot    any
}

func (s *workspaceResultRuntimeStub) Run(context.Context, agentruntime.UserInput) error {
	return nil
}

func (s *workspaceResultRuntimeStub) Compact(context.Context, agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (s *workspaceResultRuntimeStub) SwitchWorkspace(context.Context, agentruntime.WorkspaceSwitchInput) (agentruntime.WorkspaceSwitchResult, error) {
	return agentruntime.WorkspaceSwitchResult{}, nil
}

func (s *workspaceResultRuntimeStub) ResolvePermission(context.Context, agentruntime.PermissionResolutionInput) error {
	return nil
}

func (s *workspaceResultRuntimeStub) CancelActiveRun() bool {
	return false
}

func (s *workspaceResultRuntimeStub) Events() <-chan agentruntime.RuntimeEvent {
	return make(chan agentruntime.RuntimeEvent)
}

func (s *workspaceResultRuntimeStub) ListSessions(context.Context) ([]agentsession.Summary, error) {
	return append([]agentsession.Summary(nil), s.summaries...), nil
}

func (s *workspaceResultRuntimeStub) LoadSession(_ context.Context, id string) (agentsession.Session, error) {
	if session, ok := s.sessions[id]; ok {
		return session, nil
	}
	return agentsession.Session{}, nil
}

func (s *workspaceResultRuntimeStub) GetSessionContext(context.Context, string) (any, error) {
	return s.sessionContext, nil
}

func (s *workspaceResultRuntimeStub) GetSessionUsage(context.Context, string) (any, error) {
	return s.sessionUsage, nil
}

func (s *workspaceResultRuntimeStub) GetRunSnapshot(context.Context, string) (any, error) {
	return s.runSnapshot, nil
}

func newWorkspaceResultTestApp(t *testing.T, runtime agentruntime.Runtime, workdir string) App {
	t.Helper()

	defaults := config.DefaultConfig()
	defaults.Workdir = workdir
	loader := config.NewLoader(t.TempDir(), defaults)
	manager := config.NewManager(loader)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workdir
		return nil
	}); err != nil {
		t.Fatalf("update config manager: %v", err)
	}

	uiStyles := newStyles()
	sessionList := list.New([]list.Item{}, sessionDelegate{styles: uiStyles}, 0, 0)
	sessionList.SetShowTitle(false)
	sessionList.SetShowHelp(false)
	sessionList.SetShowStatusBar(false)
	sessionList.SetShowPagination(false)
	sessionList.SetFilteringEnabled(true)
	sessionList.DisableQuitKeybindings()

	input := textarea.New()
	input.Focus()
	fileBrowser := filepicker.New()
	fileBrowser.CurrentDirectory = workdir

	return App{
		state: tuistate.UIState{
			StatusText:         statusReady,
			CurrentWorkdir:     workdir,
			ActiveSessionTitle: draftSessionTitle,
			Focus:              panelInput,
		},
		appServices: appServices{
			configManager: manager,
			runtime:       runtime,
		},
		appComponents: appComponents{
			help:           help.New(),
			spinner:        spinner.New(),
			sessions:       sessionList,
			commandMenu:    newCommandMenuModel(uiStyles),
			providerPicker: newSelectionPickerItems(nil),
			modelPicker:    newSelectionPickerItems(nil),
			fileBrowser:    fileBrowser,
			progress:       progress.New(progress.WithoutPercentage()),
			transcript:     viewport.New(0, 0),
			activity:       viewport.New(0, 0),
			input:          input,
		},
		appRuntimeState: appRuntimeState{
			codeCopyBlocks: make(map[int]string),
			nowFn:          time.Now,
			focus:          panelInput,
		},
		width:  120,
		height: 40,
		styles: uiStyles,
	}
}
