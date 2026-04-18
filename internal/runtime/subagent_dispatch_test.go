package runtime

import (
	"context"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
	todotool "neo-code/internal/tools/todo"
)

func TestDispatchTodosExecutesSubAgentTasks(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	service := NewWithFactory(
		manager,
		tools.NewRegistry(),
		store,
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		&stubContextBuilder{},
	)
	service.SetSubAgentFactory(newSuccessSubAgentFactory())

	session := agentsession.New("dispatch-session")
	session.Workdir = manager.Get().Workdir
	if err := session.ReplaceTodos([]agentsession.TodoItem{
		{
			ID:       "a",
			Content:  "task-a",
			Executor: agentsession.TodoExecutorSubAgent,
			Priority: 2,
		},
		{
			ID:           "b",
			Content:      "task-b",
			Executor:     agentsession.TodoExecutorSubAgent,
			Dependencies: []string{"a"},
			Priority:     1,
		},
	}); err != nil {
		t.Fatalf("ReplaceTodos() error = %v", err)
	}
	saveSessionToMemoryStore(store, session)

	state := newRunState("run-dispatch", session)
	state.turn = 1
	state.phase = controlplane.PhaseDispatch
	progressed, err := service.dispatchTodos(context.Background(), &state, turnSnapshot{workdir: session.Workdir})
	if err != nil {
		t.Fatalf("dispatchTodos() error = %v", err)
	}
	if !progressed {
		t.Fatalf("dispatchTodos() progressed = false, want true")
	}

	a, ok := state.session.FindTodo("a")
	if !ok || a.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo a = %+v, want completed", a)
	}
	b, ok := state.session.FindTodo("b")
	if !ok || b.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo b = %+v, want completed", b)
	}
	if len(b.Artifacts) == 0 {
		t.Fatalf("todo b artifacts should not be empty")
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventSubAgentCompleted)
	assertEventContains(t, events, EventSubAgentFinished)
}

func TestDispatchTodosSkipsAgentOwnedTodos(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	service := NewWithFactory(
		manager,
		tools.NewRegistry(),
		store,
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		&stubContextBuilder{},
	)

	session := agentsession.New("dispatch-skip")
	if err := session.ReplaceTodos([]agentsession.TodoItem{
		{
			ID:       "agent-task",
			Content:  "handled by agent",
			Executor: agentsession.TodoExecutorAgent,
		},
	}); err != nil {
		t.Fatalf("ReplaceTodos() error = %v", err)
	}
	state := newRunState("run-dispatch-skip", session)
	state.phase = controlplane.PhaseDispatch
	progressed, err := service.dispatchTodos(context.Background(), &state, turnSnapshot{})
	if err != nil {
		t.Fatalf("dispatchTodos() error = %v", err)
	}
	if progressed {
		t.Fatalf("dispatchTodos() progressed = true, want false")
	}

	task, ok := state.session.FindTodo("agent-task")
	if !ok {
		t.Fatalf("FindTodo(agent-task) not found")
	}
	if task.Status != agentsession.TodoStatusPending {
		t.Fatalf("status = %q, want pending", task.Status)
	}
	events := collectRuntimeEvents(service.Events())
	if len(events) != 0 {
		t.Fatalf("expected no dispatch events for agent-owned todos, got %d", len(events))
	}
}

func TestRunAutoDispatchesSubAgentTodosFromTodoWrite(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-plan-1",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"plan","items":[{"id":"sub-1","content":"run sub agent","executor":"subagent"}]}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("all done")},
				},
			},
		},
	}
	service := NewWithFactory(
		manager,
		func() tools.Manager {
			registry := tools.NewRegistry()
			registry.Register(todotool.New())
			return registry
		}(),
		store,
		&scriptedProviderFactory{provider: scripted},
		&stubContextBuilder{},
	)
	service.SetSubAgentFactory(newSuccessSubAgentFactory())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Run(ctx, UserInput{
		RunID: "run-auto-dispatch",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("start")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	session := firstSessionFromMemoryStore(t, store)
	task, ok := session.FindTodo("sub-1")
	if !ok {
		t.Fatalf("todo sub-1 not found")
	}
	if task.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo sub-1 status = %q, want completed", task.Status)
	}
	if len(task.Artifacts) == 0 {
		t.Fatalf("todo sub-1 artifacts should not be empty")
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventSubAgentStarted)
	assertEventContains(t, events, EventSubAgentCompleted)
	assertEventContains(t, events, EventSubAgentFinished)
}

