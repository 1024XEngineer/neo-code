package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiservices "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

type workspaceSwitchTestRuntime struct {
	sessions       []agentsession.Summary
	loadedSessions map[string]agentsession.Session
	loadErr        error
	sessionContext any
	sessionUsage   any
	runSnapshot    any
}

func (r *workspaceSwitchTestRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r *workspaceSwitchTestRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r *workspaceSwitchTestRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return nil
}

func (r *workspaceSwitchTestRuntime) CancelActiveRun() bool {
	return false
}

func (r *workspaceSwitchTestRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r *workspaceSwitchTestRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return append([]agentsession.Summary(nil), r.sessions...), nil
}

func (r *workspaceSwitchTestRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if r.loadErr != nil {
		return agentsession.Session{}, r.loadErr
	}
	if r.loadedSessions == nil {
		return agentsession.Session{}, nil
	}
	return r.loadedSessions[id], nil
}

func (r *workspaceSwitchTestRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (r *workspaceSwitchTestRuntime) GetSessionContext(ctx context.Context, sessionID string) (any, error) {
	return r.sessionContext, nil
}

func (r *workspaceSwitchTestRuntime) GetSessionUsage(ctx context.Context, sessionID string) (any, error) {
	return r.sessionUsage, nil
}

func (r *workspaceSwitchTestRuntime) GetRunSnapshot(ctx context.Context, runID string) (any, error) {
	return r.runSnapshot, nil
}

type workspaceSwitchTestProvider struct{}

func (s *workspaceSwitchTestProvider) ListProviders(ctx context.Context) ([]config.ProviderCatalogItem, error) {
	return nil, nil
}

func (s *workspaceSwitchTestProvider) SelectProvider(ctx context.Context, providerID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

func (s *workspaceSwitchTestProvider) ListModels(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (s *workspaceSwitchTestProvider) ListModelsSnapshot(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (s *workspaceSwitchTestProvider) SetCurrentModel(ctx context.Context, modelID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

type workspaceSwitchTestSwitcher struct {
	calls       int
	lastWorkdir string
	err         error
}

func (s *workspaceSwitchTestSwitcher) SwitchWorkspace(ctx context.Context, workdir string) error {
	s.calls++
	s.lastWorkdir = workdir
	return s.err
}

func TestAppRejectsWorkspaceSwitchWhenBusy(t *testing.T) {
	app, switcher := newWorkspaceSwitchTestApp(t)
	app.state.IsAgentRunning = true
	app.input.SetValue("/cwd next")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(App)

	if switcher.calls != 0 {
		t.Fatalf("expected busy path to skip switcher, got %d calls", switcher.calls)
	}
	if !strings.Contains(updated.state.ExecutionError, "workspace switch is unavailable") {
		t.Fatalf("expected busy rejection error, got %q", updated.state.ExecutionError)
	}
	if len(updated.activities) == 0 || updated.activities[len(updated.activities)-1].Title != "Workspace switch rejected" {
		t.Fatalf("expected rejection activity, got %+v", updated.activities)
	}
}

func TestAppWorkspaceSwitchSuccessQuitsCurrentProgram(t *testing.T) {
	app, switcher := newWorkspaceSwitchTestApp(t)
	target := t.TempDir()
	app.input.SetValue("/cwd " + target)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected workspace switch command")
	}
	updated := model.(App)

	msg := cmd()
	result, ok := msg.(workspaceSwitchResultMsg)
	if !ok {
		t.Fatalf("expected workspace switch result msg, got %T", msg)
	}
	if !result.Relaunched || result.Workdir != target {
		t.Fatalf("unexpected switch result: %+v", result)
	}
	if switcher.calls != 0 {
		t.Fatalf("expected relaunch path to defer restart outside TUI, got %d switcher calls", switcher.calls)
	}

	model, quitCmd := updated.Update(msg)
	updated = model.(App)
	if !strings.Contains(updated.state.StatusText, "Switching workspace") {
		t.Fatalf("expected switching notice, got %q", updated.state.StatusText)
	}
	if quitCmd == nil {
		t.Fatalf("expected quit command after successful relaunch")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from quit command")
	}
	if updated.PendingWorkspaceWorkdir() != target {
		t.Fatalf("expected pending relaunch workdir %q, got %q", target, updated.PendingWorkspaceWorkdir())
	}
}

func TestAppWorkspaceSwitchFailureKeepsCurrentProgram(t *testing.T) {
	app, _ := newWorkspaceSwitchTestApp(t)
	target := t.TempDir()
	originalWorkdir := app.state.CurrentWorkdir
	app.input.SetValue("/cwd " + target)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected workspace switch command")
	}
	updated := model.(App)

	msg := cmd()
	result, ok := msg.(workspaceSwitchResultMsg)
	if !ok {
		t.Fatalf("expected workspace switch result msg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("expected relaunch request to be deferred without start error, got %+v", result)
	}

	model, quitCmd := updated.Update(msg)
	updated = model.(App)
	if quitCmd == nil {
		t.Fatalf("expected relaunch request to quit current program")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from quit command")
	}
	if updated.state.CurrentWorkdir != originalWorkdir {
		t.Fatalf("expected displayed workdir to remain %q before relaunch, got %q", originalWorkdir, updated.state.CurrentWorkdir)
	}
	if updated.PendingWorkspaceWorkdir() != target {
		t.Fatalf("expected pending relaunch workdir %q, got %q", target, updated.PendingWorkspaceWorkdir())
	}
	if updated.state.ExecutionError != "" {
		t.Fatalf("expected no immediate relaunch error, got %q", updated.state.ExecutionError)
	}
}

func TestAppWorkspaceSwitchNoopDoesNotRelaunch(t *testing.T) {
	app, switcher := newWorkspaceSwitchTestApp(t)
	currentWorkdir := app.state.CurrentWorkdir
	app.input.SetValue("/cwd .")

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected workspace command for noop path")
	}
	updated := model.(App)

	msg := cmd()
	result, ok := msg.(workspaceSwitchResultMsg)
	if !ok {
		t.Fatalf("expected workspace switch result msg, got %T", msg)
	}
	if result.Relaunched {
		t.Fatalf("expected noop path to skip relaunch, got %+v", result)
	}
	if switcher.calls != 0 {
		t.Fatalf("expected noop path to skip switcher, got %d calls", switcher.calls)
	}

	model, quitCmd := updated.Update(msg)
	updated = model.(App)
	if quitCmd != nil {
		t.Fatalf("expected noop path to stay in current program")
	}
	if updated.state.CurrentWorkdir != currentWorkdir {
		t.Fatalf("expected workdir to remain %q, got %q", currentWorkdir, updated.state.CurrentWorkdir)
	}
	if !strings.Contains(updated.state.StatusText, "Workspace already set") {
		t.Fatalf("expected noop status notice, got %q", updated.state.StatusText)
	}
}

func TestAppRefreshMessagesKeepsStartupWorkdir(t *testing.T) {
	runtime := &workspaceSwitchTestRuntime{
		sessions: []agentsession.Summary{
			{ID: "session-1", Title: "Existing Session"},
		},
		loadedSessions: map[string]agentsession.Session{
			"session-1": {
				ID:    "session-1",
				Title: "Existing Session",
				Messages: []providertypes.Message{
					{Role: roleUser, Content: "hello"},
				},
			},
		},
		sessionContext: tuiservices.RuntimeSessionContextSnapshot{
			SessionID: "session-1",
			Provider:  "openai",
			Model:     "gpt-5.4",
			Workdir:   "/session/subdir",
			Mode:      "act",
		},
	}

	app, _, cfg := newWorkspaceSwitchTestAppWithRuntime(t, runtime)
	if app.state.ActiveSessionID != "session-1" {
		t.Fatalf("expected active session to be restored, got %q", app.state.ActiveSessionID)
	}
	if app.state.CurrentWorkdir != cfg.Workdir {
		t.Fatalf("expected current workdir to stay on startup workdir %q, got %q", cfg.Workdir, app.state.CurrentWorkdir)
	}
	if app.state.RunContext.Workdir != "/session/subdir" {
		t.Fatalf("expected run context workdir from runtime snapshot, got %q", app.state.RunContext.Workdir)
	}
	if app.state.ActiveSessionTitle != "Existing Session" {
		t.Fatalf("expected session title to be restored, got %q", app.state.ActiveSessionTitle)
	}
}

func TestRuntimeEventRunContextDoesNotOverrideCurrentWorkdir(t *testing.T) {
	app, _, cfg := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	currentWorkdir := cfg.Workdir
	targetWorkdir := filepath.Join(t.TempDir(), "runtime-subdir")

	handled := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventType(tuiservices.RuntimeEventRunContext),
		RunID:     "run-1",
		SessionID: "session-1",
		Payload: tuiservices.RuntimeRunContextPayload{
			Provider: "openai",
			Model:    "gpt-5.4-mini",
			Workdir:  targetWorkdir,
			Mode:     "act",
		},
	})
	if handled {
		t.Fatalf("expected run context handler to keep transcript clean")
	}
	if app.state.CurrentWorkdir != currentWorkdir {
		t.Fatalf("expected current workdir to remain %q, got %q", currentWorkdir, app.state.CurrentWorkdir)
	}
	if app.state.RunContext.Workdir != targetWorkdir {
		t.Fatalf("expected run context workdir %q, got %q", targetWorkdir, app.state.RunContext.Workdir)
	}
	if app.state.ActiveRunID != "run-1" {
		t.Fatalf("expected active run id to update, got %q", app.state.ActiveRunID)
	}
}

func TestAppRejectBusyWorkspaceSwitchIgnoresNonWorkspaceInput(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})

	if app.rejectBusyWorkspaceSwitch("/model") {
		t.Fatalf("expected non-workspace slash command to be ignored")
	}
	if app.state.ExecutionError != "" {
		t.Fatalf("expected execution error to stay empty, got %q", app.state.ExecutionError)
	}
}

