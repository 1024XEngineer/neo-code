package subagent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentsession "neo-code/internal/session"
)

type schedulerStore struct {
	mu              sync.Mutex
	session         *agentsession.Session
	claimConflicts  map[string]int
	updateConflicts map[string]int
}

type schedulerStoreWithClaimError struct {
	*schedulerStore
	claimErrors map[string]error
}

func newSchedulerStore(t *testing.T, items []agentsession.TodoItem) *schedulerStore {
	t.Helper()
	session := agentsession.New("scheduler")
	for idx := range items {
		if strings.TrimSpace(items[idx].Executor) == "" {
			items[idx].Executor = agentsession.TodoExecutorSubAgent
		}
	}
	if err := session.ReplaceTodos(items); err != nil {
		t.Fatalf("ReplaceTodos() error = %v", err)
	}
	return &schedulerStore{
		session:         &session,
		claimConflicts:  make(map[string]int),
		updateConflicts: make(map[string]int),
	}
}

func (s *schedulerStore) ListTodos() []agentsession.TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.ListTodos()
}

func (s *schedulerStore) FindTodo(id string) (agentsession.TodoItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.FindTodo(id)
}

func (s *schedulerStore) UpdateTodo(id string, patch agentsession.TodoPatch, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.updateConflicts[id]; n > 0 {
		s.updateConflicts[id] = n - 1
		return fmt.Errorf("%w: injected update conflict", agentsession.ErrRevisionConflict)
	}
	return s.session.UpdateTodo(id, patch, expectedRevision)
}

func (s *schedulerStore) ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.claimConflicts[id]; n > 0 {
		s.claimConflicts[id] = n - 1
		return fmt.Errorf("%w: injected claim conflict", agentsession.ErrRevisionConflict)
	}
	return s.session.ClaimTodo(id, ownerType, ownerID, expectedRevision)
}

func (s *schedulerStore) CompleteTodo(id string, artifacts []string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.CompleteTodo(id, artifacts, expectedRevision)
}

func (s *schedulerStore) FailTodo(id string, reason string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.FailTodo(id, reason, expectedRevision)
}

func (s *schedulerStoreWithClaimError) ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error {
	s.mu.Lock()
	if err, ok := s.claimErrors[id]; ok && err != nil {
		delete(s.claimErrors, id)
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return s.schedulerStore.ClaimTodo(id, ownerType, ownerID, expectedRevision)
}

type scriptedFactory struct {
	mu       sync.Mutex
	attempts map[string]int
	run      func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error)
}

func newScriptedFactory(
	run func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error),
) *scriptedFactory {
	return &scriptedFactory{
		attempts: make(map[string]int),
		run:      run,
	}
}

func (f *scriptedFactory) Create(role Role) (WorkerRuntime, error) {
	policy, err := DefaultRolePolicy(role)
	if err != nil {
		return nil, err
	}
	engine := EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
		f.mu.Lock()
		f.attempts[input.Task.ID]++
		attempt := f.attempts[input.Task.ID]
		f.mu.Unlock()
		return f.run(ctx, input.Task.ID, attempt, input)
	})
	return NewWorker(role, policy, engine)
}

func successStep(taskID string) StepOutput {
	return StepOutput{
		Done: true,
		Output: Output{
			Summary:     "done: " + taskID,
			Findings:    []string{"ok"},
			Patches:     []string{"none"},
			Risks:       []string{"low"},
			NextActions: []string{"continue"},
			Artifacts:   []string{taskID + ".artifact"},
		},
	}
}

