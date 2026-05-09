package tui

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	agentruntime "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

type permissionTestRuntime struct {
	resolveErr   error
	lastResolved agentruntime.PermissionResolutionInput
	lastQuestion agentruntime.UserQuestionResolutionInput
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

func (r *permissionTestRuntime) ResolveUserQuestion(ctx context.Context, input agentruntime.UserQuestionResolutionInput) error {
	r.lastQuestion = input
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

func (r *permissionTestRuntime) ListAvailableSkills(ctx context.Context, sessionID string) ([]agentruntime.AvailableSkillState, error) {
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
	if done.RequestID != "perm-1" || done.Decision != string(agentruntime.DecisionAllowOnce) {
		t.Fatalf("unexpected submitted decision: %+v", done)
	}
	if runtime.lastResolved.Decision != agentruntime.DecisionAllowOnce {
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
	if done.Decision != string(agentruntime.DecisionReject) {
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

func TestSubmitPendingUserQuestionInputBranches(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)

	if cmd, handled := app.submitPendingUserQuestionInput("hello"); handled || cmd != nil {
		t.Fatalf("expected nil pending question to be ignored, handled=%v cmd=%v", handled, cmd)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request:    agentruntime.UserQuestionRequestedPayload{RequestID: "ask-submitting"},
		Submitting: true,
	}
	if cmd, handled := app.submitPendingUserQuestionInput("hello"); !handled || cmd != nil {
		t.Fatalf("expected submitting question to consume input, handled=%v cmd=%v", handled, cmd)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "  "},
	}
	if cmd, handled := app.submitPendingUserQuestionInput("hello"); !handled || cmd != nil {
		t.Fatalf("expected empty request id branch, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.ExecutionError != "user question request_id is empty" {
		t.Fatalf("expected empty request id error, got %q", app.state.ExecutionError)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "ask-no-skip", AllowSkip: false},
	}
	if cmd, handled := app.submitPendingUserQuestionInput("/skip"); !handled || cmd != nil {
		t.Fatalf("expected disallowed skip to be consumed, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != "This question does not allow skip" {
		t.Fatalf("unexpected status for disallowed skip: %q", app.state.StatusText)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "ask-empty", AllowSkip: false},
	}
	if cmd, handled := app.submitPendingUserQuestionInput("   "); !handled || cmd != nil {
		t.Fatalf("expected empty required answer to be consumed, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != statusUserQuestionRequired {
		t.Fatalf("expected required status for empty answer, got %q", app.state.StatusText)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-one",
			Kind:      "single_choice",
		},
	}
	if cmd, handled := app.submitPendingUserQuestionInput("a,b"); !handled || cmd != nil {
		t.Fatalf("expected invalid single_choice to be consumed, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != "single_choice requires exactly one value" {
		t.Fatalf("unexpected single_choice validation status: %q", app.state.StatusText)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-multi",
			Kind:      "multi_choice",
		},
	}
	if cmd, handled := app.submitPendingUserQuestionInput(" ,, "); !handled || cmd != nil {
		t.Fatalf("expected empty multi_choice to be consumed, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != statusUserQuestionRequired {
		t.Fatalf("expected required status for empty multi_choice, got %q", app.state.StatusText)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-skip",
			AllowSkip: true,
		},
	}
	cmd, handled := app.submitPendingUserQuestionInput("   ")
	if !handled || cmd == nil {
		t.Fatalf("expected blank allowed skip to submit, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	done, ok := msg.(userQuestionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected userQuestionResolutionFinishedMsg, got %T", msg)
	}
	if done.RequestID != "ask-skip" || done.Status != "skipped" {
		t.Fatalf("unexpected skip submit payload: %+v", done)
	}
	if runtime.lastQuestion.Status != "skipped" || runtime.lastQuestion.Message != "" || len(runtime.lastQuestion.Values) != 0 {
		t.Fatalf("unexpected runtime skip input: %+v", runtime.lastQuestion)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-choice",
			Kind:      "single_choice",
		},
	}
	cmd, handled = app.submitPendingUserQuestionInput(" alpha ")
	if !handled || cmd == nil {
		t.Fatalf("expected single_choice answer submit, handled=%v cmd=%v", handled, cmd)
	}
	_ = cmd()
	if runtime.lastQuestion.Status != "answered" || !slices.Equal(runtime.lastQuestion.Values, []string{"alpha"}) {
		t.Fatalf("unexpected runtime single_choice input: %+v", runtime.lastQuestion)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-text",
			Kind:      "text",
		},
	}
	cmd, handled = app.submitPendingUserQuestionInput("  final answer  ")
	if !handled || cmd == nil {
		t.Fatalf("expected text answer submit, handled=%v cmd=%v", handled, cmd)
	}
	_ = cmd()
	if runtime.lastQuestion.Status != "answered" || runtime.lastQuestion.Message != "final answer" {
		t.Fatalf("unexpected runtime text input: %+v", runtime.lastQuestion)
	}
}

func TestParseUserQuestionValues(t *testing.T) {
	values := parseUserQuestionValues(" alpha, , beta ,gamma ")
	if !slices.Equal(values, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("unexpected parsed values: %#v", values)
	}
}

func TestSubmitPermissionDecisionValidation(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	if cmd := app.submitPermissionDecision(agentruntime.DecisionAllowOnce); cmd != nil {
		t.Fatalf("expected nil cmd when no pending permission")
	}

	app.pendingPermission = &permissionPromptState{
		Request:  agentruntime.PermissionRequestPayload{RequestID: "  "},
		Selected: 0,
	}
	if cmd := app.submitPermissionDecision(agentruntime.DecisionAllowOnce); cmd != nil {
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
		Decision:  string(agentruntime.DecisionAllowOnce),
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
		Decision:  string(agentruntime.DecisionAllowOnce),
	})
	next := model.(App)
	if next.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared on success")
	}
	if next.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status text, got %q", next.state.StatusText)
	}
}

