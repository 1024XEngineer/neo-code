package todo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type stubMutator struct {
	session *agentsession.Session
}

func (m *stubMutator) ListTodos() []agentsession.TodoItem {
	return m.session.ListTodos()
}

func (m *stubMutator) FindTodo(id string) (agentsession.TodoItem, bool) {
	return m.session.FindTodo(id)
}

func (m *stubMutator) ReplaceTodos(items []agentsession.TodoItem) error {
	return m.session.ReplaceTodos(items)
}

func (m *stubMutator) AddTodo(item agentsession.TodoItem) error {
	return m.session.AddTodo(item)
}

func (m *stubMutator) UpdateTodo(id string, patch agentsession.TodoPatch, expectedRevision int64) error {
	return m.session.UpdateTodo(id, patch, expectedRevision)
}

func (m *stubMutator) SetTodoStatus(id string, status agentsession.TodoStatus, expectedRevision int64) error {
	return m.session.SetTodoStatus(id, status, expectedRevision)
}

func (m *stubMutator) DeleteTodo(id string, expectedRevision int64) error {
	return m.session.DeleteTodo(id, expectedRevision)
}

func (m *stubMutator) ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error {
	return m.session.ClaimTodo(id, ownerType, ownerID, expectedRevision)
}

func (m *stubMutator) CompleteTodo(id string, artifacts []string, expectedRevision int64) error {
	return m.session.CompleteTodo(id, artifacts, expectedRevision)
}

func (m *stubMutator) FailTodo(id string, reason string, expectedRevision int64) error {
	return m.session.FailTodo(id, reason, expectedRevision)
}

func TestToolExecute(t *testing.T) {
	t.Parallel()

	newSessionMutator := func(t *testing.T) *stubMutator {
		t.Helper()
		session := agentsession.New("todo-tool")
		if err := session.AddTodo(agentsession.TodoItem{ID: "base", Content: "base", Status: agentsession.TodoStatusCompleted}); err != nil {
			t.Fatalf("AddTodo(base) error = %v", err)
		}
		if err := session.AddTodo(agentsession.TodoItem{ID: "task", Content: "task", Dependencies: []string{"base"}}); err != nil {
			t.Fatalf("AddTodo(task) error = %v", err)
		}
		return &stubMutator{session: &session}
	}

	tests := []struct {
		name        string
		raw         []byte
		withMutator bool
		wantErr     bool
		want        string
	}{
		{
			name:        "missing mutator",
			raw:         []byte(`{"action":"add","item":{"id":"a","content":"x"}}`),
			withMutator: false,
			wantErr:     true,
			want:        reasonInvalidArguments,
		},
		{
			name:        "invalid json",
			raw:         []byte(`{`),
			withMutator: true,
			wantErr:     true,
			want:        reasonInvalidArguments,
		},
		{
			name:        "add success",
			raw:         []byte(`{"action":"add","item":{"id":"a","content":"implement"}}`),
			withMutator: true,
			want:        "action: add",
		},
		{
			name:        "update success",
			raw:         []byte(`{"action":"update","id":"task","expected_revision":1,"patch":{"content":"task v2"}}`),
			withMutator: true,
			want:        "action: update",
		},
		{
			name:        "set status success",
			raw:         []byte(`{"action":"set_status","id":"task","status":"in_progress","expected_revision":1}`),
			withMutator: true,
			want:        "action: set_status",
		},
		{
			name:        "set status accepts numeric id and alias",
			raw:         []byte(`{"action":"set_status","id":123,"status":"In-Progress"}`),
			withMutator: true,
			wantErr:     true,
			want:        reasonTodoNotFound,
		},
		{
			name:        "revision conflict",
			raw:         []byte(`{"action":"set_status","id":"task","status":"in_progress","expected_revision":9}`),
			withMutator: true,
			wantErr:     true,
			want:        reasonRevisionConflict,
		},
		{
			name:        "unsupported action",
			raw:         []byte(`{"action":"noop"}`),
			withMutator: true,
			wantErr:     true,
			want:        reasonInvalidAction,
		},
		{
			name:        "remove success",
			raw:         []byte(`{"action":"remove","id":"task","expected_revision":1}`),
			withMutator: true,
			want:        "action: remove",
		},
		{
			name:        "remove revision conflict",
			raw:         []byte(`{"action":"remove","id":"task","expected_revision":2}`),
			withMutator: true,
			wantErr:     true,
			want:        reasonRevisionConflict,
		},
		{
			name:        "plan success",
			raw:         []byte(`{"action":"plan","items":[{"id":"n1","content":"next"}]}`),
			withMutator: true,
			want:        "action: plan",
		},
		{
			name:        "plan rejects empty items",
			raw:         []byte(`{"action":"plan","items":[]}`),
			withMutator: true,
			wantErr:     true,
			want:        reasonInvalidArguments,
		},
	}

	tool := New()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := tools.ToolCallInput{
				Name:      tools.ToolNameTodoWrite,
				Arguments: tt.raw,
			}
			if tt.withMutator {
				input.SessionMutator = newSessionMutator(t)
			}

			result, err := tool.Execute(context.Background(), input)
			if tt.wantErr && err == nil {
				t.Fatalf("Execute() expected error, got nil result=%+v", result)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Execute() unexpected error = %v, result=%+v", err, result)
			}
			if !strings.Contains(result.Content, tt.want) {
				t.Fatalf("Execute() content = %q, want contains %q", result.Content, tt.want)
			}
		})
	}
}