func TestAppApplyWorkspaceSwitchResultUpdatesDisplayedWorkspace(t *testing.T) {
	app, _, cfg := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	nextWorkdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nextWorkdir, "README.md"), []byte("workspace"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	app.state.CurrentWorkdir = filepath.Join(cfg.Workdir, "session-subdir")

	cmd := app.applyWorkspaceSwitchResult(workspaceSwitchResultMsg{
		Notice:     "Workspace already set to " + nextWorkdir,
		Workdir:    nextWorkdir,
		Relaunched: false,
	})
	if cmd != nil {
		t.Fatalf("expected no quit command for non-relaunch path")
	}
	if app.state.CurrentWorkdir != nextWorkdir {
		t.Fatalf("expected workdir to update to %q, got %q", nextWorkdir, app.state.CurrentWorkdir)
	}
	if app.fileBrowser.CurrentDirectory != nextWorkdir {
		t.Fatalf("expected file browser directory %q, got %q", nextWorkdir, app.fileBrowser.CurrentDirectory)
	}
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title == "" {
		t.Fatalf("expected workspace activity to be appended")
	}
}

func TestAppApplyWorkspaceSwitchResultRefreshFailure(t *testing.T) {
	app, _, cfg := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	missingWorkdir := filepath.Join(t.TempDir(), "missing")
	app.state.CurrentWorkdir = filepath.Join(cfg.Workdir, "session-subdir")

	cmd := app.applyWorkspaceSwitchResult(workspaceSwitchResultMsg{
		Notice:     "Workspace already set to " + missingWorkdir,
		Workdir:    missingWorkdir,
		Relaunched: false,
	})
	if cmd != nil {
		t.Fatalf("expected no quit command when refresh fails")
	}
	if !strings.Contains(app.state.ExecutionError, "missing") {
		t.Fatalf("expected missing directory error, got %q", app.state.ExecutionError)
	}
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Failed to refresh workspace files" {
		t.Fatalf("expected refresh failure activity, got %+v", app.activities)
	}
}

