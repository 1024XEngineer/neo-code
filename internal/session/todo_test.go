package session

import (
	"errors"
	"strings"
	"testing"
	"time"
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
		ID:            "task",
		Content:       "task",
		Dependencies:  []string{"base"},
		FailureReason: "previous failure",
		RetryCount:    2,
		NextRetryAt:   time.Now().Add(2 * time.Minute),
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
	if task.FailureReason != "" || !task.NextRetryAt.IsZero() {
		t.Fatalf("claim should clear failure/next_retry_at, got %+v", task)
	}
	retryCount := 5
	if err := session.UpdateTodo("task", TodoPatch{RetryCount: &retryCount}, task.Revision); err != nil {
		t.Fatalf("UpdateTodo(retry_count) error = %v", err)
	}
	task, _ = session.FindTodo("task")
	if task.RetryCount != 5 {
		t.Fatalf("retry_count = %d, want 5", task.RetryCount)
	}

	if err := session.FailTodo("task", "compile failed", task.Revision); err != nil {
		t.Fatalf("FailTodo error = %v", err)
	}
	task, _ = session.FindTodo("task")
	if task.Status != TodoStatusFailed || task.FailureReason != "compile failed" {
		t.Fatalf("unexpected fail state: %+v", task)
	}
	if !task.NextRetryAt.IsZero() {
		t.Fatalf("FailTodo should clear next_retry_at, got %+v", task)
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

	if err := session.SetTodoStatus("b", TodoStatusInProgress, 0); err != nil {
		t.Fatalf("SetTodoStatus(b,in_progress,0) error = %v", err)
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

	normalized, err := normalizeTodoItem(TodoItem{
		ID:            "n1",
		Content:       "content",
		Status:        TodoStatusBlocked,
		FailureReason: " retry failed ",
		RetryCount:    -3,
		RetryLimit:    -2,
	})
	if err != nil {
		t.Fatalf("normalizeTodoItem error = %v", err)
	}
	if normalized.FailureReason != "retry failed" {
		t.Fatalf("blocked todo should keep failure reason, got %q", normalized.FailureReason)
	}
	if normalized.RetryCount != 0 || normalized.RetryLimit != 0 {
		t.Fatalf("negative retry fields should be normalized to 0, got count=%d limit=%d", normalized.RetryCount, normalized.RetryLimit)
	}

	normalizedDefaultExecutor, err := normalizeTodoItem(TodoItem{
		ID:        "missing-executor",
		Content:   "legacy payload",
		OwnerType: TodoOwnerTypeSubAgent,
	})
	if err != nil {
		t.Fatalf("expected missing executor to default to agent, got %v", err)
	}
	if normalizedDefaultExecutor.Executor != TodoExecutorAgent {
		t.Fatalf("default executor = %q, want %q", normalizedDefaultExecutor.Executor, TodoExecutorAgent)
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
	retryCount := 1
	retryLimit := 3
	nextRetryAt := time.Now().Add(3 * time.Minute).UTC()
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
		RetryCount:    &retryCount,
		RetryLimit:    &retryLimit,
		NextRetryAt:   &nextRetryAt,
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
	if next.RetryCount != 1 || next.RetryLimit != 3 {
		t.Fatalf("retry fields not applied, got %+v", next)
	}
	if !next.NextRetryAt.IsZero() {
		t.Fatalf("in_progress should clear next_retry_at, got %+v", next)
	}

	blocked := TodoStatusBlocked
	blockedReason := "retry later"
	blockedPatch := TodoPatch{
		Status:        &blocked,
		FailureReason: &blockedReason,
	}
	blockedNext, err := applyTodoPatch(base, blockedPatch)
	if err != nil {
		t.Fatalf("applyTodoPatch(blocked) error = %v", err)
	}
	if blockedNext.FailureReason != blockedReason {
		t.Fatalf("blocked status should preserve failure reason, got %q", blockedNext.FailureReason)
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

func TestTodoExecutorNormalizationAndValidation(t *testing.T) {
	t.Parallel()

	session := New("todo-executor")
	if err := session.AddTodo(TodoItem{
		ID:       "task-1",
		Content:  "run with subagent",
		Executor: " SubAgent ",
	}); err != nil {
		t.Fatalf("AddTodo(task-1) error = %v", err)
	}
	item, ok := session.FindTodo("task-1")
	if !ok {
		t.Fatalf("FindTodo(task-1) not found")
	}
	if item.Executor != TodoExecutorSubAgent {
		t.Fatalf("executor = %q, want %q", item.Executor, TodoExecutorSubAgent)
	}

	if err := session.AddTodo(TodoItem{
		ID:       "task-invalid",
		Content:  "invalid executor",
		Executor: "robot",
	}); err == nil || !strings.Contains(err.Error(), "invalid todo executor") {
		t.Fatalf("AddTodo(task-invalid) error = %v, want invalid executor", err)
	}
}

func TestSessionUpdateTodoExecutorPatch(t *testing.T) {
	t.Parallel()

	session := New("todo-executor-patch")
	if err := session.AddTodo(TodoItem{
		ID:      "task-1",
		Content: "run with agent by default",
	}); err != nil {
		t.Fatalf("AddTodo(task-1) error = %v", err)
	}
	item, ok := session.FindTodo("task-1")
	if !ok {
		t.Fatalf("FindTodo(task-1) not found")
	}
	if item.Executor != TodoExecutorAgent {
		t.Fatalf("default executor = %q, want %q", item.Executor, TodoExecutorAgent)
	}

	executor := "subagent"
	if err := session.UpdateTodo("task-1", TodoPatch{
		Executor: &executor,
	}, item.Revision); err != nil {
		t.Fatalf("UpdateTodo(task-1) error = %v", err)
	}
	updated, ok := session.FindTodo("task-1")
	if !ok {
		t.Fatalf("FindTodo(task-1) not found after update")
	}
	if updated.Executor != TodoExecutorSubAgent {
		t.Fatalf("executor = %q, want %q", updated.Executor, TodoExecutorSubAgent)
	}
}

func TestTodoSupersedesAndContentChecksValidation(t *testing.T) {
	t.Parallel()

	// ContentChecks normalize: 空 artifact 或空 contains 被过滤
	item, err := normalizeTodoItem(TodoItem{
		ID:      "cc",
		Content: "content checks test",
		ContentChecks: []TodoContentCheck{
			{Artifact: "  ", Contains: []string{"x"}},
			{Artifact: "a.go", Contains: []string{"  ", "token"}},
			{Artifact: "b.go", Contains: []string{"t1", "t1", "t2"}},
		},
	})
	if err != nil {
		t.Fatalf("normalizeTodoItem error = %v", err)
	}
	if len(item.ContentChecks) != 2 {
		t.Fatalf("expected 2 content checks after normalize, got %d", len(item.ContentChecks))
	}
	if item.ContentChecks[0].Artifact != "a.go" || len(item.ContentChecks[0].Contains) != 1 || item.ContentChecks[0].Contains[0] != "token" {
		t.Fatalf("unexpected first check: %+v", item.ContentChecks[0])
	}
	if item.ContentChecks[1].Artifact != "b.go" || len(item.ContentChecks[1].Contains) != 2 {
		t.Fatalf("unexpected second check: %+v", item.ContentChecks[1])
	}

	// Clone 深拷贝 ContentChecks 和 Supersedes
	original := TodoItem{
		ID:            "clone",
		Content:       "clone test",
		Supersedes:    []string{"a", "b"},
		ContentChecks: []TodoContentCheck{{Artifact: "x.go", Contains: []string{"ok"}}},
	}
	cloned := original.Clone()
	cloned.Supersedes[0] = "mutated"
	cloned.ContentChecks[0].Contains[0] = "mutated"
	if original.Supersedes[0] != "a" || original.ContentChecks[0].Contains[0] != "ok" {
		t.Fatalf("Clone should deep copy Supersedes and ContentChecks")
	}

	// normalizeAndValidateTodos 校验 Supersedes 存在性
	session := New("supersedes-validation")
	if err := session.AddTodo(TodoItem{ID: "real", Content: "real"}); err != nil {
		t.Fatalf("AddTodo(real) error = %v", err)
	}
	if err := session.AddTodo(TodoItem{ID: "ghost", Content: "ghost", Supersedes: []string{"missing"}}); err == nil || !strings.Contains(err.Error(), "unknown superseded todo") {
		t.Fatalf("expected unknown superseded todo error, got %v", err)
	}

	// Supersedes 自引用校验
	if err := session.AddTodo(TodoItem{ID: "self", Content: "self", Supersedes: []string{"self"}}); err == nil || !strings.Contains(err.Error(), "cannot supersede itself") {
		t.Fatalf("expected self-supersede error, got %v", err)
	}

	// Patch 覆盖 Supersedes 和 ContentChecks
	if err := session.AddTodo(TodoItem{ID: "patch-target", Content: "patch", Supersedes: []string{"real"}}); err != nil {
		t.Fatalf("AddTodo(patch-target) error = %v", err)
	}
	newSupersedes := []string{"real"}
	newChecks := []TodoContentCheck{{Artifact: "main.go", Contains: []string{"func main"}}}
	if err := session.UpdateTodo("patch-target", TodoPatch{
		Supersedes:    &newSupersedes,
		ContentChecks: &newChecks,
	}, 1); err != nil {
		t.Fatalf("UpdateTodo error = %v", err)
	}
	patched, _ := session.FindTodo("patch-target")
	if len(patched.Supersedes) != 1 || patched.Supersedes[0] != "real" || len(patched.ContentChecks) != 1 {
		t.Fatalf("unexpected patched state: %+v", patched)
	}

	// required canceled todo 必须有 replacement（通过 Supersedes 声明）
	if err := session.ReplaceTodos([]TodoItem{
		{ID: "orig", Content: "orig", Status: TodoStatusCanceled, Required: boolPtr(true)},
		{ID: "repl", Content: "repl", Required: boolPtr(true), Supersedes: []string{"orig"}},
	}); err != nil {
		t.Fatalf("ReplaceTodos with valid replacement error = %v", err)
	}
	if err := session.ReplaceTodos([]TodoItem{
		{ID: "orphan", Content: "orphan", Status: TodoStatusCanceled, Required: boolPtr(true)},
	}); err == nil || !strings.Contains(err.Error(), "must declare an explicit replacement") {
		t.Fatalf("expected replacement required error, got %v", err)
	}
}

func TestReplaceTodosRejectsEmpty(t *testing.T) {
	t.Parallel()

	session := New("empty-replace")
	if err := session.AddTodo(TodoItem{ID: "a", Content: "task a"}); err != nil {
		t.Fatalf("AddTodo: %v", err)
	}

	if err := session.ReplaceTodos([]TodoItem{}); err == nil {
		t.Fatalf("ReplaceTodos([]) should fail")
	}
	if err := session.ReplaceTodos(nil); err == nil {
		t.Fatalf("ReplaceTodos(nil) should fail")
	}

	// 原有 todo 不应被清掉。
	got := session.ListTodos()
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("ReplaceTodos rejection should not mutate session, got %+v", got)
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestBlockedReasonInvariantOnLifecycle(t *testing.T) {
	t.Parallel()

	session := New("blocked-reason-lifecycle")
	if err := session.AddTodo(TodoItem{
		ID:            "t1",
		Content:       "task",
		Status:        TodoStatusBlocked,
		BlockedReason: TodoBlockedReasonExternalResourceWait,
	}); err != nil {
		t.Fatalf("AddTodo: %v", err)
	}

	got, _ := session.FindTodo("t1")
	if got.BlockedReason != TodoBlockedReasonExternalResourceWait {
		t.Fatalf("blocked todo should keep reason, got %q", got.BlockedReason)
	}

	if err := session.ClaimTodo("t1", TodoOwnerTypeAgent, "agent-1", got.Revision); err != nil {
		t.Fatalf("ClaimTodo: %v", err)
	}
	got, _ = session.FindTodo("t1")
	if got.Status != TodoStatusInProgress {
		t.Fatalf("ClaimTodo status = %q, want in_progress", got.Status)
	}
	if got.BlockedReason != "" {
		t.Fatalf("ClaimTodo should clear blocked_reason, got %q", got.BlockedReason)
	}

	if err := session.CompleteTodo("t1", []string{"output.txt"}, got.Revision); err != nil {
		t.Fatalf("CompleteTodo: %v", err)
	}
	got, _ = session.FindTodo("t1")
	if got.Status != TodoStatusCompleted || got.BlockedReason != "" {
		t.Fatalf("CompleteTodo state = %+v", got)
	}

	// FailTodo also clears blocked_reason
	if err := session.AddTodo(TodoItem{
		ID:            "t2",
		Content:       "task2",
		Status:        TodoStatusBlocked,
		BlockedReason: TodoBlockedReasonPermissionWait,
	}); err != nil {
		t.Fatalf("AddTodo t2: %v", err)
	}
	got2, _ := session.FindTodo("t2")
	if err := session.FailTodo("t2", "boom", got2.Revision); err != nil {
		t.Fatalf("FailTodo: %v", err)
	}
	got2, _ = session.FindTodo("t2")
	if got2.Status != TodoStatusFailed || got2.BlockedReason != "" || got2.FailureReason != "boom" {
		t.Fatalf("FailTodo state = %+v", got2)
	}
}

func TestNormalizeTodoItemBlockedReasonInvariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     TodoStatus
		input      TodoBlockedReason
		wantReason TodoBlockedReason
		wantErr    bool
	}{
		{name: "blocked keeps real reason", status: TodoStatusBlocked, input: TodoBlockedReasonInternalDependency, wantReason: TodoBlockedReasonInternalDependency},
		{name: "blocked keeps unknown enum value", status: TodoStatusBlocked, input: TodoBlockedReasonUnknown, wantReason: TodoBlockedReasonUnknown},
		{name: "blocked allows empty", status: TodoStatusBlocked, input: "", wantReason: ""},
		{name: "pending clears any value", status: TodoStatusPending, input: TodoBlockedReasonUnknown, wantReason: ""},
		{name: "in_progress clears any value", status: TodoStatusInProgress, input: TodoBlockedReasonExternalResourceWait, wantReason: ""},
		{name: "completed clears any value", status: TodoStatusCompleted, input: TodoBlockedReasonInternalDependency, wantReason: ""},
		{name: "failed clears any value", status: TodoStatusFailed, input: TodoBlockedReasonUnknown, wantReason: ""},
		{name: "canceled clears any value", status: TodoStatusCanceled, input: TodoBlockedReasonUnknown, wantReason: ""},
		{name: "invalid enum returns error", status: TodoStatusBlocked, input: TodoBlockedReason("garbage"), wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			normalized, err := normalizeTodoItem(TodoItem{
				ID:            "x",
				Content:       "x",
				Status:        tc.status,
				BlockedReason: tc.input,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; normalized=%+v", normalized)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if normalized.BlockedReason != tc.wantReason {
				t.Fatalf("blocked_reason = %q, want %q", normalized.BlockedReason, tc.wantReason)
			}
		})
	}
}
