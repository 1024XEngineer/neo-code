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
							Arguments: `{"action":"add","item":{"id":"todo-1","content":"implement feature","priority":3}}`,
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

	events := collectRuntimeEvents(service.Events())
	foundTodoUpdated := false
	for _, event := range events {
		if event.Type == EventTodoUpdated {
			foundTodoUpdated = true
			break
		}
	}
	if !foundTodoUpdated {
		t.Fatalf("expected %q event in runtime events", EventTodoUpdated)
	}
}
