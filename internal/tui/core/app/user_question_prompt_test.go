package tui

import (
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

	resolved := agentruntime.UserQuestionResolvedPayload{RequestID: "ask-1", Status: "answered"}
	if got, ok := parseUserQuestionResolvedPayload(resolved); !ok || got.Status != "answered" {
		t.Fatalf("parse resolved payload failed: %+v ok=%v", got, ok)
	}
	if _, ok := parseUserQuestionResolvedPayload("bad"); ok {
		t.Fatalf("string payload should be rejected")
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
}

func TestFormatUserQuestionPromptLines(t *testing.T) {
	t.Parallel()

	lines := formatUserQuestionPromptLines(userQuestionPromptState{
		Request: agentruntime.UserQuestionRequestedPayload{
			Title:       "Pick options",
			Kind:        "multi_choice",
			Description: "choose",
			Required:    true,
			AllowSkip:   true,
			MaxChoices:  2,
			Options: []any{
				map[string]any{"label": "alpha"},
				map[string]any{"label": "beta"},
			},
		},
		Submitting: true,
	})

	joined := ""
	for _, line := range lines {
		joined += line + "\n"
	}
	if !containsLine(lines, "Required: yes") {
		t.Fatalf("expected Required hint in lines: %q", joined)
	}
	if !containsLine(lines, "Allow skip: yes") {
		t.Fatalf("expected Allow skip hint in lines: %q", joined)
	}
	if !containsLine(lines, "Max choices: 2") {
		t.Fatalf("expected Max choices hint in lines: %q", joined)
	}
	if !containsLine(lines, "  1. alpha") {
		t.Fatalf("expected formatted option line in lines: %q", joined)
	}
	if !containsLine(lines, "Submitting user question answer...") {
		t.Fatalf("expected submitting hint in lines: %q", joined)
	}
}

func containsLine(lines []string, target string) bool {
	for _, line := range lines {
		if line == target {
			return true
		}
	}
	return false
}
