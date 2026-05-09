package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	agentruntime "neo-code/internal/tui/services"
)

func TestUserQuestionChoiceParsingHelpers(t *testing.T) {
	options := parseUserQuestionOptionLabels([]any{
		map[string]any{"label": "alpha"},
		"beta",
		map[string]any{"label": " "},
		3,
	})
	if len(options) != 2 || options[0] != "alpha" || options[1] != "beta" {
		t.Fatalf("options = %#v, want [\"alpha\",\"beta\"]", options)
	}

	values, invalid := normalizeUserQuestionChoiceValues([]string{"1", "beta", "1", "gamma"}, options)
	if len(values) != 2 || values[0] != "alpha" || values[1] != "beta" {
		t.Fatalf("values = %#v, want [\"alpha\",\"beta\"]", values)
	}
	if len(invalid) != 1 || invalid[0] != "gamma" {
		t.Fatalf("invalid = %#v, want [\"gamma\"]", invalid)
	}
}

func TestSubmitPendingUserQuestionInputText(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-1",
			Kind:      "text",
		},
	}

	cmd, handled := app.submitPendingUserQuestionInput("  hello world  ")
	if !handled || cmd == nil {
		t.Fatalf("expected text input to submit ask_user answer, handled=%v cmd=%v", handled, cmd)
	}
	if !app.pendingUserQuestion.Submitting {
		t.Fatalf("expected pending user question entering submitting state")
	}
	if app.state.StatusText != statusUserQuestionSubmitting {
		t.Fatalf("status text = %q, want %q", app.state.StatusText, statusUserQuestionSubmitting)
	}

	msg := cmd()
	done, ok := msg.(userQuestionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected userQuestionResolutionFinishedMsg, got %T", msg)
	}
	if done.RequestID != "ask-1" || done.Status != "answered" || done.Err != nil {
		t.Fatalf("unexpected resolve message: %+v", done)
	}
	if runtime.lastQuestion.RequestID != "ask-1" || runtime.lastQuestion.Status != "answered" {
		t.Fatalf("unexpected runtime ask_user resolve input: %+v", runtime.lastQuestion)
	}
	if runtime.lastQuestion.Message != "hello world" {
		t.Fatalf("message = %q, want %q", runtime.lastQuestion.Message, "hello world")
	}
	if len(runtime.lastQuestion.Values) != 0 {
		t.Fatalf("values should be empty for text question, got %#v", runtime.lastQuestion.Values)
	}
}

func TestSubmitPendingUserQuestionInputSkipValidation(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-2",
			Kind:      "text",
			AllowSkip: false,
		},
	}

	cmd, handled := app.submitPendingUserQuestionInput("/skip")
	if !handled || cmd != nil {
		t.Fatalf("expected skip disallowed branch to consume input without cmd, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != "This question does not allow skip" {
		t.Fatalf("status text = %q", app.state.StatusText)
	}

	app.pendingUserQuestion.Request.AllowSkip = true
	cmd, handled = app.submitPendingUserQuestionInput("/skip")
	if !handled || cmd == nil {
		t.Fatalf("expected skip allowed branch to submit answer, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	done, ok := msg.(userQuestionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected userQuestionResolutionFinishedMsg, got %T", msg)
	}
	if done.Status != "skipped" {
		t.Fatalf("status = %q, want skipped", done.Status)
	}
	if runtime.lastQuestion.Status != "skipped" {
		t.Fatalf("runtime status = %q, want skipped", runtime.lastQuestion.Status)
	}
}

func TestSubmitPendingUserQuestionInputSingleChoiceSupportsIndexAndOptionValidation(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-3",
			Kind:      "single_choice",
			Options: []any{
				map[string]any{"label": "alpha"},
				map[string]any{"label": "beta"},
			},
		},
	}

	cmd, handled := app.submitPendingUserQuestionInput("2")
	if !handled || cmd == nil {
		t.Fatalf("expected index-based single_choice answer to submit, handled=%v cmd=%v", handled, cmd)
	}
	done := cmd().(userQuestionResolutionFinishedMsg)
	if done.Status != "answered" {
		t.Fatalf("status = %q, want answered", done.Status)
	}
	if len(runtime.lastQuestion.Values) != 1 || runtime.lastQuestion.Values[0] != "beta" {
		t.Fatalf("values = %#v, want [\"beta\"]", runtime.lastQuestion.Values)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-4",
			Kind:      "single_choice",
			Options: []any{
				map[string]any{"label": "alpha"},
				map[string]any{"label": "beta"},
			},
		},
	}
	cmd, handled = app.submitPendingUserQuestionInput("gamma")
	if !handled || cmd != nil {
		t.Fatalf("expected invalid single_choice option to be rejected, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText == "" {
		t.Fatalf("expected validation message for invalid single_choice option")
	}
}

