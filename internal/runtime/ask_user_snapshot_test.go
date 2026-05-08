package runtime

import "testing"

func TestParseAskUserRequestedPayloadFromMap(t *testing.T) {
	t.Parallel()

	payload, ok := parseAskUserRequestedPayload(map[string]any{
		"request_id":  "ask-1",
		"question_id": "q-1",
		"title":       "Pick one",
		"description": "desc",
		"kind":        "single_choice",
		"options":     []any{"A", "B"},
		"required":    true,
		"allow_skip":  false,
		"max_choices": float64(1),
		"timeout_sec": float64(300),
	})
	if !ok {
		t.Fatal("expected payload to be parsed")
	}
	if payload.RequestID != "ask-1" || payload.QuestionID != "q-1" || payload.Kind != "single_choice" {
		t.Fatalf("unexpected parsed payload: %+v", payload)
	}
	if len(payload.Options) != 2 {
		t.Fatalf("options len = %d, want 2", len(payload.Options))
	}
}

func TestParseAskUserResolvedPayloadFromMap(t *testing.T) {
	t.Parallel()

	payload, ok := parseAskUserResolvedPayload(map[string]any{
		"request_id":  "ask-2",
		"question_id": "q-2",
		"status":      "answered",
		"values":      []any{"a", "b"},
		"message":     " done ",
	})
	if !ok {
		t.Fatal("expected payload to be parsed")
	}
	if payload.RequestID != "ask-2" || payload.Status != "answered" {
		t.Fatalf("unexpected parsed payload: %+v", payload)
	}
	if payload.Message != "done" {
		t.Fatalf("message = %q, want done", payload.Message)
	}
	if len(payload.Values) != 2 || payload.Values[0] != "a" || payload.Values[1] != "b" {
		t.Fatalf("values = %#v", payload.Values)
	}
}

func TestPendingUserQuestionLifecycleInRunState(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := newRunState("run-pending-q", newRuntimeSession("session-pending-q"))

	service.setPendingUserQuestion(&state, UserQuestionRequestedPayload{
		RequestID:   "ask-3",
		QuestionID:  "q-3",
		Title:       "Title",
		Description: "Desc",
		Kind:        "text",
		Options:     []any{"x"},
	})

	snapshot := buildRuntimeSnapshot(&state)
	if snapshot.PendingUserQuestion == nil {
		t.Fatal("expected snapshot pending user question")
	}
	if snapshot.PendingUserQuestion.RequestID != "ask-3" {
		t.Fatalf("request_id = %q, want ask-3", snapshot.PendingUserQuestion.RequestID)
	}

	snapshot.PendingUserQuestion.Options[0] = "mutated"
	afterMutation := buildRuntimeSnapshot(&state)
	if got := afterMutation.PendingUserQuestion.Options[0]; got != "x" {
		t.Fatalf("expected cloned options to be immutable from snapshot mutation, got %v", got)
	}

	service.clearPendingUserQuestionIfMatches(&state, "ask-3")
	cleared := buildRuntimeSnapshot(&state)
	if cleared.PendingUserQuestion != nil {
		t.Fatalf("expected pending user question to be cleared, got %+v", cleared.PendingUserQuestion)
	}
}