func TestToolExecuteRevisionConflictMetadata(t *testing.T) {
	t.Parallel()

	session := agentsession.New("revision-meta")
	if err := session.AddTodo(agentsession.TodoItem{ID: "task", Content: "task"}); err != nil {
		t.Fatalf("AddTodo error = %v", err)
	}
	mutator := &stubMutator{session: &session}

	tool := New()
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		Arguments:      []byte(`{"action":"set_status","id":"task","status":"in_progress","expected_revision":9}`),
		SessionMutator: mutator,
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if result.Metadata["reason_code"] != reasonRevisionConflict {
		t.Fatalf("reason_code = %q, want %q", result.Metadata["reason_code"], reasonRevisionConflict)
	}
	if result.Metadata["current_revision"] == nil {
		t.Fatalf("expected current_revision in metadata, got nil")
	}
	if result.Metadata["current_revision"].(int64) != 1 {
		t.Fatalf("current_revision = %v, want 1", result.Metadata["current_revision"])
	}
	if result.Metadata["current_status"] != string(agentsession.TodoStatusPending) {
		t.Fatalf("current_status = %q, want %q", result.Metadata["current_status"], agentsession.TodoStatusPending)
	}
}

func TestToolMetadataMethods(t *testing.T) {
	t.Parallel()

	tool := New()
	if tool.Name() != tools.ToolNameTodoWrite {
		t.Fatalf("Name() = %q, want %q", tool.Name(), tools.ToolNameTodoWrite)
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatalf("Description() should not be empty")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyCompact {
		t.Fatalf("MicroCompactPolicy() should be compact")
	}
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Fatalf("Schema() type = %+v", schema["type"])
	}
	if _, ok := schema["oneOf"].([]any); !ok {
		t.Fatalf("Schema() oneOf should exist for action-specific requirements")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() properties should be map, got %T", schema["properties"])
	}
	if _, ok := properties["item"]; !ok {
		t.Fatalf("Schema() should include item property")
	}
	if _, ok := properties["items"]; !ok {
		t.Fatalf("Schema() should include items property")
	}
	patch, ok := properties["patch"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() patch should be object, got %T", properties["patch"])
	}
	patchProps, ok := patch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() patch.properties should be object, got %T", patch["properties"])
	}
	patchExecutor, ok := patchProps["executor"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() patch.executor should be object, got %T", patchProps["executor"])
	}
	enumValues, ok := patchExecutor["enum"].([]string)
	if !ok {
		t.Fatalf("Schema() patch.executor.enum should be []string, got %T", patchExecutor["enum"])
	}
	if len(enumValues) != 2 || enumValues[0] != "agent" || enumValues[1] != "subagent" {
		t.Fatalf("Schema() patch.executor.enum = %v, want [agent subagent]", enumValues)
	}
	artifacts, ok := properties["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() artifacts should be object, got %T", properties["artifacts"])
	}
	if artifacts["type"] != "array" {
		t.Fatalf("Schema() artifacts.type = %+v, want array", artifacts["type"])
	}
	items, ok := artifacts["items"].(map[string]any)
	if !ok {
		t.Fatalf("Schema() artifacts.items should be object, got %T", artifacts["items"])
	}
	if items["type"] != "string" {
		t.Fatalf("Schema() artifacts.items.type = %+v, want string", items["type"])
	}
}

