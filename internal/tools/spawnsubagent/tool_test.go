package spawnsubagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

type stubMutator struct {
	session *agentsession.Session
}

type failingAddMutator struct {
	*stubMutator
	err error
}

type stubSubAgentInvoker struct {
	result tools.SubAgentRunResult
	err    error
	last   tools.SubAgentRunInput
}

func (i *stubSubAgentInvoker) Run(ctx context.Context, input tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
	if err := ctx.Err(); err != nil {
		return tools.SubAgentRunResult{}, err
	}
	i.last = input
	return i.result, i.err
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

func (m *failingAddMutator) AddTodo(item agentsession.TodoItem) error {
	if m.err != nil {
		return m.err
	}
	return m.stubMutator.AddTodo(item)
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

func TestToolMetadata(t *testing.T) {
	t.Parallel()

	tool := New()
	if tool.Name() != tools.ToolNameSpawnSubAgent {
		t.Fatalf("Name() = %q, want %q", tool.Name(), tools.ToolNameSpawnSubAgent)
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatalf("Description() should not be empty")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyCompact {
		t.Fatalf("MicroCompactPolicy() = %q, want compact", tool.MicroCompactPolicy())
	}
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Fatalf("Schema().type = %v, want object", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema().properties type = %T, want map[string]any", schema["properties"])
	}
	if _, ok := properties["items"]; !ok {
		t.Fatalf("Schema() should include items")
	}
}

func TestToolExecuteCreatesSubAgentTodos(t *testing.T) {
	t.Parallel()

	session := agentsession.New("spawn-subagent")
	mutator := &stubMutator{session: &session}
	tool := New()

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameSpawnSubAgent,
		SessionMutator: mutator,
		Arguments: []byte(`{
			"items":[
				{"id":"t2","content":"write tests","dependencies":["t1"],"priority":2},
				{"id":"t1","content":"create calculator module","priority":3}
			]
		}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Content, "created_count: 2") {
		t.Fatalf("Execute() content = %q, want created_count", result.Content)
	}
	t1, ok := mutator.FindTodo("t1")
	if !ok {
		t.Fatalf("todo t1 should exist")
	}
	if t1.Executor != agentsession.TodoExecutorSubAgent {
		t.Fatalf("t1 executor = %q, want %q", t1.Executor, agentsession.TodoExecutorSubAgent)
	}
	if t1.Status != agentsession.TodoStatusPending {
		t.Fatalf("t1 status = %q, want pending", t1.Status)
	}

	t2, ok := mutator.FindTodo("t2")
	if !ok {
		t.Fatalf("todo t2 should exist")
	}
	if len(t2.Dependencies) != 1 || t2.Dependencies[0] != "t1" {
		t.Fatalf("t2 dependencies = %v, want [t1]", t2.Dependencies)
	}
}

func TestToolExecuteValidatesInputs(t *testing.T) {
	t.Parallel()

	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tools.ToolNameSpawnSubAgent,
		Arguments: []byte(`{"items":[{"id":"t1","content":"x"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "session mutator is unavailable") {
		t.Fatalf("missing mutator error = %v", err)
	}

	session := agentsession.New("spawn-subagent-errors")
	mutator := &stubMutator{session: &session}

	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "unknown dependency",
			payload: `{"items":[{"id":"t2","content":"x","dependencies":["missing"]}]}`,
			wantErr: "unknown dependency",
		},
		{
			name:    "duplicate ids",
			payload: `{"items":[{"id":"t1","content":"x"},{"id":"t1","content":"y"}]}`,
			wantErr: "duplicate todo id",
		},
		{
			name:    "self dependency",
			payload: `{"items":[{"id":"t1","content":"x","dependencies":["t1"]}]}`,
			wantErr: "cannot depend on itself",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:           tools.ToolNameSpawnSubAgent,
				SessionMutator: mutator,
				Arguments:      []byte(tt.payload),
			})
			if execErr == nil || !strings.Contains(execErr.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, want contains %q", execErr, tt.wantErr)
			}
		})
	}
}

func TestParseSpawnInputAndHelpers(t *testing.T) {
	t.Parallel()

	input, err := parseSpawnInput([]byte(`{"items":[{"id":" t1 ","content":" c1 ","dependencies":["dep","dep"," "],"acceptance":[" ok ","ok"]}]}`))
	if err != nil {
		t.Fatalf("parseSpawnInput() error = %v", err)
	}
	if len(input.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(input.Items))
	}
	item := input.Items[0]
	if item.ID != "t1" || item.Content != "c1" {
		t.Fatalf("normalized item = %+v", item)
	}
	if len(item.Dependencies) != 1 || item.Dependencies[0] != "dep" {
		t.Fatalf("dependencies = %v, want [dep]", item.Dependencies)
	}
	if len(item.Acceptance) != 1 || item.Acceptance[0] != "ok" {
		t.Fatalf("acceptance = %v, want [ok]", item.Acceptance)
	}

	_, err = parseSpawnInput([]byte(`{"items":[]}`))
	if err == nil || !strings.Contains(err.Error(), "either prompt or items is required") {
		t.Fatalf("empty items error = %v", err)
	}

	_, err = parseSpawnInput([]byte(`{`))
	if err == nil || !strings.Contains(err.Error(), "parse arguments") {
		t.Fatalf("invalid json error = %v", err)
	}

	result := renderTodoSpawnResult([]string{"a", "b"})
	if !strings.Contains(result, "created_count: 2") || !strings.Contains(result, "- a") {
		t.Fatalf("renderTodoSpawnResult() = %q", result)
	}
}