func TestSchedulerConfigNormalize(t *testing.T) {
	t.Parallel()

	cfg := (SchedulerConfig{}).normalize()
	if cfg.MaxConcurrency != 2 {
		t.Fatalf("MaxConcurrency = %d, want 2", cfg.MaxConcurrency)
	}
	if cfg.DefaultRole != RoleCoder {
		t.Fatalf("DefaultRole = %q, want %q", cfg.DefaultRole, RoleCoder)
	}
	if cfg.DefaultBudget.MaxSteps <= 0 || cfg.DefaultBudget.Timeout <= 0 {
		t.Fatalf("DefaultBudget not normalized: %+v", cfg.DefaultBudget)
	}
	if cfg.MaxRetries != 0 {
		t.Fatalf("MaxRetries = %d, want 0", cfg.MaxRetries)
	}
	if cfg.PollInterval <= 0 {
		t.Fatalf("PollInterval should be positive")
	}
	if cfg.WorkerIDPrefix == "" {
		t.Fatalf("WorkerIDPrefix should not be empty")
	}
	if cfg.Clock == nil || cfg.RoleSelector == nil || cfg.BudgetSelector == nil || cfg.Backoff == nil || cfg.Observer == nil {
		t.Fatalf("normalize() should set defaults")
	}
	if cfg.ContextBuilder == nil || cfg.ContextSkills == nil || cfg.ContextFiles == nil {
		t.Fatalf("normalize() should set context defaults")
	}
	if cfg.ContextMaxChars <= 0 || cfg.ContextMaxTodoFragments <= 0 ||
		cfg.ContextMaxDependencyArtifacts <= 0 || cfg.ContextMaxRelatedFiles <= 0 {
		t.Fatalf("context budget defaults not normalized: %+v", cfg)
	}

	role := cfg.RoleSelector(agentsession.TodoItem{OwnerID: string(RoleReviewer)})
	if role != cfg.DefaultRole {
		t.Fatalf("RoleSelector() = %q, want default %q", role, cfg.DefaultRole)
	}
	role = cfg.RoleSelector(agentsession.TodoItem{OwnerID: "unknown"})
	if role != cfg.DefaultRole {
		t.Fatalf("RoleSelector fallback = %q, want %q", role, cfg.DefaultRole)
	}
	if got := cfg.Backoff(3); got != 4*time.Second {
		t.Fatalf("Backoff(3) = %v, want 4s", got)
	}
	if cfg.FailureMode != SchedulerFailureContinueOnError {
		t.Fatalf("FailureMode = %q, want %q", cfg.FailureMode, SchedulerFailureContinueOnError)
	}
	if cfg.RecoveryMode != SchedulerRecoveryRetry {
		t.Fatalf("RecoveryMode = %q, want %q", cfg.RecoveryMode, SchedulerRecoveryRetry)
	}
	if cfg.RecoveryReason == "" {
		t.Fatalf("RecoveryReason should not be empty")
	}
	if got := cfg.BudgetSelector(agentsession.TodoItem{}, cfg.DefaultBudget); got != cfg.DefaultBudget {
		t.Fatalf("BudgetSelector() = %+v, want %+v", got, cfg.DefaultBudget)
	}
	slice := cfg.ContextBuilder(TaskContextSliceInput{
		Task: agentsession.TodoItem{ID: "t1", Content: "goal"},
		Todos: map[string]agentsession.TodoItem{
			"t1": {ID: "t1", Content: "goal"},
		},
		MaxChars: cfg.ContextMaxChars,
	})
	if slice.TaskID != "t1" {
		t.Fatalf("ContextBuilder() task id = %q, want t1", slice.TaskID)
	}
	if skills := cfg.ContextSkills(agentsession.TodoItem{}, map[string]agentsession.TodoItem{}); skills != nil {
		t.Fatalf("ContextSkills default should return nil, got %v", skills)
	}
	if files := cfg.ContextFiles(agentsession.TodoItem{}, map[string]agentsession.TodoItem{}); files != nil {
		t.Fatalf("ContextFiles default should return nil, got %v", files)
	}
}

func TestNewSchedulerValidationErrors(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{{ID: "a", Content: "a"}})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep("a"), nil
	})

	if _, err := NewScheduler(nil, factory, SchedulerConfig{}); err == nil {
		t.Fatalf("NewScheduler(nil, factory) should fail")
	}
	if _, err := NewScheduler(store, nil, SchedulerConfig{}); err == nil {
		t.Fatalf("NewScheduler(store, nil) should fail")
	}
}

func TestSchedulerRunEarlyErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("context canceled before run", func(t *testing.T) {
		t.Parallel()
		store := newSchedulerStore(t, []agentsession.TodoItem{{ID: "a", Content: "a"}})
		factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
			_ = ctx
			_ = taskID
			_ = attempt
			_ = input
			return successStep("a"), nil
		})
		scheduler, err := NewScheduler(store, factory, SchedulerConfig{})
		if err != nil {
			t.Fatalf("NewScheduler() error = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := scheduler.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", err)
		}
	})

	t.Run("invalid graph", func(t *testing.T) {
		t.Parallel()
		store := newSchedulerStore(t, []agentsession.TodoItem{{ID: "a", Content: "a"}})
		store.session.Todos = append(store.session.Todos, agentsession.TodoItem{ID: "", Content: "bad"})
		factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
			_ = ctx
			_ = taskID
			_ = attempt
			_ = input
			return successStep("a"), nil
		})
		scheduler, err := NewScheduler(store, factory, SchedulerConfig{})
		if err != nil {
			t.Fatalf("NewScheduler() error = %v", err)
		}

		_, err = scheduler.Run(context.Background())
		if err == nil || !strings.Contains(err.Error(), "empty todo id") {
			t.Fatalf("Run() error = %v, want empty todo id", err)
		}
	})
}