func TestUpdateUserQuestionResolutionFinishedMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "ask-1"},
	}

	model, _ := app.Update(userQuestionResolutionFinishedMsg{
		RequestID: "ask-1",
		Status:    "answered",
		Err:       errors.New("gateway failed"),
	})
	next := model.(App)
	if next.pendingUserQuestion == nil || next.pendingUserQuestion.Submitting {
		t.Fatalf("expected pending question to remain and reset submitting on error")
	}
	if next.state.ExecutionError != "gateway failed" {
		t.Fatalf("expected execution error, got %q", next.state.ExecutionError)
	}

	next.pendingUserQuestion.Submitting = true
	model, _ = next.Update(userQuestionResolutionFinishedMsg{
		RequestID: "ask-1",
		Status:    "  ",
	})
	final := model.(App)
	if final.pendingUserQuestion != nil {
		t.Fatalf("expected success to clear pending question")
	}
	if final.state.ExecutionError != "" || final.state.StatusText != statusUserQuestionSubmitted {
		t.Fatalf("expected submitted status, got status=%q err=%q", final.state.StatusText, final.state.ExecutionError)
	}
}

func TestUpdateRuntimeClosedClearsPendingPermission(t *testing.T) {
	app := newPermissionTestApp(&permissionTestRuntime{})
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-6"},
	}
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "ask-closed"},
	}
	model, _ := app.Update(RuntimeClosedMsg{})
	next := model.(App)
	if next.pendingPermission != nil {
		t.Fatalf("expected runtime closed to clear pending permission")
	}
	if next.pendingUserQuestion != nil {
		t.Fatalf("expected runtime closed to clear pending user question")
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
	if done.RequestID != "perm-old" || done.Decision != string(agentruntime.DecisionReject) {
		t.Fatalf("unexpected auto-reject payload: %+v", done)
	}
	if runtime.lastResolved.RequestID != "perm-old" || runtime.lastResolved.Decision != agentruntime.DecisionReject {
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
	if runtime.lastResolved.RequestID != "perm-old" || runtime.lastResolved.Decision != agentruntime.DecisionReject {
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
	cmd := runResolvePermission(runtime, "perm-7", agentruntime.DecisionReject)
	msg := cmd()
	done, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if done.Err == nil || done.Err.Error() != "resolve failed" {
		t.Fatalf("expected forwarded resolve error, got %#v", done.Err)
	}
	if runtime.lastResolved.RequestID != "perm-7" || runtime.lastResolved.Decision != agentruntime.DecisionReject {
		t.Fatalf("unexpected runtime resolve input: %+v", runtime.lastResolved)
	}
}
