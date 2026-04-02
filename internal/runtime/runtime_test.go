package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	"neo-code/internal/tools"
)

type memoryStore struct {
	sessions map[string]Session
	saves    int
}

type failingStore struct {
	Store
	saveErr          error
	failOnSave       int
	saveCalls        int
	ignoreContextErr bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: map[string]Session{}}
}

func (s *failingStore) Save(ctx context.Context, session *Session) error {
	s.saveCalls++
	if s.failOnSave > 0 && s.saveCalls == s.failOnSave {
		return s.saveErr
	}
	if s.ignoreContextErr && s.saveErr != nil {
		return s.saveErr
	}
	if s.Store == nil {
		return nil
	}
	return s.Store.Save(ctx, session)
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
	chatFn    func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error)
}

func (p *scriptedProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	p.requests = append(p.requests, cloneChatRequest(req))

	callIndex := p.callCount
	p.callCount++

	if p.chatFn != nil {
		return p.chatFn(ctx, req, events)
	}

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

func (f *scriptedProviderFactory) Build(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
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
	executeFn func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error)
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
	if t.executeFn != nil {
		return t.executeFn(ctx, input)
	}
	if input.EmitChunk != nil {
		input.EmitChunk([]byte("chunk"))
	}
	return tools.ToolResult{
		Name:    t.name,
		Content: t.content,
		IsError: t.isError,
	}, t.err
}

type stubContextBuilder struct {
	buildFn   func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error)
	callCount int
	lastInput agentcontext.BuildInput
	builds    []agentcontext.BuildInput
}

func (b *stubContextBuilder) Build(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
	b.callCount++
	b.lastInput = cloneBuildInput(input)
	b.builds = append(b.builds, cloneBuildInput(input))
	if b.buildFn != nil {
		return b.buildFn(ctx, input)
	}
	return agentcontext.BuildResult{
		SystemPrompt: "stub system prompt",
		Messages:     append([]provider.Message(nil), input.Messages...),
	}, nil
}

type stubToolManager struct {
	specs        []provider.ToolSpec
	result       tools.ToolResult
	err          error
	listErr      error
	listCalls    int
	executeCalls int
	lastInput    tools.ToolCallInput
}

func (m *stubToolManager) ListAvailableSpecs(ctx context.Context, input tools.SpecListInput) ([]provider.ToolSpec, error) {
	m.listCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]provider.ToolSpec(nil), m.specs...), nil
}

func (m *stubToolManager) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	m.executeCalls++
	m.lastInput = input
	result := m.result
	if result.Name == "" {
		result.Name = input.Name
	}
	return result, m.err
}

func TestServiceRun(t *testing.T) {
	tests := []struct {
		name                string
		input               UserInput
		providerResponses   []provider.ChatResponse
		providerStreams     [][]provider.StreamEvent
		registerTool        tools.Tool
		contextBuilder      agentcontext.Builder
		expectProviderCalls int
		expectToolCalls     int
		expectMessageRoles  []string
		expectEventTypes    []EventType
		assert              func(t *testing.T, store *memoryStore, provider *scriptedProvider, tool *stubTool)
	}{
		{
			name:  "normal dialogue exits after final assistant reply",
			input: UserInput{RunID: "run-normal", Content: "hello"},
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
			contextBuilder: &stubContextBuilder{
				buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
					return agentcontext.BuildResult{
						SystemPrompt: "custom system prompt",
						Messages: []provider.Message{
							{Role: "user", Content: "trimmed history"},
						},
					}, nil
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
				if scripted.requests[0].SystemPrompt != "custom system prompt" {
					t.Fatalf("expected system prompt from context builder, got %q", scripted.requests[0].SystemPrompt)
				}
				if len(scripted.requests[0].Messages) != 1 || scripted.requests[0].Messages[0].Content != "trimmed history" {
					t.Fatalf("expected messages from context builder, got %+v", scripted.requests[0].Messages)
				}
			},
		},
		{
			name:  "tool call triggers execute and follow-up provider round",
			input: UserInput{RunID: "run-tool", Content: "edit file"},
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
				responses: tt.providerResponses,
				streams:   tt.providerStreams,
			}
			factory := &scriptedProviderFactory{provider: scripted}

			service := NewWithFactory(manager, registry, store, factory, tt.contextBuilder)
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
			assertEventsRunID(t, events, tt.input.RunID)

			if tt.assert != nil {
				tt.assert(t, store, scripted, registeredTool)
			}
		})
	}
}