func TestBuildTaskGraphValidation(t *testing.T) {
	t.Parallel()

	t.Run("duplicate id", func(t *testing.T) {
		t.Parallel()
		_, err := buildTaskGraph([]agentsession.TodoItem{
			{ID: "a", Content: "a"},
			{ID: "a", Content: "dup"},
		})
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("buildTaskGraph() error = %v, want duplicate", err)
		}
	})

	t.Run("unknown dependency", func(t *testing.T) {
		t.Parallel()
		_, err := buildTaskGraph([]agentsession.TodoItem{
			{ID: "a", Content: "a", Dependencies: []string{"x"}},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown dependency") {
			t.Fatalf("buildTaskGraph() error = %v, want unknown dependency", err)
		}
	})

	t.Run("cycle", func(t *testing.T) {
		t.Parallel()
		_, err := buildTaskGraph([]agentsession.TodoItem{
			{ID: "a", Content: "a", Dependencies: []string{"b"}},
			{ID: "b", Content: "b", Dependencies: []string{"a"}},
		})
		if err == nil || !errors.Is(err, agentsession.ErrCyclicDependency) {
			t.Fatalf("buildTaskGraph() error = %v, want cyclic dependency", err)
		}
	})
}

func TestSchedulerRunDependencyUnlock(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "a", Content: "task-a", Priority: 2},
		{ID: "b", Content: "task-b", Dependencies: []string{"a"}, Priority: 1},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = attempt
		_ = input
		return successStep(taskID), nil
	})

	var (
		mu     sync.Mutex
		events []SchedulerEvent
	)
	cfg := SchedulerConfig{
		MaxConcurrency: 2,
		PollInterval:   2 * time.Millisecond,
		Observer: func(event SchedulerEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	}
	scheduler, err := NewScheduler(store, factory, cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.Succeeded) != 2 {
		t.Fatalf("Succeeded = %v, want 2 tasks", result.Succeeded)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed = %v, want empty", result.Failed)
	}
	if len(result.BlockedLeft) != 0 {
		t.Fatalf("BlockedLeft = %v, want empty", result.BlockedLeft)
	}

	a, ok := store.FindTodo("a")
	if !ok || a.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo a status = %+v, want completed", a)
	}
	b, ok := store.FindTodo("b")
	if !ok || b.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo b status = %+v, want completed", b)
	}
	if len(b.Artifacts) == 0 || b.Artifacts[0] != "b.artifact" {
		t.Fatalf("todo b artifacts = %v, want b.artifact", b.Artifacts)
	}

	mu.Lock()
	defer mu.Unlock()
	if !hasEvent(events, SchedulerEventBlocked, "b") {
		t.Fatalf("expected blocked event for b, events=%+v", events)
	}
	if !hasEvent(events, SchedulerEventCompleted, "a") || !hasEvent(events, SchedulerEventCompleted, "b") {
		t.Fatalf("expected completed events for a/b, events=%+v", events)
	}
}

func TestSchedulerBuildsTaskContextSlice(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{
			ID:           "dep",
			Content:      "完成依赖",
			Status:       agentsession.TodoStatusCompleted,
			Artifacts:    []string{"artifacts/dep-result.txt"},
			Dependencies: nil,
		},
		{
			ID:           "main",
			Content:      "执行主任务",
			Status:       agentsession.TodoStatusPending,
			Dependencies: []string{"dep"},
			Acceptance:   []string{"通过测试"},
		},
	})

	inputCh := make(chan StepInput, 2)
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = attempt
		select {
		case inputCh <- input:
		default:
		}
		return successStep(taskID), nil
	})

	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		ContextSkills: func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []string {
			_ = todo
			_ = snapshot
			return []string{"skill-a", "skill-b"}
		},
		ContextFiles: func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []TaskContextFileSummary {
			_ = snapshot
			return []TaskContextFileSummary{
				{Path: "internal/subagent/scheduler.go", Summary: "调度器实现"},
				{Path: "internal/subagent/scheduler_types.go", Summary: "配置定义"},
			}
		},
		ContextMaxChars: 4000,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var captured StepInput
	select {
	case captured = <-inputCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting captured step input")
	}

	if captured.Task.ID != "main" {
		t.Fatalf("captured task id = %q, want main", captured.Task.ID)
	}
	if captured.Task.ContextSlice.TaskID != "main" {
		t.Fatalf("context task id = %q, want main", captured.Task.ContextSlice.TaskID)
	}
	if len(captured.Task.ContextSlice.DependencyArtifacts) == 0 {
		t.Fatalf("expected dependency artifacts in context slice")
	}
	if !slices.Equal(captured.Task.ContextSlice.ActivatedSkills, []string{"skill-a", "skill-b"}) {
		t.Fatalf("ActivatedSkills = %v, want [skill-a skill-b]", captured.Task.ContextSlice.ActivatedSkills)
	}
	if len(captured.Task.ContextSlice.RelatedFiles) == 0 {
		t.Fatalf("expected related files in context slice")
	}
}