func TestSubmitPendingUserQuestionInputMultiChoiceEnforcesMaxChoices(t *testing.T) {
	runtime := &permissionTestRuntime{}
	app := newPermissionTestApp(runtime)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID:  "ask-5",
			Kind:       "multi_choice",
			MaxChoices: 2,
			Options: []any{
				map[string]any{"label": "alpha"},
				map[string]any{"label": "beta"},
				map[string]any{"label": "gamma"},
			},
		},
	}

	cmd, handled := app.submitPendingUserQuestionInput("1, beta, 1")
	if !handled || cmd == nil {
		t.Fatalf("expected valid multi_choice answer to submit, handled=%v cmd=%v", handled, cmd)
	}
	done := cmd().(userQuestionResolutionFinishedMsg)
	if done.Status != "answered" {
		t.Fatalf("status = %q, want answered", done.Status)
	}
	if len(runtime.lastQuestion.Values) != 2 || runtime.lastQuestion.Values[0] != "alpha" || runtime.lastQuestion.Values[1] != "beta" {
		t.Fatalf("values = %#v, want [\"alpha\",\"beta\"]", runtime.lastQuestion.Values)
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID:  "ask-6",
			Kind:       "multi_choice",
			MaxChoices: 2,
			Options: []any{
				map[string]any{"label": "alpha"},
				map[string]any{"label": "beta"},
				map[string]any{"label": "gamma"},
			},
		},
	}
	cmd, handled = app.submitPendingUserQuestionInput("1,2,3")
	if !handled || cmd != nil {
		t.Fatalf("expected over-limit multi_choice answer to be rejected, handled=%v cmd=%v", handled, cmd)
	}
	if app.state.StatusText != "multi_choice accepts up to 2 values" {
		t.Fatalf("status text = %q", app.state.StatusText)
	}
}

func TestUpdateUserQuestionResolutionFinishedMessageAdditional(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request:    agentruntime.UserQuestionRequestedPayload{RequestID: "ask-7"},
		Submitting: true,
	}

	model, _ := app.Update(userQuestionResolutionFinishedMsg{
		RequestID: "ask-7",
		Status:    "answered",
		Err:       errors.New("network"),
	})
	next := model.(App)
	if next.pendingUserQuestion == nil || next.pendingUserQuestion.Submitting {
		t.Fatalf("expected pending user question to remain while reset submitting after error")
	}
	if next.state.ExecutionError == "" {
		t.Fatalf("expected execution error for failed ask_user submit")
	}

	model, _ = next.Update(userQuestionResolutionFinishedMsg{
		RequestID: "ask-7",
		Status:    "answered",
	})
	next = model.(App)
	if next.pendingUserQuestion != nil {
		t.Fatalf("expected pending user question to clear after success")
	}
	if next.state.StatusText != statusUserQuestionSubmitted {
		t.Fatalf("status text = %q, want %q", next.state.StatusText, statusUserQuestionSubmitted)
	}
}

func TestPendingUserQuestionAllowsImmediateSlashCommands(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-help",
			Kind:      "text",
			Required:  true,
		},
	}
	app.input.SetValue("/help")
	app.state.InputText = "/help"

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := model.(App)

	if next.pendingUserQuestion == nil || next.pendingUserQuestion.Request.RequestID != "ask-help" {
		t.Fatalf("expected pending ask_user question to remain while running slash command")
	}
	if next.state.ActivePicker != pickerHelp {
		t.Fatalf("expected /help to open help picker, active=%v", next.state.ActivePicker)
	}
}