func TestToolExecuteActionSequence(t *testing.T) {
	t.Parallel()

	session := agentsession.New("todo-seq")
	mutator := &stubMutator{session: &session}
	tool := New()

	// plan
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments: []byte(`{
			"action":"plan",
			"items":[
				{"id":"base","content":"base","status":"completed"},
				{"id":"task","content":"task","dependencies":["base"]}
			]
		}`),
	})
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}

	// claim
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"claim","id":"task","owner_type":"subagent","owner_id":"w1","expected_revision":1}`),
	})
	if err != nil {
		t.Fatalf("claim error = %v", err)
	}
	if !strings.Contains(result.Content, "action: claim") {
		t.Fatalf("unexpected claim content: %q", result.Content)
	}

	// complete
	result, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"complete","id":"task","artifacts":["done.md"],"expected_revision":2}`),
	})
	if err != nil {
		t.Fatalf("complete error = %v", err)
	}
	if !strings.Contains(result.Content, "action: complete") {
		t.Fatalf("unexpected complete content: %q", result.Content)
	}

	// add + set_status + fail
	_, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"add","item":{"id":"task2","content":"task2"}}`),
	})
	if err != nil {
		t.Fatalf("add task2 error = %v", err)
	}
	_, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"set_status","id":"task2","status":"in_progress","expected_revision":1}`),
	})
	if err != nil {
		t.Fatalf("set_status task2 error = %v", err)
	}
	result, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"fail","id":"task2","reason":"build failed","expected_revision":2}`),
	})
	if err != nil {
		t.Fatalf("fail task2 error = %v", err)
	}
	if !strings.Contains(result.Content, "action: fail") {
		t.Fatalf("unexpected fail content: %q", result.Content)
	}
}

func TestToolExecuteReasonMapping(t *testing.T) {
	t.Parallel()

	tool := New()
	newMutator := func(payload string) tools.ToolCallInput {
		session := agentsession.New("todo-reason")
		mutator := &stubMutator{session: &session}
		return tools.ToolCallInput{
			Name:           tools.ToolNameTodoWrite,
			SessionMutator: mutator,
			Arguments:      []byte(payload),
		}
	}

	tests := []struct {
		name       string
		input      tools.ToolCallInput
		prepare    func(m *stubMutator) error
		wantReason string
	}{
		{
			name:       "todo not found",
			input:      newMutator(`{"action":"remove","id":"missing"}`),
			wantReason: reasonTodoNotFound,
		},
		{
			name:       "invalid arguments from dispatch",
			input:      newMutator(`{"action":"update","id":"a"}`),
			wantReason: reasonInvalidArguments,
		},
		{
			name:  "invalid transition",
			input: newMutator(`{"action":"complete","id":"a","expected_revision":1}`),
			prepare: func(m *stubMutator) error {
				return m.AddTodo(agentsession.TodoItem{ID: "a", Content: "a", Status: agentsession.TodoStatusBlocked})
			},
			wantReason: reasonInvalidTransition,
		},
		{
			name:  "dependency violation",
			input: newMutator(`{"action":"set_status","id":"b","status":"in_progress","expected_revision":1}`),
			prepare: func(m *stubMutator) error {
				if err := m.AddTodo(agentsession.TodoItem{ID: "a", Content: "a"}); err != nil {
					return err
				}
				return m.AddTodo(agentsession.TodoItem{ID: "b", Content: "b", Dependencies: []string{"a"}})
			},
			wantReason: reasonDependencyViolation,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mutator, _ := tt.input.SessionMutator.(*stubMutator)
			if tt.prepare != nil {
				if err := tt.prepare(mutator); err != nil {
					t.Fatalf("prepare error = %v", err)
				}
			}
			result, err := tool.Execute(context.Background(), tt.input)
			if err == nil {
				t.Fatalf("expected error result")
			}
			gotReason, _ := result.Metadata["reason_code"].(string)
			if gotReason != tt.wantReason {
				t.Fatalf("reason_code = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

func TestParseInput(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"action":" ADD ","id":"  a  ","executor":" SubAgent ","owner_type":" SubAgent ","owner_id":" worker "}`)
	input, err := parseInput(raw)
	if err != nil {
		t.Fatalf("parseInput() error = %v", err)
	}
	if input.Action != "add" || input.ID != "a" || input.Executor != "SubAgent" ||
		input.OwnerType != "SubAgent" || input.OwnerID != "worker" {
		t.Fatalf("parseInput() got %+v", input)
	}

	_, err = parseInput([]byte(`{"action":"add","item":{"id":"task-1","title":"legacy-title"}}`))
	if err == nil || !strings.Contains(err.Error(), "item.content") {
		t.Fatalf("parseInput() expected migration error for item.title, err=%v", err)
	}

	_, err = parseInput([]byte(`{"action":"plan","items":[{"id":"task-1","title":"legacy-1"},{"id":"task-2","content":"new-2"}]}`))
	if err == nil || !strings.Contains(err.Error(), "items[0].content") {
		t.Fatalf("parseInput() expected migration error for items[].title, err=%v", err)
	}

	_, err = parseInput([]byte(`{`))
	if err == nil {
		t.Fatalf("parseInput() expected error for invalid json")
	}

	tooLong := strings.Repeat("x", maxTodoWriteTextLen+1)
	_, err = parseInput([]byte(`{"action":"add","item":{"id":"a","content":"` + tooLong + `"}}`))
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("parseInput() expected invalid arguments for too long content, err=%v", err)
	}

	items := make([]string, maxTodoWriteItems+1)
	for idx := range items {
		items[idx] = `{"id":"t` + string(rune('a'+(idx%26))) + `","content":"ok"}`
	}
	_, err = parseInput([]byte(`{"action":"plan","items":[` + strings.Join(items, ",") + `]}`))
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("parseInput() expected invalid arguments for too many items, err=%v", err)
	}

	tooManyDeps := make([]string, maxTodoWriteListItems+1)
	for idx := range tooManyDeps {
		tooManyDeps[idx] = `"dep"`
	}
	_, err = parseInput([]byte(`{"action":"add","item":{"id":"a","content":"x","dependencies":[` + strings.Join(tooManyDeps, ",") + `]}}`))
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("parseInput() expected invalid arguments for too many dependencies, err=%v", err)
	}

	_, err = parseInput([]byte(`{"action":"remove","id":"a","expected_revision":-1}`))
	if err == nil || !strings.Contains(err.Error(), "expected_revision must be >= 0") {
		t.Fatalf("parseInput() expected invalid arguments for negative expected_revision, err=%v", err)
	}

	tooLongExecutor := strings.Repeat("x", maxTodoWriteTextLen+1)
	_, err = parseInput([]byte(`{"action":"update","id":"a","patch":{"executor":"` + tooLongExecutor + `"}}`))
	if err == nil || !strings.Contains(err.Error(), "patch.executor exceeds max length") {
		t.Fatalf("parseInput() expected invalid arguments for too long patch.executor, err=%v", err)
	}
}

