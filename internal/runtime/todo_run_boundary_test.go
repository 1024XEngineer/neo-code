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

func TestShouldResetTodosForUserRunContinueVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		goal      string
		wantReset bool
	}{
		// 续做语义 → 应保留旧 todo
		{"empty", "", false},
		{"chinese exact", "继续", false},
		{"chinese with content", "继续修这个", false},
		{"chinese with punctuation", "继续。", false},
		{"chinese alt prefix 接着", "接着做", false},
		{"chinese alt prefix 续做", "续做完未做的", false},
		{"chinese alt prefix 再继续", "再继续完善", false},
		{"chinese alt prefix 再来", "再来一次", false},
		{"english exact lowercase", "continue", false},
		{"english with content", "continue with the failing test", false},
		{"english punctuation", "Continue!", false},
		{"english keep going", "keep going", false},
		{"english keep going with content", "keep going on the bug fix", false},
		{"english keep doing", "keep doing it", false},
		{"english go on", "go on please", false},
		{"english resume", "resume task", false},
		{"english carry on", "carry on with the migration", false},
		{"trailing chinese punctuation", "继续，", false},
		{"trailing question mark", "go on?", false},

		// 新指令 → 应清空旧 todo
		{"new task chinese", "修复登录 bug", true},
		{"start over", "重新开始项目", true},
		{"keep without going", "keep it simple please", true},
		{"continueworking no boundary", "continueworking", true},
		{"new task english", "implement search api", true},
		{"explore", "开始下一个任务", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldResetTodosForUserRun(tc.goal)
			if got != tc.wantReset {
				t.Fatalf("shouldResetTodosForUserRun(%q) = %v, want %v", tc.goal, got, tc.wantReset)
			}
		})
	}
}