func TestServiceRunDelegatesToContextBuilder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "delegated prompt",
				Messages: []provider.Message{
					{Role: "user", Content: "delegated message"},
				},
			}, nil
		},
	}

	scripted := &scriptedProvider{
		responses: []provider.ChatResponse{
			{
				Message: provider.Message{
					Role:    "assistant",
					Content: "done",
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	input := UserInput{RunID: "run-context-builder", Content: "hello"}
	if err := service.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if builder.callCount != 1 {
		t.Fatalf("expected builder to be called once, got %d", builder.callCount)
	}
	if builder.lastInput.Metadata.Workdir == "" {
		t.Fatalf("expected workdir to be forwarded to builder metadata")
	}
	if builder.lastInput.Metadata.Shell == "" {
		t.Fatalf("expected shell to be forwarded to builder metadata")
	}
	if builder.lastInput.Metadata.Provider == "" {
		t.Fatalf("expected provider to be forwarded to builder metadata")
	}
	if builder.lastInput.Metadata.Model == "" {
		t.Fatalf("expected model to be forwarded to builder metadata")
	}
	if len(builder.lastInput.Messages) != 1 || builder.lastInput.Messages[0].Content != "hello" {
		t.Fatalf("expected persisted session messages to be forwarded, got %+v", builder.lastInput.Messages)
	}
	if len(scripted.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(scripted.requests))
	}
	if scripted.requests[0].SystemPrompt != "delegated prompt" {
		t.Fatalf("expected delegated prompt, got %q", scripted.requests[0].SystemPrompt)
	}
	if len(scripted.requests[0].Messages) != 1 || scripted.requests[0].Messages[0].Content != "delegated message" {
		t.Fatalf("expected delegated messages, got %+v", scripted.requests[0].Messages)
	}
}

func TestServiceRunUsesToolManager(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		specs: []provider.ToolSpec{
			{Name: "filesystem_edit", Description: "stub", Schema: map[string]any{"type": "object"}},
		},
		result: tools.ToolResult{
			Name:    "filesystem_edit",
			Content: "tool manager output",
		},
	}

	scripted := &scriptedProvider{
		responses: []provider.ChatResponse{
			{
				Message: provider.Message{
					Role: "assistant",
					ToolCalls: []provider.ToolCall{
						{ID: "call-manager", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
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
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{RunID: "run-tool-manager", Content: "edit file"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if toolManager.listCalls != 2 {
		t.Fatalf("expected 2 spec list calls, got %d", toolManager.listCalls)
	}
	if toolManager.executeCalls != 1 {
		t.Fatalf("expected 1 execute call, got %d", toolManager.executeCalls)
	}
	if toolManager.lastInput.ID != "call-manager" {
		t.Fatalf("expected forwarded tool call id, got %q", toolManager.lastInput.ID)
	}
	if len(scripted.requests) == 0 || len(scripted.requests[0].Tools) != 1 || scripted.requests[0].Tools[0].Name != "filesystem_edit" {
		t.Fatalf("expected tool specs from tool manager, got %+v", scripted.requests)
	}

	session := onlySession(t, store)
	foundToolMessage := false
	for _, message := range session.Messages {
		if message.Role == provider.RoleTool && message.Content == "tool manager output" {
			foundToolMessage = true
			break
		}
	}
	if !foundToolMessage {
		t.Fatalf("expected tool manager result in session messages, got %+v", session.Messages)
	}
}

func TestServiceRunHandlesToolManagerSpecError(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		listErr: errors.New("tool specs unavailable"),
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{
		provider: &scriptedProvider{},
	}, nil)
	input := UserInput{RunID: "run-tool-spec-error", Content: "hello"}
	err := service.Run(context.Background(), input)
	if err == nil || !containsError(err, "tool specs unavailable") {
		t.Fatalf("expected tool spec error, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventError})
	assertNoEventType(t, events, EventAgentDone)
	assertEventsRunID(t, events, input.RunID)

	session := onlySession(t, store)
	if len(session.Messages) != 1 || session.Messages[0].Role != provider.RoleUser {
		t.Fatalf("expected only user message to persist, got %+v", session.Messages)
	}
}

func TestServiceNewWithFactoryDefaultsToolManager(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{
		provider: &scriptedProvider{
			responses: []provider.ChatResponse{
				{
					Message: provider.Message{
						Role:    provider.RoleAssistant,
						Content: "done",
					},
					FinishReason: "stop",
				},
			},
		},
	}, nil)

	if err := service.Run(context.Background(), UserInput{RunID: "run-default-tool-manager", Content: "hello"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventAgentDone})
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
			input:    UserInput{RunID: "run-max-loops", Content: "loop"},
			maxLoops: 1,
			provider: &scriptedProvider{
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
			input:      UserInput{RunID: "run-factory-error", Content: "hello"},
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
				RunID:     "run-existing-session",
				Content:   "continue",
			},
			provider: &scriptedProvider{
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
		{
			name:  "retryable provider error triggers runtime retry then succeeds",
			input: UserInput{RunID: "run-retry-success", Content: "hello"},
			provider: func() *scriptedProvider {
				callIdx := 0
				return &scriptedProvider{
					name: "retry-then-success",
					chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
						callIdx++
						if callIdx == 1 {
							return provider.ChatResponse{}, &provider.ProviderError{
								StatusCode: 500,
								Code:       provider.ErrorCodeServer,
								Message:    "internal server error",
								Retryable:  true,
							}
						}
						return provider.ChatResponse{
							Message: provider.Message{
								Role:    "assistant",
								Content: "recovered",
							},
							FinishReason: "stop",
						}, nil
					},
				}
			}(),
			expectEvents: []EventType{EventUserMessage, EventProviderRetry, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if scripted.callCount < 2 {
					t.Fatalf("expected at least 2 provider calls (initial + retry), got %d", scripted.callCount)
				}
				session := onlySession(t, store)
				if len(session.Messages) != 2 {
					t.Fatalf("expected user + assistant messages, got %d", len(session.Messages))
				}
				if session.Messages[1].Content != "recovered" {
					t.Fatalf("expected assistant content %q, got %q", "recovered", session.Messages[1].Content)
				}
			},
		},
		{
			name:  "non-retryable provider error does not trigger runtime retry",
			input: UserInput{RunID: "run-no-retry", Content: "hello"},
			provider: &scriptedProvider{
				name: "auth-error-no-retry",
				chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
					return provider.ChatResponse{}, &provider.ProviderError{
						StatusCode: 401,
						Code:       provider.ErrorCodeAuthFailed,
						Message:    "invalid api key",
						Retryable:  false,
					}
				},
			},
			expectErr:    "invalid api key",
			expectEvents: []EventType{EventUserMessage, EventError},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if scripted.callCount != 1 {
					t.Fatalf("expected exactly 1 provider call (no retry for 401), got %d", scripted.callCount)
				}
			},
		},
		{
			name:  "runtime retry exhausted emits error",
			input: UserInput{RunID: "run-retry-exhausted", Content: "hello"},
			provider: &scriptedProvider{
				name: "always-500",
				chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
					return provider.ChatResponse{}, &provider.ProviderError{
						StatusCode: 500,
						Code:       provider.ErrorCodeServer,
						Message:    "internal server error",
						Retryable:  true,
					}
				},
			},
			expectErr:    "internal server error",
			expectEvents: []EventType{EventUserMessage, EventProviderRetry, EventError},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				// 1 initial + 2 retries = 3 calls
				if scripted.callCount != defaultProviderRetryMax+1 {
					t.Fatalf("expected %d provider calls (1 initial + %d retries), got %d",
						defaultProviderRetryMax+1, defaultProviderRetryMax, scripted.callCount)
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

			service := NewWithFactory(manager, registry, store, factory, nil)
			err := service.Run(context.Background(), tt.input)
			if tt.expectErr != "" {
				if err == nil || err.Error() != tt.expectErr && !containsError(err, tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.expectEvents) > 0 {
				events := collectRuntimeEvents(service.Events())
				assertEventSequence(t, events, tt.expectEvents)
				if tt.input.RunID != "" {
					assertEventsRunID(t, events, tt.input.RunID)
				}
			}
			if tt.assert != nil {
				tt.assert(t, store, tt.provider, tt.registerTool)
			}
		})
	}
}

func TestServiceCancelActiveRun(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	started := make(chan struct{})
	scripted := &scriptedProvider{
		name: "cancel-active-run-provider",
		chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
			close(started)
			<-ctx.Done()
			return provider.ChatResponse{}, ctx.Err()
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-cancel-active", Content: "hello"}

	go func() {
		errCh <- service.Run(context.Background(), input)
	}()

	<-started
	if !service.CancelActiveRun() {
		t.Fatalf("expected active run cancel to return true")
	}

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if service.CancelActiveRun() {
		t.Fatalf("expected no active run after cancellation")
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventRunCanceled})
	assertNoEventType(t, events, EventError)
	assertEventsRunID(t, events, input.RunID)
}

func TestServiceRunCanceledByProvider(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	started := make(chan struct{})
	scripted := &scriptedProvider{
		name: "blocking-provider",
		chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
			close(started)
			<-ctx.Done()
			return provider.ChatResponse{}, ctx.Err()
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-provider-cancel", Content: "hello"}

	go func() {
		errCh <- service.Run(ctx, input)
	}()

	<-started
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventRunCanceled})
	assertNoEventType(t, events, EventError)
	assertEventsRunID(t, events, input.RunID)

	session := onlySession(t, store)
	if len(session.Messages) != 1 || session.Messages[0].Role != "user" {
		t.Fatalf("expected only the user message to persist, got %+v", session.Messages)
	}
}

func TestServiceRunPreservesProviderErrorAfterCancel(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	started := make(chan struct{})
	providerErr := errors.New("provider failed after cancel")
	scripted := &scriptedProvider{
		name: "provider-error-after-cancel",
		chatFn: func(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
			close(started)
			<-ctx.Done()
			return provider.ChatResponse{}, providerErr
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-provider-error-after-cancel", Content: "hello"}

	go func() {
		errCh <- service.Run(ctx, input)
	}()

	<-started
	cancel()

	err := <-errCh
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error %q, got %v", providerErr, err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventError})
	assertNoEventType(t, events, EventRunCanceled)
	assertEventsRunID(t, events, input.RunID)
}

func TestServiceRunCanceledDuringToolExecution(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	toolStarted := make(chan struct{})
	blockingTool := &stubTool{
		name: "filesystem_edit",
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if input.EmitChunk != nil {
				input.EmitChunk([]byte("chunk"))
			}
			close(toolStarted)
			<-ctx.Done()
			return tools.ToolResult{Name: "filesystem_edit"}, ctx.Err()
		},
	}
	registry.Register(blockingTool)

	scripted := &scriptedProvider{
		name: "tool-cancel-provider",
		responses: []provider.ChatResponse{
			{
				Message: provider.Message{
					Role: "assistant",
					ToolCalls: []provider.ToolCall{
						{ID: "cancel-call", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-tool-cancel", Content: "edit file"}

	go func() {
		errCh <- service.Run(ctx, input)
	}()

	<-toolStarted
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventRunCanceled})
	assertNoEventType(t, events, EventToolResult)
	assertNoEventType(t, events, EventError)
	assertEventsRunID(t, events, input.RunID)

	session := onlySession(t, store)
	if len(session.Messages) != 2 {
		t.Fatalf("expected user and assistant tool-call messages before cancel, got %+v", session.Messages)
	}
}

func TestServiceRunPreservesToolErrorAfterCancel(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	toolStarted := make(chan struct{})
	toolErr := errors.New("tool failed after cancel")
	blockingTool := &stubTool{
		name: "filesystem_edit",
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if input.EmitChunk != nil {
				input.EmitChunk([]byte("chunk"))
			}
			close(toolStarted)
			<-ctx.Done()
			return tools.ToolResult{Name: "filesystem_edit"}, toolErr
		},
	}
	registry.Register(blockingTool)

	scripted := &scriptedProvider{
		name: "tool-error-after-cancel-provider",
		responses: []provider.ChatResponse{
			{
				Message: provider.Message{
					Role: "assistant",
					ToolCalls: []provider.ToolCall{
						{ID: "tool-error-call", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-tool-error-after-cancel", Content: "edit file"}

	go func() {
		errCh <- service.Run(ctx, input)
	}()

	<-toolStarted
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled after tool error is preserved, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventToolResult, EventRunCanceled})
	assertNoEventType(t, events, EventError)
	assertEventsRunID(t, events, input.RunID)

	session := onlySession(t, store)
	if len(session.Messages) != 2 {
		t.Fatalf("expected user and assistant tool-call messages to persist before cancel, got %+v", session.Messages)
	}
}

func TestServiceRunPreservesSessionSaveErrorAfterCancel(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	baseStore := newMemoryStore()
	saveErr := errors.New("session save failed")
	store := &failingStore{
		Store:            baseStore,
		saveErr:          saveErr,
		failOnSave:       1,
		ignoreContextErr: true,
	}
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{
		provider: &scriptedProvider{name: "unused"},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := UserInput{RunID: "run-save-error-after-cancel", Content: "hello"}
	err := service.Run(ctx, input)
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected save error %q, got %v", saveErr, err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventError})
	assertNoEventType(t, events, EventRunCanceled)
	assertEventsRunID(t, events, input.RunID)
}

func TestServiceRunToolTimeoutIsNotCancellation(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.ToolTimeoutSec = 0
		return nil
	}); err != nil {
		t.Fatalf("update tool timeout: %v", err)
	}

	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	timeoutTool := &stubTool{
		name: "filesystem_edit",
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			<-ctx.Done()
			return tools.ToolResult{Name: "filesystem_edit"}, ctx.Err()
		},
	}
	registry.Register(timeoutTool)

	scripted := &scriptedProvider{
		name: "timeout-provider",
		responses: []provider.ChatResponse{
			{
				Message: provider.Message{
					Role: "assistant",
					ToolCalls: []provider.ToolCall{
						{ID: "timeout-call", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: provider.Message{
					Role:    "assistant",
					Content: "done after timeout",
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	input := UserInput{RunID: "run-tool-timeout", Content: "edit file"}
	if err := service.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolResult, EventAgentDone})
	assertNoEventType(t, events, EventRunCanceled)
	assertEventsRunID(t, events, input.RunID)

	session := onlySession(t, store)
	if len(session.Messages) != 4 {
		t.Fatalf("expected user, assistant, tool, assistant messages, got %+v", session.Messages)
	}
	if !session.Messages[2].IsError {
		t.Fatalf("expected timed out tool result to be marked as error")
	}
}

func TestServiceConstructorsAndDelegates(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	service := NewWithFactory(manager, registry, store, nil, nil)
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

func newRuntimeConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	restoreRuntimeEnv(t, config.OpenAIDefaultAPIKeyEnv)
	if err := os.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	manager := config.NewManager(config.NewLoader(t.TempDir(), builtin.DefaultConfig()))
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

func restoreRuntimeEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	t.Cleanup(func() {
		if !ok {
			_ = os.Unsetenv(key)
			return
		}
		_ = os.Setenv(key, value)
	})
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

func assertNoEventType(t *testing.T, events []RuntimeEvent, unexpected EventType) {
	t.Helper()
	for _, event := range events {
		if event.Type == unexpected {
			t.Fatalf("did not expect event %q in %+v", unexpected, events)
		}
	}
}

func assertEventsRunID(t *testing.T, events []RuntimeEvent, runID string) {
	t.Helper()
	for _, event := range events {
		if event.RunID != runID {
			t.Fatalf("expected run id %q, got %+v", runID, events)
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

func cloneBuildInput(input agentcontext.BuildInput) agentcontext.BuildInput {
	cloned := input
	cloned.Messages = append([]provider.Message(nil), input.Messages...)
	return cloned
}

func containsError(err error, target string) bool {
	return err != nil && strings.Contains(err.Error(), target)
}

func TestIsRetryableProviderError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"retryable provider error", &provider.ProviderError{Retryable: true}, true},
		{"non-retryable provider error", &provider.ProviderError{Retryable: false}, false},
		{"plain error", errors.New("something failed"), false},
		{"wrapped retryable", fmt.Errorf("wrapped: %w", &provider.ProviderError{Retryable: true}), true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryableProviderError(tt.err); got != tt.want {
				t.Fatalf("isRetryableProviderError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		attempt int
		min     time.Duration
		max     time.Duration
	}{
		{
			name:    "first retry stays within jittered base window",
			attempt: 1,
			min:     500 * time.Millisecond,
			max:     1500 * time.Millisecond,
		},
		{
			name:    "second retry stays within jittered doubled window",
			attempt: 2,
			min:     1 * time.Second,
			max:     3 * time.Second,
		},
		{
			name:    "large retry is capped at max wait",
			attempt: 20,
			min:     providerRetryMaxWait,
			max:     providerRetryMaxWait,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := providerRetryBackoff(tt.attempt)
			if got < tt.min || got > tt.max {
				t.Fatalf("providerRetryBackoff(%d) = %v, want within [%v, %v]", tt.attempt, got, tt.min, tt.max)
			}
		})
	}
}
