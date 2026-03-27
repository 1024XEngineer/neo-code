package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neocode/internal/provider"
	"neocode/internal/tools"
)

type fakeProvider struct {
	calls int
}

func (f *fakeProvider) Name() string {
	return "fake"
}

func (f *fakeProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	f.calls++
	if f.calls == 1 {
		return provider.ChatResponse{
			Message: provider.Message{
				Role: provider.RoleAssistant,
				ToolCalls: []provider.ToolCall{
					{
						ID:        "call-1",
						Name:      "echo",
						Arguments: `{"value":"hello"}`,
					},
				},
			},
		}, nil
	}

	foundToolResult := false
	for _, message := range req.Messages {
		if message.Role == provider.RoleTool && strings.Contains(message.Content, "echo: hello") {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		return provider.ChatResponse{}, context.DeadlineExceeded
	}

	return provider.ChatResponse{
		Message: provider.Message{
			Role:    provider.RoleAssistant,
			Content: "done",
		},
	}, nil
}

type echoTool struct{}

func (t *echoTool) Name() string {
	return "echo"
}

func (t *echoTool) Description() string {
	return "Echo back a value."
}

func (t *echoTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *echoTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	var args struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: "echo: " + args.Value}, nil
}

func TestServiceRunExecutesToolLoop(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(&echoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	service, err := New(&fakeProvider{}, registry, tools.NewExecutor(registry), "test-model", t.TempDir())
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	session := service.Sessions()[0]
	events := service.Subscribe(32)

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		Content:   "say hello",
	}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	stored, ok := service.Session(session.ID)
	if !ok {
		t.Fatalf("expected session to exist")
	}
	if len(stored.Messages) != 4 {
		t.Fatalf("expected 4 messages in session, got %d", len(stored.Messages))
	}
	if stored.Messages[len(stored.Messages)-1].Content != "done" {
		t.Fatalf("unexpected final assistant message: %q", stored.Messages[len(stored.Messages)-1].Content)
	}

	foundToolFinished := false
	for {
		select {
		case event := <-events:
			if event.Type == EventToolFinished {
				foundToolFinished = true
			}
		default:
			if !foundToolFinished {
				t.Fatalf("expected tool finished event")
			}
			return
		}
	}
}

type loopingProvider struct{}

func (l *loopingProvider) Name() string {
	return "looping"
}

func (l *loopingProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{
		Message: provider.Message{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{
					ID:        "call-1",
					Name:      "echo",
					Arguments: `{"value":"x"}`,
				},
			},
		},
	}, nil
}

func TestServiceRunStopsAtMaxTurns(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(&echoTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	service, err := New(
		&loopingProvider{},
		registry,
		tools.NewExecutor(registry),
		"test-model",
		t.TempDir(),
		WithMaxTurns(1),
	)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	session := service.Sessions()[0]

	err = service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		Content:   "loop forever",
	})
	if err == nil || !strings.Contains(err.Error(), "max turns") {
		t.Fatalf("expected max turns error, got %v", err)
	}
}

type textProvider struct {
	name    string
	content string
}

func (p *textProvider) Name() string {
	return p.name
}

func (p *textProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{
		Message: provider.Message{
			Role:    provider.RoleAssistant,
			Content: p.content,
		},
	}, nil
}

func TestServiceSwitchProviderUpdatesStatus(t *testing.T) {
	registry := tools.NewRegistry()
	service, err := New(
		&textProvider{name: "primary", content: "ok"},
		registry,
		tools.NewExecutor(registry),
		"model-a",
		t.TempDir(),
		WithProviders([]ProviderBinding{
			{Name: "primary", Model: "model-a", Client: &textProvider{name: "primary", content: "ok"}},
			{Name: "backup", Model: "model-b", Client: &textProvider{name: "backup", content: "ok"}},
		}, "primary"),
	)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	if err := service.SwitchProvider("backup"); err != nil {
		t.Fatalf("switch provider: %v", err)
	}

	status := service.Status()
	if status.Provider != "backup" {
		t.Fatalf("expected provider backup, got %q", status.Provider)
	}
	if status.Model != "model-b" {
		t.Fatalf("expected model-b, got %q", status.Model)
	}
}

func TestServiceRunPublishesAgentChunks(t *testing.T) {
	registry := tools.NewRegistry()
	service, err := New(
		&textProvider{name: "primary", content: strings.Repeat("a", 80)},
		registry,
		tools.NewExecutor(registry),
		"model-a",
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	session := service.Sessions()[0]
	events := service.Subscribe(64)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		Content:   "stream some text",
	}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	foundChunk := false
	for {
		select {
		case event := <-events:
			if event.Type == EventAgentChunk {
				foundChunk = true
			}
		default:
			if !foundChunk {
				t.Fatalf("expected at least one agent chunk event")
			}
			return
		}
	}
}

type failingTool struct{}

func (t *failingTool) Name() string {
	return "fail"
}

func (t *failingTool) Description() string {
	return "Always fails."
}

func (t *failingTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *failingTool) Execute(context.Context, tools.Invocation) (tools.Result, error) {
	return tools.Result{Content: "failed to execute command"}, context.DeadlineExceeded
}

type failingToolProvider struct {
	calls int
}

func (p *failingToolProvider) Name() string {
	return "failing"
}

func (p *failingToolProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	p.calls++
	if p.calls == 1 {
		return provider.ChatResponse{
			Message: provider.Message{
				Role: provider.RoleAssistant,
				ToolCalls: []provider.ToolCall{
					{
						ID:        "call-fail-1",
						Name:      "fail",
						Arguments: `{}`,
					},
				},
			},
		}, nil
	}

	return provider.ChatResponse{
		Message: provider.Message{
			Role:    provider.RoleAssistant,
			Content: "recovered after tool failure",
		},
	}, nil
}

func TestServiceRunContinuesAfterToolError(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(&failingTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	service, err := New(
		&failingToolProvider{},
		registry,
		tools.NewExecutor(registry),
		"test-model",
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	session := service.Sessions()[0]
	events := service.Subscribe(64)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		Content:   "trigger a failing tool",
	}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	stored, ok := service.Session(session.ID)
	if !ok {
		t.Fatalf("expected session to exist")
	}
	if len(stored.Messages) != 4 {
		t.Fatalf("expected 4 messages in session, got %d", len(stored.Messages))
	}
	if got := stored.Messages[2].Content; got != "tool error: failed to execute command" {
		t.Fatalf("expected normalized tool error message, got %q", got)
	}
	if got := stored.Messages[3].Content; got != "recovered after tool failure" {
		t.Fatalf("expected runtime to continue after tool error, got %q", got)
	}

	foundToolFinished := false
	foundErrorEvent := false
	for {
		select {
		case event := <-events:
			switch event.Type {
			case EventToolFinished:
				foundToolFinished = true
			case EventError:
				foundErrorEvent = true
			}
		default:
			if !foundToolFinished {
				t.Fatalf("expected tool finished event")
			}
			if !foundErrorEvent {
				t.Fatalf("expected error event for tool failure")
			}
			return
		}
	}
}
