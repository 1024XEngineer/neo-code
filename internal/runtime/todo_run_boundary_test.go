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
	state.userGoal = "新任务"
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

func TestShouldResetTodosForUserRunBoundaryVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		goal      string
		wantReset bool
	}{
		// 空输入 → 保留
		{"empty", "", false},

		// 明确新任务 → 清空
		{"chinese exact 新任务", "新任务", true},
		{"chinese 帮我做个新任务", "帮我做个新任务", true},
		{"chinese 换个任务", "换个任务", true},
		{"chinese 新需求", "新需求", true},
		{"english exact new task", "new task", true},
		{"english 新任务", "new task please", true},
		{"english switch task", "switch task", true},
		{"english different task", "different task", true},

		// 默认保留：绝大多数输入不再被硬编码清空，交给 prompt 引导模型自行处理
		{"chinese 继续", "继续", false},
		{"chinese 继续修这个", "继续修这个", false},
		{"chinese 接着做", "接着做", false},
		{"chinese 刚才的代码还有问题", "刚才的代码还有问题", false},
		{"chinese 再优化一下", "再优化一下", false},
		{"chinese 补充测试用例", "补充测试用例", false},
		{"chinese 修复登录bug", "修复登录 bug", false},
		{"chinese 开始下一个任务", "开始下一个任务", false},
		{"chinese 重新实现", "重新实现", false},
		{"english continue", "continue", false},
		{"english continue with the failing test", "continue with the failing test", false},
		{"english implement search api", "implement search api", false},
		{"english keep going", "keep going", false},
		{"english keep it simple", "keep it simple please", false},
		{"english resume", "resume task", false},
		{"english go on", "go on please", false},
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
