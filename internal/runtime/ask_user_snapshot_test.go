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

func TestAskUserSnapshotHelpersHandleEdgeCases(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := newRunState("run-edge-q", newRuntimeSession("session-edge-q"))

	service.setPendingUserQuestion(nil, UserQuestionRequestedPayload{RequestID: "ignored"})
	service.setPendingUserQuestion(&state, UserQuestionRequestedPayload{
		RequestID: "ask-edge",
		Title:     "title",
		Options:   []any{"keep"},
	})

	service.clearPendingUserQuestionIfMatches(&state, "other")
	if buildRuntimeSnapshot(&state).PendingUserQuestion == nil {
		t.Fatal("expected non-matching request id to keep pending question")
	}

	service.clearPendingUserQuestionIfMatches(&state, " ASK-EDGE ")
	if buildRuntimeSnapshot(&state).PendingUserQuestion != nil {
		t.Fatal("expected case-insensitive request id match to clear pending question")
	}

	service.setPendingUserQuestion(&state, UserQuestionRequestedPayload{RequestID: "ask-blank"})
	service.clearPendingUserQuestionIfMatches(&state, "   ")
	if buildRuntimeSnapshot(&state).PendingUserQuestion != nil {
		t.Fatal("expected blank request id to clear pending question")
	}

	service.clearPendingUserQuestionIfMatches(nil, "ignored")
	var nilService *Service
	nilService.setPendingUserQuestion(&state, UserQuestionRequestedPayload{RequestID: "nil-service"})
	if buildRuntimeSnapshot(&state).PendingUserQuestion != nil {
		t.Fatal("nil service should not mutate run state")
	}
}

func TestParseAskUserPayloadHelpersHandlePointersAndInvalidMaps(t *testing.T) {
	t.Parallel()

	requested, ok := parseAskUserRequestedPayload(&UserQuestionRequestedPayload{
		RequestID: "ask-pointer",
		Kind:      "text",
	})
	if !ok || requested.RequestID != "ask-pointer" {
		t.Fatalf("pointer payload = %+v, ok=%v", requested, ok)
	}
	if _, ok := parseAskUserRequestedPayload((*UserQuestionRequestedPayload)(nil)); ok {
		t.Fatal("nil requested payload pointer should not parse")
	}
	if _, ok := parseAskUserRequestedPayload(map[string]any{"request_id": "   "}); ok {
		t.Fatal("blank request_id should be rejected")
	}
	if _, ok := parseAskUserRequestedPayload("invalid"); ok {
		t.Fatal("non-map requested payload should not parse")
	}

	resolved, ok := parseAskUserResolvedPayload(&UserQuestionResolvedPayload{
		RequestID: "ask-resolved",
		Status:    "answered",
	})
	if !ok || resolved.RequestID != "ask-resolved" {
		t.Fatalf("resolved pointer payload = %+v, ok=%v", resolved, ok)
	}
	if _, ok := parseAskUserResolvedPayload((*UserQuestionResolvedPayload)(nil)); ok {
		t.Fatal("nil resolved payload pointer should not parse")
	}
	if _, ok := parseAskUserResolvedPayload(123); ok {
		t.Fatal("non-map resolved payload should not parse")
	}
}

func TestAskUserSnapshotScalarConverters(t *testing.T) {
	t.Parallel()

	if got := trimAnyString("  hello  "); got != "hello" {
		t.Fatalf("trimAnyString() = %q, want hello", got)
	}
	if got := trimAnyString(123); got != "" {
		t.Fatalf("trimAnyString(non-string) = %q, want empty", got)
	}
	if got := toAnyBool(true); !got {
		t.Fatal("toAnyBool(true) = false, want true")
	}
	if got := toAnyBool("true"); got {
		t.Fatal("toAnyBool(non-bool) = true, want false")
	}

	intCases := map[string]any{
		"int":     int(7),
		"int32":   int32(8),
		"int64":   int64(9),
		"float64": float64(10),
	}
	want := map[string]int{
		"int":     7,
		"int32":   8,
		"int64":   9,
		"float64": 10,
	}
	for name, value := range intCases {
		if got := toAnyInt(value); got != want[name] {
			t.Fatalf("toAnyInt(%s) = %d, want %d", name, got, want[name])
		}
	}
	if got := toAnyInt("11"); got != 0 {
		t.Fatalf("toAnyInt(non-number) = %d, want 0", got)
	}
}