func TestSchedulerContextBuilderUsesReadOnlySnapshot(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "task", Content: "task"},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = attempt
		_ = input
		return successStep(taskID), nil
	})

	var seenReadOnly bool
	builder := func(input TaskContextSliceInput) TaskContextSlice {
		seenReadOnly = input.ReadOnlyTodos
		return BuildTaskContextSlice(input)
	}
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		ContextBuilder: builder,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !seenReadOnly {
		t.Fatalf("ContextBuilder input ReadOnlyTodos = false, want true")
	}
}

func TestSchedulerRunConcurrencyLimit(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "t1", Content: "t1"},
		{ID: "t2", Content: "t2"},
		{ID: "t3", Content: "t3"},
	})

	var running int32
	var maxRunning int32
	started := make(chan string, 8)
	release := make(chan struct{})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = attempt
		_ = input
		nowRunning := atomic.AddInt32(&running, 1)
		for {
			prev := atomic.LoadInt32(&maxRunning)
			if nowRunning <= prev || atomic.CompareAndSwapInt32(&maxRunning, prev, nowRunning) {
				break
			}
		}
		started <- taskID
		select {
		case <-release:
		case <-ctx.Done():
			atomic.AddInt32(&running, -1)
			return StepOutput{}, ctx.Err()
		}
		atomic.AddInt32(&running, -1)
		return successStep(taskID), nil
	})

	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 2,
		PollInterval:   2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, runErr := scheduler.Run(ctx)
		done <- runErr
	}()

	timeout := time.After(500 * time.Millisecond)
	count := 0
	for count < 2 {
		select {
		case <-started:
			count++
		case <-timeout:
			t.Fatalf("timeout waiting first two tasks to start")
		}
	}
	close(release)

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run() error = %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting scheduler complete")
	}

	if got := atomic.LoadInt32(&maxRunning); got != 2 {
		t.Fatalf("max concurrency = %d, want 2", got)
	}
}

func TestSchedulerRunRetryAndGiveUp(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "r1", Content: "retry once then pass", RetryLimit: 1},
		{ID: "r2", Content: "always fail", RetryLimit: 1},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = input
		switch taskID {
		case "r1":
			if attempt == 1 {
				return StepOutput{}, errors.New("r1 first fail")
			}
			return successStep(taskID), nil
		case "r2":
			return StepOutput{}, errors.New("r2 always fail")
		default:
			return successStep(taskID), nil
		}
	})

	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 2,
		MaxRetries:     2,
		PollInterval:   2 * time.Millisecond,
		Backoff: func(attempt int) time.Duration {
			_ = attempt
			return 0
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := result.Retried["r1"]; got != 1 {
		t.Fatalf("Retried[r1] = %d, want 1", got)
	}
	if got := result.Retried["r2"]; got != 1 {
		t.Fatalf("Retried[r2] = %d, want 1", got)
	}
	if !contains(result.Failed, "r2") {
		t.Fatalf("Failed = %v, want r2", result.Failed)
	}

	r1, _ := store.FindTodo("r1")
	if r1.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("r1 status = %q, want completed", r1.Status)
	}
	r2, _ := store.FindTodo("r2")
	if r2.Status != agentsession.TodoStatusFailed {
		t.Fatalf("r2 status = %q, want failed", r2.Status)
	}
	if r2.RetryCount != 2 {
		t.Fatalf("r2 retry_count = %d, want 2", r2.RetryCount)
	}
	if !strings.Contains(r2.FailureReason, "always fail") {
		t.Fatalf("r2 failure_reason = %q, want contains always fail", r2.FailureReason)
	}
}

