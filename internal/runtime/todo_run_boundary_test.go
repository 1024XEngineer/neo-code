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

func TestResetTodosForUserRunKeepsTodosForActivePlan(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	required := true
	session := agentsession.New("todo-boundary-plan")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:     "plan-1",
		Status: agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Todos: []agentsession.TodoItem{{ID: "plan-todo", Content: "plan task"}},
		},
	}
	session.Todos = []agentsession.TodoItem{{
		ID:       "plan-todo",
		Content:  "plan task",
		Status:   agentsession.TodoStatusPending,
		Required: &required,
	}}
	created, err := store.CreateSession(context.Background(), createSessionInputFromSession(session))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	service := &Service{sessionStore: store, events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-boundary-plan", created)
	if err := service.resetTodosForUserRun(context.Background(), &state); err != nil {
		t.Fatalf("resetTodosForUserRun() error = %v", err)
	}
	if len(state.session.Todos) != 1 {
		t.Fatalf("state todos = %+v, want preserved", state.session.Todos)
	}
	if events := collectRuntimeEvents(service.Events()); len(events) != 0 {
		t.Fatalf("active plan should not emit reset events, got %+v", events)
	}
}

func TestResetTodosForUserRunPrunesTodosOutsideActivePlan(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	required := true
	session := agentsession.New("todo-boundary-prune")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:     "plan-1",
		Status: agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Todos: []agentsession.TodoItem{{ID: "plan-todo", Content: "plan task"}},
		},
	}
	session.Todos = []agentsession.TodoItem{
		{ID: "plan-todo", Content: "plan task", Status: agentsession.TodoStatusPending, Required: &required},
		{ID: "old-todo", Content: "old task", Status: agentsession.TodoStatusPending, Required: &required},
	}
	created, err := store.CreateSession(context.Background(), createSessionInputFromSession(session))
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	service := &Service{sessionStore: store, events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-boundary-prune", created)
	if err := service.resetTodosForUserRun(context.Background(), &state); err != nil {
		t.Fatalf("resetTodosForUserRun() error = %v", err)
	}
	if len(state.session.Todos) != 1 || state.session.Todos[0].ID != "plan-todo" {
		t.Fatalf("state todos = %+v, want only plan-owned todo", state.session.Todos)
	}
}

func TestShouldResetTodosForUserRunBoundaryVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		session   agentsession.Session
		wantReset bool
	}{
		{name: "no plan resets", session: agentsession.New("no plan"), wantReset: true},
		{name: "draft plan keeps", session: sessionWithPlanStatus(agentsession.PlanStatusDraft), wantReset: false},
		{name: "approved plan keeps", session: sessionWithPlanStatus(agentsession.PlanStatusApproved), wantReset: false},
		{name: "completed plan resets", session: sessionWithPlanStatus(agentsession.PlanStatusCompleted), wantReset: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldResetTodosForUserRun(tc.session)
			if got != tc.wantReset {
				t.Fatalf("shouldResetTodosForUserRun() = %v, want %v", got, tc.wantReset)
			}
		})
	}
}

func sessionWithPlanStatus(status agentsession.PlanStatus) agentsession.Session {
	session := agentsession.New("plan-boundary")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:     "plan-1",
		Status: status,
	}
	return session
}
