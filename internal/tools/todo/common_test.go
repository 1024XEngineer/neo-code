package todo

import (
	"errors"
	"strings"
	"testing"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func TestParseInputAndLegacyBranches(t *testing.T) {
	t.Parallel()

	oversized := []byte(`{"action":"add","item":{"id":"a","content":"` + strings.Repeat("x", maxTodoWriteArgumentsBytes) + `"}}`)
	if _, err := parseInput(oversized); err == nil || !strings.Contains(err.Error(), "payload exceeds") {
		t.Fatalf("parseInput(oversized) err = %v", err)
	}

	input, err := parseInput([]byte(`{
		"action":" PLAN ",
		"id":" task-1 ",
		"executor":" subagent ",
		"owner_type":" subagent ",
		"owner_id":" worker-1 ",
		"reason":"  blocked by dep ",
		"items":[{"id":"a","content":"task a","status":"pending"}],
		"item":{"id":"b","content":"task b"}
	}`))
	if err != nil {
		t.Fatalf("parseInput(normal) err = %v", err)
	}
	if input.Action != actionPlan || input.ID != "task-1" || input.Executor != "subagent" {
		t.Fatalf("normalized input = %+v", input)
	}
	if len(input.Items) != 1 || input.Items[0].Content != "task a" {
		t.Fatalf("items mapping failed: %+v", input.Items)
	}
	if input.Item == nil || input.Item.Content != "task b" {
		t.Fatalf("item mapping failed: %+v", input.Item)
	}

	_, err = parseInput([]byte(`{"action":"add","item":{"id":"task-1","title":"legacy"}}`))
	if err == nil || !strings.Contains(err.Error(), "item.content") {
		t.Fatalf("parseInput(legacy item.title) err = %v", err)
	}
	_, err = parseInput([]byte(`{"action":"plan","items":[{"id":"task-1","title":"legacy"}]}`))
	if err == nil || !strings.Contains(err.Error(), "items[0].content") {
		t.Fatalf("parseInput(legacy items[].title) err = %v", err)
	}
}

func TestParseInputNormalizesNumericIDsAndStatusAliases(t *testing.T) {
	t.Parallel()

	input, err := parseInput([]byte(`{
		"action":"set_status",
		"id": 3,
		"status":"In-Progress"
	}`))
	if err != nil {
		t.Fatalf("parseInput(set_status numeric id) err = %v", err)
	}
	if input.ID != "3" {
		t.Fatalf("normalized id = %q, want 3", input.ID)
	}
	if input.Status != agentsession.TodoStatusInProgress {
		t.Fatalf("normalized status = %q, want %q", input.Status, agentsession.TodoStatusInProgress)
	}

	normalizedPlan, err := parseInput([]byte(`{
		"action":"plan",
		"items":[
			{"id":1, "content":"A", "status":"done", "dependencies":[2, "3"]},
			{"id":"2", "content":"B", "status":"cancelled"}
		]
	}`))
	if err != nil {
		t.Fatalf("parseInput(plan normalize) err = %v", err)
	}
	if len(normalizedPlan.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(normalizedPlan.Items))
	}
	if normalizedPlan.Items[0].ID != "1" || normalizedPlan.Items[0].Status != agentsession.TodoStatusCompleted {
		t.Fatalf("item[0] = %+v", normalizedPlan.Items[0])
	}
	if got := normalizedPlan.Items[0].Dependencies; len(got) != 2 || got[0] != "2" || got[1] != "3" {
		t.Fatalf("item[0].dependencies = %+v, want [2 3]", got)
	}
	if normalizedPlan.Items[1].Status != agentsession.TodoStatusCanceled {
		t.Fatalf("item[1].status = %q, want %q", normalizedPlan.Items[1].Status, agentsession.TodoStatusCanceled)
	}
}

