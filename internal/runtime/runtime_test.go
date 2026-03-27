package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	"github.com/dust/neo-code/internal/tools"
)

type memoryStore struct {
	sessions map[string]Session
	saves    int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: map[string]Session{}}
}

func (s *memoryStore) Save(ctx context.Context, session *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session == nil {
		return errors.New("nil session")
	}
	s.saves++
	s.sessions[session.ID] = cloneSession(*session)
	return nil
}

func (s *memoryStore) Load(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	session, ok := s.sessions[id]
	if !ok {
		return Session{}, errors.New("not found")
	}
	return cloneSession(session), nil
}

func (s *memoryStore) ListSummaries(ctx context.Context) ([]SessionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	summaries := make([]SessionSummary, 0, len(s.sessions))
	for _, session := range s.sessions {
		summaries = append(summaries, SessionSummary{
			ID:        session.ID,
			Title:     session.Title,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		})
	}
	return summaries, nil
}

type scriptedProvider struct {
	name      string
	responses []provider.ChatResponse
	streams   [][]provider.StreamEvent
	requests  []provider.ChatRequest
	callCount int
}

func (p *scriptedProvider) Name() string {
	return p.name
}

func (p *scriptedProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	p.requests = append(p.requests, cloneChatRequest(req))

	callIndex := p.callCount
	p.callCount++

	if callIndex < len(p.streams) {
		for _, event := range p.streams[callIndex] {
			select {
			case events <- event:
			case <-ctx.Done():
				return provider.ChatResponse{}, ctx.Err()
			}
		}
	}

	if callIndex >= len(p.responses) {
		return provider.ChatResponse{}, fmt.Errorf("unexpected provider call %d", callIndex)
	}
	return p.responses[callIndex], nil
}

type scriptedProviderFactory struct {
	provider provider.Provider
	calls    int
	err      error
}

func (f *scriptedProviderFactory) Build(cfg config.Config) (provider.Provider, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.provider, nil
}

type stubTool struct {
	name      string
	content   string
	isError   bool
	err       error
	callCount int
	lastInput tools.ToolCallInput
}

func (t *stubTool) Name() string {
	return t.name
}

func (t *stubTool) Description() string {
	return "stub tool"
}

func (t *stubTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}

func (t *stubTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	t.callCount++
	t.lastInput = input
	if input.EmitChunk != nil {
		input.EmitChunk([]byte("chunk"))
	}
	return tools.ToolResult{
		Name:    t.name,
		Content: t.content,
		IsError: t.isError,
	}, t.err
}