func TestSchedulerRunRecoveryFromInProgress(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{
			ID:         "recover-me",
			Content:    "recover interrupted task",
			Status:     agentsession.TodoStatusInProgress,
			RetryLimit: 2,
		},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = attempt
		_ = input
		return successStep(taskID), nil
	})

	var (
		mu     sync.Mutex
		events []SchedulerEvent
	)
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		Backoff: func(attempt int) time.Duration {
			_ = attempt
			return 0
		},
		Observer: func(event SchedulerEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(result.Recovered, "recover-me") {
		t.Fatalf("Recovered = %v, want recover-me", result.Recovered)
	}

	item, ok := store.FindTodo("recover-me")
	if !ok {
		t.Fatalf("FindTodo(recover-me) not found")
	}
	if item.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("status = %q, want completed", item.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	if !hasEvent(events, SchedulerEventSubAgentRetried, "recover-me") {
		t.Fatalf("expected subagent_retried event, events=%+v", events)
	}
}

func TestSchedulerRunRecoveryModeFail(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{
			ID:         "recover-fail",
			Content:    "recover interrupted task",
			Status:     agentsession.TodoStatusInProgress,
			RetryLimit: 2,
		},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep("unused"), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		RecoveryMode:   SchedulerRecoveryFail,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(result.Recovered, "recover-fail") {
		t.Fatalf("Recovered = %v, want recover-fail", result.Recovered)
	}
	if !contains(result.Failed, "recover-fail") {
		t.Fatalf("Failed = %v, want recover-fail", result.Failed)
	}

	item, ok := store.FindTodo("recover-fail")
	if !ok {
		t.Fatalf("FindTodo(recover-fail) not found")
	}
	if item.Status != agentsession.TodoStatusFailed {
		t.Fatalf("status = %q, want failed", item.Status)
	}
	if !strings.Contains(item.FailureReason, "recovered interrupted") {
		t.Fatalf("FailureReason = %q, want recovered interrupted", item.FailureReason)
	}
}

func TestSchedulerRunRecoveryExceedRetryLimitMarkedFailed(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{
			ID:         "recover-over-limit",
			Content:    "recover interrupted task",
			Status:     agentsession.TodoStatusInProgress,
			RetryLimit: 1,
			RetryCount: 1,
		},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep("unused"), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		RecoveryMode:   SchedulerRecoveryRetry,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(result.Recovered, "recover-over-limit") {
		t.Fatalf("Recovered = %v, want recover-over-limit", result.Recovered)
	}
	if !contains(result.Failed, "recover-over-limit") {
		t.Fatalf("Failed = %v, want recover-over-limit", result.Failed)
	}

	item, ok := store.FindTodo("recover-over-limit")
	if !ok {
		t.Fatalf("FindTodo(recover-over-limit) not found")
	}
	if item.Status != agentsession.TodoStatusFailed {
		t.Fatalf("status = %q, want failed", item.Status)
	}
}

func TestSchedulerRunFailureModes(t *testing.T) {
	t.Parallel()

	t.Run("continue on error", func(t *testing.T) {
		t.Parallel()
		store := newSchedulerStore(t, []agentsession.TodoItem{
			{ID: "a", Content: "a"},
			{ID: "b", Content: "b"},
		})
		factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
			_ = ctx
			_ = attempt
			_ = input
			if taskID == "a" {
				return StepOutput{}, errors.New("boom-a")
			}
			return successStep(taskID), nil
		})

		scheduler, err := NewScheduler(store, factory, SchedulerConfig{
			MaxConcurrency: 2,
			PollInterval:   2 * time.Millisecond,
			FailureMode:    SchedulerFailureContinueOnError,
		})
		if err != nil {
			t.Fatalf("NewScheduler() error = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result, err := scheduler.Run(ctx)
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if !contains(result.Failed, "a") || !contains(result.Succeeded, "b") {
			t.Fatalf("result = %+v, want a failed and b succeeded", result)
		}
	})

	t.Run("fail fast", func(t *testing.T) {
		t.Parallel()
		store := newSchedulerStore(t, []agentsession.TodoItem{
			{ID: "a", Content: "a"},
			{ID: "b", Content: "b"},
		})
		started := make(chan string, 2)
		factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
			_ = attempt
			_ = input
			select {
			case started <- taskID:
			default:
			}
			if taskID == "a" {
				return StepOutput{}, errors.New("boom-a")
			}
			<-ctx.Done()
			return StepOutput{}, ctx.Err()
		})

		scheduler, err := NewScheduler(store, factory, SchedulerConfig{
			MaxConcurrency: 2,
			PollInterval:   2 * time.Millisecond,
			FailureMode:    SchedulerFailureFailFast,
		})
		if err != nil {
			t.Fatalf("NewScheduler() error = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err = scheduler.Run(ctx)
		if err == nil || !errors.Is(err, errSchedulerFailFast) {
			t.Fatalf("Run() error = %v, want fail-fast error", err)
		}

		a, _ := store.FindTodo("a")
		if a.Status != agentsession.TodoStatusFailed {
			t.Fatalf("todo a status = %q, want failed", a.Status)
		}
		b, _ := store.FindTodo("b")
		if b.Status != agentsession.TodoStatusCanceled {
			t.Fatalf("todo b status = %q, want canceled", b.Status)
		}
	})
}

func TestSchedulerRunStandardEventSequence(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "seq", Content: "sequence task", RetryLimit: 2},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = input
		if attempt == 1 {
			return StepOutput{}, errors.New("first attempt fail")
		}
		return successStep("seq"), nil
	})

	var (
		mu     sync.Mutex
		events []SchedulerEventType
	)
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		Backoff: func(attempt int) time.Duration {
			_ = attempt
			return 0
		},
		Observer: func(event SchedulerEvent) {
			mu.Lock()
			switch event.Type {
			case SchedulerEventSubAgentStarted, SchedulerEventSubAgentRetried, SchedulerEventSubAgentCompleted:
				events = append(events, event.Type)
			}
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []SchedulerEventType{
		SchedulerEventSubAgentStarted,
		SchedulerEventSubAgentRetried,
		SchedulerEventSubAgentStarted,
		SchedulerEventSubAgentCompleted,
	}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d, got=%v", len(events), len(want), events)
	}
	for idx := range want {
		if events[idx] != want[idx] {
			t.Fatalf("events[%d] = %q, want %q, all=%v", idx, events[idx], want[idx], events)
		}
	}
}