func TestToolExecuteErrorBranches(t *testing.T) {
	t.Parallel()

	tool := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tool.Execute(ctx, tools.ToolCallInput{
		Name:      tools.ToolNameSpawnSubAgent,
		Arguments: []byte(`{"items":[{"id":"t1","content":"x"}]}`),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() canceled err = %v, want context canceled", err)
	}

	session := agentsession.New("spawn-add-fail")
	mutator := &failingAddMutator{
		stubMutator: &stubMutator{session: &session},
		err:         errors.New("injected add todo failure"),
	}
	_, err = tool.Execute(context.Background(), tools.ToolCallInput{
		Name:           tools.ToolNameSpawnSubAgent,
		SessionMutator: mutator,
		Arguments:      []byte(`{"items":[{"id":"t1","content":"x"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "injected add todo failure") {
		t.Fatalf("Execute() add failure err = %v", err)
	}
}

func TestToolExecuteInlineMode(t *testing.T) {
	t.Parallel()

	tool := New()
	parentToken := &security.CapabilityToken{
		AllowedTools: []string{"spawn_subagent", "filesystem_read_file"},
	}
	invoker := &stubSubAgentInvoker{
		result: tools.SubAgentRunResult{
			Role:       subagent.RoleCoder,
			TaskID:     "inline-1",
			State:      subagent.StateSucceeded,
			StopReason: subagent.StopReasonCompleted,
			StepCount:  2,
			Output: subagent.Output{
				Summary:   "done",
				Findings:  []string{"f1"},
				Artifacts: []string{"a.txt"},
			},
		},
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:            tools.ToolNameSpawnSubAgent,
		AgentID:         "agent-main",
		Workdir:         "/tmp/workdir",
		CapabilityToken: parentToken,
		SubAgentInvoker: invoker,
		Arguments: []byte(`{
				"prompt":"review code quality",
			"id":"inline-1",
			"role":"coder",
			"max_steps":3,
			"timeout_sec":90
		}`),
	})
	if err != nil {
		t.Fatalf("Execute() inline error = %v", err)
	}
	if !strings.Contains(result.Content, "mode: inline") || !strings.Contains(result.Content, "state: succeeded") {
		t.Fatalf("unexpected inline content: %q", result.Content)
	}
	if invoker.last.TaskID != "inline-1" || invoker.last.Goal != "review code quality" {
		t.Fatalf("unexpected invoker input: %+v", invoker.last)
	}
	if invoker.last.Timeout != 90*time.Second {
		t.Fatalf("timeout = %v, want 90s", invoker.last.Timeout)
	}
	if invoker.last.ParentCapabilityToken == nil || len(invoker.last.ParentCapabilityToken.AllowedTools) == 0 {
		t.Fatalf("parent capability token should be forwarded: %+v", invoker.last.ParentCapabilityToken)
	}
}

func TestToolExecuteInlineModeErrors(t *testing.T) {
	t.Parallel()

	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      tools.ToolNameSpawnSubAgent,
		Arguments: []byte(`{"prompt":"do something"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "subagent invoker is unavailable") {
		t.Fatalf("missing invoker error = %v", err)
	}

	invoker := &stubSubAgentInvoker{err: errors.New("subagent failed")}
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:            tools.ToolNameSpawnSubAgent,
		SubAgentInvoker: invoker,
		Arguments:       []byte(`{"prompt":"do something"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "subagent failed") {
		t.Fatalf("expected inline run error, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected result.IsError=true")
	}
}

func TestParseSpawnInputValidationBranches(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("x", maxSpawnTextLen+1)
	tooManyItems := make([]string, 0, maxSpawnItems+1)
	for i := 0; i < maxSpawnItems+1; i++ {
		tooManyItems = append(tooManyItems, fmt.Sprintf(`{"id":"t%d","content":"c"}`, i))
	}
	tooManyDeps := make([]string, 0, maxSpawnListItems+1)
	for i := 0; i < maxSpawnListItems+1; i++ {
		tooManyDeps = append(tooManyDeps, fmt.Sprintf(`"d%d"`, i))
	}
	tooManyAcc := make([]string, 0, maxSpawnListItems+1)
	for i := 0; i < maxSpawnListItems+1; i++ {
		tooManyAcc = append(tooManyAcc, fmt.Sprintf(`"a%d"`, i))
	}
	hugeJSON := []byte(`{"items":[{"id":"t1","content":"` + strings.Repeat("z", maxSpawnArgumentsBytes) + `"}]}`)

	tests := []struct {
		name    string
		raw     []byte
		wantErr string
	}{
		{name: "empty arguments", raw: nil, wantErr: "arguments is empty"},
		{name: "too large payload", raw: hugeJSON, wantErr: "payload exceeds"},
		{name: "too many items", raw: []byte(`{"items":[` + strings.Join(tooManyItems, ",") + `]}`), wantErr: "items exceeds max length"},
		{name: "id empty", raw: []byte(`{"items":[{"id":"  ","content":"x"}]}`), wantErr: "id is empty"},
		{name: "content empty", raw: []byte(`{"items":[{"id":"t1","content":"  "}]}`), wantErr: "content is empty"},
		{name: "id too long", raw: []byte(`{"items":[{"id":"` + tooLong + `","content":"x"}]}`), wantErr: ".id exceeds max length"},
		{name: "content too long", raw: []byte(`{"items":[{"id":"t1","content":"` + tooLong + `"}]}`), wantErr: ".content exceeds max length"},
		{name: "dependencies too many", raw: []byte(`{"items":[{"id":"t1","content":"x","dependencies":[` + strings.Join(tooManyDeps, ",") + `]}]}`), wantErr: "dependencies exceeds max items"},
		{name: "acceptance too many", raw: []byte(`{"items":[{"id":"t1","content":"x","acceptance":[` + strings.Join(tooManyAcc, ",") + `]}]}`), wantErr: "acceptance exceeds max items"},
		{name: "dependency entry too long", raw: []byte(`{"items":[{"id":"t1","content":"x","dependencies":["` + tooLong + `"]}]}`), wantErr: ".dependencies[0] exceeds max length"},
		{name: "acceptance entry too long", raw: []byte(`{"items":[{"id":"t1","content":"x","acceptance":["` + tooLong + `"]}]}`), wantErr: ".acceptance[0] exceeds max length"},
		{name: "negative retry limit", raw: []byte(`{"items":[{"id":"t1","content":"x","retry_limit":-1}]}`), wantErr: "retry_limit must be >= 0"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSpawnInput(tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseSpawnInput() err = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveSpawnOrderAdditionalBranches(t *testing.T) {
	t.Parallel()

	_, err := resolveSpawnOrder([]agentsession.TodoItem{{ID: "exists", Content: "old"}}, []spawnItem{
		{ID: "exists", Content: "new"},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("resolveSpawnOrder(existing) err = %v", err)
	}

	_, err = resolveSpawnOrder(nil, []spawnItem{
		{ID: "a", Content: "a", Dependencies: []string{"b"}},
		{ID: "b", Content: "b", Dependencies: []string{"a"}},
	})
	if err == nil || !strings.Contains(err.Error(), "cyclic dependencies detected") {
		t.Fatalf("resolveSpawnOrder(cycle) err = %v", err)
	}
}

func TestResolveSpawnOrderWithExistingDependency(t *testing.T) {
	t.Parallel()

	existing := []agentsession.TodoItem{
		{ID: "base", Content: "base", Status: agentsession.TodoStatusCompleted},
	}
	items := []spawnItem{
		{ID: "t2", Content: "task2", Dependencies: []string{"t1"}},
		{ID: "t1", Content: "task1", Dependencies: []string{"base"}},
	}
	ordered, err := resolveSpawnOrder(existing, items)
	if err != nil {
		t.Fatalf("resolveSpawnOrder() error = %v", err)
	}
	if len(ordered) != 2 || ordered[0].ID != "t1" || ordered[1].ID != "t2" {
		raw, _ := json.Marshal(ordered)
		t.Fatalf("resolveSpawnOrder() = %s, want [t1 t2]", string(raw))
	}
}