func TestTodoPatchInputToSessionPatch(t *testing.T) {
	t.Parallel()

	content := "new content"
	status := agentsession.TodoStatusInProgress
	dependencies := []string{"a"}
	priority := 2
	executor := agentsession.TodoExecutorSubAgent
	ownerType := agentsession.TodoOwnerTypeSubAgent
	ownerID := "worker-1"
	acceptance := []string{"done"}
	artifacts := []string{"out.txt"}
	reason := "failed"

	input := &todoPatchInput{
		Content:       &content,
		Status:        &status,
		Dependencies:  &dependencies,
		Priority:      &priority,
		Executor:      &executor,
		OwnerType:     &ownerType,
		OwnerID:       &ownerID,
		Acceptance:    &acceptance,
		Artifacts:     &artifacts,
		FailureReason: &reason,
	}
	patch := input.toSessionPatch()

	encoded, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch error = %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("expected non-empty patch json")
	}

	emptyPatch := (*todoPatchInput)(nil).toSessionPatch()
	if encoded, err := json.Marshal(emptyPatch); err != nil || len(encoded) == 0 {
		t.Fatalf("nil patch conversion should still be serializable, err=%v", err)
	}
}

func TestDispatchValidationErrors(t *testing.T) {
	t.Parallel()

	session := agentsession.New("dispatch-errors")
	if err := session.AddTodo(agentsession.TodoItem{ID: "a", Content: "a"}); err != nil {
		t.Fatalf("AddTodo(a) error = %v", err)
	}
	call := tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: &stubMutator{session: &session},
	}
	tool := New()

	tests := []struct {
		name string
		in   writeInput
		want string
	}{
		{name: "add without item", in: writeInput{Action: actionAdd}, want: "requires item"},
		{name: "update without id", in: writeInput{Action: actionUpdate, Patch: &todoPatchInput{}}, want: "requires id and patch"},
		{name: "update without patch", in: writeInput{Action: actionUpdate, ID: "a"}, want: "requires id and patch"},
		{name: "set_status without id", in: writeInput{Action: actionSetStatus, Status: agentsession.TodoStatusPending}, want: "requires id"},
		{name: "set_status invalid status", in: writeInput{Action: actionSetStatus, ID: "a", Status: "paused"}, want: "requires valid status"},
		{name: "remove without id", in: writeInput{Action: actionRemove}, want: "requires id"},
		{name: "plan without items", in: writeInput{Action: actionPlan}, want: "requires items"},
		{name: "claim without id", in: writeInput{Action: actionClaim, OwnerType: "subagent", OwnerID: "w1"}, want: "requires id"},
		{name: "claim without owner", in: writeInput{Action: actionClaim, ID: "a"}, want: "requires owner_type and owner_id"},
		{name: "complete without id", in: writeInput{Action: actionComplete}, want: "requires id"},
		{name: "fail without id", in: writeInput{Action: actionFail}, want: "requires id"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tool.dispatch(call, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("dispatch() error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestCommonHelpersCoverage(t *testing.T) {
	t.Parallel()

	items := []agentsession.TodoItem{
		{
			ID:           "b",
			Content:      "task b",
			Status:       agentsession.TodoStatusPending,
			Priority:     1,
			Revision:     1,
			Executor:     agentsession.TodoExecutorSubAgent,
			Dependencies: []string{"a"},
		},
		{
			ID:        "a",
			Content:   "task a",
			Status:    agentsession.TodoStatusInProgress,
			Priority:  5,
			Revision:  2,
			Executor:  agentsession.TodoExecutorSubAgent,
			OwnerType: agentsession.TodoOwnerTypeSubAgent,
			OwnerID:   "worker-1",
		},
	}

	rendered := renderTodos("plan", items)
	if !strings.Contains(rendered, "- [in_progress] a") || !strings.Contains(rendered, "- [pending] b") {
		t.Fatalf("renderTodos() missing expected todos content: %q", rendered)
	}
	if !strings.Contains(rendered, "executor=subagent") {
		t.Fatalf("renderTodos() should include executor, got %q", rendered)
	}
	if !strings.Contains(renderTodos("plan", nil), "count: 0") {
		t.Fatalf("renderTodos(nil) should include count 0")
	}
	// 覆盖 renderTodos 的排序分支：同优先级按状态、再按 ID 排序。
	sorted := renderTodos("plan", []agentsession.TodoItem{
		{ID: "z", Content: "z", Status: agentsession.TodoStatusPending, Priority: 1, Revision: 1},
		{ID: "b", Content: "b", Status: agentsession.TodoStatusBlocked, Priority: 1, Revision: 1},
		{ID: "a", Content: "a", Status: agentsession.TodoStatusBlocked, Priority: 1, Revision: 1},
	})
	idxA := strings.Index(sorted, " a ")
	idxB := strings.Index(sorted, " b ")
	idxZ := strings.Index(sorted, " z ")
	if !(idxA < idxB && idxB < idxZ) {
		t.Fatalf("renderTodos() sort order unexpected: %q", sorted)
	}

	if got := mapReason(errors.New("other custom error")); got != "other custom error" {
		t.Fatalf("mapReason fallback = %q", got)
	}
	if got := mapReason(errors.New("unsupported action \"noop\"")); got != reasonInvalidAction {
		t.Fatalf("mapReason unsupported action = %q", got)
	}

	errResult := errorResult("x_reason", "x_details", map[string]any{"k": "v"})
	if errResult.Metadata["reason_code"] != "x_reason" || errResult.Metadata["k"] != "v" {
		t.Fatalf("errorResult metadata unexpected: %+v", errResult.Metadata)
	}
}

func TestTodoWriteCompleteErgonomicsAndMetadata(t *testing.T) {
	t.Parallel()

	tool := New()
	session := agentsession.New("todo-complete-ergonomics")
	mutator := &stubMutator{session: &session}
	required := true
	if err := mutator.AddTodo(agentsession.TodoItem{
		ID:       "todo-1",
		Content:  "finish feature",
		Required: &required,
		Status:   agentsession.TodoStatusPending,
	}); err != nil {
		t.Fatalf("AddTodo(todo-1) error = %v", err)
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"complete","id":"todo-1","expected_revision":1}`),
	})
	if err != nil {
		t.Fatalf("complete pending error = %v", err)
	}
	if autoClaimed, _ := result.Metadata["auto_claimed"].(bool); !autoClaimed {
		t.Fatalf("auto_claimed = %#v, want true", result.Metadata["auto_claimed"])
	}
	path, ok := result.Metadata["transition_path"].([]string)
	if !ok || len(path) != 3 || path[0] != "pending" || path[1] != "in_progress" || path[2] != "completed" {
		t.Fatalf("transition_path = %#v, want [pending in_progress completed]", result.Metadata["transition_path"])
	}
	if stateFact, _ := result.Metadata["state_fact"].(string); stateFact != "todo_completed" {
		t.Fatalf("state_fact = %q, want %q", stateFact, "todo_completed")
	}
	snapshot, ok := result.Metadata["todo_snapshot"].([]map[string]any)
	if !ok || len(snapshot) == 0 {
		t.Fatalf("todo_snapshot metadata unexpected: %#v", result.Metadata["todo_snapshot"])
	}
	if _, ok := result.Metadata["todo_summary"].(map[string]any); !ok {
		t.Fatalf("todo_summary metadata missing: %#v", result.Metadata["todo_summary"])
	}

	result, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"complete","id":"todo-1","expected_revision":3}`),
	})
	if err != nil {
		t.Fatalf("complete completed error = %v", err)
	}
	if noOp, _ := result.Metadata["no_op"].(bool); !noOp {
		t.Fatalf("no_op = %#v, want true", result.Metadata["no_op"])
	}
	if reasonCode, _ := result.Metadata["reason_code"].(string); reasonCode != reasonTerminalNoop {
		t.Fatalf("reason_code = %q, want %q", reasonCode, reasonTerminalNoop)
	}
}

