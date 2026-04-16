package session

import (
	"errors"
	"strings"
	"testing"
)

func TestTodoStatusValidAndTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from TodoStatus
		to   TodoStatus
		ok   bool
	}{
		{name: "pending to in_progress", from: TodoStatusPending, to: TodoStatusInProgress, ok: true},
		{name: "in_progress to completed", from: TodoStatusInProgress, to: TodoStatusCompleted, ok: true},
		{name: "blocked to canceled", from: TodoStatusBlocked, to: TodoStatusCanceled, ok: true},
		{name: "completed to pending denied", from: TodoStatusCompleted, to: TodoStatusPending, ok: false},
		{name: "failed to in_progress denied", from: TodoStatusFailed, to: TodoStatusInProgress, ok: false},
		{name: "canceled to completed denied", from: TodoStatusCanceled, to: TodoStatusCompleted, ok: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.from.ValidTransition(tt.to); got != tt.ok {
				t.Fatalf("ValidTransition(%q,%q)=%v want %v", tt.from, tt.to, got, tt.ok)
			}
		})
	}
}

func TestSessionAddFindDeleteTodo(t *testing.T) {
	t.Parallel()

	session := New("todo")
	if err := session.AddTodo(TodoItem{ID: "base", Content: "base"}); err != nil {
		t.Fatalf("AddTodo(base) error = %v", err)
	}
	if err := session.AddTodo(TodoItem{ID: "child", Content: "child", Dependencies: []string{"base"}}); err != nil {
		t.Fatalf("AddTodo(child) error = %v", err)
	}

	found, ok := session.FindTodo("child")
	if !ok {
		t.Fatalf("FindTodo(child) not found")
	}
	if found.Revision != 1 {
		t.Fatalf("revision = %d, want 1", found.Revision)
	}

	if err := session.DeleteTodo("base", 1); err == nil || !errors.Is(err, ErrDependencyViolation) {
		t.Fatalf("DeleteTodo(base) error = %v, want dependency violation", err)
	}
	if err := session.DeleteTodo("child", found.Revision); err != nil {
		t.Fatalf("DeleteTodo(child) error = %v", err)
	}
}