func TestSchedulerRunProgressEventDeduplicatedForRetryBackoff(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{
			ID:          "backoff",
			Content:     "wait retry window",
			Status:      agentsession.TodoStatusPending,
			RetryCount:  1,
			RetryLimit:  3,
			NextRetryAt: time.Now().Add(200 * time.Millisecond),
		},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep("unused"), nil
	})

	var (
		mu            sync.Mutex
		progressCount int
	)
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
		Observer: func(event SchedulerEvent) {
			if event.Type != SchedulerEventSubAgentProgress || event.TaskID != "backoff" || event.Reason != "retry_backoff" {
				return
			}
			mu.Lock()
			progressCount++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := scheduler.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want deadline exceeded", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if progressCount != 1 {
		t.Fatalf("progressCount = %d, want 1", progressCount)
	}
}

func TestSchedulerHandleOneOutcomeIgnoresStaleAttempt(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "stale", Content: "task"},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep("stale"), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	state := newSchedulerState(1)
	state.running["stale"] = runningTask{id: "stale", attempt: 2}
	state.outcomes <- taskOutcome{
		id:      "stale",
		attempt: 1,
		result: Result{
			State: StateSucceeded,
			Output: Output{
				Summary: "done",
			},
		},
	}
	summary := &ScheduleResult{Retried: map[string]int{}}
	if err := scheduler.handleOneOutcome(context.Background(), state, summary); err != nil {
		t.Fatalf("handleOneOutcome() error = %v", err)
	}
	running, ok := state.running["stale"]
	if !ok {
		t.Fatalf("running task removed by stale outcome")
	}
	if running.attempt != 2 {
		t.Fatalf("running attempt = %d, want 2", running.attempt)
	}
	item, _ := store.FindTodo("stale")
	if item.Status != agentsession.TodoStatusPending {
		t.Fatalf("todo status = %q, want pending", item.Status)
	}
	if err := store.ClaimTodo("stale", agentsession.TodoOwnerTypeSubAgent, "subagent-stale", item.Revision); err != nil {
		t.Fatalf("ClaimTodo(stale) error = %v", err)
	}

	state.outcomes <- taskOutcome{
		id:      "stale",
		attempt: 2,
		result: Result{
			State: StateSucceeded,
			Output: Output{
				Summary: "done",
			},
		},
	}
	if err := scheduler.handleOneOutcome(context.Background(), state, summary); err != nil {
		t.Fatalf("handleOneOutcome() for latest attempt error = %v", err)
	}
	item, _ = store.FindTodo("stale")
	if item.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("todo status = %q, want completed", item.Status)
	}
}

func TestSchedulerRunRevisionConflict(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "a", Content: "task-a"},
	})
	store.claimConflicts["a"] = 1
	store.updateConflicts["a"] = 1

	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = input
		_ = attempt
		return successStep(taskID), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(result.Succeeded, "a") {
		t.Fatalf("Succeeded = %v, want a", result.Succeeded)
	}
}

func TestSchedulerRunStopsOnDependencyDeadEnd(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "root", Content: "root", Status: agentsession.TodoStatusFailed},
		{ID: "child", Content: "child", Dependencies: []string{"root"}, Status: agentsession.TodoStatusPending},
	})
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep(taskID), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := scheduler.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(result.BlockedLeft, "child") {
		t.Fatalf("BlockedLeft = %v, want child", result.BlockedLeft)
	}
}

func TestSchedulerRunCancellationWriteback(t *testing.T) {
	t.Parallel()

	store := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "t1", Content: "task-1"},
	})
	started := make(chan struct{}, 1)
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = taskID
		_ = attempt
		_ = input
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return StepOutput{}, ctx.Err()
	})

	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 1,
		PollInterval:   2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, runErr := scheduler.Run(ctx)
		done <- runErr
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting task start")
	}
	cancel()

	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting scheduler stop")
	}

	item, _ := store.FindTodo("t1")
	if item.Status != agentsession.TodoStatusCanceled {
		t.Fatalf("todo status = %q, want canceled", item.Status)
	}
}

