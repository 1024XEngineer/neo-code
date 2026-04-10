package tui

import (
	"context"
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
			nowFn:          time.Now,
			codeCopyBlocks: map[int]string{},
			focus:          panelInput,
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

func TestWorkspaceRebuildFinishedResetsToFreshDraft(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	oldManager := newWorkspaceConfigManager(t, oldRoot)
	newManager := newWorkspaceConfigManager(t, newRoot)
	provider := &workspaceTestProvider{}
	oldRuntime := &workspaceTestRuntime{
		sessions: []agentsession.Summary{{ID: "old-session", Title: "Old"}},
		session:  agentsession.Session{ID: "old-session", Title: "Old", Workdir: oldRoot},
	}
	newRuntime := &workspaceTestRuntime{}

	app := newWorkspaceTestApp(oldManager, oldRuntime, provider)
	app.state.ActiveSessionID = "old-session"
	app.state.ActiveSessionTitle = "Old"
	app.input.SetValue("stale input")
	app.state.InputText = "stale input"

	model, _ := app.Update(workspaceRebuildFinishedMsg{
		Notice: "[System] Workspace switched to " + newRoot + ".",
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
	if next.state.ActiveSessionID != "" {
		t.Fatalf("expected draft session after rebuild, got %q", next.state.ActiveSessionID)
	}
	if next.state.CurrentWorkdir != newRoot {
		t.Fatalf("expected rebuilt workdir %q, got %q", newRoot, next.state.CurrentWorkdir)
	}
	if len(next.state.Sessions) != 0 {
		t.Fatalf("expected session list to refresh from rebuilt runtime, got %+v", next.state.Sessions)
	}
	if next.state.InputText != "" {
		t.Fatalf("expected draft input to be cleared, got %q", next.state.InputText)
	}
}
