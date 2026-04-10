package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	tuistate "neo-code/internal/tui/state"
)

type permissionPromptRuntime struct {
	lastResolved agentruntime.PermissionResolutionInput
}

func (r *permissionPromptRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r *permissionPromptRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r *permissionPromptRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	r.lastResolved = input
	return nil
}

func (r *permissionPromptRuntime) CancelActiveRun() bool {
	return false
}

func (r *permissionPromptRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r *permissionPromptRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (r *permissionPromptRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (r *permissionPromptRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func newPermissionPromptApp(runtime agentruntime.Runtime) *App {
	input := textarea.New()
	spin := spinner.New()
	sessionList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	uiStyles := newStyles()
	return &App{
		state: tuistate.UIState{Focus: panelInput},
		appServices: appServices{
			runtime: runtime,
		},
		appComponents: appComponents{
			keys:           newKeyMap(),
			spinner:        spin,
			sessions:       sessionList,
			commandMenu:    newCommandMenuModel(uiStyles),
			providerPicker: newSelectionPickerItems(nil),
			modelPicker:    newSelectionPickerItems(nil),
			helpPicker:     newHelpPickerItems(nil),
			input:          input,
			transcript:     viewport.New(0, 0),
			activity:       viewport.New(0, 0),
		},
		appRuntimeState: appRuntimeState{
			nowFn:          time.Now,
			codeCopyBlocks: map[int]string{},
			focus:          panelInput,
		},
		width:  120,
		height: 40,
		styles: uiStyles,
	}
}

func TestUpdatePendingPermissionInputSelectAndSubmit(t *testing.T) {
	runtime := &permissionPromptRuntime{}
	app := newPermissionPromptApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "perm-1"},
		Selected: 0,
	}
	app.pendingPermissionID = "perm-1"

	cmd, handled := app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyDown})
	if !handled || cmd != nil {
		t.Fatalf("expected handled down key without cmd, handled=%v cmd=%v", handled, cmd)
	}
	if app.pendingPermission.Selected != 1 {
		t.Fatalf("expected selection moved to 1, got %d", app.pendingPermission.Selected)
	}

	cmd, handled = app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected enter key to submit permission decision, handled=%v cmd=%v", handled, cmd)
	}

	msg := cmd()
	done, ok := msg.(permissionResolvedMsg)
	if !ok {
		t.Fatalf("expected permissionResolvedMsg, got %T", msg)
	}
	if done.RequestID != "perm-1" || done.Decision != string(agentruntime.PermissionResolutionAllowSession) {
		t.Fatalf("unexpected submitted decision: %+v", done)
	}
	if runtime.lastResolved.Decision != agentruntime.PermissionResolutionAllowSession {
		t.Fatalf("runtime decision mismatch: %+v", runtime.lastResolved)
	}
}

func TestRenderPermissionPromptUsesPanelContent(t *testing.T) {
	app := newPermissionPromptApp(&permissionPromptRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{
			ToolName:  "bash",
			Operation: "write",
			Target:    "file.txt",
		},
	}
	rendered := app.renderPermissionPrompt()
	if rendered == "" {
		t.Fatalf("expected rendered prompt")
	}
	if !containsAll(rendered, []string{"Permission request:", "Allow once", "Reject"}) {
		t.Fatalf("unexpected rendered prompt: %q", rendered)
	}
}

func TestRuntimeEventPermissionRequestCreatesPromptState(t *testing.T) {
	app := newPermissionPromptApp(&permissionPromptRuntime{})
	event := agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionRequest,
		Payload: agentruntime.PermissionRequestPayload{
			RequestID: "perm-2",
			ToolName:  "webfetch",
			Target:    "https://example.com",
		},
	}

	if dirty := runtimeEventPermissionRequestHandler(app, event); dirty {
		t.Fatalf("permission request should not mark transcript dirty")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-2" {
		t.Fatalf("expected pending permission prompt state to be recorded")
	}
	if app.pendingPermissionTool != "webfetch" {
		t.Fatalf("expected mirrored tool name to be recorded")
	}
}

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
