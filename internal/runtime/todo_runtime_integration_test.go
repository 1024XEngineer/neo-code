package runtime

import (
	"context"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
	todotool "neo-code/internal/tools/todo"
)

func TestServiceRunTodoWriteToolCall(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(todotool.New())

	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-1",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"add","item":{"id":"todo-1","content":"implement feature","priority":3,"required":false}}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-2",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"set_status","id":"todo-1","status":"canceled","expected_revision":1}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(
		manager,
		registry,
		store,
		&scriptedProviderFactory{provider: providerImpl},
		&stubContextBuilder{},
	)

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-todo-tool",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("请记录一个待办并继续")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(providerImpl.requests) < 1 {
		t.Fatalf("expected provider requests, got 0")
	}
	toolFound := false
	for _, spec := range providerImpl.requests[0].Tools {
		if strings.EqualFold(strings.TrimSpace(spec.Name), tools.ToolNameTodoWrite) {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Fatalf("expected first request tools to include %q", tools.ToolNameTodoWrite)
	}

	session := onlySession(t, store)
	if len(session.Todos) != 1 {
		t.Fatalf("expected 1 todo item, got %d", len(session.Todos))
	}
	if session.Todos[0].ID != "todo-1" || session.Todos[0].Content != "implement feature" {
		t.Fatalf("unexpected todo item: %+v", session.Todos[0])
	}
	if session.Todos[0].Status != "canceled" {
		t.Fatalf("expected todo to be closed before completion, got %+v", session.Todos[0])
	}

	events := collectRuntimeEvents(service.Events())
	foundTodoUpdated := false
	for _, event := range events {
		if event.Type == EventTodoUpdated {
			foundTodoUpdated = true
			payload, ok := event.Payload.(TodoEventPayload)
			if !ok {
				t.Fatalf("todo updated payload type = %T, want TodoEventPayload", event.Payload)
			}
			if strings.TrimSpace(payload.Action) == "" {
				t.Fatalf("todo updated payload action should not be empty: %+v", payload)
			}
			break
		}
	}
	if !foundTodoUpdated {
		t.Fatalf("expected %q event in runtime events", EventTodoUpdated)
	}
}

func TestTodoWriteErrorEmitsConflictInsteadOfUpdated(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(todotool.New())

	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-1",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"add","item":{"id":"todo-1","content":"implement feature","priority":3,"required":true}}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-2",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"set_status","id":"todo-1","status":"completed","expected_revision":99}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-3",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"set_status","id":"todo-1","status":"in_progress","expected_revision":1}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{
							ID:        "todo-call-4",
							Name:      tools.ToolNameTodoWrite,
							Arguments: `{"action":"complete","id":"todo-1","expected_revision":2}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(
		manager,
		registry,
		store,
		&scriptedProviderFactory{provider: providerImpl},
		&stubContextBuilder{},
	)

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-todo-tool-conflict",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("请记录并完成待办")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	updatedCount := 0
	conflictCount := 0
	snapshotUpdatedCount := 0
	for _, event := range events {
		switch event.Type {
		case EventTodoUpdated:
			updatedCount++
		case EventTodoConflict:
			conflictCount++
			payload, ok := event.Payload.(TodoEventPayload)
			if !ok {
				t.Fatalf("todo conflict payload type = %T, want TodoEventPayload", event.Payload)
			}
			if payload.Summary.RequiredTotal != 1 || payload.Summary.RequiredCompleted != 0 || payload.Summary.RequiredOpen != 1 {
				t.Fatalf("unexpected todo conflict summary: %+v", payload)
			}
			if !strings.Contains(strings.ToLower(strings.TrimSpace(payload.Reason)), "revision_conflict") {
				t.Fatalf("unexpected todo conflict reason: %+v", payload)
			}
		case EventTodoSnapshotUpdated:
			snapshotUpdatedCount++
		}
	}
	if updatedCount != 3 {
		t.Fatalf("todo_updated count = %d, want 3 (add + set_status + complete)", updatedCount)
	}
	if conflictCount != 1 {
		t.Fatalf("todo_conflict count = %d, want 1", conflictCount)
	}
	// conflict 也会触发 snapshot_updated（当前快照回传）
	if snapshotUpdatedCount != 4 {
		t.Fatalf("todo_snapshot_updated count = %d, want 4 (add + set_status + conflict + complete)", snapshotUpdatedCount)
	}
}
