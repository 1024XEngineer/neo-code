package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	agentruntime "neo-code/internal/runtime"
	approvalflow "neo-code/internal/runtime/approval"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuistate "neo-code/internal/tui/state"
)

type permissionTestRuntime struct {
	resolveErr   error
	lastResolved agentruntime.PermissionResolutionInput
}

func (r *permissionTestRuntime) PrepareUserInput(ctx context.Context, input agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	return agentruntime.UserInput{
		SessionID: input.SessionID,
		RunID:     input.RunID,
		Parts:     nil,
		Workdir:   input.Workdir,
	}, nil
}

func (r *permissionTestRuntime) Submit(ctx context.Context, input agentruntime.PrepareInput) error {
	_, err := r.PrepareUserInput(ctx, input)
	return err
}

func (r *permissionTestRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r *permissionTestRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r *permissionTestRuntime) ExecuteSystemTool(ctx context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (r *permissionTestRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	r.lastResolved = input
	return r.resolveErr
}

func (r *permissionTestRuntime) CancelActiveRun() bool {
	return false
}

func (r *permissionTestRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r *permissionTestRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (r *permissionTestRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (r *permissionTestRuntime) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r *permissionTestRuntime) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r *permissionTestRuntime) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return nil, nil
}

func newPermissionTestApp(runtime agentruntime.Runtime) *App {
	input := textarea.New()
	spin := spinner.New()
	sessionList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	app := &App{
		state: tuistate.UIState{
			Focus: panelInput,
		},
		appServices: appServices{
			runtime: runtime,
		},
		appComponents: appComponents{
			keys:          newKeyMap(),
			spinner:       spin,
			sessionPicker: sessionList,
			input:         input,
			transcript:    viewport.New(0, 0),
			activity:      viewport.New(0, 0),
		},
		appRuntimeState: appRuntimeState{
			nowFn: time.Now,
			focus: panelInput,
			activities: []tuistate.ActivityEntry{
				{Kind: "test", Title: "seed"},
			},
			layoutCached: true,
			cachedWidth:  128,
			cachedHeight: 40,
		},
	}
	return app
}

func TestUpdatePendingPermissionInputSelectAndSubmit(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "perm-1"},
		Selected: 0,
	}

	cmd, handled := app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyDown})
	if !handled || cmd != nil {
		t.Fatalf("expected handled down key without cmd, handled=%v cmd=%v", handled, cmd)
	}
	if app.pendingPermission.Selected != 1 {
		t.Fatalf("expected selection moved to 1, got %d", app.pendingPermission.Selected)
	}

	cmd, handled = app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyUp})
	if !handled || cmd != nil {
		t.Fatalf("expected handled up key without cmd, handled=%v cmd=%v", handled, cmd)
	}
	if app.pendingPermission.Selected != 0 {
		t.Fatalf("expected selection moved back to 0, got %d", app.pendingPermission.Selected)
	}

	cmd, handled = app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !handled || cmd != nil {
		t.Fatalf("expected unknown shortcut to be consumed without cmd, handled=%v cmd=%v", handled, cmd)
	}

	cmd, handled = app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected enter key to submit permission decision, handled=%v cmd=%v", handled, cmd)
	}

	msg := cmd()
	done, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if done.RequestID != "perm-1" || done.Decision != approvalflow.DecisionAllowOnce {
		t.Fatalf("unexpected submitted decision: %+v", done)
	}
	if runtime.lastResolved.Decision != approvalflow.DecisionAllowOnce {
		t.Fatalf("runtime decision mismatch: %+v", runtime.lastResolved)
	}
}

func TestUpdatePendingPermissionInputWithoutPendingState(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	cmd, handled := app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyEnter})
	if handled || cmd != nil {
		t.Fatalf("expected no handling when pending permission is nil, handled=%v cmd=%v", handled, cmd)
	}
}

func TestUpdatePendingPermissionInputShortcut(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "perm-2"},
		Selected: 0,
	}

	cmd, handled := app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !handled || cmd == nil {
		t.Fatalf("expected shortcut n to trigger submit, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	done, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if done.Decision != approvalflow.DecisionReject {
		t.Fatalf("expected reject decision, got %q", done.Decision)
	}
}

func TestUpdatePendingPermissionInputSubmittingConsumesInput(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-3"},
		Selected:   0,
		Submitting: true,
	}
	cmd, handled := app.updatePendingPermissionInput(tea.KeyMsg{Type: tea.KeyDown})
	if !handled || cmd != nil {
		t.Fatalf("expected submitting state to consume key without cmd, handled=%v cmd=%v", handled, cmd)
	}
}

func TestSubmitPermissionDecisionValidation(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	if cmd := app.submitPermissionDecision(approvalflow.DecisionAllowOnce); cmd != nil {
		t.Fatalf("expected nil cmd when no pending permission")
	}

	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "  "},
		Selected: 0,
	}
	if cmd := app.submitPermissionDecision(approvalflow.DecisionAllowOnce); cmd != nil {
		t.Fatalf("expected nil cmd for empty request id")
	}
}

func TestRuntimePermissionEventHandlers(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	requestEvent := agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionRequested,
		Payload: agentruntime.PermissionRequestPayload{
			RequestID: "perm-4",
			ToolName:  "bash",
			Target:    "git status",
		},
	}
	if dirty := runtimeEventPermissionRequestHandler(app, requestEvent); dirty {
		t.Fatalf("permission request should not mark transcript dirty")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-4" {
		t.Fatalf("expected pending permission to be recorded")
	}

	resolvedEvent := agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionResolved,
		Payload: agentruntime.PermissionResolvedPayload{
			RequestID:     "perm-4",
			Decision:      "allow",
			RememberScope: "once",
			ResolvedAs:    "approved",
		},
	}
	if dirty := runtimeEventPermissionResolvedHandler(app, resolvedEvent); dirty {
		t.Fatalf("permission resolved should not mark transcript dirty")
	}
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared after resolved")
	}
}

