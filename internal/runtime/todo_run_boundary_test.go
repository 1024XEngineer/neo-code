package runtime

import (
	"context"
	"testing"

	agentsession "neo-code/internal/session"
)

func TestResetTodosForUserRunClearsSessionAndEmitsEmptySnapshot(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	required := true
	session := agentsession.New("todo-boundary")
	session.Todos = []agentsession.TodoItem{{
		ID:       "old-todo",
		Content:  "old task",
		Status:   agentsession.TodoStatusPending,
		Required: &required,
	}}
	created, err := store.CreateSession(context.Background(), createSessionInputFromSession(session))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	service := &Service{sessionStore: store, events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-boundary", created)
	state.userGoal = "改做另一个任务"
	if err := service.resetTodosForUserRun(context.Background(), &state); err != nil {
		t.Fatalf("resetTodosForUserRun() error = %v", err)
	}

	if len(state.session.Todos) != 0 {
		t.Fatalf("state todos = %+v, want empty", state.session.Todos)
	}
	persisted, err := store.LoadSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(persisted.Todos) != 0 {
		t.Fatalf("persisted todos = %+v, want empty", persisted.Todos)
	}

	events := collectRuntimeEvents(service.Events())
	foundEmptySnapshot := false
	for _, event := range events {
		if event.Type != EventTodoSnapshotUpdated {
			continue
		}
		payload, ok := event.Payload.(TodoEventPayload)
		if !ok {
			t.Fatalf("todo snapshot payload type = %T", event.Payload)
		}
		if len(payload.Items) == 0 && payload.Summary.Total == 0 && payload.Summary.RequiredOpen == 0 {
			foundEmptySnapshot = true
		}
	}
	if !foundEmptySnapshot {
		t.Fatalf("expected empty todo snapshot event, got %+v", events)
	}
}

func TestResetTodosForUserRunKeepsTodosForContinuePrompt(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	required := true
	session := agentsession.New("todo-boundary-continue")
	session.Todos = []agentsession.TodoItem{{
		ID:       "old-todo",
		Content:  "old task",
		Status:   agentsession.TodoStatusPending,
		Required: &required,
	}}
	created, err := store.CreateSession(context.Background(), createSessionInputFromSession(session))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	service := &Service{sessionStore: store, events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-boundary-continue", created)
	state.userGoal = "继续"
	if err := service.resetTodosForUserRun(context.Background(), &state); err != nil {
		t.Fatalf("resetTodosForUserRun() error = %v", err)
	}
	if len(state.session.Todos) != 1 {
		t.Fatalf("state todos = %+v, want preserved", state.session.Todos)
	}
	if events := collectRuntimeEvents(service.Events()); len(events) != 0 {
		t.Fatalf("continue prompt should not emit reset events, got %+v", events)
	}
}
