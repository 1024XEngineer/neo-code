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
		Notice:       "已切换到新工作区",
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
	if updated.state.StatusText != "已切换到新工作区" {
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
		Notice:  "已更新目录",
		Workdir: target,
	})
	updated := model.(App)

	if updated.state.ActiveSessionID != "session-current" {
		t.Fatalf("expected same-workspace update to keep active session, got %q", updated.state.ActiveSessionID)
	}
	if updated.state.CurrentWorkdir != filepath.Clean(target) {
		t.Fatalf("expected current workdir %q, got %q", filepath.Clean(target), updated.state.CurrentWorkdir)
	}
	if updated.state.StatusText != "已更新目录" {
		t.Fatalf("expected status text to reflect workdir notice, got %q", updated.state.StatusText)
	}
	if len(updated.fileCandidates) == 0 {
		t.Fatalf("expected file candidates to be refreshed for target workdir")
	}
}

type workspaceResultRuntimeStub struct {
	summaries []agentsession.Summary
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

func (s *workspaceResultRuntimeStub) LoadSession(context.Context, string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
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
