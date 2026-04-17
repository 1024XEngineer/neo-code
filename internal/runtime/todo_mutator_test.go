package runtime

import (
	"context"
	"errors"
	"slices"
	"testing"

	agentsession "neo-code/internal/session"
)

type mutatorStore struct {
	last agentsession.Session
	err  error
}

func (s *mutatorStore) CreateSession(ctx context.Context, input agentsession.CreateSessionInput) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	if s.err != nil {
		return agentsession.Session{}, s.err
	}
	session := agentsession.NewWithWorkdir(input.Title, input.Workdir)
	if input.ID != "" {
		session.ID = input.ID
	}
	s.last = cloneSessionForPersistence(session)
	return cloneSessionForPersistence(session), nil
}

func (s *mutatorStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	if s.last.ID == id {
		return cloneSessionForPersistence(s.last), nil
	}
	return agentsession.Session{}, errors.New("not found")
}

func (s *mutatorStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *mutatorStore) AppendMessages(ctx context.Context, input agentsession.AppendMessagesInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	if s.last.ID != input.SessionID {
		return errors.New("not found")
	}
	s.last.Messages = append(s.last.Messages, cloneMessagesForPersistence(input.Messages)...)
	s.last.UpdatedAt = input.UpdatedAt
	s.last.Provider = input.Provider
	s.last.Model = input.Model
	s.last.Workdir = input.Workdir
	s.last.TokenInputTotal += input.TokenInputDelta
	s.last.TokenOutputTotal += input.TokenOutputDelta
	return nil
}

func (s *mutatorStore) UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	if s.last.ID != "" && s.last.ID != input.SessionID {
		return errors.New("not found")
	}
	s.last.ID = input.SessionID
	s.last.Title = input.Title
	s.last.UpdatedAt = input.UpdatedAt
	s.last.Provider = input.Provider
	s.last.Model = input.Model
	s.last.Workdir = input.Workdir
	s.last.TaskState = input.TaskState.Clone()
	s.last.ActivatedSkills = agentsessionCloneSkillActivations(input.ActivatedSkills)
	s.last.Todos = cloneTodosForPersistence(input.Todos)
	s.last.TokenInputTotal = input.TokenInputTotal
	s.last.TokenOutputTotal = input.TokenOutputTotal
	return nil
}

func (s *mutatorStore) ReplaceTranscript(ctx context.Context, input agentsession.ReplaceTranscriptInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	if s.last.ID != "" && s.last.ID != input.SessionID {
		return errors.New("not found")
	}
	s.last.ID = input.SessionID
	s.last.Messages = cloneMessagesForPersistence(input.Messages)
	s.last.UpdatedAt = input.UpdatedAt
	s.last.Provider = input.Provider
	s.last.Model = input.Model
	s.last.Workdir = input.Workdir
	s.last.TaskState = input.TaskState.Clone()
	s.last.ActivatedSkills = agentsessionCloneSkillActivations(input.ActivatedSkills)
	s.last.Todos = cloneTodosForPersistence(input.Todos)
	s.last.TokenInputTotal = input.TokenInputTotal
	s.last.TokenOutputTotal = input.TokenOutputTotal
	return nil
}

func TestRuntimeSessionMutatorMutateAndSave(t *testing.T) {
	t.Parallel()

	store := &mutatorStore{}
	service := &Service{sessionStore: store}
	session := agentsession.New("todo-mutator")
	state := newRunState("run-1", session)

	mutator := newRuntimeSessionMutator(context.Background(), service, &state)
	if mutator == nil {
		t.Fatalf("expected mutator instance")
	}

	if err := mutator.AddTodo(agentsession.TodoItem{ID: "a", Content: "task"}); err != nil {
		t.Fatalf("AddTodo() error = %v", err)
	}
	if len(state.session.Todos) != 1 || state.session.Todos[0].ID != "a" {
		t.Fatalf("unexpected state todos: %+v", state.session.Todos)
	}
	if len(store.last.Todos) != 1 || store.last.Todos[0].ID != "a" {
		t.Fatalf("unexpected persisted todos: %+v", store.last.Todos)
	}
}