func TestUpdatePermissionResolutionFinishedMessage(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-5"},
		Selected:   0,
		Submitting: true,
	}
	app.state.IsAgentRunning = true
	app.state.IsCompacting = true
	app.state.StatusText = "busy"

	model, _ := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-5",
		Decision:  approvalflow.DecisionAllowOnce,
		Err:       errors.New("network"),
	})
	next := model.(App)
	if next.pendingPermission == nil || next.pendingPermission.Submitting {
		t.Fatalf("expected pending permission to remain and reset submitting on error")
	}
	if next.state.ExecutionError == "" {
		t.Fatalf("expected execution error after failed permission submit")
	}
}

func TestUpdatePermissionResolutionFinishedMessageSuccessClearsPendingPermission(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-5-success"},
		Selected:   0,
		Submitting: true,
	}

	model, _ := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-5-success",
		Decision:  approvalflow.DecisionAllowOnce,
	})
	next := model.(App)
	if next.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared on success")
	}
	if next.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status text, got %q", next.state.StatusText)
	}
}

func TestUpdateRuntimeClosedClearsPendingPermission(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-6"},
	}
	model, _ := app.Update(RuntimeClosedMsg{})
	next := model.(App)
	if next.pendingPermission != nil {
		t.Fatalf("expected runtime closed to clear pending permission")
	}
}

func TestRuntimePermissionRequestHandlerAutoRejectsSupersededRequest(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "perm-old"},
		Selected: 1,
	}

	event := agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionRequested,
		Payload: agentruntime.PermissionRequestPayload{
			RequestID: "perm-new",
			ToolName:  "bash",
			Target:    "pwd",
		},
	}
	if dirty := runtimeEventPermissionRequestHandler(app, event); dirty {
		t.Fatalf("permission request should not mark transcript dirty")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-new" {
		t.Fatalf("expected latest permission request to replace old one")
	}
	if app.deferredEventCmd == nil {
		t.Fatalf("expected superseded request to schedule auto-reject command")
	}

	msg := app.deferredEventCmd()
	done, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if done.RequestID != "perm-old" || done.Decision != approvalflow.DecisionReject {
		t.Fatalf("unexpected auto-reject payload: %+v", done)
	}
	if runtime.lastResolved.RequestID != "perm-old" || runtime.lastResolved.Decision != approvalflow.DecisionReject {
		t.Fatalf("unexpected runtime resolve input: %+v", runtime.lastResolved)
	}
}

func TestRuntimePermissionRequestHandlerDoesNotAutoRejectSubmittingRequest(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-old"},
		Submitting: true,
	}

	runtimeEventPermissionRequestHandler(app, agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionRequested,
		Payload: agentruntime.PermissionRequestPayload{
			RequestID: "perm-new",
		},
	})
	if app.deferredEventCmd != nil {
		t.Fatalf("expected no auto-reject command when current request is already submitting")
	}
}

func TestHandleRuntimeEventQueuesDeferredCommand(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-old"},
	}

	model, cmd := app.Update(RuntimeMsg{Event: agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionRequested,
		Payload: agentruntime.PermissionRequestPayload{
			RequestID: "perm-new",
		},
	}})
	next := model.(App)
	if next.deferredEventCmd != nil {
		t.Fatalf("expected deferred event cmd to be consumed during update")
	}
	if cmd == nil {
		t.Fatalf("expected runtime update to batch deferred command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected deferred command batch, got %T", msg)
	}
	if len(batch) == 0 {
		t.Fatalf("expected deferred command batch to contain work")
	}
	if _, ok := batch[0]().(permissionResolutionFinishedMsg); !ok {
		t.Fatalf("expected deferred batch command to resolve permission")
	}
	if runtime.lastResolved.RequestID != "perm-old" || runtime.lastResolved.Decision != approvalflow.DecisionReject {
		t.Fatalf("expected deferred auto-reject to run, got %+v", runtime.lastResolved)
	}
}

func TestRuntimePermissionResolvedHandlerUsesExactRequestIDMatch(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "Perm-1"},
	}

	runtimeEventPermissionResolvedHandler(app, agentruntime.RuntimeEvent{
		Type: agentruntime.EventPermissionResolved,
		Payload: agentruntime.PermissionResolvedPayload{
			RequestID: "perm-1",
		},
	})
	if app.pendingPermission == nil {
		t.Fatalf("expected mismatched request id case to keep pending permission")
	}
}

func TestRunResolvePermissionForwardsRuntimeError(t *testing.T) {
	runtime := &permissionTestRuntime{resolveErr: errors.New("resolve failed")}
	cmd := runResolvePermission(runtime, "perm-7", approvalflow.DecisionReject)
	msg := cmd()
	done, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if done.Err == nil || done.Err.Error() != "resolve failed" {
		t.Fatalf("expected forwarded resolve error, got %#v", done.Err)
	}
	if runtime.lastResolved.RequestID != "perm-7" || runtime.lastResolved.Decision != approvalflow.DecisionReject {
		t.Fatalf("unexpected runtime resolve input: %+v", runtime.lastResolved)
	}
}