func TestSessionDeleteTodoRevisionConflict(t *testing.T) {
	t.Parallel()

	session := New("todo-delete-revision")
	if err := session.AddTodo(TodoItem{ID: "a", Content: "a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}
	if err := session.DeleteTodo("a", 2); err == nil || !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("DeleteTodo(a,2) error = %v, want revision conflict", err)
	}
	if err := session.DeleteTodo("a", 1); err != nil {
		t.Fatalf("DeleteTodo(a,1) error = %v", err)
	}
}

func TestSessionUpdateTodoRevisionAndTransition(t *testing.T) {
	t.Parallel()

	session := New("revision")
	if err := session.AddTodo(TodoItem{ID: "a", Content: "task a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}

	patch := TodoPatch{}
	content := "task a v2"
	patch.Content = &content
	if err := session.UpdateTodo("a", patch, 2); err == nil || !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("UpdateTodo expected revision conflict, got %v", err)
	}

	if err := session.SetTodoStatus("a", TodoStatusInProgress, 1); err != nil {
		t.Fatalf("SetTodoStatus in_progress error = %v", err)
	}
	item, ok := session.FindTodo("a")
	if !ok {
		t.Fatalf("FindTodo(a) not found")
	}
	if item.Revision != 2 {
		t.Fatalf("revision = %d, want 2", item.Revision)
	}

	if err := session.SetTodoStatus("a", TodoStatusPending, item.Revision); err == nil || !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("SetTodoStatus reverse transition error = %v, want invalid transition", err)
	}
}

func TestSessionDependencyRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []TodoItem
		want  string
	}{
		{
			name: "self dependency",
			items: []TodoItem{
				{ID: "a", Content: "task", Dependencies: []string{"a"}},
			},
			want: "cannot depend on itself",
		},
		{
			name: "unknown dependency",
			items: []TodoItem{
				{ID: "a", Content: "task", Dependencies: []string{"missing"}},
			},
			want: "unknown dependency",
		},
		{
			name: "cycle dependency",
			items: []TodoItem{
				{ID: "a", Content: "a", Dependencies: []string{"b"}},
				{ID: "b", Content: "b", Dependencies: []string{"c"}},
				{ID: "c", Content: "c", Dependencies: []string{"a"}},
			},
			want: ErrCyclicDependency.Error(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := New("deps")
			err := session.ReplaceTodos(tt.items)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ReplaceTodos error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestSessionDependencyGateOnStatus(t *testing.T) {
	t.Parallel()

	session := New("status-deps")
	if err := session.AddTodo(TodoItem{ID: "base", Content: "base"}); err != nil {
		t.Fatalf("AddTodo(base) error = %v", err)
	}
	if err := session.AddTodo(TodoItem{ID: "child", Content: "child", Dependencies: []string{"base"}}); err != nil {
		t.Fatalf("AddTodo(child) error = %v", err)
	}

	if err := session.SetTodoStatus("child", TodoStatusInProgress, 1); err == nil || !errors.Is(err, ErrDependencyViolation) {
		t.Fatalf("SetTodoStatus(child,in_progress) error = %v, want dependency violation", err)
	}

	if err := session.SetTodoStatus("base", TodoStatusInProgress, 1); err != nil {
		t.Fatalf("SetTodoStatus(base,in_progress) error = %v", err)
	}
	if err := session.SetTodoStatus("base", TodoStatusCompleted, 2); err != nil {
		t.Fatalf("SetTodoStatus(base,completed) error = %v", err)
	}
	if err := session.SetTodoStatus("child", TodoStatusInProgress, 1); err != nil {
		t.Fatalf("SetTodoStatus(child,in_progress) error = %v", err)
	}
}

func TestSessionClaimCompleteFail(t *testing.T) {
	t.Parallel()

	session := New("claim-complete-fail")
	if err := session.AddTodo(TodoItem{
		ID:         "base",
		Content:    "base",
		Status:     TodoStatusCompleted,
		Revision:   1,
		OwnerType:  TodoOwnerTypeAgent,
		Acceptance: []string{"done"},
	}); err != nil {
		t.Fatalf("AddTodo(base) error = %v", err)
	}
	if err := session.AddTodo(TodoItem{
		ID:           "task",
		Content:      "task",
		Dependencies: []string{"base"},
	}); err != nil {
		t.Fatalf("AddTodo(task) error = %v", err)
	}

	if err := session.ClaimTodo("task", TodoOwnerTypeSubAgent, "worker-1", 1); err != nil {
		t.Fatalf("ClaimTodo error = %v", err)
	}
	task, _ := session.FindTodo("task")
	if task.OwnerType != TodoOwnerTypeSubAgent || task.OwnerID != "worker-1" {
		t.Fatalf("unexpected owner after claim: %+v", task)
	}

	if err := session.FailTodo("task", "compile failed", task.Revision); err != nil {
		t.Fatalf("FailTodo error = %v", err)
	}
	task, _ = session.FindTodo("task")
	if task.Status != TodoStatusFailed || task.FailureReason != "compile failed" {
		t.Fatalf("unexpected fail state: %+v", task)
	}

	if err := session.SetTodoStatus("task", TodoStatusInProgress, task.Revision); err == nil || !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition error = %v, want invalid transition", err)
	}
}

func TestTodoVersionLifecycle(t *testing.T) {
	t.Parallel()

	session := New("todo-version")
	if session.TodoVersion != 0 {
		t.Fatalf("initial todo_version = %d, want 0", session.TodoVersion)
	}
	if err := session.AddTodo(TodoItem{ID: "a", Content: "a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}
	if session.TodoVersion != CurrentTodoVersion {
		t.Fatalf("todo_version = %d, want %d", session.TodoVersion, CurrentTodoVersion)
	}
}

func TestTodoHelpersAndCollections(t *testing.T) {
	t.Parallel()

	if !TodoStatusCompleted.IsTerminal() || !TodoStatusFailed.IsTerminal() || !TodoStatusCanceled.IsTerminal() {
		t.Fatalf("expected completed/failed/canceled as terminal")
	}
	if TodoStatusPending.IsTerminal() || TodoStatusInProgress.IsTerminal() || TodoStatusBlocked.IsTerminal() {
		t.Fatalf("expected pending/in_progress/blocked as non-terminal")
	}

	session := New("helpers")
	if err := session.AddTodo(TodoItem{ID: "a", Content: "a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}

	list := session.ListTodos()
	if len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("ListTodos unexpected result: %+v", list)
	}
	list[0].ID = "mutated"
	item, ok := session.FindTodo("a")
	if !ok || item.ID != "a" {
		t.Fatalf("FindTodo should return clone, got %+v ok=%v", item, ok)
	}

	_, err := session.GetTodoByID("missing")
	if err == nil || !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("GetTodoByID(missing) error = %v, want not found", err)
	}
}

func TestSessionReplaceTodosAndUpdateTodoStatusCompatibility(t *testing.T) {
	t.Parallel()

	session := New("replace")
	items := []TodoItem{
		{ID: "a", Content: "base", Status: TodoStatusCompleted},
		{ID: "b", Content: "child", Dependencies: []string{"a"}},
	}
	if err := session.ReplaceTodos(items); err != nil {
		t.Fatalf("ReplaceTodos error = %v", err)
	}
	if len(session.Todos) != 2 || session.TodoVersion != CurrentTodoVersion {
		t.Fatalf("unexpected session after replace: %+v", session)
	}

	if err := session.UpdateTodoStatus("b", TodoStatusInProgress); err != nil {
		t.Fatalf("UpdateTodoStatus(b,in_progress) error = %v", err)
	}
	b, _ := session.FindTodo("b")
	if b.Status != TodoStatusInProgress {
		t.Fatalf("expected in_progress, got %+v", b)
	}
}

func TestSessionCompleteTodoPath(t *testing.T) {
	t.Parallel()

	session := New("complete")
	if err := session.AddTodo(TodoItem{ID: "a", Content: "a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}
	if err := session.SetTodoStatus("a", TodoStatusInProgress, 1); err != nil {
		t.Fatalf("SetTodoStatus(a,in_progress) error = %v", err)
	}
	if err := session.CompleteTodo("a", []string{"out.txt"}, 2); err != nil {
		t.Fatalf("CompleteTodo(a) error = %v", err)
	}
	item, _ := session.FindTodo("a")
	if item.Status != TodoStatusCompleted || len(item.Artifacts) != 1 || item.Artifacts[0] != "out.txt" {
		t.Fatalf("unexpected complete state: %+v", item)
	}
}

func TestSessionNilReceiverErrors(t *testing.T) {
	t.Parallel()

	var session *Session
	if err := session.ReplaceTodos(nil); err == nil {
		t.Fatalf("ReplaceTodos nil receiver should fail")
	}
	if err := session.AddTodo(TodoItem{}); err == nil {
		t.Fatalf("AddTodo nil receiver should fail")
	}
	if err := session.UpdateTodo("a", TodoPatch{}, 0); err == nil {
		t.Fatalf("UpdateTodo nil receiver should fail")
	}
	if err := session.DeleteTodo("a", 0); err == nil {
		t.Fatalf("DeleteTodo nil receiver should fail")
	}
}

func TestTodoInternalHelpers(t *testing.T) {
	t.Parallel()

	if _, err := ensureTodoID(" "); err == nil {
		t.Fatalf("ensureTodoID should reject blank id")
	}
	if err := ensureTodoRevision(TodoItem{ID: "a", Revision: 2}, 1); err == nil || !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("ensureTodoRevision expected conflict, got %v", err)
	}
	if err := ensureTodoRevision(TodoItem{ID: "a", Revision: 2}, 2); err != nil {
		t.Fatalf("ensureTodoRevision expected nil, got %v", err)
	}
	if !isValidTodoOwnerType(TodoOwnerTypeUser) || !isValidTodoOwnerType(TodoOwnerTypeAgent) || !isValidTodoOwnerType(TodoOwnerTypeSubAgent) {
		t.Fatalf("expected valid owner types")
	}
	if isValidTodoOwnerType("robot") {
		t.Fatalf("unexpected valid owner type")
	}
	if normalizeTodoOwnerType(" SubAgent ") != TodoOwnerTypeSubAgent {
		t.Fatalf("normalizeTodoOwnerType unexpected value")
	}
	if got := normalizeTodoTextList([]string{" a ", "", "a", "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalizeTodoTextList unexpected result: %+v", got)
	}
	if got := normalizeTodoDependencies([]string{" a ", "a", "b"}); len(got) != 2 {
		t.Fatalf("normalizeTodoDependencies unexpected result: %+v", got)
	}
}

func TestApplyTodoPatchCoverage(t *testing.T) {
	t.Parallel()

	base := TodoItem{
		ID:           "a",
		Content:      "content",
		Status:       TodoStatusPending,
		Dependencies: []string{"x"},
		Priority:     1,
		OwnerType:    TodoOwnerTypeUser,
		OwnerID:      "u1",
		Acceptance:   []string{"acc"},
		Artifacts:    []string{"old"},
		Revision:     1,
	}
	content := "  content2 "
	deps := []string{"d1", "d1", "d2"}
	priority := 3
	ownerType := "subagent"
	ownerID := "w1"
	acceptance := []string{"ok"}
	artifacts := []string{"a.txt"}
	reason := "boom"
	status := TodoStatusInProgress
	patch := TodoPatch{
		Content:       &content,
		Dependencies:  &deps,
		Priority:      &priority,
		OwnerType:     &ownerType,
		OwnerID:       &ownerID,
		Acceptance:    &acceptance,
		Artifacts:     &artifacts,
		FailureReason: &reason,
		Status:        &status,
	}

	next, err := applyTodoPatch(base, patch)
	if err != nil {
		t.Fatalf("applyTodoPatch error = %v", err)
	}
	if next.Content != "content2" || next.OwnerType != TodoOwnerTypeSubAgent || next.OwnerID != "w1" || next.Priority != 3 {
		t.Fatalf("applyTodoPatch unexpected normalized fields: %+v", next)
	}
	if next.Status != TodoStatusInProgress || len(next.Dependencies) != 2 {
		t.Fatalf("applyTodoPatch unexpected status/deps: %+v", next)
	}
	if next.FailureReason != "" {
		t.Fatalf("non-failed status should clear failure reason, got %q", next.FailureReason)
	}

	invalidStatus := TodoStatus("paused")
	if _, err := applyTodoPatch(base, TodoPatch{Status: &invalidStatus}); err == nil {
		t.Fatalf("invalid status should fail")
	}
	completed := TodoStatusCompleted
	if _, err := applyTodoPatch(TodoItem{ID: "t", Content: "c", Status: TodoStatusCompleted, Revision: 1}, TodoPatch{Status: &completed}); err != nil {
		t.Fatalf("same terminal status should be allowed, got %v", err)
	}
	if _, err := applyTodoPatch(
		TodoItem{ID: "t2", Content: "c", Status: TodoStatusCompleted, Revision: 1},
		TodoPatch{Status: &status},
	); err == nil || !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition should fail with invalid transition, got %v", err)
	}
}