func TestRunWorkspaceSwitchCommandReturnsValidationError(t *testing.T) {
	cmd := runWorkspaceSwitchCommand("", "", "/cwd nowhere")
	if cmd == nil {
		t.Fatalf("expected workspace switch command")
	}
	msg := cmd()
	result, ok := msg.(workspaceSwitchResultMsg)
	if !ok {
		t.Fatalf("expected workspace switch result msg, got %T", msg)
	}
	if result.Err == nil {
		t.Fatalf("expected validation error, got %+v", result)
	}
}

func TestRefreshMessagesClearsDraftStateWithoutActiveSession(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	app.activeMessages = []providertypes.Message{{Role: roleUser, Content: "hello"}}
	app.activities = []tuistate.ActivityEntry{{Title: "existing"}}
	app.state.ActiveSessionID = ""

	if err := app.refreshMessages(); err != nil {
		t.Fatalf("refreshMessages() error = %v", err)
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected active messages to clear, got %+v", app.activeMessages)
	}
	if len(app.activities) != 0 {
		t.Fatalf("expected activities to clear, got %+v", app.activities)
	}
}

func TestRefreshMessagesReturnsLoadError(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{
		loadErr: errors.New("load failed"),
	})
	app.state.ActiveSessionID = "session-1"

	err := app.refreshMessages()
	if err == nil || err.Error() != "load failed" {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestStartDraftSessionRestoresStartupWorkdir(t *testing.T) {
	app, _, cfg := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	app.state.ActiveSessionID = "session-1"
	app.state.ActiveSessionTitle = "Existing Session"
	app.state.IsCompacting = true
	app.state.CurrentTool = "filesystem"
	app.state.ActiveRunID = "run-1"
	app.state.RunContext = tuistate.ContextWindowState{RunID: "run-1", Workdir: "/session/subdir"}
	app.state.TokenUsage = tuistate.TokenUsageState{RunTotalTokens: 123}
	app.state.CurrentWorkdir = filepath.Join(cfg.Workdir, "session-subdir")
	app.input.SetValue("draft text")
	app.state.InputText = "draft text"
	app.activeMessages = []providertypes.Message{{Role: roleUser, Content: "hello"}}
	app.activities = []tuistate.ActivityEntry{{Title: "existing"}}

	app.startDraftSession()

	if app.state.ActiveSessionID != "" {
		t.Fatalf("expected draft session id to be cleared, got %q", app.state.ActiveSessionID)
	}
	if app.state.CurrentWorkdir != cfg.Workdir {
		t.Fatalf("expected current workdir to reset to startup workdir %q, got %q", cfg.Workdir, app.state.CurrentWorkdir)
	}
	if app.state.RunContext != (tuistate.ContextWindowState{}) {
		t.Fatalf("expected run context to reset, got %+v", app.state.RunContext)
	}
	if app.state.TokenUsage != (tuistate.TokenUsageState{}) {
		t.Fatalf("expected token usage to reset, got %+v", app.state.TokenUsage)
	}
	if app.input.Value() != "" || app.state.InputText != "" {
		t.Fatalf("expected draft input to be cleared")
	}
}

func TestSyncConfigStatePreservesExplicitCurrentWorkdir(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	app.state.CurrentWorkdir = "/explicit-workspace"

	app.syncConfigState(config.Config{
		SelectedProvider: "provider-b",
		CurrentModel:     "model-b",
		Workdir:          "/config-workspace",
	})

	if app.state.CurrentProvider != "provider-b" || app.state.CurrentModel != "model-b" {
		t.Fatalf("expected provider and model to sync, got provider=%q model=%q", app.state.CurrentProvider, app.state.CurrentModel)
	}
	if app.state.CurrentWorkdir != "/explicit-workspace" {
		t.Fatalf("expected explicit workdir to be preserved, got %q", app.state.CurrentWorkdir)
	}
}

func TestSyncConfigStateSetsWorkdirWhenEmpty(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	app.state.CurrentWorkdir = ""

	app.syncConfigState(config.Config{
		SelectedProvider: "provider-b",
		CurrentModel:     "model-b",
		Workdir:          "/config-workspace",
	})

	if app.state.CurrentWorkdir != "/config-workspace" {
		t.Fatalf("expected blank workdir to sync from config, got %q", app.state.CurrentWorkdir)
	}
}

func TestAppConstructorsAndInit(t *testing.T) {
	cfg, manager := newWorkspaceSwitchTestConfigManager(t)
	runtime := &workspaceSwitchTestRuntime{}
	switcher := &workspaceSwitchTestSwitcher{}

	app, err := New(&cfg, manager, runtime, &workspaceSwitchTestProvider{}, switcher)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.workspaceSwitcher != switcher {
		t.Fatalf("expected workspace switcher to be injected")
	}

	appWithBootstrap, err := NewWithBootstrap(tuibootstrap.Options{
		Config:            &cfg,
		ConfigManager:     manager,
		Runtime:           runtime,
		ProviderService:   &workspaceSwitchTestProvider{},
		WorkspaceSwitcher: switcher,
	})
	if err != nil {
		t.Fatalf("NewWithBootstrap() error = %v", err)
	}
	if appWithBootstrap.state.CurrentWorkdir != cfg.Workdir {
		t.Fatalf("expected bootstrap app workdir %q, got %q", cfg.Workdir, appWithBootstrap.state.CurrentWorkdir)
	}
	if cmd := appWithBootstrap.Init(); cmd == nil {
		t.Fatalf("expected Init() to return startup commands")
	}
}

func TestNewWithBootstrapReturnsBuildError(t *testing.T) {
	cfg, manager := newWorkspaceSwitchTestConfigManager(t)

	_, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          &cfg,
		ConfigManager:   manager,
		Runtime:         &workspaceSwitchTestRuntime{},
		ProviderService: &workspaceSwitchTestProvider{},
	})
	if err == nil {
		t.Fatalf("expected bootstrap error when workspace switcher is missing")
	}
}

