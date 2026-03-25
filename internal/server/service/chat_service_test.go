package service

import (
	"context"
	"strings"
	"testing"

	"go-llm-demo/internal/server/domain"
)

type stubMemoryService struct {
	buildContext string
	saveCalls    int
}

func (s *stubMemoryService) BuildContext(context.Context, string) (string, error) {
	return s.buildContext, nil
}

func (s *stubMemoryService) Save(context.Context, string, string) error {
	s.saveCalls++
	return nil
}

func (s *stubMemoryService) GetStats(context.Context) (*domain.MemoryStats, error) {
	return &domain.MemoryStats{}, nil
}

func (s *stubMemoryService) Clear(context.Context) error {
	return nil
}

func (s *stubMemoryService) ClearSession(context.Context) error {
	return nil
}

type stubWorkingMemoryService struct {
	buildContext string
	refreshCalls int
}

func (s *stubWorkingMemoryService) BuildContext(context.Context, []domain.Message) (string, error) {
	return s.buildContext, nil
}

func (s *stubWorkingMemoryService) Refresh(context.Context, []domain.Message) error {
	s.refreshCalls++
	return nil
}

func (s *stubWorkingMemoryService) Clear(context.Context) error {
	return nil
}

func (s *stubWorkingMemoryService) Get(context.Context) (*domain.WorkingMemoryState, error) {
	return &domain.WorkingMemoryState{}, nil
}

type stubTodoService struct {
	todos []domain.Todo
}

func (s *stubTodoService) AddTodo(context.Context, string, domain.TodoPriority) (*domain.Todo, error) {
	return nil, nil
}

func (s *stubTodoService) UpdateTodoStatus(context.Context, string, domain.TodoStatus) error {
	return nil
}

func (s *stubTodoService) ListTodos(context.Context) ([]domain.Todo, error) {
	return append([]domain.Todo(nil), s.todos...), nil
}

func (s *stubTodoService) ClearTodos(context.Context) error {
	return nil
}

func (s *stubTodoService) RemoveTodo(context.Context, string) error {
	return nil
}

type stubRoleService struct {
	prompt string
}

func (s *stubRoleService) GetActivePrompt(context.Context) (string, error) {
	return s.prompt, nil
}

func (s *stubRoleService) SetActive(context.Context, string) error {
	return nil
}

func (s *stubRoleService) List(context.Context) ([]domain.Role, error) {
	return nil, nil
}

func (s *stubRoleService) Create(context.Context, string, string, string) (*domain.Role, error) {
	return nil, nil
}

func (s *stubRoleService) Delete(context.Context, string) error {
	return nil
}

type captureChatProvider struct {
	messages []domain.Message
	tools    []domain.ToolSchema
}

func (p *captureChatProvider) GetModelName() string {
	return "test-model"
}

func (p *captureChatProvider) Chat(_ context.Context, messages []domain.Message, tools []domain.ToolSchema) (<-chan domain.ChatEvent, error) {
	p.messages = append([]domain.Message(nil), messages...)
	p.tools = append([]domain.ToolSchema(nil), tools...)
	ch := make(chan domain.ChatEvent, 1)
	ch <- domain.ChatEvent{Type: domain.ChatEventDelta, Content: "done"}
	close(ch)
	return ch, nil
}

func drain(ch <-chan domain.ChatEvent) {
	for range ch {
	}
}

func TestSendPassesToolsAndInjectsRuntimeContext(t *testing.T) {
	mem := &stubMemoryService{buildContext: "[MEMORY]\nremember this"}
	work := &stubWorkingMemoryService{buildContext: "[WORKING_MEMORY]\ncurrent task"}
	todo := &stubTodoService{todos: []domain.Todo{{ID: "todo-1", Content: "write tests", Status: domain.TodoPending, Priority: domain.TodoPriorityHigh}}}
	role := &stubRoleService{prompt: "You are NeoCode."}
	provider := &captureChatProvider{}
	toolSchemas := []domain.ToolSchema{{Type: "function", Function: domain.ToolFunctionSchema{Name: "list"}}}

	gateway := NewChatService(mem, work, todo, role, provider, toolSchemas)
	out, err := gateway.Send(context.Background(), &domain.ChatRequest{
		Messages: []domain.Message{{Role: "user", Content: "hello"}},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	drain(out)

	if len(provider.messages) < 2 {
		t.Fatalf("expected system prompt plus user message, got %+v", provider.messages)
	}
	if provider.messages[0].Role != "system" {
		t.Fatalf("expected leading system message, got %+v", provider.messages[0])
	}
	if len(provider.tools) != 1 || provider.tools[0].Function.Name != "list" {
		t.Fatalf("expected tools to be passed through, got %+v", provider.tools)
	}

	systemContent := provider.messages[0].Content
	for _, want := range []string{
		"You are NeoCode.",
		"[WORKING_MEMORY]",
		"[TODO_LIST]",
		"[MEMORY]",
	} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("expected system context to contain %q, got %q", want, systemContent)
		}
	}
	if mem.saveCalls != 1 {
		t.Fatalf("expected memory save to be called once, got %d", mem.saveCalls)
	}
	if work.refreshCalls != 1 {
		t.Fatalf("expected working memory refresh to be called once, got %d", work.refreshCalls)
	}
}

func TestSendPrependsRolePromptWhenSystemMessageExistsLater(t *testing.T) {
	mem := &stubMemoryService{}
	role := &stubRoleService{prompt: "You are NeoCode."}
	provider := &captureChatProvider{}

	gateway := NewChatService(mem, nil, &stubTodoService{}, role, provider, nil)
	out, err := gateway.Send(context.Background(), &domain.ChatRequest{
		Messages: []domain.Message{
			{Role: "user", Content: "hello"},
			{Role: "system", Content: "[TOOL_CONTEXT]\nexisting"},
		},
		Model: "test-model",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	drain(out)

	if len(provider.messages) == 0 {
		t.Fatal("expected provider messages")
	}
	if provider.messages[0].Role != "system" {
		t.Fatalf("expected prepended system message, got %+v", provider.messages)
	}
	if !strings.Contains(provider.messages[0].Content, "You are NeoCode.") {
		t.Fatalf("expected role prompt to be preserved, got %q", provider.messages[0].Content)
	}
}

func TestInjectSystemContextPreservesExistingLeadingSystemContent(t *testing.T) {
	messages := []domain.Message{
		{Role: "system", Content: "existing system content"},
		{Role: "user", Content: "hello"},
	}

	got := injectSystemContext(messages, "role prompt", "runtime context")
	if len(got) != 2 {
		t.Fatalf("expected message count to stay the same, got %+v", got)
	}
	if !strings.Contains(got[0].Content, "role prompt") {
		t.Fatalf("expected role prompt in leading system message, got %q", got[0].Content)
	}
	if !strings.Contains(got[0].Content, "existing system content") {
		t.Fatalf("expected existing system content to be preserved, got %q", got[0].Content)
	}
}