func TestRunAutoDispatchesExistingSubAgentTodosWithoutToolCalls(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("skip direct tools")},
				},
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("all done")},
				},
			},
		},
	}
	service := NewWithFactory(
		manager,
		tools.NewRegistry(),
		store,
		&scriptedProviderFactory{provider: scripted},
		&stubContextBuilder{},
	)
	service.SetSubAgentFactory(newSuccessSubAgentFactory())

	seed := agentsession.New("dispatch-seeded")
	seed.Workdir = manager.Get().Workdir
	if err := seed.ReplaceTodos([]agentsession.TodoItem{
		{
			ID:       "seed-sub-1",
			Content:  "run from existing todo",
			Executor: agentsession.TodoExecutorSubAgent,
		},
	}); err != nil {
		t.Fatalf("ReplaceTodos(seed) error = %v", err)
	}
	saveSessionToMemoryStore(store, seed)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Run(ctx, UserInput{
		SessionID: seed.ID,
		RunID:     "run-auto-dispatch-existing",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	session := firstSessionFromMemoryStore(t, store)
	task, ok := session.FindTodo("seed-sub-1")
	if !ok {
		t.Fatalf("todo seed-sub-1 not found")
	}
	if task.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo seed-sub-1 status = %q, want completed", task.Status)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventSubAgentStarted)
	assertEventContains(t, events, EventSubAgentCompleted)
	assertEventContains(t, events, EventSubAgentFinished)
}

func TestRunKeepsDrivingAgentPathForMixedExecutorDependencies(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue planning")},
				},
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-claim-agent",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"claim","id":"agent-1","owner_type":"agent","owner_id":"main-agent"}`,
						},
						{
							ID:        "todo-complete-agent",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"complete","id":"agent-1","artifacts":["agent-1.done"]}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("all done")},
				},
			},
		},
	}
	service := NewWithFactory(
		manager,
		func() tools.Manager {
			registry := tools.NewRegistry()
			registry.Register(todotool.New())
			return registry
		}(),
		store,
		&scriptedProviderFactory{provider: scripted},
		&stubContextBuilder{},
	)
	service.SetSubAgentFactory(newSuccessSubAgentFactory())

	seed := agentsession.New("dispatch-mixed-deps")
	seed.Workdir = manager.Get().Workdir
	if err := seed.ReplaceTodos([]agentsession.TodoItem{
		{
			ID:       "agent-1",
			Content:  "agent prerequisite",
			Executor: agentsession.TodoExecutorAgent,
		},
		{
			ID:           "sub-1",
			Content:      "subagent follow-up",
			Executor:     agentsession.TodoExecutorSubAgent,
			Dependencies: []string{"agent-1"},
		},
	}); err != nil {
		t.Fatalf("ReplaceTodos(seed) error = %v", err)
	}
	saveSessionToMemoryStore(store, seed)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Run(ctx, UserInput{
		SessionID: seed.ID,
		RunID:     "run-mixed-dependency-keep-driving",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if scripted.callCount != 3 {
		t.Fatalf("provider call count = %d, want 3", scripted.callCount)
	}

	session := firstSessionFromMemoryStore(t, store)
	agentTodo, ok := session.FindTodo("agent-1")
	if !ok || agentTodo.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("agent todo = %+v, want completed", agentTodo)
	}
	subTodo, ok := session.FindTodo("sub-1")
	if !ok || subTodo.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("sub todo = %+v, want completed", subTodo)
	}
}