func TestRuntimeSessionMutatorCanceledContext(t *testing.T) {
	t.Parallel()

	store := &mutatorStore{}
	service := &Service{sessionStore: store}
	session := agentsession.New("todo-mutator-cancel")
	state := newRunState("run-2", session)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mutator := newRuntimeSessionMutator(ctx, service, &state)
	if err := mutator.AddTodo(agentsession.TodoItem{ID: "a", Content: "task"}); err == nil {
		t.Fatalf("expected canceled context error")
	}
}

func TestRuntimeSessionMutatorNilInputs(t *testing.T) {
	t.Parallel()

	if mutator := newRuntimeSessionMutator(context.Background(), nil, nil); mutator != nil {
		t.Fatalf("expected nil mutator when dependencies are nil")
	}
}

func TestRuntimeSessionMutatorMethods(t *testing.T) {
	t.Parallel()

	store := &mutatorStore{}
	service := &Service{sessionStore: store}
	session := agentsession.New("todo-mutator-methods")
	state := newRunState("run-methods", session)
	mutator := newRuntimeSessionMutator(context.Background(), service, &state)
	if mutator == nil {
		t.Fatalf("expected mutator instance")
	}

	if todos := mutator.ListTodos(); todos != nil {
		t.Fatalf("ListTodos() = %+v, want nil for empty session", todos)
	}
	if _, ok := mutator.FindTodo("missing"); ok {
		t.Fatalf("FindTodo(missing) expected false")
	}

	items := []agentsession.TodoItem{
		{ID: "a", Content: "task a", Status: agentsession.TodoStatusPending, Priority: 2},
		{ID: "b", Content: "task b", Status: agentsession.TodoStatusPending, Priority: 1, Dependencies: []string{"a"}},
	}
	if err := mutator.ReplaceTodos(items); err != nil {
		t.Fatalf("ReplaceTodos() error = %v", err)
	}

	snapshot := mutator.ListTodos()
	if len(snapshot) != 2 {
		t.Fatalf("ListTodos() len = %d, want 2", len(snapshot))
	}
	snapshot[0].Content = "mutated"
	again := mutator.ListTodos()
	if again[0].Content == "mutated" {
		t.Fatalf("ListTodos() should return deep copy")
	}

	found, ok := mutator.FindTodo("a")
	if !ok || found.ID != "a" {
		t.Fatalf("FindTodo(a) = (%+v, %v), want found", found, ok)
	}
	revA := found.Revision

	updatedContent := "task a updated"
	updatedPriority := 4
	patch := agentsession.TodoPatch{
		Content:  &updatedContent,
		Priority: &updatedPriority,
	}
	if err := mutator.UpdateTodo("a", patch, revA); err != nil {
		t.Fatalf("UpdateTodo() error = %v", err)
	}
	afterUpdate, ok := mutator.FindTodo("a")
	if !ok {
		t.Fatalf("FindTodo(a) after update expected true")
	}
	if afterUpdate.Content != updatedContent || afterUpdate.Priority != updatedPriority {
		t.Fatalf("UpdateTodo() not applied, got %+v", afterUpdate)
	}
	if afterUpdate.Revision <= revA {
		t.Fatalf("revision should increase, before=%d after=%d", revA, afterUpdate.Revision)
	}

	if err := mutator.SetTodoStatus("a", agentsession.TodoStatusInProgress, afterUpdate.Revision); err != nil {
		t.Fatalf("SetTodoStatus(in_progress) error = %v", err)
	}
	inProgress, _ := mutator.FindTodo("a")
	if inProgress.Status != agentsession.TodoStatusInProgress {
		t.Fatalf("status = %q, want in_progress", inProgress.Status)
	}

	if err := mutator.CompleteTodo("a", []string{"artifact.log"}, inProgress.Revision); err != nil {
		t.Fatalf("CompleteTodo() error = %v", err)
	}
	completed, _ := mutator.FindTodo("a")
	if completed.Status != agentsession.TodoStatusCompleted {
		t.Fatalf("status = %q, want completed", completed.Status)
	}
	if !slices.Equal(completed.Artifacts, []string{"artifact.log"}) {
		t.Fatalf("artifacts = %+v, want [artifact.log]", completed.Artifacts)
	}

	if err := mutator.AddTodo(agentsession.TodoItem{ID: "c", Content: "task c"}); err != nil {
		t.Fatalf("AddTodo(c) error = %v", err)
	}
	cTodo, _ := mutator.FindTodo("c")
	if err := mutator.ClaimTodo("c", agentsession.TodoOwnerTypeSubAgent, "sub-1", cTodo.Revision); err != nil {
		t.Fatalf("ClaimTodo() error = %v", err)
	}
	claimed, _ := mutator.FindTodo("c")
	if claimed.OwnerType != agentsession.TodoOwnerTypeSubAgent || claimed.OwnerID != "sub-1" {
		t.Fatalf("owner = %s/%s, want subagent/sub-1", claimed.OwnerType, claimed.OwnerID)
	}
	if err := mutator.FailTodo("c", "failed reason", claimed.Revision); err != nil {
		t.Fatalf("FailTodo() error = %v", err)
	}
	failed, _ := mutator.FindTodo("c")
	if failed.Status != agentsession.TodoStatusFailed || failed.FailureReason != "failed reason" {
		t.Fatalf("FailTodo() not applied, got %+v", failed)
	}

	if err := mutator.DeleteTodo("c", failed.Revision); err != nil {
		t.Fatalf("DeleteTodo(c) error = %v", err)
	}
	if _, ok := mutator.FindTodo("c"); ok {
		t.Fatalf("DeleteTodo(c) should remove todo")
	}
}

func TestRuntimeSessionMutatorErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("unavailable mutator", func(t *testing.T) {
		t.Parallel()
		m := &runtimeSessionMutator{}
		if err := m.AddTodo(agentsession.TodoItem{ID: "a", Content: "task"}); err == nil {
			t.Fatalf("expected unavailable mutator error")
		}
		if todos := m.ListTodos(); todos != nil {
			t.Fatalf("ListTodos() = %+v, want nil", todos)
		}
		if _, ok := m.FindTodo("a"); ok {
			t.Fatalf("FindTodo() expected false on unavailable mutator")
		}
	})

	t.Run("save error", func(t *testing.T) {
		t.Parallel()
		store := &mutatorStore{err: errors.New("disk failed")}
		service := &Service{sessionStore: store}
		session := agentsession.New("todo-mutator-save-error")
		state := newRunState("run-save-error", session)
		mutator := newRuntimeSessionMutator(context.Background(), service, &state)
		if mutator == nil {
			t.Fatalf("expected mutator instance")
		}
		err := mutator.AddTodo(agentsession.TodoItem{ID: "a", Content: "task"})
		if err == nil || err.Error() != "disk failed" {
			t.Fatalf("AddTodo() err = %v, want disk failed", err)
		}
		if len(state.session.Todos) != 0 {
			t.Fatalf("state should remain unchanged on save error, got %+v", state.session.Todos)
		}
	})

	t.Run("mutation error", func(t *testing.T) {
		t.Parallel()
		store := &mutatorStore{}
		service := &Service{sessionStore: store}
		session := agentsession.New("todo-mutator-mutate-error")
		state := newRunState("run-mutate-error", session)
		mutator := newRuntimeSessionMutator(context.Background(), service, &state)
		if mutator == nil {
			t.Fatalf("expected mutator instance")
		}
		if err := mutator.AddTodo(agentsession.TodoItem{ID: "a", Content: "task"}); err != nil {
			t.Fatalf("seed AddTodo() error = %v", err)
		}
		err := mutator.SetTodoStatus("a", agentsession.TodoStatusInProgress, 99)
		if err == nil || !errors.Is(err, agentsession.ErrRevisionConflict) {
			t.Fatalf("SetTodoStatus() err = %v, want revision conflict", err)
		}
	})
}