func TestSchedulerRunClaimErrorCancelsRunningTodo(t *testing.T) {
	t.Parallel()

	baseStore := newSchedulerStore(t, []agentsession.TodoItem{
		{ID: "a", Content: "task-a"},
		{ID: "b", Content: "task-b"},
	})
	store := &schedulerStoreWithClaimError{
		schedulerStore: baseStore,
		claimErrors: map[string]error{
			"b": errors.New("injected claim failure"),
		},
	}
	factory := newScriptedFactory(func(ctx context.Context, taskID string, attempt int, input StepInput) (StepOutput, error) {
		_ = ctx
		_ = taskID
		_ = attempt
		_ = input
		return successStep(taskID), nil
	})
	scheduler, err := NewScheduler(store, factory, SchedulerConfig{
		MaxConcurrency: 2,
		PollInterval:   2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	_, err = scheduler.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "claim todo") {
		t.Fatalf("Run() error = %v, want claim todo error", err)
	}

	item, ok := store.FindTodo("a")
	if !ok {
		t.Fatalf("FindTodo(a) not found")
	}
	if item.Status != agentsession.TodoStatusCanceled {
		t.Fatalf("todo a status = %q, want canceled", item.Status)
	}
}

type fakeFactory struct {
	create func(role Role) (WorkerRuntime, error)
}

func (f fakeFactory) Create(role Role) (WorkerRuntime, error) {
	return f.create(role)
}

type fakeWorker struct {
	startErr   error
	stepResult StepResult
	stepErr    error
	stepFunc   func(ctx context.Context) (StepResult, error)
	result     Result
	resultErr  error
	state      State
}

func (w *fakeWorker) Start(task Task, budget Budget, capability Capability) error {
	_ = task
	_ = budget
	_ = capability
	if w.startErr != nil {
		return w.startErr
	}
	w.state = StateRunning
	return nil
}

func (w *fakeWorker) Step(ctx context.Context) (StepResult, error) {
	if w.stepFunc != nil {
		return w.stepFunc(ctx)
	}
	_ = ctx
	return w.stepResult, w.stepErr
}

func (w *fakeWorker) Stop(reason StopReason) error {
	if reason == StopReasonCanceled {
		w.state = StateCanceled
	}
	return nil
}

func (w *fakeWorker) Result() (Result, error) {
	return w.result, w.resultErr
}

func (w *fakeWorker) State() State {
	return w.state
}

func (w *fakeWorker) Policy() RolePolicy {
	return RolePolicy{}
}

func TestExecuteTaskWithFactoryBranches(t *testing.T) {
	t.Parallel()

	t.Run("factory create failed", func(t *testing.T) {
		t.Parallel()
		_, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return nil, errors.New("create failed")
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "create failed") {
			t.Fatalf("executeTaskWithFactory() error = %v, want create failed", err)
		}
	})

	t.Run("start failed", func(t *testing.T) {
		t.Parallel()
		_, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{startErr: errors.New("start failed")}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "start failed") {
			t.Fatalf("executeTaskWithFactory() error = %v, want start failed", err)
		}
	})

	t.Run("step canceled with result fallback", func(t *testing.T) {
		t.Parallel()
		result, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepErr:   context.Canceled,
					resultErr: errors.New("no result"),
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if result.State != StateCanceled || result.StopReason != StopReasonCanceled {
			t.Fatalf("result = %+v, want canceled fallback", result)
		}
	})

	t.Run("step timeout with result fallback", func(t *testing.T) {
		t.Parallel()
		result, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepErr:   context.DeadlineExceeded,
					resultErr: errors.New("no result"),
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline exceeded", err)
		}
		if result.State != StateFailed || result.StopReason != StopReasonTimeout {
			t.Fatalf("result = %+v, want timeout fallback", result)
		}
	})

	t.Run("step failed with result", func(t *testing.T) {
		t.Parallel()
		result, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepErr: errors.New("boom"),
					result: Result{
						State: StateFailed,
						Error: "boom",
					},
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("error = %v, want boom", err)
		}
		if result.State != StateFailed {
			t.Fatalf("result state = %q, want failed", result.State)
		}
	})

	t.Run("done with failed state and no error message", func(t *testing.T) {
		t.Parallel()
		_, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepResult: StepResult{Done: true},
					result: Result{
						State:      StateFailed,
						StopReason: StopReasonError,
					},
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "state=failed") {
			t.Fatalf("error = %v, want fallback state error", err)
		}
	})

	t.Run("step error fallback without result", func(t *testing.T) {
		t.Parallel()
		result, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepErr:   errors.New("step failed"),
					resultErr: errors.New("result unavailable"),
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "step failed") {
			t.Fatalf("error = %v, want step failed", err)
		}
		if result.State != StateFailed || result.StopReason != StopReasonError {
			t.Fatalf("result = %+v, want failed fallback", result)
		}
	})

	t.Run("step retry then success", func(t *testing.T) {
		t.Parallel()
		result, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				calls := 0
				return &fakeWorker{
					stepFunc: func(ctx context.Context) (StepResult, error) {
						_ = ctx
						calls++
						if calls == 1 {
							return StepResult{Done: false}, nil
						}
						return StepResult{Done: true}, nil
					},
					result: Result{
						State:      StateSucceeded,
						StopReason: StopReasonCompleted,
					},
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if result.State != StateSucceeded {
			t.Fatalf("result state = %q, want succeeded", result.State)
		}
	})

	t.Run("done but result failed with explicit message", func(t *testing.T) {
		t.Parallel()
		_, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepResult: StepResult{Done: true},
					result: Result{
						State: StateFailed,
						Error: "explicit fail",
					},
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "explicit fail") {
			t.Fatalf("error = %v, want explicit fail", err)
		}
	})

	t.Run("done but result unavailable", func(t *testing.T) {
		t.Parallel()
		_, err := executeTaskWithFactory(context.Background(), fakeFactory{
			create: func(role Role) (WorkerRuntime, error) {
				_ = role
				return &fakeWorker{
					stepResult: StepResult{Done: true},
					resultErr:  errors.New("result unavailable"),
				}, nil
			},
		}, scheduleTaskInput{Role: RoleCoder, Task: Task{ID: "t", Goal: "g"}})
		if err == nil || !strings.Contains(err.Error(), "result unavailable") {
			t.Fatalf("error = %v, want result unavailable", err)
		}
	})
}