func TestServiceRun(t *testing.T) {
	tests := []struct {
		name                string
		input               UserInput
		providerResponses   []provider.ChatResponse
		providerStreams     [][]provider.StreamEvent
		registerTool        tools.Tool
		expectProviderCalls int
		expectToolCalls     int
		expectMessageRoles  []string
		expectEventTypes    []EventType
		assert              func(t *testing.T, store *memoryStore, provider *scriptedProvider, tool *stubTool)
	}{
		{
			name:  "normal dialogue exits after final assistant reply",
			input: UserInput{Content: "hello"},
			providerResponses: []provider.ChatResponse{
				{
					Message: provider.Message{
						Role:    "assistant",
						Content: "plain answer",
					},
					FinishReason: "stop",
				},
			},
			providerStreams: [][]provider.StreamEvent{
				{
					{Type: provider.StreamEventTextDelta, Text: "plain "},
					{Type: provider.StreamEventTextDelta, Text: "answer"},
				},
			},
			expectProviderCalls: 1,
			expectToolCalls:     0,
			expectMessageRoles:  []string{"user", "assistant"},
			expectEventTypes:    []EventType{EventUserMessage, EventAgentChunk, EventAgentChunk, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if len(scripted.requests) != 1 {
					t.Fatalf("expected 1 provider request, got %d", len(scripted.requests))
				}
				if len(scripted.requests[0].Tools) == 0 {
					t.Fatalf("expected tool specs to be forwarded")
				}
				if scripted.requests[0].SystemPrompt == "" {
					t.Fatalf("expected system prompt to be set")
				}
			},
		},
		{
			name:  "tool call triggers execute and follow-up provider round",
			input: UserInput{Content: "edit file"},
			providerResponses: []provider.ChatResponse{
				{
					Message: provider.Message{
						Role: "assistant",
						ToolCalls: []provider.ToolCall{
							{
								ID:        "call-1",
								Name:      "filesystem_edit",
								Arguments: `{"path":"main.go"}`,
							},
						},
					},
					FinishReason: "tool_calls",
				},
				{
					Message: provider.Message{
						Role:    "assistant",
						Content: "done",
					},
					FinishReason: "stop",
				},
			},
			registerTool: &stubTool{
				name:    "filesystem_edit",
				content: "tool output",
			},
			expectProviderCalls: 2,
			expectToolCalls:     1,
			expectMessageRoles:  []string{"user", "assistant", "tool", "assistant"},
			expectEventTypes:    []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventToolResult, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if tool == nil {
					t.Fatalf("expected stub tool")
				}
				if tool.lastInput.ID != "call-1" {
					t.Fatalf("expected tool call id call-1, got %q", tool.lastInput.ID)
				}
				if tool.lastInput.SessionID == "" {
					t.Fatalf("expected session id to be forwarded to tool")
				}
				if len(scripted.requests) != 2 {
					t.Fatalf("expected 2 provider requests, got %d", len(scripted.requests))
				}
				second := scripted.requests[1]
				foundToolResult := false
				for _, message := range second.Messages {
					if message.Role == "tool" && message.ToolCallID == "call-1" && message.Content == "tool output" {
						foundToolResult = true
						break
					}
				}
				if !foundToolResult {
					t.Fatalf("expected tool result message in second provider request: %+v", second.Messages)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newRuntimeConfigManager(t)
			store := newMemoryStore()

			registry := tools.NewRegistry()
			defaultTool := &stubTool{name: "filesystem_read_file", content: "default"}
			registry.Register(defaultTool)

			var registeredTool *stubTool
			if tt.registerTool != nil {
				if stub, ok := tt.registerTool.(*stubTool); ok {
					registeredTool = stub
				}
				registry.Register(tt.registerTool)
			}

			scripted := &scriptedProvider{
				name:      "scripted",
				responses: tt.providerResponses,
				streams:   tt.providerStreams,
			}
			factory := &scriptedProviderFactory{provider: scripted}

			service := NewWithFactory(manager, registry, store, factory)
			if err := service.Run(context.Background(), tt.input); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if factory.calls != tt.expectProviderCalls {
				t.Fatalf("expected %d provider builds, got %d", tt.expectProviderCalls, factory.calls)
			}
			if registeredTool != nil && registeredTool.callCount != tt.expectToolCalls {
				t.Fatalf("expected %d tool executes, got %d", tt.expectToolCalls, registeredTool.callCount)
			}

			session := onlySession(t, store)
			if len(session.Messages) != len(tt.expectMessageRoles) {
				t.Fatalf("expected %d session messages, got %d", len(tt.expectMessageRoles), len(session.Messages))
			}
			for idx, role := range tt.expectMessageRoles {
				if session.Messages[idx].Role != role {
					t.Fatalf("expected message[%d] role %q, got %q", idx, role, session.Messages[idx].Role)
				}
			}

			events := collectRuntimeEvents(service.Events())
			assertEventSequence(t, events, tt.expectEventTypes)

			if tt.assert != nil {
				tt.assert(t, store, scripted, registeredTool)
			}
		})
	}
}

func TestServiceTrimMessagesPreservesToolPairs(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	service := NewWithFactory(manager, tools.NewRegistry(), newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{name: "noop"}})

	messages := make([]provider.Message, 0, maxContextTurns+4)
	for i := 0; i < 8; i++ {
		messages = append(messages, provider.Message{Role: "user", Content: fmt.Sprintf("u-%d", i)})
	}
	messages = append(messages,
		provider.Message{
			Role: "assistant",
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-result"},
		provider.Message{Role: "assistant", Content: "after-tool"},
		provider.Message{Role: "user", Content: "latest"},
	)

	trimmed := service.trimMessages(messages)
	if len(trimmed) > len(messages) {
		t.Fatalf("trimmed messages should not grow")
	}

	foundAssistantToolCall := false
	foundToolResult := false
	for _, message := range trimmed {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			foundAssistantToolCall = true
		}
		if message.Role == "tool" && message.ToolCallID == "call-1" {
			foundToolResult = true
		}
	}
	if foundAssistantToolCall != foundToolResult {
		t.Fatalf("expected tool call and tool result to be preserved together, got %+v", trimmed)
	}
}

