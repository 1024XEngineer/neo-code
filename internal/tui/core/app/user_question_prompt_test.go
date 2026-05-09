package tui

import (
	"strings"
	"testing"

	agentruntime "neo-code/internal/tui/services"
)

func TestParseUserQuestionPayloads(t *testing.T) {
	t.Parallel()

	requested := agentruntime.UserQuestionRequestedPayload{RequestID: "ask-1", QuestionID: "q1"}
	if got, ok := parseUserQuestionRequestedPayload(requested); !ok || got.RequestID != "ask-1" {
		t.Fatalf("parse requested payload failed: %+v ok=%v", got, ok)
	}
	if _, ok := parseUserQuestionRequestedPayload((*agentruntime.UserQuestionRequestedPayload)(nil)); ok {
		t.Fatalf("nil pointer requested payload should be rejected")
	}
	if got, ok := parseUserQuestionRequestedPayload(&requested); !ok || got.QuestionID != "q1" {
		t.Fatalf("parse requested pointer payload failed: %+v ok=%v", got, ok)
	}

	resolved := agentruntime.UserQuestionResolvedPayload{RequestID: "ask-1", Status: "answered"}
	if got, ok := parseUserQuestionResolvedPayload(resolved); !ok || got.Status != "answered" {
		t.Fatalf("parse resolved payload failed: %+v ok=%v", got, ok)
	}
	if got, ok := parseUserQuestionResolvedPayload(&resolved); !ok || got.RequestID != "ask-1" {
		t.Fatalf("parse resolved pointer payload failed: %+v ok=%v", got, ok)
	}
	if _, ok := parseUserQuestionResolvedPayload("bad"); ok {
		t.Fatalf("string payload should be rejected")
	}
}

func TestFormatUserQuestionPromptLinesAndRenderFallbacks(t *testing.T) {
	t.Parallel()

	lines := formatUserQuestionPromptLines(userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			Kind:        " multi_choice ",
			Title:       " ",
			Description: "",
			Options:     []any{"A", "B"},
		},
		Submitting: true,
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Question: (untitled question)") {
		t.Fatalf("expected fallback title, got %q", joined)
	}
	if !strings.Contains(joined, "Description: (no description)") {
		t.Fatalf("expected fallback description, got %q", joined)
	}
	if !strings.Contains(joined, "Kind: multi_choice") {
		t.Fatalf("expected normalized kind, got %q", joined)
	}
	if !strings.Contains(joined, "1. A") || !strings.Contains(joined, "2. B") {
		t.Fatalf("expected option list, got %q", joined)
	}
	if !strings.Contains(joined, "Submitting user question answer...") {
		t.Fatalf("expected submitting hint, got %q", joined)
	}

	app, _ := newTestApp(t)
	app.input.SetValue("picked")
	rendered := app.renderUserQuestionPrompt()
	if rendered != app.input.View() {
		t.Fatalf("expected plain input view when no pending question")
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			RequestID: "ask-1",
			Title:     "Need choice",
			Kind:      "single_choice",
		},
	}
	rendered = app.renderUserQuestionPrompt()
	if !strings.Contains(rendered, "Question: Need choice") || !strings.Contains(rendered, "picked") {
		t.Fatalf("expected rendered prompt to include question and input, got %q", rendered)
	}
}

func TestRuntimeUserQuestionEventHandlers(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t)
	if handled := runtimeEventUserQuestionRequestedHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}); handled {
		t.Fatalf("invalid requested payload should return false")
	}
	if handled := runtimeEventUserQuestionResolvedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}); handled {
		t.Fatalf("invalid resolved payload should return false")
	}

	runtimeEventUserQuestionRequestedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.UserQuestionRequestedPayload{
			RequestID:  "ask-1",
			QuestionID: "q1",
			Title:      "Need input",
			Kind:       "text",
		},
	})
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "User question requested" {
		t.Fatalf("expected user question requested activity")
	}
	if app.pendingUserQuestion == nil || app.state.StatusText != statusUserQuestionRequired || app.state.ExecutionError != "" {
		t.Fatalf("expected pending prompt and required status after request")
	}

	runtimeEventUserQuestionResolvedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.UserQuestionResolvedPayload{
			RequestID:  "ask-1",
			QuestionID: "q1",
			Status:     "answered",
		},
	})
	last := app.activities[len(app.activities)-1]
	if last.Title != "User question answered" {
		t.Fatalf("unexpected resolved activity: %+v", last)
	}
	if app.pendingUserQuestion != nil || app.state.StatusText != statusUserQuestionSubmitted {
		t.Fatalf("expected resolved question to clear pending state")
	}

	app.pendingUserQuestion = &userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{RequestID: "ask-2"},
	}
	runtimeEventUserQuestionResolvedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.UserQuestionResolvedPayload{
			RequestID:  "ask-2",
			QuestionID: "q2",
			Status:     "timeout",
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "User question timed out" {
		t.Fatalf("unexpected timeout activity: %+v", last)
	}
	if app.state.ExecutionError != "User question timed out" || app.state.StatusText != "User question timed out" {
		t.Fatalf("expected timeout status and error, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}

	runtimeEventUserQuestionResolvedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.UserQuestionResolvedPayload{
			RequestID:  "ask-3",
			QuestionID: "q3",
			Status:     "skipped",
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "User question skipped" {
		t.Fatalf("unexpected skipped activity: %+v", last)
	}

	runtimeEventUserQuestionResolvedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.UserQuestionResolvedPayload{
			RequestID:  "ask-4",
			QuestionID: "q4",
			Status:     "custom",
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "User question resolved" || app.state.StatusText != "User question resolved" {
		t.Fatalf("expected generic resolved branch, got title=%q status=%q", last.Title, app.state.StatusText)
	}
}