func TestHandleRuntimeEventUnknownTypeStillPinsSession(t *testing.T) {
	app, _, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	app.state.ActiveSessionID = ""

	handled := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventType("unknown"),
		SessionID: "session-unknown",
	})
	if handled {
		t.Fatalf("expected unknown event type to be ignored")
	}
	if app.state.ActiveSessionID != "session-unknown" {
		t.Fatalf("expected active session to pin from first runtime event, got %q", app.state.ActiveSessionID)
	}
}

func newWorkspaceSwitchTestApp(t *testing.T) (App, *workspaceSwitchTestSwitcher) {
	app, switcher, _ := newWorkspaceSwitchTestAppWithRuntime(t, &workspaceSwitchTestRuntime{})
	return app, switcher
}

func newWorkspaceSwitchTestAppWithRuntime(
	t *testing.T,
	runtime *workspaceSwitchTestRuntime,
) (App, *workspaceSwitchTestSwitcher, config.Config) {
	t.Helper()

	cfg, manager := newWorkspaceSwitchTestConfigManager(t)

	switcher := &workspaceSwitchTestSwitcher{}
	app, err := newApp(tuibootstrap.Container{
		Config:            cfg,
		ConfigManager:     manager,
		Runtime:           runtime,
		ProviderService:   &workspaceSwitchTestProvider{},
		WorkspaceSwitcher: switcher,
		Mode:              tuibootstrap.ModeLive,
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return app, switcher, cfg
}

func newWorkspaceSwitchTestConfigManager(t *testing.T) (config.Config, *config.Manager) {
	t.Helper()

	cfg := config.DefaultConfig().Clone()
	cfg.Workdir = t.TempDir()

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg, manager
}