func TestTodoWriteFailSetsTerminalFailureMetadata(t *testing.T) {
	t.Parallel()

	tool := New()
	session := agentsession.New("todo-fail-metadata")
	mutator := &stubMutator{session: &session}
	required := true
	if err := mutator.AddTodo(agentsession.TodoItem{
		ID:       "todo-fail",
		Content:  "unreachable target",
		Required: &required,
		Status:   agentsession.TodoStatusInProgress,
	}); err != nil {
		t.Fatalf("AddTodo(todo-fail) error = %v", err)
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameTodoWrite,
		SessionMutator: mutator,
		Arguments:      []byte(`{"action":"fail","id":"todo-fail","reason":"dependency missing","expected_revision":1}`),
	})
	if err != nil {
		t.Fatalf("fail todo error = %v", err)
	}
	if terminalFailure, _ := result.Metadata["terminal_failure"].(bool); !terminalFailure {
		t.Fatalf("terminal_failure = %#v, want true", result.Metadata["terminal_failure"])
	}
	if doNotRetry, _ := result.Metadata["do_not_retry"].(bool); !doNotRetry {
		t.Fatalf("do_not_retry = %#v, want true", result.Metadata["do_not_retry"])
	}
	if stateFact, _ := result.Metadata["state_fact"].(string); stateFact != "todo_failed" {
		t.Fatalf("state_fact = %q, want %q", stateFact, "todo_failed")
	}
}