func TestHasSubAgentTodoWaitingForAgentDependency(t *testing.T) {
	t.Parallel()

	if !hasSubAgentTodoWaitingForAgentDependency([]agentsession.TodoItem{
		{
			ID:       "agent",
			Executor: agentsession.TodoExecutorAgent,
			Status:   agentsession.TodoStatusPending,
		},
		{
			ID:           "sub",
			Executor:     agentsession.TodoExecutorSubAgent,
			Status:       agentsession.TodoStatusBlocked,
			Dependencies: []string{"agent"},
		},
	}) {
		t.Fatalf("expected pending agent dependency to require follow-up")
	}

	if hasSubAgentTodoWaitingForAgentDependency([]agentsession.TodoItem{
		{
			ID:       "agent",
			Executor: agentsession.TodoExecutorAgent,
			Status:   agentsession.TodoStatusCompleted,
		},
		{
			ID:           "sub",
			Executor:     agentsession.TodoExecutorSubAgent,
			Status:       agentsession.TodoStatusBlocked,
			Dependencies: []string{"agent"},
		},
	}) {
		t.Fatalf("completed agent dependency should not require follow-up")
	}
}

func TestEmitSubAgentSchedulerEventEmitsOnlySchedulerSpecificEvents(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManagerWithProviderEnvs(t, nil)
	store := newMemoryStore()
	service := NewWithFactory(
		manager,
		tools.NewRegistry(),
		store,
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		&stubContextBuilder{},
	)
	state := newRunState("run-emit-scheduler-events", agentsession.New("emit-scheduler-events"))

	service.emitSubAgentSchedulerEvent(context.Background(), &state, subagent.SchedulerEvent{
		Type:    subagent.SchedulerEventSubAgentStarted,
		TaskID:  "task-1",
		Attempt: 1,
	})
	service.emitSubAgentSchedulerEvent(context.Background(), &state, subagent.SchedulerEvent{
		Type:    subagent.SchedulerEventSubAgentCompleted,
		TaskID:  "task-1",
		Attempt: 1,
	})
	service.emitSubAgentSchedulerEvent(context.Background(), &state, subagent.SchedulerEvent{
		Type:    subagent.SchedulerEventSubAgentRetried,
		TaskID:  "task-1",
		Attempt: 2,
		Reason:  "retry_after_failure",
	})
	service.emitSubAgentSchedulerEvent(context.Background(), &state, subagent.SchedulerEvent{
		Type:   subagent.SchedulerEventBlocked,
		TaskID: "task-2",
		Reason: "dependency_unmet",
	})
	service.emitSubAgentSchedulerEvent(context.Background(), &state, subagent.SchedulerEvent{
		Type:      subagent.SchedulerEventFinished,
		QueueSize: 3,
		Running:   0,
	})

	events := collectRuntimeEvents(service.Events())
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	assertEventContains(t, events, EventSubAgentRetried)
	assertEventContains(t, events, EventSubAgentBlocked)
	assertEventContains(t, events, EventSubAgentFinished)

	for _, event := range events {
		if event.Type != EventSubAgentFinished {
			continue
		}
		payload, ok := event.Payload.(SubAgentEventPayload)
		if !ok {
			t.Fatalf("payload type = %T, want SubAgentEventPayload", event.Payload)
		}
		if payload.TaskID != "" {
			t.Fatalf("finished payload task_id = %q, want empty", payload.TaskID)
		}
		if payload.State != "" {
			t.Fatalf("finished payload state = %q, want empty", payload.State)
		}
		if payload.Reason != "dispatch_round_finished" {
			t.Fatalf("finished payload reason = %q, want dispatch_round_finished", payload.Reason)
		}
		if payload.QueueSize != 3 || payload.Running != 0 {
			t.Fatalf("finished payload queue/running = %d/%d, want 3/0", payload.QueueSize, payload.Running)
		}
	}
}

func newSuccessSubAgentFactory() subagent.Factory {
	return subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		_ = role
		_ = policy
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			_ = ctx
			return subagent.StepOutput{
				Done:  true,
				Delta: "completed",
				Output: subagent.Output{
					Summary:     "completed " + input.Task.ID,
					Findings:    []string{"ok"},
					Patches:     []string{"none"},
					Risks:       []string{"low"},
					NextActions: []string{"continue"},
					Artifacts:   []string{input.Task.ID + ".artifact"},
				},
			}, nil
		})
	})
}

func firstSessionFromMemoryStore(t *testing.T, store *memoryStore) agentsession.Session {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, session := range store.sessions {
		return session
	}
	t.Fatalf("memory store has no sessions")
	return agentsession.Session{}
}

func saveSessionToMemoryStore(store *memoryStore, session agentsession.Session) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saves++
	store.sessions[session.ID] = cloneSession(session)
}