func TestValidateInputLimitsAndPatchBranches(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("x", maxTodoWriteTextLen+1)
	tooManyValues := make([]string, 0, maxTodoWriteListItems+1)
	for i := 0; i < maxTodoWriteListItems+1; i++ {
		tooManyValues = append(tooManyValues, "v")
	}
	tests := []struct {
		name  string
		input writeInput
		want  string
	}{
		{
			name: "negative expected revision",
			input: writeInput{
				ExpectedRevision: -1,
			},
			want: "expected_revision must be >= 0",
		},
		{
			name: "id too long",
			input: writeInput{
				ID: tooLong,
			},
			want: "id exceeds max length",
		},
		{
			name: "item field too long",
			input: writeInput{
				Item: &agentsession.TodoItem{ID: "a", Content: tooLong},
			},
			want: "item.content exceeds max length",
		},
		{
			name: "items too many",
			input: writeInput{
				Items: make([]agentsession.TodoItem, maxTodoWriteItems+1),
			},
			want: "items exceeds max length",
		},
		{
			name: "artifacts too many",
			input: writeInput{
				Artifacts: tooManyValues,
			},
			want: "artifacts exceeds max items",
		},
		{
			name: "patch content too long",
			input: writeInput{
				Patch: &todoPatchInput{Content: &tooLong},
			},
			want: "patch.content exceeds max length",
		},
		{
			name: "patch owner_type too long",
			input: writeInput{
				Patch: &todoPatchInput{OwnerType: &tooLong},
			},
			want: "patch.owner_type exceeds max length",
		},
		{
			name: "patch executor too long",
			input: writeInput{
				Patch: &todoPatchInput{Executor: &tooLong},
			},
			want: "patch.executor exceeds max length",
		},
		{
			name: "patch owner_id too long",
			input: writeInput{
				Patch: &todoPatchInput{OwnerID: &tooLong},
			},
			want: "patch.owner_id exceeds max length",
		},
		{
			name: "patch failure_reason too long",
			input: writeInput{
				Patch: &todoPatchInput{FailureReason: &tooLong},
			},
			want: "patch.failure_reason exceeds max length",
		},
		{
			name: "patch dependencies too many",
			input: writeInput{
				Patch: &todoPatchInput{Dependencies: &tooManyValues},
			},
			want: "patch.dependencies exceeds max items",
		},
		{
			name: "patch acceptance too many",
			input: writeInput{
				Patch: &todoPatchInput{Acceptance: &tooManyValues},
			},
			want: "patch.acceptance exceeds max items",
		},
		{
			name: "patch artifacts too many",
			input: writeInput{
				Patch: &todoPatchInput{Artifacts: &tooManyValues},
			},
			want: "patch.artifacts exceeds max items",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputLimits(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateInputLimits() err = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestCommonResultAndReasonHelpers(t *testing.T) {
	t.Parallel()

	if got := mapReason(nil); got != "" {
		t.Fatalf("mapReason(nil) = %q, want empty", got)
	}
	if got := mapReason(errTodoInvalidArguments); got != reasonInvalidArguments {
		t.Fatalf("mapReason(errTodoInvalidArguments) = %q", got)
	}
	if got := mapReason(errors.New("unsupported action: noop")); got != reasonInvalidAction {
		t.Fatalf("mapReason(unsupported) = %q", got)
	}
	if got := mapReason(agentsession.ErrTodoNotFound); got != reasonTodoNotFound {
		t.Fatalf("mapReason(todo not found) = %q", got)
	}
	if got := mapReason(agentsession.ErrInvalidTransition); got != reasonInvalidTransition {
		t.Fatalf("mapReason(invalid transition) = %q", got)
	}
	if got := mapReason(agentsession.ErrDependencyViolation); got != reasonDependencyViolation {
		t.Fatalf("mapReason(dependency violation) = %q", got)
	}
	if got := mapReason(agentsession.ErrRevisionConflict); got != reasonRevisionConflict {
		t.Fatalf("mapReason(revision conflict) = %q", got)
	}
	if got := mapReason(errors.New("unexpected")); got == "" {
		t.Fatalf("mapReason(default) should not be empty")
	}

	out := errorResult(" reason ", " detail ", map[string]any{"k": "v"})
	if !out.IsError || out.Metadata["reason_code"] != "reason" || out.Metadata["k"] != "v" {
		t.Fatalf("errorResult() = %+v", out)
	}

	result := successResult("plan", []agentsession.TodoItem{
		{ID: "b", Content: "second", Priority: 1, Status: agentsession.TodoStatusPending, Executor: "agent", Revision: 2},
		{ID: "a", Content: "first", Priority: 2, Status: agentsession.TodoStatusInProgress, Executor: "subagent", Revision: 3},
	})
	if result.Name != tools.ToolNameTodoWrite {
		t.Fatalf("successResult().Name = %q", result.Name)
	}
	if !strings.Contains(result.Content, "- [in_progress] a") || !strings.Contains(result.Content, "- [pending] b") {
		t.Fatalf("successResult().Content = %q", result.Content)
	}
}