func TestSchedulerHelpersCoverage(t *testing.T) {
	t.Parallel()

	items := []agentsession.TodoItem{
		{ID: "a", Content: "a", Status: agentsession.TodoStatusCompleted},
		{ID: "b", Content: "b", Dependencies: []string{"a"}, Status: agentsession.TodoStatusPending},
	}
	byID := mapTodosByID(items)
	if !dependenciesCompleted(byID["b"], byID) {
		t.Fatalf("dependenciesCompleted should be true")
	}

	waited := effectivePriority(agentsession.TodoItem{Priority: 1}, time.Now().Add(-20*time.Second), time.Now(), 5*time.Second)
	if waited <= 1 {
		t.Fatalf("effectivePriority should include aging boost, got %d", waited)
	}

	list := []string{"a"}
	appendUniqueString(&list, "a")
	appendUniqueString(&list, "b")
	if len(list) != 2 || list[1] != "b" {
		t.Fatalf("appendUniqueString unexpected result: %v", list)
	}

	if !isRevisionConflict(fmt.Errorf("%w: x", agentsession.ErrRevisionConflict)) {
		t.Fatalf("isRevisionConflict should match wrapped error")
	}
	if isRevisionConflict(errors.New("other")) {
		t.Fatalf("isRevisionConflict should reject unrelated errors")
	}

	if err := waitWithContext(context.Background(), 0); err != nil {
		t.Fatalf("waitWithContext(0) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitWithContext(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitWithContext canceled error = %v", err)
	}

	left := collectBlockedLeft([]string{"a", "b", "c"}, []agentsession.TodoItem{
		{ID: "a", Content: "a", Status: agentsession.TodoStatusCompleted},
		{ID: "b", Content: "b", Status: agentsession.TodoStatusBlocked},
	}, map[string]runningTask{
		"c": {id: "c"},
	})
	if len(left) != 2 || left[0] != "b" || left[1] != "c" {
		t.Fatalf("collectBlockedLeft() = %v, want [b c]", left)
	}

	outcome := taskOutcome{err: errors.New(" boom ")}
	if got := resolveTaskFailureReason(outcome); got != "boom" {
		t.Fatalf("resolveTaskFailureReason(err) = %q, want boom", got)
	}
	outcome = taskOutcome{result: Result{Error: " failed "}}
	if got := resolveTaskFailureReason(outcome); got != "failed" {
		t.Fatalf("resolveTaskFailureReason(result) = %q, want failed", got)
	}
	if got := resolveTaskFailureReason(taskOutcome{}); got == "" {
		t.Fatalf("resolveTaskFailureReason() should return fallback")
	}

	if got := defaultRetryBackoff(0); got != 0 {
		t.Fatalf("defaultRetryBackoff(0) = %v, want 0", got)
	}
	bounded := defaultRetryBackoffWithBounds(time.Second, 8*time.Second)
	if got := bounded(1); got != time.Second {
		t.Fatalf("bounded(1) = %v, want 1s", got)
	}
	if got := bounded(2); got != 2*time.Second {
		t.Fatalf("bounded(2) = %v, want 2s", got)
	}
	if got := bounded(4); got != 8*time.Second {
		t.Fatalf("bounded(4) = %v, want 8s", got)
	}
	if got := bounded(8); got != 8*time.Second {
		t.Fatalf("bounded(8) = %v, want capped 8s", got)
	}
	cfg := (SchedulerConfig{MaxRetries: -1}).normalize()
	if cfg.MaxRetries != 0 {
		t.Fatalf("normalize MaxRetries = %d, want 0", cfg.MaxRetries)
	}
	if _, err := buildTaskGraph([]agentsession.TodoItem{{ID: " ", Content: "bad"}}); err == nil {
		t.Fatalf("buildTaskGraph should reject empty id")
	}
}

func hasEvent(events []SchedulerEvent, eventType SchedulerEventType, taskID string) bool {
	for _, event := range events {
		if event.Type == eventType && event.TaskID == taskID {
			return true
		}
	}
	return false
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