func TestServiceTrimMessagesBoundaries(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	service := NewWithFactory(manager, tools.NewRegistry(), newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{name: "noop"}})

	tests := []struct {
		name    string
		input   []provider.Message
		wantLen int
		assert  func(t *testing.T, original []provider.Message, trimmed []provider.Message)
	}{
		{
			name: "within max turns returns full cloned slice",
			input: []provider.Message{
				{Role: "user", Content: "one"},
				{Role: "assistant", Content: "two"},
			},
			wantLen: 2,
			assert: func(t *testing.T, original []provider.Message, trimmed []provider.Message) {
				t.Helper()
				if &trimmed[0] == &original[0] {
					t.Fatalf("expected trimmed slice to be cloned")
				}
			},
		},
		{
			name: "long message list with limited spans keeps full history",
			input: func() []provider.Message {
				messages := make([]provider.Message, 0, maxContextTurns+3)
				for i := 0; i < maxContextTurns-1; i++ {
					messages = append(messages, provider.Message{Role: "user", Content: fmt.Sprintf("u-%d", i)})
				}
				messages = append(messages,
					provider.Message{
						Role: "assistant",
						ToolCalls: []provider.ToolCall{
							{ID: "call-1", Name: "filesystem_edit", Arguments: "{}"},
						},
					},
					provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-1"},
					provider.Message{Role: "tool", ToolCallID: "call-1", Content: "tool-2"},
				)
				return messages
			}(),
			wantLen: maxContextTurns + 2,
			assert: func(t *testing.T, original []provider.Message, trimmed []provider.Message) {
				t.Helper()
				if len(trimmed) != len(original) {
					t.Fatalf("expected full history to remain, got %d want %d", len(trimmed), len(original))
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			trimmed := service.trimMessages(tt.input)
			if len(trimmed) != tt.wantLen {
				t.Fatalf("expected len %d, got %d", tt.wantLen, len(trimmed))
			}
			if tt.assert != nil {
				tt.assert(t, tt.input, trimmed)
			}
		})
	}
}

func TestServiceRunErrorPaths(t *testing.T) {
	tests := []struct {
		name         string
		input        UserInput
		maxLoops     int
		provider     *scriptedProvider
		factoryErr   error
		registerTool *stubTool
		seedSession  *Session
		expectErr    string
		expectEvents []EventType
		assert       func(t *testing.T, store *memoryStore, provider *scriptedProvider, tool *stubTool)
	}{
		{
			name:      "empty input returns validation error",
			input:     UserInput{Content: "   "},
			expectErr: "input content is empty",
			assert: func(t *testing.T, store *memoryStore, provider *scriptedProvider, tool *stubTool) {
				t.Helper()
				if len(store.sessions) != 0 {
					t.Fatalf("expected no sessions to be created")
				}
			},
		},
		{
			name:     "max loops reached after repeated tool cycles",
			input:    UserInput{Content: "loop"},
			maxLoops: 1,
			provider: &scriptedProvider{
				name: "looping",
				responses: []provider.ChatResponse{
					{
						Message: provider.Message{
							Role: "assistant",
							ToolCalls: []provider.ToolCall{
								{ID: "loop-call", Name: "filesystem_edit", Arguments: `{"path":"x"}`},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			},
			registerTool: &stubTool{name: "filesystem_edit", content: "loop tool output"},
			expectErr:    "max loop reached",
			expectEvents: []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventToolResult, EventError},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if scripted.callCount != 1 {
					t.Fatalf("expected one provider call before loop exit, got %d", scripted.callCount)
				}
				session := onlySession(t, store)
				if len(session.Messages) != 3 {
					t.Fatalf("expected user, assistant, tool messages before abort, got %d", len(session.Messages))
				}
			},
		},
		{
			name:       "provider factory error emits runtime error",
			input:      UserInput{Content: "hello"},
			factoryErr: errors.New("factory failed"),
			expectErr:  "factory failed",
			expectEvents: []EventType{
				EventUserMessage,
				EventError,
			},
		},
		{
			name: "existing session is reused",
			input: UserInput{
				SessionID: "existing-session",
				Content:   "continue",
			},
			provider: &scriptedProvider{
				name: "resume",
				responses: []provider.ChatResponse{
					{
						Message: provider.Message{
							Role:    "assistant",
							Content: "resumed",
						},
						FinishReason: "stop",
					},
				},
			},
			seedSession: &Session{
				ID:        "existing-session",
				Title:     "Resume Me",
				CreatedAt: newSession("seed").CreatedAt,
				UpdatedAt: newSession("seed").UpdatedAt,
				Messages: []provider.Message{
					{Role: "user", Content: "earlier"},
				},
			},
			expectEvents: []EventType{EventUserMessage, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				session, ok := store.sessions["existing-session"]
				if !ok {
					t.Fatalf("expected existing session to be updated")
				}
				if len(session.Messages) != 3 {
					t.Fatalf("expected original message plus new user/assistant, got %d", len(session.Messages))
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newRuntimeConfigManager(t)
			if tt.maxLoops > 0 {
				if err := manager.Update(context.Background(), func(cfg *config.Config) error {
					cfg.MaxLoops = tt.maxLoops
					return nil
				}); err != nil {
					t.Fatalf("update max loops: %v", err)
				}
			}

			store := newMemoryStore()
			if tt.seedSession != nil {
				store.sessions[tt.seedSession.ID] = cloneSession(*tt.seedSession)
			}

			registry := tools.NewRegistry()
			registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})
			if tt.registerTool != nil {
				registry.Register(tt.registerTool)
			}

			factory := &scriptedProviderFactory{
				provider: tt.provider,
				err:      tt.factoryErr,
			}

			service := NewWithFactory(manager, registry, store, factory)
			err := service.Run(context.Background(), tt.input)
			if tt.expectErr != "" {
				if err == nil || err.Error() != tt.expectErr && !containsError(err, tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.expectEvents) > 0 {
				assertEventSequence(t, collectRuntimeEvents(service.Events()), tt.expectEvents)
			}
			if tt.assert != nil {
				tt.assert(t, store, tt.provider, tt.registerTool)
			}
		})
	}
}

func TestServiceConstructorsAndDelegates(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	service := New(manager, registry, store)
	if service == nil {
		t.Fatalf("expected service")
	}
	if service.Events() == nil {
		t.Fatalf("expected events channel")
	}

	session := newSession("List Me")
	store.sessions[session.ID] = cloneSession(session)

	summaries, err := service.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != session.ID {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}

	loaded, err := service.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.ID != session.ID {
		t.Fatalf("expected loaded session %q, got %q", session.ID, loaded.ID)
	}

	sessionStore := NewSessionStore(t.TempDir())
	if sessionStore == nil {
		t.Fatalf("expected JSON session store")
	}
}

func TestDefaultProviderFactoryBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*config.Config)
		expectErr  string
		expectName string
	}{
		{
			name: "openai provider",
			mutate: func(cfg *config.Config) {
				cfg.SelectedProvider = config.ProviderOpenAI
				cfg.CurrentModel = config.DefaultOpenAIModel
			},
			expectName: config.ProviderOpenAI,
		},
		{
			name: "anthropic provider",
			mutate: func(cfg *config.Config) {
				cfg.SelectedProvider = config.ProviderAnthropic
				cfg.CurrentModel = config.DefaultAnthropicModel
			},
			expectName: config.ProviderAnthropic,
		},
		{
			name: "gemini provider",
			mutate: func(cfg *config.Config) {
				cfg.SelectedProvider = config.ProviderGemini
				cfg.CurrentModel = config.DefaultGeminiModel
			},
			expectName: config.ProviderGemini,
		},
		{
			name: "unsupported provider type",
			mutate: func(cfg *config.Config) {
				cfg.SelectedProvider = "custom"
				cfg.CurrentModel = "custom-model"
				cfg.Providers = append(cfg.Providers, config.ProviderConfig{
					Name:      "custom",
					Type:      "custom",
					BaseURL:   "https://example.com",
					Model:     "custom-model",
					APIKeyEnv: "CUSTOM_API_KEY",
				})
			},
			expectErr: `unsupported provider type "custom"`,
		},
	}

	factory := DefaultProviderFactory{}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			tt.mutate(cfg)
			got, err := factory.Build(cfg.Clone())
			if tt.expectErr != "" {
				if err == nil || !containsError(err, tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.Name() != tt.expectName {
				t.Fatalf("expected provider %q, got %+v", tt.expectName, got)
			}
		})
	}
}

func newRuntimeConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	manager := config.NewManager(config.NewLoader(t.TempDir()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = t.TempDir()
		cfg.ToolTimeoutSec = 1
		cfg.MaxLoops = 4
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	return manager
}

func onlySession(t *testing.T, store *memoryStore) Session {
	t.Helper()
	if len(store.sessions) != 1 {
		t.Fatalf("expected exactly 1 session, got %d", len(store.sessions))
	}
	for _, session := range store.sessions {
		return session
	}
	return Session{}
}

func collectRuntimeEvents(events <-chan RuntimeEvent) []RuntimeEvent {
	collected := make([]RuntimeEvent, 0, 8)
	for {
		select {
		case event := <-events:
			collected = append(collected, event)
		default:
			return collected
		}
	}
}

func assertEventSequence(t *testing.T, events []RuntimeEvent, expected []EventType) {
	t.Helper()
	for _, eventType := range expected {
		found := false
		for _, event := range events {
			if event.Type == eventType {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected event %q in %+v", eventType, events)
		}
	}
}

func cloneSession(session Session) Session {
	cloned := session
	cloned.Messages = append([]provider.Message(nil), session.Messages...)
	return cloned
}

func cloneChatRequest(req provider.ChatRequest) provider.ChatRequest {
	cloned := req
	cloned.Messages = append([]provider.Message(nil), req.Messages...)
	cloned.Tools = append([]provider.ToolSpec(nil), req.Tools...)
	return cloned
}

func containsError(err error, target string) bool {
	return err != nil && strings.Contains(err.Error(), target)
}
