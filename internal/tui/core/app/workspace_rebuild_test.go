package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"

	"neo-code/internal/config"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuistate "neo-code/internal/tui/state"
)

type workspaceTestRuntime struct {
	sessions []agentsession.Summary
	session  agentsession.Session
}

func (r *workspaceTestRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r *workspaceTestRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r *workspaceTestRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return nil
}

func (r *workspaceTestRuntime) CancelActiveRun() bool {
	return false
}

func (r *workspaceTestRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r *workspaceTestRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return append([]agentsession.Summary(nil), r.sessions...), nil
}

func (r *workspaceTestRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return r.session, nil
}

func (r *workspaceTestRuntime) UpdateSessionWorkdir(ctx context.Context, sessionID string, requestedPath string) (agentsession.Session, error) {
	return r.session, nil
}

type workspaceTestProvider struct{}

func (p *workspaceTestProvider) ListProviders(ctx context.Context) ([]config.ProviderCatalogItem, error) {
	return nil, nil
}

func (p *workspaceTestProvider) SelectProvider(ctx context.Context, providerID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

func (p *workspaceTestProvider) ListModels(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (p *workspaceTestProvider) ListModelsSnapshot(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (p *workspaceTestProvider) SetCurrentModel(ctx context.Context, modelID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

func newWorkspaceConfigManager(t *testing.T, workdir string) *config.Manager {
	t.Helper()

	defaults := config.DefaultConfig()
	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workdir
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	return manager
}

func newWorkspaceTestApp(manager *config.Manager, runtime agentruntime.Runtime, provider ProviderController) *App {
	input := textarea.New()
	spin := spinner.New()
	sessionList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	uiStyles := newStyles()
	app := &App{
		state: tuistate.UIState{
			Focus:          panelInput,
			CurrentWorkdir: manager.Get().Workdir,
		},
		appServices: appServices{
			configManager: manager,
			providerSvc:   provider,
			runtime:       runtime,
			workspaceRoot: manager.Get().Workdir,
		},
		appComponents: appComponents{
			keys:           newKeyMap(),
			spinner:        spin,
			sessions:       sessionList,
			commandMenu:    newCommandMenuModel(uiStyles),
			providerPicker: newSelectionPickerItems(nil),
			modelPicker:    newSelectionPickerItems(nil),
			input:          input,
			transcript:     viewport.New(0, 0),
			activity:       viewport.New(0, 0),
		},
		appRuntimeState: appRuntimeState{
			nowFn:             time.Now,
			codeCopyBlocks:    map[int]string{},
			focus:             panelInput,
			runtimeListenerID: 1,
		},
		styles: uiStyles,
	}
	return app
}

func TestSessionWorkdirResultBlocksCrossWorkspaceWhileBusy(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.rebuildWorkspace = func(ctx context.Context, requestedPath string) (tuibootstrap.WorkspaceBinding, error) {
		t.Fatalf("rebuild should not be called while busy")
		return tuibootstrap.WorkspaceBinding{}, nil
	}
	app.state.IsAgentRunning = true

	model, _ := app.Update(sessionWorkdirResultMsg{
		Notice:          "[System] Workspace switched to /tmp/other.",
		Workdir:         t.TempDir(),
		RequiresRebuild: true,
	})
	next := model.(App)
	if next.state.ExecutionError == "" {
		t.Fatalf("expected busy error when rebuild is blocked")
	}
}

func TestSessionWorkdirResultRequiresConfiguredRebuild(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})

	model, _ := app.Update(sessionWorkdirResultMsg{
		Notice:          "[System] Workspace switched to /tmp/other.",
		Workdir:         t.TempDir(),
		RequiresRebuild: true,
	})
	next := model.(App)
	if next.state.ExecutionError == "" {
		t.Fatalf("expected rebuild configuration error")
	}
}

func TestRunWorkspaceRebuildReturnsFinishedMsg(t *testing.T) {
	wantPath := t.TempDir()
	cmd := runWorkspaceRebuild(7, func(ctx context.Context, requestedPath string) (tuibootstrap.WorkspaceBinding, error) {
		if requestedPath != wantPath {
			t.Fatalf("expected requested path %q, got %q", wantPath, requestedPath)
		}
		return tuibootstrap.WorkspaceBinding{Workdir: requestedPath}, nil
	}, "notice", wantPath)

	msg := cmd()
	finished, ok := msg.(workspaceRebuildFinishedMsg)
	if !ok {
		t.Fatalf("expected workspaceRebuildFinishedMsg, got %T", msg)
	}
	if finished.RebuildID != 7 {
		t.Fatalf("expected rebuild id 7, got %d", finished.RebuildID)
	}
	if finished.Notice != "notice" {
		t.Fatalf("unexpected notice: %+v", finished)
	}
	if finished.Binding.Workdir != wantPath {
		t.Fatalf("expected binding workdir %q, got %q", wantPath, finished.Binding.Workdir)
	}
}

func TestWorkspaceRebuildFinishedResetsToFreshDraft(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(newRoot, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new workspace file: %v", err)
	}
	oldManager := newWorkspaceConfigManager(t, oldRoot)
	newManager := newWorkspaceConfigManager(t, newRoot)
	provider := &workspaceTestProvider{}
	oldRuntime := &workspaceTestRuntime{
		sessions: []agentsession.Summary{{ID: "old-session", Title: "Old"}},
		session:  agentsession.Session{ID: "old-session", Title: "Old", Workdir: oldRoot},
	}
	newRuntime := &workspaceTestRuntime{}

	app := newWorkspaceTestApp(oldManager, oldRuntime, provider)
	app.rebuildRequestID = 1
	app.state.ActiveSessionID = "old-session"
	app.state.ActiveSessionTitle = "Old"
	app.input.SetValue("stale input")
	app.state.InputText = "stale input"
	app.fileCandidates = []string{"old.txt"}
	app.fileBrowser.CurrentDirectory = oldRoot

	model, _ := app.Update(workspaceRebuildFinishedMsg{
		RebuildID: 1,
		Notice:    "[System] Workspace switched to " + newRoot + ".",
		Binding: tuibootstrap.WorkspaceBinding{
			Config:          newManager.Get(),
			ConfigManager:   newManager,
			Runtime:         newRuntime,
			ProviderService: provider,
			WorkspaceRoot:   newRoot,
			Workdir:         newRoot,
		},
	})
	next := model.(App)
	if next.state.IsRebuilding {
		t.Fatalf("expected rebuild flag to be cleared")
	}
	if next.state.ActiveSessionID != "" {
		t.Fatalf("expected draft session after rebuild, got %q", next.state.ActiveSessionID)
	}
	if next.state.CurrentWorkdir != newRoot {
		t.Fatalf("expected rebuilt workdir %q, got %q", newRoot, next.state.CurrentWorkdir)
	}
	if next.fileBrowser.CurrentDirectory != newRoot {
		t.Fatalf("expected file browser directory %q, got %q", newRoot, next.fileBrowser.CurrentDirectory)
	}
	if !containsWorkspaceCandidate(next.fileCandidates, "new.txt") {
		t.Fatalf("expected rebuilt workspace files to include new.txt, got %+v", next.fileCandidates)
	}
	if len(next.state.Sessions) != 0 {
		t.Fatalf("expected session list to refresh from rebuilt runtime, got %+v", next.state.Sessions)
	}
	if next.state.InputText != "" {
		t.Fatalf("expected draft input to be cleared, got %q", next.state.InputText)
	}
}

func TestWorkspaceRebuildFinishedReportsRebuildError(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.rebuildRequestID = 1
	app.state.IsRebuilding = true

	model, _ := app.Update(workspaceRebuildFinishedMsg{RebuildID: 1, Err: errors.New("rebuild failed")})
	next := model.(App)
	if next.state.ExecutionError != "rebuild failed" {
		t.Fatalf("expected rebuild error to surface, got %q", next.state.ExecutionError)
	}
	if next.state.IsRebuilding {
		t.Fatalf("expected rebuild flag to clear after failure")
	}
}

func TestSessionWorkdirResultStartsBusyRebuild(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.rebuildWorkspace = func(ctx context.Context, requestedPath string) (tuibootstrap.WorkspaceBinding, error) {
		return tuibootstrap.WorkspaceBinding{}, nil
	}

	model, cmd := app.Update(sessionWorkdirResultMsg{
		Notice:          "[System] Workspace switched to /tmp/other.",
		Workdir:         t.TempDir(),
		RequiresRebuild: true,
	})
	next := model.(App)
	if !next.state.IsRebuilding {
		t.Fatalf("expected rebuild flag to be set")
	}
	if next.state.StatusText != statusRebuildingWorkspace {
		t.Fatalf("expected rebuild status, got %q", next.state.StatusText)
	}
	if cmd == nil {
		t.Fatalf("expected rebuild command to be returned")
	}
}

func TestWorkspaceRebuildFinishedIgnoresStaleResult(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.rebuildRequestID = 2
	app.state.IsRebuilding = true
	app.state.StatusText = statusRebuildingWorkspace

	model, _ := app.Update(workspaceRebuildFinishedMsg{
		RebuildID: 1,
		Err:       errors.New("stale rebuild should be ignored"),
	})
	next := model.(App)
	if !next.state.IsRebuilding {
		t.Fatalf("expected stale rebuild to keep current rebuild state")
	}
	if next.state.ExecutionError != "" {
		t.Fatalf("expected stale rebuild error to be ignored, got %q", next.state.ExecutionError)
	}
}

func TestRuntimeMessagesIgnoreStaleSubscription(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.runtimeListenerID = 2
	app.state.StatusText = statusThinking

	model, _ := app.Update(RuntimeClosedMsg{SubscriptionID: 1})
	next := model.(App)
	if next.state.StatusText != statusThinking {
		t.Fatalf("expected stale runtime close to be ignored, got %q", next.state.StatusText)
	}

	model, _ = next.Update(RuntimeMsg{
		SubscriptionID: 1,
		Event: agentruntime.RuntimeEvent{
			Type:    agentruntime.EventError,
			Payload: "stale event",
		},
	})
	next = model.(App)
	if next.state.ExecutionError != "" {
		t.Fatalf("expected stale runtime event to be ignored, got %q", next.state.ExecutionError)
	}
}

func TestApplyWorkspaceBindingValidatesDependencies(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	manager := newWorkspaceConfigManager(t, t.TempDir())
	provider := &workspaceTestProvider{}
	runtime := &workspaceTestRuntime{}

	if err := app.applyWorkspaceBinding(tuibootstrap.WorkspaceBinding{}); err == nil {
		t.Fatalf("expected config manager validation error")
	}
	if err := app.applyWorkspaceBinding(tuibootstrap.WorkspaceBinding{
		ConfigManager:   manager,
		ProviderService: provider,
	}); err == nil {
		t.Fatalf("expected runtime validation error")
	}
	if err := app.applyWorkspaceBinding(tuibootstrap.WorkspaceBinding{
		ConfigManager: manager,
		Runtime:       runtime,
	}); err == nil {
		t.Fatalf("expected provider validation error")
	}
}

func TestApplyWorkspaceBindingFallsBackToConfigPaths(t *testing.T) {
	root := t.TempDir()
	manager := newWorkspaceConfigManager(t, root)
	provider := &workspaceTestProvider{}
	runtime := &workspaceTestRuntime{}
	app := newPermissionTestApp(&permissionTestRuntime{})

	if err := app.applyWorkspaceBinding(tuibootstrap.WorkspaceBinding{
		Config:          manager.Get(),
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: provider,
	}); err != nil {
		t.Fatalf("apply workspace binding: %v", err)
	}
	if app.workspaceRoot != root {
		t.Fatalf("expected workspace root fallback %q, got %q", root, app.workspaceRoot)
	}
	if app.state.CurrentWorkdir != root {
		t.Fatalf("expected workdir fallback %q, got %q", root, app.state.CurrentWorkdir)
	}
}

func containsWorkspaceCandidate(candidates []string, name string) bool {
	for _, candidate := range candidates {
		if strings.Contains(candidate, name) {
			return true
		}
	}
	return false
}
