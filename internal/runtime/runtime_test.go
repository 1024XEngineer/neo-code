package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type memoryStore struct {
	sessions map[string]agentsession.Session
	saves    int
}

type failingStore struct {
	agentsession.Store
	saveErr          error
	failOnSave       int
	saveCalls        int
	ignoreContextErr bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: map[string]agentsession.Session{}}
}

func (s *failingStore) Save(ctx context.Context, session *agentsession.Session) error {
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

func (s *memoryStore) Save(ctx context.Context, session *agentsession.Session) error {
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

func (s *memoryStore) Load(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	session, ok := s.sessions[id]
	if !ok {
		return agentsession.Session{}, errors.New("not found")
	}
	return cloneSession(session), nil
}

func (s *memoryStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	summaries := make([]agentsession.Summary, 0, len(s.sessions))
	for _, session := range s.sessions {
		summaries = append(summaries, agentsession.Summary{
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
	streams   [][]providertypes.StreamEvent
	responses []scriptedResponse
	requests  []providertypes.ChatRequest
	callCount int
	chatFn    func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error
}

type scriptedResponse struct {
	Message      providertypes.Message
	FinishReason string
}

func (p *scriptedProvider) Chat(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
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
				return ctx.Err()
			}
		}
		if callIndex >= len(p.responses) && !streamContainsMessageDone(p.streams[callIndex]) {
			select {
			case events <- providertypes.NewMessageDoneStreamEvent("", nil):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if callIndex < len(p.responses) {
		response := p.responses[callIndex]
		for index, toolCall := range response.Message.ToolCalls {
			select {
			case events <- providertypes.NewToolCallStartStreamEvent(index, toolCall.ID, toolCall.Name):
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case events <- providertypes.NewToolCallDeltaStreamEvent(index, toolCall.ID, toolCall.Arguments):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if response.Message.Content != "" {
			select {
			case events <- providertypes.NewTextDeltaStreamEvent(response.Message.Content):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case events <- providertypes.NewMessageDoneStreamEvent(response.FinishReason, nil):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// streamContainsMessageDone 判断测试流中是否已显式包含结束事件，避免辅助 provider 重复补发 message_done。
func streamContainsMessageDone(events []providertypes.StreamEvent) bool {
	for _, event := range events {
		if event.Type == providertypes.StreamEventMessageDone {
			return true
		}
	}
	return false
}

type scriptedProviderFactory struct {
	provider provider.Provider
	calls    int
	configs  []config.ResolvedProviderConfig
	err      error
}

func (f *scriptedProviderFactory) Build(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
	f.calls++
	f.configs = append(f.configs, cfg)
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
	policy    tools.MicroCompactPolicy
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

func (t *stubTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return t.policy
}

func (t *stubTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	t.callCount++
	t.lastInput = input
	if t.executeFn != nil {
		return t.executeFn(ctx, input)
	}
	if input.EmitChunk != nil {
		if err := input.EmitChunk([]byte("chunk")); err != nil {
			return tools.NewErrorResult(t.name, "emit failed", "", nil), err
		}
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
		Messages:     append([]providertypes.Message(nil), input.Messages...),
	}, nil
}

type stubToolManager struct {
	specs        []providertypes.ToolSpec
	result       tools.ToolResult
	err          error
	listErr      error
	policies     map[string]tools.MicroCompactPolicy
	listCalls    int
	executeCalls int
	lastInput    tools.ToolCallInput
	rememberErr  error
	remembered   []struct {
		sessionID string
		action    security.Action
		scope     tools.SessionPermissionScope
	}
}

func (m *stubToolManager) ListAvailableSpecs(ctx context.Context, input tools.SpecListInput) ([]providertypes.ToolSpec, error) {
	m.listCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]providertypes.ToolSpec(nil), m.specs...), nil
}

func (m *stubToolManager) MicroCompactPolicy(name string) tools.MicroCompactPolicy {
	if policy, ok := m.policies[name]; ok {
		return policy
	}
	return tools.MicroCompactPolicyCompact
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

func (m *stubToolManager) RememberSessionDecision(sessionID string, action security.Action, scope tools.SessionPermissionScope) error {
	m.remembered = append(m.remembered, struct {
		sessionID string
		action    security.Action
		scope     tools.SessionPermissionScope
	}{
		sessionID: sessionID,
		action:    action,
		scope:     scope,
	})
	return m.rememberErr
}

func TestServiceRun(t *testing.T) {
	tests := []struct {
		name                string
		input               UserInput
		providerStreams     [][]providertypes.StreamEvent
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
			providerStreams: [][]providertypes.StreamEvent{
				{
					providertypes.NewTextDeltaStreamEvent("plain "),
					providertypes.NewTextDeltaStreamEvent("answer"),
				},
			},
			contextBuilder: &stubContextBuilder{
				buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
					return agentcontext.BuildResult{
						SystemPrompt: "custom system prompt",
						Messages: []providertypes.Message{
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
			// 第一轮：工具调用事件流（tool_call_start + tool_call_delta）
			// 第二轮：普通文本回复
			providerStreams: [][]providertypes.StreamEvent{
				{
					providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_edit"),
					providertypes.NewToolCallDeltaStreamEvent(0, "call-1", `{"path":"main.go"}`),
				},
				{
					providertypes.NewTextDeltaStreamEvent("done"),
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
				streams: tt.providerStreams,
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

func TestServiceRunMergesLateToolCallMetadata(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	tool := &stubTool{name: "filesystem_edit", content: "tool output"}
	registry := tools.NewRegistry()
	registry.Register(tool)

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallDeltaStreamEvent(0, "", `{"path":"main.go"`),
				providertypes.NewToolCallStartStreamEvent(0, "call-late", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-late", `}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	if err := service.Run(context.Background(), UserInput{RunID: "run-late-tool-metadata", Content: "edit"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if tool.callCount != 1 {
		t.Fatalf("expected tool to execute once, got %d", tool.callCount)
	}
	if tool.lastInput.ID != "call-late" {
		t.Fatalf("expected merged tool call id %q, got %q", "call-late", tool.lastInput.ID)
	}
	if tool.lastInput.Name != "filesystem_edit" {
		t.Fatalf("expected merged tool name %q, got %q", "filesystem_edit", tool.lastInput.Name)
	}
	if got := string(tool.lastInput.Arguments); got != `{"path":"main.go"}` {
		t.Fatalf("expected merged tool arguments %q, got %q", `{"path":"main.go"}`, got)
	}

	session := onlySession(t, store)
	if len(session.Messages) < 3 {
		t.Fatalf("expected assistant/tool follow-up messages, got %+v", session.Messages)
	}
	if len(session.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected persisted assistant tool call, got %+v", session.Messages[1])
	}
	if session.Messages[1].ToolCalls[0].ID != "call-late" || session.Messages[1].ToolCalls[0].Name != "filesystem_edit" {
		t.Fatalf("expected merged assistant tool call metadata, got %+v", session.Messages[1].ToolCalls[0])
	}
	if session.Messages[2].ToolCallID != "call-late" {
		t.Fatalf("expected tool result to reference merged tool call id, got %+v", session.Messages[2])
	}
}

func TestServiceRunRejectsToolCallWithoutID(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	tool := &stubTool{name: "filesystem_edit", content: "tool output"}
	registry := tools.NewRegistry()
	registry.Register(tool)

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "", `{}`),
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	err := service.Run(context.Background(), UserInput{RunID: "run-missing-tool-id", Content: "edit"})
	if err == nil || !containsError(err, "without id") {
		t.Fatalf("expected missing tool id error, got %v", err)
	}
	if tool.callCount != 0 {
		t.Fatalf("expected tool execution to be blocked, got %d calls", tool.callCount)
	}
}

func TestServiceRunRejectsMalformedProviderStreamEvent(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				{Type: providertypes.StreamEventTextDelta},
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	err := service.Run(context.Background(), UserInput{RunID: "run-malformed-stream-event", Content: "hello"})
	if err == nil || !containsError(err, "text_delta event payload is nil") {
		t.Fatalf("expected malformed stream event error, got %v", err)
	}
}

func TestServiceRunRejectsProviderCompletionWithoutMessageDone(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("partial"):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	err := service.Run(context.Background(), UserInput{RunID: "run-missing-message-done", Content: "hello"})
	if err == nil || !containsError(err, "without message_done") {
		t.Fatalf("expected missing message_done error, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventAgentChunk, EventError})
	assertNoEventType(t, events, EventAgentDone)

	session := onlySession(t, store)
	if len(session.Messages) != 1 || session.Messages[0].Role != providertypes.RoleUser {
		t.Fatalf("expected only user message to persist after missing message_done, got %+v", session.Messages)
	}
}

func TestServiceRunMalformedProviderStreamEventDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	stream := []providertypes.StreamEvent{{Type: providertypes.StreamEventTextDelta}}
	for i := 0; i < 40; i++ {
		stream = append(stream, providertypes.NewTextDeltaStreamEvent("ignored"))
	}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{stream},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.Run(context.Background(), UserInput{RunID: "run-malformed-stream-no-deadlock", Content: "hello"})
	}()

	select {
	case err := <-errCh:
		if err == nil || !containsError(err, "text_delta event payload is nil") {
			t.Fatalf("expected malformed stream event error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected run to fail instead of deadlocking on malformed stream event")
	}
}

type stubCompactRunner struct {
	runFn  func(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error)
	calls  []contextcompact.Input
	result contextcompact.Result
	err    error
}

func (r *stubCompactRunner) Run(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error) {
	cloned := input
	cloned.Messages = append([]providertypes.Message(nil), input.Messages...)
	r.calls = append(r.calls, cloned)
	if r.runFn != nil {
		return r.runFn(ctx, input)
	}
	return r.result, r.err
}

func TestServiceRunDelegatesToContextBuilder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("memory reject")
	session.ID = "session-memory-reject"
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "delegated prompt",
				Messages: []providertypes.Message{
					{Role: "user", Content: "delegated message"},
				},
			}, nil
		},
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("done")},
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
	if builder.lastInput.Compact.DisableMicroCompact {
		t.Fatalf("expected micro compact to stay enabled by default")
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

func TestServiceRunCanDisableMicroCompactViaConfig(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Context.Compact.MicroCompactDisabled = true
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "delegated prompt",
				Messages:     append([]providertypes.Message(nil), input.Messages...),
			}, nil
		},
	}

	scripted := &scriptedProvider{
		responses: []scriptedResponse{{
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Content: "done"},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	if err := service.Run(context.Background(), UserInput{RunID: "run-disable-micro-compact", Content: "hello"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !builder.lastInput.Compact.DisableMicroCompact {
		t.Fatalf("expected config to disable micro compact in build input")
	}
}

func TestServiceRunPersistsSessionProviderAndModel(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{RunID: "run-session-provider-model", Content: "hello"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	session := onlySession(t, store)
	cfg := manager.Get()
	if session.Provider != cfg.SelectedProvider {
		t.Fatalf("expected session provider %q, got %q", cfg.SelectedProvider, session.Provider)
	}
	if session.Model != cfg.CurrentModel {
		t.Fatalf("expected session model %q, got %q", cfg.CurrentModel, session.Model)
	}
}

func TestServiceRunDefaultBuilderUsesToolManagerMicroCompactPolicies(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "preserve_tool", content: "default", policy: tools.MicroCompactPolicyPreserveHistory})
	registry.Register(&stubTool{name: "bash", content: "default"})
	registry.Register(&stubTool{name: "webfetch", content: "default"})

	session := agentsession.New("preserve history")
	session.ID = "session-preserve-history"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older user"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "preserve_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Content: "preserved result"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Content: "recent bash result"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Content: "latest webfetch result"},
	}
	store.sessions[session.ID] = cloneSession(session)

	scripted := &scriptedProvider{
		responses: []scriptedResponse{{
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Content: "done"},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-preserve-history-policy",
		Content:   "latest explicit instruction",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(scripted.requests))
	}
	if got := scripted.requests[0].Messages[2].Content; got != "preserved result" {
		t.Fatalf("expected preserved tool result to remain visible, got %q", got)
	}
}

func TestServiceRunDefaultBuilderUsesGenericToolManagerMicroCompactPolicies(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		policies: map[string]tools.MicroCompactPolicy{
			"preserve_tool": tools.MicroCompactPolicyPreserveHistory,
		},
	}

	session := agentsession.New("preserve history by manager")
	session.ID = "session-preserve-history-manager"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older user"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "preserve_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Content: "preserved result"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Content: "recent bash result"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Content: "latest webfetch result"},
	}
	store.sessions[session.ID] = cloneSession(session)

	scripted := &scriptedProvider{
		responses: []scriptedResponse{{
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Content: "done"},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-preserve-history-generic-manager",
		Content:   "latest explicit instruction",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(scripted.requests))
	}
	if got := scripted.requests[0].Messages[2].Content; got != "preserved result" {
		t.Fatalf("expected preserved tool result to remain visible, got %q", got)
	}
}

func TestServiceRunFailurePreservesExistingSessionProviderAndModel(t *testing.T) {
	t.Parallel()

	geminiEnv := runtimeTestAPIKeyEnv(t) + "_GEMINI"
	manager := newRuntimeConfigManagerWithProviderEnvs(t, map[string]string{
		config.GeminiName: geminiEnv,
	})
	setRuntimeProviderEnv(t, geminiEnv, "gemini-key")
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = config.GeminiName
		cfg.CurrentModel = "gemini-current-model"
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("preserve-metadata")
	session.ID = "session-preserve-metadata"
	session.Provider = config.OpenAIName
	session.Model = "openai-original-model"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "earlier"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{
		err: errors.New("factory failed"),
	}, nil)
	err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-preserve-metadata",
		Content:   "continue",
	})
	if err == nil || !containsError(err, "factory failed") {
		t.Fatalf("expected factory failure, got %v", err)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if saved.Provider != config.OpenAIName {
		t.Fatalf("expected provider to remain %q, got %q", config.OpenAIName, saved.Provider)
	}
	if saved.Model != "openai-original-model" {
		t.Fatalf("expected model to remain %q, got %q", "openai-original-model", saved.Model)
	}
	if len(saved.Messages) != 2 || saved.Messages[1].Content != "continue" {
		t.Fatalf("expected failed run to append only user message, got %+v", saved.Messages)
	}
}

func TestServiceRunUsesToolManager(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "filesystem_edit", Description: "stub", Schema: map[string]any{"type": "object"}},
		},
		result: tools.ToolResult{
			Name:    "filesystem_edit",
			Content: "tool manager output",
		},
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-manager", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-manager", `{"path":"main.go"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
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
		if message.Role == providertypes.RoleTool && message.Content == "tool manager output" {
			foundToolMessage = true
			break
		}
	}
	if !foundToolMessage {
		t.Fatalf("expected tool manager result in session messages, got %+v", session.Messages)
	}
}

func TestServiceRunWaitsForPermissionResolutionAndContinues(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("memory reject")
	session.ID = "session-memory-reject"
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	tool := &stubTool{name: "webfetch", content: "fetched"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:       "ask-webfetch",
			Type:     security.ActionTypeRead,
			Resource: "webfetch",
			Decision: security.DecisionAsk,
			Reason:   "requires approval",
		},
	})
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-ask", "webfetch"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-ask", `{"url":"https://example.com/private"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- service.Run(context.Background(), UserInput{RunID: "run-permission-ask", Content: "fetch private"})
	}()

	var requestPayload PermissionRequestPayload
	deadline := time.After(3 * time.Second)
waitRequest:
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting permission request event")
		case event := <-service.Events():
			if event.Type != EventPermissionRequest {
				continue
			}
			payload, ok := event.Payload.(PermissionRequestPayload)
			if !ok {
				t.Fatalf("expected PermissionRequestPayload, got %#v", event.Payload)
			}
			requestPayload = payload
			break waitRequest
		}
	}

	if strings.TrimSpace(requestPayload.RequestID) == "" {
		t.Fatalf("expected non-empty permission request id")
	}
	if strings.TrimSpace(requestPayload.RememberScope) != "" {
		t.Fatalf("expected empty remember scope for permission_request, got %q", requestPayload.RememberScope)
	}
	if requestPayload.ToolName != "webfetch" || requestPayload.Decision != "ask" {
		t.Fatalf("unexpected permission request payload: %+v", requestPayload)
	}

	if err := service.ResolvePermission(context.Background(), PermissionResolutionInput{
		RequestID: requestPayload.RequestID,
		Decision:  PermissionResolutionAllowSession,
	}); err != nil {
		t.Fatalf("ResolvePermission() error = %v", err)
	}
	if err := <-runErrCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if tool.callCount != 1 {
		t.Fatalf("expected allowed tool to execute once, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventPermissionResolved,
		EventToolResult,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventError)

	var resolvedPayload PermissionResolvedPayload
	for _, event := range events {
		switch event.Type {
		case EventPermissionResolved:
			payload, ok := event.Payload.(PermissionResolvedPayload)
			if !ok {
				t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
			}
			resolvedPayload = payload
		}
	}

	if resolvedPayload.ToolName != "webfetch" || resolvedPayload.Decision != "allow" {
		t.Fatalf("unexpected permission resolved payload: %+v", resolvedPayload)
	}
	if resolvedPayload.ResolvedAs != "approved" {
		t.Fatalf("expected resolved_as approved, got %+v", resolvedPayload)
	}
	if resolvedPayload.RememberScope != string(tools.SessionPermissionScopeAlways) {
		t.Fatalf("expected remember scope always_session, got %+v", resolvedPayload)
	}
}

func TestServiceRunEmitsPermissionResolvedForDeny(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	tool := &stubTool{name: "bash", content: "should-not-run"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:       "deny-bash",
			Type:     security.ActionTypeBash,
			Resource: "bash",
			Decision: security.DecisionDeny,
			Reason:   "bash denied",
		},
	})
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-deny", "bash"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-deny", `{"command":"echo hi"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{RunID: "run-permission-deny", Content: "run bash"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if tool.callCount != 0 {
		t.Fatalf("expected blocked tool not to execute, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventPermissionResolved,
		EventToolResult,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventPermissionRequest)
	assertNoEventType(t, events, EventError)

	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
		}
		if payload.ToolName != "bash" || payload.Decision != "deny" || payload.ResolvedAs != "denied" {
			t.Fatalf("unexpected permission resolved payload: %+v", payload)
		}
		if payload.RuleID != "deny-bash" {
			t.Fatalf("expected deny-bash rule id, got %+v", payload)
		}
		return
	}
	t.Fatalf("expected permission resolved event payload")
}

func TestServiceRunEmitsRememberScopeWhenSessionRejectMemoryHits(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("memory reject")
	session.ID = "session-memory-reject"
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	tool := &stubTool{name: "webfetch", content: "should-not-run"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:       "ask-webfetch",
			Type:     security.ActionTypeRead,
			Resource: "webfetch",
			Decision: security.DecisionAsk,
			Reason:   "requires approval",
		},
	})
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	toolManager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}
	if err := toolManager.RememberSessionDecision("session-memory-reject", security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			Operation:  "fetch",
			TargetType: security.TargetTypeURL,
			Target:     "https://example.com/private",
		},
	}, tools.SessionPermissionScopeReject); err != nil {
		t.Fatalf("remember session reject: %v", err)
	}

	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: "assistant",
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-memory-reject", Name: "webfetch", Arguments: `{"url":"https://example.com/private"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message:      providertypes.Message{Role: "assistant", Content: "done"},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{
		SessionID: "session-memory-reject",
		RunID:     "run-memory-reject",
		Content:   "fetch private",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if tool.callCount != 0 {
		t.Fatalf("expected remembered reject to skip tool execution, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})
	assertNoEventType(t, events, EventPermissionRequest)

	for _, event := range events {
		if event.Type != EventPermissionResolved {
			continue
		}
		payload, ok := event.Payload.(PermissionResolvedPayload)
		if !ok {
			t.Fatalf("expected PermissionResolvedPayload, got %#v", event.Payload)
		}
		if payload.RememberScope != string(tools.SessionPermissionScopeReject) {
			t.Fatalf("expected remember_scope reject, got %+v", payload)
		}
		return
	}
	t.Fatalf("expected permission resolved event payload")
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
	if len(session.Messages) != 1 || session.Messages[0].Role != providertypes.RoleUser {
		t.Fatalf("expected only user message to persist, got %+v", session.Messages)
	}
}

func TestServiceNewWithFactoryDefaultsToolManager(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{
		provider: &scriptedProvider{
			streams: [][]providertypes.StreamEvent{
				{providertypes.NewTextDeltaStreamEvent("done")},
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
		seedSession  *agentsession.Session
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
				streams: [][]providertypes.StreamEvent{
					{
						providertypes.NewToolCallStartStreamEvent(0, "loop-call", "filesystem_edit"),
						providertypes.NewToolCallDeltaStreamEvent(0, "loop-call", `{"path":"x"}`),
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
				streams: [][]providertypes.StreamEvent{
					{providertypes.NewTextDeltaStreamEvent("resumed")},
				},
			},
			seedSession: &agentsession.Session{
				ID:        "existing-session",
				Title:     "Resume Me",
				CreatedAt: agentsession.New("seed").CreatedAt,
				UpdatedAt: agentsession.New("seed").UpdatedAt,
				Messages: []providertypes.Message{
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
					chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
						callIdx++
						if callIdx == 1 {
							return &provider.ProviderError{
								StatusCode: 500,
								Code:       provider.ErrorCodeServer,
								Message:    "internal server error",
								Retryable:  true,
							}
						}
						events <- providertypes.NewTextDeltaStreamEvent("recovered")
						events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
						return nil
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
				chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
					return &provider.ProviderError{
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
				chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
					return &provider.ProviderError{
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
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
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
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
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
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return providerErr
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
				if err := input.EmitChunk([]byte("chunk")); err != nil {
					return tools.NewErrorResult(input.Name, "emit failed", "", nil), err
				}
			}
			close(toolStarted)
			<-ctx.Done()
			return tools.ToolResult{Name: "filesystem_edit"}, ctx.Err()
		},
	}
	registry.Register(blockingTool)

	scripted := &scriptedProvider{
		name: "tool-cancel-provider",
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "cancel-call", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "cancel-call", `{"path":"main.go"}`),
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
				if err := input.EmitChunk([]byte("chunk")); err != nil {
					return tools.NewErrorResult(input.Name, "emit failed", "", nil), err
				}
			}
			close(toolStarted)
			<-ctx.Done()
			return tools.ToolResult{Name: "filesystem_edit"}, toolErr
		},
	}
	registry.Register(blockingTool)

	scripted := &scriptedProvider{
		name: "tool-error-after-cancel-provider",
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "tool-error-call", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "tool-error-call", `{"path":"main.go"}`),
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
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "timeout-call", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "timeout-call", `{"path":"main.go"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done after timeout")},
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

func TestServiceCompactManualAppliesAndPersists(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("manual")
	session.ID = "session-manual"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
		{Role: providertypes.RoleUser, Content: "before"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Content: "[compact_summary]\ndone:\n- ok\n\nin_progress:\n- continue"},
				{Role: providertypes.RoleAssistant, Content: "latest"},
			},
			Applied: true,
			Metrics: contextcompact.Metrics{
				BeforeChars: 80,
				AfterChars:  30,
				SavedRatio:  0.625,
				TriggerMode: string(contextcompact.ModeManual),
			},
			TranscriptID:   "transcript_manual",
			TranscriptPath: "/tmp/manual.jsonl",
		},
	}

	result, err := service.Compact(context.Background(), CompactInput{
		SessionID: session.ID,
		RunID:     "run-manual",
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !result.Applied || result.TriggerMode != string(contextcompact.ModeManual) {
		t.Fatalf("unexpected compact result: %+v", result)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load compacted session: %v", err)
	}
	if len(saved.Messages) != 2 || !strings.Contains(saved.Messages[0].Content, "compact_summary") {
		t.Fatalf("expected persisted compacted messages, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactDone})
	assertEventsRunID(t, events, "run-manual")
}

func TestServiceCompactManualFailureReturnsError(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("manual-fail")
	session.ID = "session-manual-fail"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
		{Role: providertypes.RoleUser, Content: "before"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.compactRunner = &stubCompactRunner{err: errors.New("manual compact failed")}

	_, err := service.Compact(context.Background(), CompactInput{
		SessionID: session.ID,
		RunID:     "run-manual-fail",
	})
	if err == nil || !strings.Contains(err.Error(), "manual compact failed") {
		t.Fatalf("expected compact failure, got %v", err)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load original session: %v", err)
	}
	if len(saved.Messages) != 3 || saved.Messages[2].Content != "before" {
		t.Fatalf("expected original session untouched, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactError})
	assertNoEventType(t, events, EventCompactDone)
}

func TestServiceCompactUsesSessionProviderAndModelWhenPresent(t *testing.T) {
	t.Parallel()

	geminiEnv := runtimeTestAPIKeyEnv(t) + "_GEMINI"
	manager := newRuntimeConfigManagerWithProviderEnvs(t, map[string]string{
		config.GeminiName: geminiEnv,
	})
	setRuntimeProviderEnv(t, geminiEnv, "gemini-key")
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = config.GeminiName
		cfg.CurrentModel = "gemini-current-model"
		cfg.Context.Compact.ManualStrategy = config.CompactManualStrategyFullReplace
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("manual-provider")
	session.ID = "session-manual-provider"
	session.Provider = config.OpenAIName
	session.Model = "session-model"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
		{Role: providertypes.RoleUser, Content: "before"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(strings.Join([]string{
				"[compact_summary]",
				"done:",
				"- ok",
				"",
				"in_progress:",
				"- continue",
				"",
				"decisions:",
				"- kept existing provider and model",
				"",
				"code_changes:",
				"- none",
				"",
				"constraints:",
				"- none",
			}, "\n"))},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	service := NewWithFactory(manager, registry, store, factory, nil)

	result, err := service.Compact(context.Background(), CompactInput{
		SessionID: session.ID,
		RunID:     "run-manual-session-provider",
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compact to apply")
	}
	if len(factory.configs) != 1 || factory.configs[0].Name != config.OpenAIName {
		t.Fatalf("expected session provider config to be used, got %+v", factory.configs)
	}
	if len(scripted.requests) != 1 || scripted.requests[0].Model != "session-model" {
		t.Fatalf("expected session model to be used, got %+v", scripted.requests)
	}
}

func TestServiceCompactFallsBackToCurrentProviderWhenSessionMetadataMissing(t *testing.T) {
	t.Parallel()

	geminiEnv := runtimeTestAPIKeyEnv(t) + "_GEMINI"
	manager := newRuntimeConfigManagerWithProviderEnvs(t, map[string]string{
		config.GeminiName: geminiEnv,
	})
	setRuntimeProviderEnv(t, geminiEnv, "gemini-key")
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = config.GeminiName
		cfg.CurrentModel = "gemini-current-model"
		cfg.Context.Compact.ManualStrategy = config.CompactManualStrategyFullReplace
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("manual-fallback")
	session.ID = "session-manual-fallback"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
		{Role: providertypes.RoleUser, Content: "before"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(strings.Join([]string{
				"[compact_summary]",
				"done:",
				"- ok",
				"",
				"in_progress:",
				"- continue",
				"",
				"decisions:",
				"- fallback to current selection",
				"",
				"code_changes:",
				"- none",
				"",
				"constraints:",
				"- none",
			}, "\n"))},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	service := NewWithFactory(manager, registry, store, factory, nil)

	result, err := service.Compact(context.Background(), CompactInput{
		SessionID: session.ID,
		RunID:     "run-manual-fallback-provider",
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compact to apply")
	}
	if len(factory.configs) != 1 || factory.configs[0].Name != config.GeminiName {
		t.Fatalf("expected current selected provider fallback, got %+v", factory.configs)
	}
	if len(scripted.requests) != 1 || scripted.requests[0].Model != "gemini-current-model" {
		t.Fatalf("expected current selected model fallback, got %+v", scripted.requests)
	}
}

func TestServiceManualCompactThenRunContinuesToolRound(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("manual-continue")
	session.ID = "session-manual-continue"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "legacy request"},
		{Role: providertypes.RoleAssistant, Content: "legacy answer"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	tool := &stubTool{name: "filesystem_read_file", content: "file content"}
	registry.Register(tool)

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_read_file"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-1", `{"path":"main.go"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	service.compactRunner = &stubCompactRunner{
		runFn: func(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error) {
			return contextcompact.Result{
				Messages: []providertypes.Message{
					{Role: providertypes.RoleAssistant, Content: "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue"},
					{Role: providertypes.RoleAssistant, Content: "latest answer"},
				},
				Applied: true,
				Metrics: contextcompact.Metrics{
					BeforeChars: 40,
					AfterChars:  20,
					SavedRatio:  0.5,
					TriggerMode: string(contextcompact.ModeManual),
				},
				TranscriptID:   "transcript_manual_then_run",
				TranscriptPath: "/tmp/manual-then-run.jsonl",
			}, nil
		},
	}

	if _, err := service.Compact(context.Background(), CompactInput{
		SessionID: session.ID,
		RunID:     "run-manual-first",
	}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-after-manual",
		Content:   "continue",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if tool.callCount != 1 {
		t.Fatalf("expected tool to run once after manual compact, got %d", tool.callCount)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(saved.Messages) < 6 || !strings.Contains(saved.Messages[0].Content, "compact_summary") {
		t.Fatalf("expected compacted history + new tool round, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventCompactStart,
		EventCompactDone,
		EventUserMessage,
		EventToolStart,
		EventToolResult,
		EventAgentDone,
	})
}

func TestServiceSerializesRunAndCompact(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("serialized")
	session.ID = "session-serialized"
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	providerStarted := make(chan struct{})
	unblockProvider := make(chan struct{})
	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			select {
			case <-providerStarted:
			default:
				close(providerStarted)
			}
			<-unblockProvider
			events <- providertypes.NewTextDeltaStreamEvent("done")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	compactEntered := make(chan struct{}, 1)
	service.compactRunner = &stubCompactRunner{
		runFn: func(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error) {
			compactEntered <- struct{}{}
			return contextcompact.Result{
				Messages: append([]providertypes.Message(nil), input.Messages...),
				Metrics: contextcompact.Metrics{
					BeforeChars: 1,
					AfterChars:  1,
					TriggerMode: string(contextcompact.ModeManual),
				},
			}, nil
		},
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- service.Run(context.Background(), UserInput{
			SessionID: session.ID,
			RunID:     "run-serialized",
			Content:   "hello",
		})
	}()

	<-providerStarted

	compactErrCh := make(chan error, 1)
	go func() {
		_, err := service.Compact(context.Background(), CompactInput{
			SessionID: session.ID,
			RunID:     "compact-serialized",
		})
		compactErrCh <- err
	}()

	select {
	case <-compactEntered:
		t.Fatalf("expected compact to wait until run completes")
	case <-time.After(120 * time.Millisecond):
	}

	close(unblockProvider)

	if err := <-runErrCh; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-compactErrCh; err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	select {
	case <-compactEntered:
	default:
		t.Fatalf("expected compact to execute after run finished")
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

	session := agentsession.New("List Me")
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

	sessionStore := agentsession.NewStore(t.TempDir(), t.TempDir())
	if sessionStore == nil {
		t.Fatalf("expected JSON session store")
	}
}

func TestServiceRunUsesSessionWorkdirForContextAndTools(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	defaultWorkdir := t.TempDir()
	sessionWorkdir := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update default workdir: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.NewWithWorkdir("Session Workdir", sessionWorkdir)
	store.sessions[session.ID] = cloneSession(session)

	tool := &stubTool{name: "filesystem_edit", content: "ok"}
	registry := tools.NewRegistry()
	registry.Register(tool)

	builder := &stubContextBuilder{}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-session-workdir", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-session-workdir", `{"path":"main.go"}`),
			},
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-session-workdir",
		Content:   "edit",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if builder.lastInput.Metadata.Workdir != sessionWorkdir {
		t.Fatalf("expected context workdir %q, got %q", sessionWorkdir, builder.lastInput.Metadata.Workdir)
	}
	if tool.lastInput.Workdir != sessionWorkdir {
		t.Fatalf("expected tool input workdir %q, got %q", sessionWorkdir, tool.lastInput.Workdir)
	}
}

func TestServiceRunUsesInputWorkdirForNewSession(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	defaultWorkdir := t.TempDir()
	draftRoot := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update default workdir: %v", err)
	}

	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})
	builder := &stubContextBuilder{}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	if err := service.Run(context.Background(), UserInput{
		RunID:   "run-new-session-workdir",
		Content: "hello",
		Workdir: draftRoot,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	created := onlySession(t, store)
	if created.Workdir != draftRoot {
		t.Fatalf("expected session workdir %q, got %q", draftRoot, created.Workdir)
	}
	if builder.lastInput.Metadata.Workdir != draftRoot {
		t.Fatalf("expected context metadata workdir %q, got %q", draftRoot, builder.lastInput.Metadata.Workdir)
	}
}

func TestServiceSetSessionWorkdir(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	defaultWorkdir := t.TempDir()
	target := filepath.Join(defaultWorkdir, "sub")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update default workdir: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("set workdir")
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	updated, err := service.SetSessionWorkdir(context.Background(), session.ID, "sub")
	if err != nil {
		t.Fatalf("SetSessionWorkdir() error = %v", err)
	}
	if updated.Workdir != target {
		t.Fatalf("expected updated workdir %q, got %q", target, updated.Workdir)
	}

	loaded, err := service.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Workdir != target {
		t.Fatalf("expected in-memory workdir %q, got %q", target, loaded.Workdir)
	}

	another := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	reloaded, err := another.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() with new service error = %v", err)
	}
	if reloaded.Workdir != target {
		t.Fatalf("expected session workdir to persist across service lifetime, got %q", reloaded.Workdir)
	}

	_, err = service.SetSessionWorkdir(context.Background(), "", "sub")
	if err == nil || !containsError(err, "session id is empty") {
		t.Fatalf("expected empty session id error, got %v", err)
	}
}

func newRuntimeConfigManager(t *testing.T) *config.Manager {
	return newRuntimeConfigManagerWithProviderEnvs(t, nil)
}

func newRuntimeConfigManagerWithProviderEnvs(t *testing.T, providerEnvs map[string]string) *config.Manager {
	t.Helper()

	apiKeyEnv := runtimeTestAPIKeyEnv(t)
	defaultWorkdir := t.TempDir()
	restoreRuntimeEnv(t, apiKeyEnv)
	if err := os.Setenv(apiKeyEnv, "test-key"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	defaults := config.DefaultConfig()
	selected := config.NormalizeProviderName(defaults.SelectedProvider)
	for i := range defaults.Providers {
		if config.NormalizeProviderName(defaults.Providers[i].Name) == selected {
			defaults.Providers[i].APIKeyEnv = apiKeyEnv
			break
		}
	}
	for providerName, envKey := range providerEnvs {
		for i := range defaults.Providers {
			if config.NormalizeProviderName(defaults.Providers[i].Name) == config.NormalizeProviderName(providerName) {
				defaults.Providers[i].APIKeyEnv = envKey
				break
			}
		}
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.ToolTimeoutSec = 1
		cfg.MaxLoops = 4
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	return manager
}

func runtimeTestAPIKeyEnv(t *testing.T) string {
	t.Helper()

	const fallback = "NEOCODE_RUNTIME_TEST_API_KEY"
	name := strings.TrimSpace(t.Name())
	if name == "" {
		return fallback
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	suffix := strings.Trim(b.String(), "_")
	if suffix == "" {
		suffix = "CASE"
	}

	return "NEOCODE_RUNTIME_TEST_API_KEY_" + suffix
}

func setRuntimeProviderEnv(t *testing.T, key string, value string) {
	t.Helper()
	restoreRuntimeEnv(t, key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env %s: %v", key, err)
	}
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

func onlySession(t *testing.T, store *memoryStore) agentsession.Session {
	t.Helper()
	if len(store.sessions) != 1 {
		t.Fatalf("expected exactly 1 session, got %d", len(store.sessions))
	}
	for _, session := range store.sessions {
		return session
	}
	return agentsession.Session{}
}

func resolvedProviderForTests(cfg config.Config, providerName string) (config.ResolvedProviderConfig, error) {
	providerCfg, err := cfg.ProviderByName(providerName)
	if err != nil {
		return config.ResolvedProviderConfig{}, err
	}
	return providerCfg.Resolve()
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

func cloneSession(session agentsession.Session) agentsession.Session {
	cloned := session
	cloned.Messages = append([]providertypes.Message(nil), session.Messages...)
	return cloned
}

func cloneChatRequest(req providertypes.ChatRequest) providertypes.ChatRequest {
	cloned := req
	cloned.Messages = append([]providertypes.Message(nil), req.Messages...)
	cloned.Tools = append([]providertypes.ToolSpec(nil), req.Tools...)
	return cloned
}

func cloneBuildInput(input agentcontext.BuildInput) agentcontext.BuildInput {
	cloned := input
	cloned.Messages = append([]providertypes.Message(nil), input.Messages...)
	return cloned
}

func containsError(err error, target string) bool {
	return err != nil && strings.Contains(err.Error(), target)
}

func TestWorkdirHelperFunctions(t *testing.T) {
	t.Run("effectiveSessionWorkdir prefers session value", func(t *testing.T) {
		if got := effectiveSessionWorkdir("  /session ", "/default"); got != "/session" {
			t.Fatalf("expected session workdir, got %q", got)
		}
		if got := effectiveSessionWorkdir("", " /default "); got != "/default" {
			t.Fatalf("expected default workdir, got %q", got)
		}
	})

	t.Run("resolve workdir handles empty relative absolute and invalid cases", func(t *testing.T) {
		defaultDir := t.TempDir()
		currentDir := t.TempDir()
		relativeTarget := filepath.Join(currentDir, "nested")
		if err := os.MkdirAll(relativeTarget, 0o755); err != nil {
			t.Fatalf("mkdir relative target: %v", err)
		}
		absoluteTarget := t.TempDir()

		got, err := resolveWorkdirForSession(defaultDir, "", "")
		if err != nil || got != filepath.Clean(defaultDir) {
			t.Fatalf("expected default dir %q, got %q / %v", filepath.Clean(defaultDir), got, err)
		}

		got, err = resolveWorkdirForSession(defaultDir, currentDir, "nested")
		if err != nil || got != filepath.Clean(relativeTarget) {
			t.Fatalf("expected relative target %q, got %q / %v", filepath.Clean(relativeTarget), got, err)
		}

		got, err = resolveWorkdirForSession(defaultDir, currentDir, absoluteTarget)
		if err != nil || got != filepath.Clean(absoluteTarget) {
			t.Fatalf("expected absolute target %q, got %q / %v", filepath.Clean(absoluteTarget), got, err)
		}

		_, err = resolveWorkdirForSession("", "", "")
		if err == nil || !containsError(err, "workdir is empty") {
			t.Fatalf("expected empty workdir error, got %v", err)
		}

		filePath := filepath.Join(defaultDir, "note.txt")
		if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		_, err = normalizeExistingWorkdir(filePath)
		if err == nil || !containsError(err, "is not a directory") {
			t.Fatalf("expected non-directory error, got %v", err)
		}
	})
}

func TestServiceSetSessionWorkdirNoopDoesNotSave(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	defaultWorkdir := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update default workdir: %v", err)
	}

	store := newMemoryStore()
	target := t.TempDir()
	session := agentsession.NewWithWorkdir("noop", target)
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})
	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

	beforeSaves := store.saves
	updated, err := service.SetSessionWorkdir(context.Background(), session.ID, target)
	if err != nil {
		t.Fatalf("SetSessionWorkdir() error = %v", err)
	}
	if updated.Workdir != target {
		t.Fatalf("expected unchanged workdir %q, got %q", target, updated.Workdir)
	}
	if store.saves != beforeSaves {
		t.Fatalf("expected no extra save on noop update, saves before=%d after=%d", beforeSaves, store.saves)
	}
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
		{"stream interrupted sentinel", provider.ErrStreamInterrupted, false},
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

func TestPermissionEventViewPayloadMapping(t *testing.T) {
	t.Parallel()

	view := permissionEventView{
		toolName:   "webfetch",
		actionType: string(security.ActionTypeRead),
		operation:  "fetch",
		targetType: string(security.TargetTypeURL),
		target:     "https://example.com",
		decision:   "ask",
		reason:     "need approval",
		ruleID:     "rule-1",
		scope:      string(tools.SessionPermissionScopeAlways),
		resolvedAs: "rejected",
	}

	requestPayload := view.toRequestPayload()
	if requestPayload.ToolName != view.toolName ||
		requestPayload.ActionType != view.actionType ||
		requestPayload.Operation != view.operation ||
		requestPayload.TargetType != view.targetType ||
		requestPayload.Target != view.target ||
		requestPayload.Decision != view.decision ||
		requestPayload.Reason != view.reason ||
		requestPayload.RuleID != view.ruleID ||
		requestPayload.RememberScope != view.scope {
		t.Fatalf("unexpected request payload: %+v", requestPayload)
	}

	resolvedPayload := view.toResolvedPayload()
	if resolvedPayload.ToolName != view.toolName ||
		resolvedPayload.ActionType != view.actionType ||
		resolvedPayload.Operation != view.operation ||
		resolvedPayload.TargetType != view.targetType ||
		resolvedPayload.Target != view.target ||
		resolvedPayload.Decision != view.decision ||
		resolvedPayload.Reason != view.reason ||
		resolvedPayload.RuleID != view.ruleID ||
		resolvedPayload.RememberScope != view.scope ||
		resolvedPayload.ResolvedAs != view.resolvedAs {
		t.Fatalf("unexpected resolved payload: %+v", resolvedPayload)
	}
}

func TestStreamAccumulatorBuildMessageRejectsMissingToolName(t *testing.T) {
	t.Parallel()

	acc := newStreamAccumulator()
	acc.accumulateToolCallStart(0, "call-1", "")
	acc.accumulateToolCallDelta(0, "call-1", "{}")

	_, err := acc.buildMessage()
	if err == nil || !containsError(err, "without name") {
		t.Fatalf("expected missing tool name error, got %v", err)
	}
}

func TestLoadSessionReturnsStoreError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

	_, err := service.LoadSession(context.Background(), "missing")
	if err == nil || !containsError(err, "not found") {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestSetSessionWorkdirReturnsStoreError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

	_, err := service.SetSessionWorkdir(context.Background(), "missing", t.TempDir())
	if err == nil || !containsError(err, "not found") {
		t.Fatalf("expected load error from SetSessionWorkdir, got %v", err)
	}
}

func TestSetSessionWorkdirReturnsResolveError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	defaultWorkdir := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = defaultWorkdir
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("set bad workdir")
	session.ID = "session-set-bad-workdir"
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	_, err := service.SetSessionWorkdir(context.Background(), session.ID, filepath.Join(defaultWorkdir, "missing-dir"))
	if err == nil || !containsError(err, "resolve workdir") {
		t.Fatalf("expected resolve workdir error, got %v", err)
	}
}

func TestServiceRunFailsWhenInitialUserMessageSaveFails(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	baseStore := newMemoryStore()
	store := &failingStore{
		Store:      baseStore,
		saveErr:    errors.New("save failed on first write"),
		failOnSave: 1,
	}
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	err := service.Run(context.Background(), UserInput{
		RunID:   "run-initial-save-fail",
		Content: "hello",
	})
	if err == nil || !containsError(err, "save failed on first write") {
		t.Fatalf("expected initial save error, got %v", err)
	}
}

func TestServiceRunFailsWhenAssistantSaveFails(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	baseStore := newMemoryStore()
	store := &failingStore{
		Store:      baseStore,
		saveErr:    errors.New("save failed on assistant"),
		failOnSave: 2,
	}
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("assistant reply")},
		},
	}
	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	err := service.Run(context.Background(), UserInput{
		RunID:   "run-assistant-save-fail",
		Content: "hello",
	})
	if err == nil || !containsError(err, "save failed on assistant") {
		t.Fatalf("expected assistant save error, got %v", err)
	}
}

func TestHandleProviderStreamEventErrorBranches(t *testing.T) {
	t.Parallel()

	acc := newStreamAccumulator()

	err := handleProviderStreamEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventToolCallStart},
		acc,
		nil,
		nil,
		nil,
	)
	if err == nil || !containsError(err, "tool_call_start event payload is nil") {
		t.Fatalf("expected tool_call_start payload error, got %v", err)
	}

	err = handleProviderStreamEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventToolCallDelta},
		acc,
		nil,
		nil,
		nil,
	)
	if err == nil || !containsError(err, "tool_call_delta event payload is nil") {
		t.Fatalf("expected tool_call_delta payload error, got %v", err)
	}

	err = handleProviderStreamEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventMessageDone},
		acc,
		nil,
		nil,
		nil,
	)
	if err == nil || !containsError(err, "message_done event payload is nil") {
		t.Fatalf("expected message_done payload error, got %v", err)
	}
}

func TestEmitDropsWhenChannelFullAndContextCanceled(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 1),
	}
	service.events <- RuntimeEvent{Type: EventAgentChunk}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		service.emit(ctx, EventError, "run-id", "session-id", "payload")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("emit should return when channel is full and context is canceled")
	}
}

func TestCallProviderWithRetryReturnsCombinedForwardError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})
	store := newMemoryStore()

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: providertypes.StreamEventTextDelta}
			return errors.New("provider chat failed")
		},
	}
	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)

	_, err := service.callProviderWithRetry(
		context.Background(),
		"run-forward-error",
		"session-forward-error",
		providertypes.ChatRequest{
			Model:        "test-model",
			SystemPrompt: "prompt",
			Messages:     []providertypes.Message{{Role: providertypes.RoleUser, Content: "hello"}},
		},
	)
	if err == nil || !containsError(err, "provider stream handling failed after provider error") {
		t.Fatalf("expected combined forward/provider error, got %v", err)
	}
}

func TestServiceRunPersistsAndRestoresTokenUsage(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{}
	scripted := &scriptedProvider{}
	scripted.chatFn = func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
		usage := &providertypes.Usage{}
		if scripted.callCount == 1 {
			usage.InputTokens = 100
			usage.OutputTokens = 50
		} else {
			usage.InputTokens = 25
			usage.OutputTokens = 10
		}

		select {
		case events <- providertypes.NewTextDeltaStreamEvent("assistant reply"):
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case events <- providertypes.NewMessageDoneStreamEvent("stop", usage):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)

	if err := service.Run(context.Background(), UserInput{
		RunID:   "run-token-usage-first",
		Content: "hello",
	}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	firstSession := onlySession(t, store)
	if firstSession.TokenInputTotal != 100 {
		t.Fatalf("expected first session input total 100, got %d", firstSession.TokenInputTotal)
	}
	if firstSession.TokenOutputTotal != 50 {
		t.Fatalf("expected first session output total 50, got %d", firstSession.TokenOutputTotal)
	}
	if len(builder.builds) != 1 {
		t.Fatalf("expected 1 build after first run, got %d", len(builder.builds))
	}
	if builder.builds[0].Metadata.SessionInputTokens != 0 {
		t.Fatalf("expected first build to start from zero input tokens, got %d", builder.builds[0].Metadata.SessionInputTokens)
	}
	if builder.builds[0].Metadata.SessionOutputTokens != 0 {
		t.Fatalf("expected first build to start from zero output tokens, got %d", builder.builds[0].Metadata.SessionOutputTokens)
	}

	firstEvents := collectRuntimeEvents(service.Events())
	var firstTokenUsage TokenUsagePayload
	foundFirstTokenUsage := false
	for _, event := range firstEvents {
		if event.Type != EventTokenUsage {
			continue
		}
		payload, ok := event.Payload.(TokenUsagePayload)
		if !ok {
			t.Fatalf("expected TokenUsagePayload, got %T", event.Payload)
		}
		firstTokenUsage = payload
		foundFirstTokenUsage = true
	}
	if !foundFirstTokenUsage {
		t.Fatalf("expected token usage event in %+v", firstEvents)
	}
	if firstTokenUsage.InputTokens != 100 || firstTokenUsage.OutputTokens != 50 {
		t.Fatalf("unexpected first token usage payload: %+v", firstTokenUsage)
	}
	if firstTokenUsage.SessionInputTokens != 100 || firstTokenUsage.SessionOutputTokens != 50 {
		t.Fatalf("expected first session totals to be accumulated, got %+v", firstTokenUsage)
	}

	if err := service.Run(context.Background(), UserInput{
		SessionID: firstSession.ID,
		RunID:     "run-token-usage-second",
		Content:   "continue",
	}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	secondSession, err := store.Load(context.Background(), firstSession.ID)
	if err != nil {
		t.Fatalf("load second session: %v", err)
	}
	if secondSession.TokenInputTotal != 125 {
		t.Fatalf("expected second session input total 125, got %d", secondSession.TokenInputTotal)
	}
	if secondSession.TokenOutputTotal != 60 {
		t.Fatalf("expected second session output total 60, got %d", secondSession.TokenOutputTotal)
	}
	if len(builder.builds) != 2 {
		t.Fatalf("expected 2 builds after second run, got %d", len(builder.builds))
	}
	if builder.builds[1].Metadata.SessionInputTokens != 100 {
		t.Fatalf("expected restored session input tokens 100, got %d", builder.builds[1].Metadata.SessionInputTokens)
	}
	if builder.builds[1].Metadata.SessionOutputTokens != 50 {
		t.Fatalf("expected restored session output tokens 50, got %d", builder.builds[1].Metadata.SessionOutputTokens)
	}

	secondEvents := collectRuntimeEvents(service.Events())
	var secondTokenUsage TokenUsagePayload
	foundSecondTokenUsage := false
	for _, event := range secondEvents {
		if event.Type != EventTokenUsage {
			continue
		}
		payload, ok := event.Payload.(TokenUsagePayload)
		if !ok {
			t.Fatalf("expected TokenUsagePayload, got %T", event.Payload)
		}
		secondTokenUsage = payload
		foundSecondTokenUsage = true
	}
	if !foundSecondTokenUsage {
		t.Fatalf("expected token usage event in %+v", secondEvents)
	}
	if secondTokenUsage.InputTokens != 25 || secondTokenUsage.OutputTokens != 10 {
		t.Fatalf("unexpected second token usage payload: %+v", secondTokenUsage)
	}
	if secondTokenUsage.SessionInputTokens != 125 || secondTokenUsage.SessionOutputTokens != 60 {
		t.Fatalf("expected second session totals to be accumulated, got %+v", secondTokenUsage)
	}
}

func TestServiceRunAutoCompactsAndResetsSessionTokens(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Context.AutoCompact.Enabled = true
		cfg.Context.AutoCompact.InputTokenThreshold = 100
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("auto-compact")
	session.ID = "session-auto-compact"
	session.TokenInputTotal = 100
	session.TokenOutputTotal = 40
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older request"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	tool := &stubTool{name: "filesystem_read_file", content: "file content"}
	registry.Register(tool)

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt:      "auto compact prompt",
				Messages:          append([]providertypes.Message(nil), input.Messages...),
				ShouldAutoCompact: input.Metadata.SessionInputTokens >= input.Compact.AutoCompactThreshold,
			}, nil
		},
	}
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"main.go"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message:      providertypes.Message{Content: "done"},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	compactRunner := &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Content: "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue"},
				{Role: providertypes.RoleAssistant, Content: "latest answer"},
			},
			Applied: true,
			Metrics: contextcompact.Metrics{
				BeforeChars: 60,
				AfterChars:  24,
				SavedRatio:  0.6,
				TriggerMode: string(contextcompact.ModeManual),
			},
			TranscriptID:   "transcript_auto",
			TranscriptPath: "/tmp/auto.jsonl",
		},
	}
	service.compactRunner = compactRunner

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-auto-compact",
		Content:   "continue",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(compactRunner.calls) != 1 {
		t.Fatalf("expected auto compact to run once, got %d", len(compactRunner.calls))
	}
	if len(builder.builds) != 3 {
		t.Fatalf("expected 3 build attempts, got %d", len(builder.builds))
	}
	if builder.builds[0].Metadata.SessionInputTokens != 100 {
		t.Fatalf("expected first build to see pre-compact tokens, got %d", builder.builds[0].Metadata.SessionInputTokens)
	}
	if builder.builds[0].Metadata.SessionOutputTokens != 40 {
		t.Fatalf("expected first build to see pre-compact output tokens, got %d", builder.builds[0].Metadata.SessionOutputTokens)
	}
	if builder.builds[0].Compact.AutoCompactThreshold != 100 {
		t.Fatalf("expected auto compact threshold 100, got %d", builder.builds[0].Compact.AutoCompactThreshold)
	}
	if builder.builds[1].Metadata.SessionInputTokens != 0 {
		t.Fatalf("expected second build to see reset input tokens, got %d", builder.builds[1].Metadata.SessionInputTokens)
	}
	if builder.builds[1].Metadata.SessionOutputTokens != 0 {
		t.Fatalf("expected second build to see reset output tokens, got %d", builder.builds[1].Metadata.SessionOutputTokens)
	}
	if len(scripted.requests) != 2 {
		t.Fatalf("expected 2 provider requests after tool follow-up, got %d", len(scripted.requests))
	}
	if len(scripted.requests[0].Messages) != 2 {
		t.Fatalf("expected rebuilt compacted context to be sent, got %+v", scripted.requests[0].Messages)
	}
	if scripted.requests[0].Messages[0].Content != "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue" {
		t.Fatalf("expected first provider request to use compact summary, got %+v", scripted.requests[0].Messages)
	}
	if scripted.requests[0].Messages[1].Content != "latest answer" {
		t.Fatalf("expected first provider request to use compacted latest answer, got %+v", scripted.requests[0].Messages)
	}

	if service.sessionInputTokens != 0 {
		t.Fatalf("expected service input tokens to reset, got %d", service.sessionInputTokens)
	}
	if service.sessionOutputTokens != 0 {
		t.Fatalf("expected service output tokens to reset, got %d", service.sessionOutputTokens)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load compacted session: %v", err)
	}
	if saved.TokenInputTotal != 0 {
		t.Fatalf("expected persisted input tokens to reset, got %d", saved.TokenInputTotal)
	}
	if saved.TokenOutputTotal != 0 {
		t.Fatalf("expected persisted output tokens to reset, got %d", saved.TokenOutputTotal)
	}
	if tool.callCount != 1 {
		t.Fatalf("expected tool to execute once, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactDone,
		EventToolStart,
		EventToolResult,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventCompactError)
}

func TestServiceRunReactivelyCompactsOnContextTooLong(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("reactive-compact")
	session.ID = "session-reactive-compact"
	session.TokenInputTotal = 220
	session.TokenOutputTotal = 70
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older request"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "reactive compact prompt",
				Messages:     append([]providertypes.Message(nil), input.Messages...),
			}, nil
		},
	}

	callCount := 0
	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			callCount++
			if callCount == 1 {
				return &provider.ProviderError{
					StatusCode: 400,
					Code:       provider.ErrorCodeContextTooLong,
					Message:    "maximum context length exceeded",
					Retryable:  false,
				}
			}
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("recovered"):
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case events <- providertypes.NewMessageDoneStreamEvent("stop", nil):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Content: "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue"},
				{Role: providertypes.RoleUser, Content: "continue"},
			},
			Applied: true,
			Metrics: contextcompact.Metrics{
				BeforeChars: 120,
				AfterChars:  48,
				SavedRatio:  0.6,
				TriggerMode: string(contextcompact.ModeReactive),
			},
			TranscriptID:   "transcript_reactive",
			TranscriptPath: "/tmp/reactive.jsonl",
		},
	}

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-reactive-compact",
		Content:   "continue",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	compactRunner := service.compactRunner.(*stubCompactRunner)
	if len(compactRunner.calls) != 1 {
		t.Fatalf("expected reactive compact to run once, got %d", len(compactRunner.calls))
	}
	if compactRunner.calls[0].Mode != contextcompact.ModeReactive {
		t.Fatalf("expected compact mode %q, got %q", contextcompact.ModeReactive, compactRunner.calls[0].Mode)
	}
	if len(builder.builds) != 2 {
		t.Fatalf("expected 2 build attempts, got %d", len(builder.builds))
	}
	if builder.builds[0].Metadata.SessionInputTokens != 220 {
		t.Fatalf("expected first build to see pre-compact input tokens, got %d", builder.builds[0].Metadata.SessionInputTokens)
	}
	if builder.builds[1].Metadata.SessionInputTokens != 0 {
		t.Fatalf("expected second build to see reset input tokens, got %d", builder.builds[1].Metadata.SessionInputTokens)
	}
	if scripted.callCount != 2 {
		t.Fatalf("expected provider to be called twice, got %d", scripted.callCount)
	}

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load compacted session: %v", err)
	}
	if saved.TokenInputTotal != 0 || saved.TokenOutputTotal != 0 {
		t.Fatalf("expected persisted token totals to reset, got input=%d output=%d", saved.TokenInputTotal, saved.TokenOutputTotal)
	}
	if len(saved.Messages) != 3 {
		t.Fatalf("expected compacted transcript plus final assistant reply, got %+v", saved.Messages)
	}
	if saved.Messages[2].Content != "recovered" {
		t.Fatalf("expected final assistant reply %q, got %q", "recovered", saved.Messages[2].Content)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactDone,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventCompactError)

	foundReactiveDone := false
	for _, event := range events {
		if event.Type != EventCompactDone {
			continue
		}
		payload, ok := event.Payload.(CompactDonePayload)
		if !ok {
			t.Fatalf("expected CompactDonePayload, got %T", event.Payload)
		}
		if payload.TriggerMode != string(contextcompact.ModeReactive) {
			t.Fatalf("expected trigger mode %q, got %q", contextcompact.ModeReactive, payload.TriggerMode)
		}
		foundReactiveDone = true
	}
	if !foundReactiveDone {
		t.Fatalf("expected reactive compact_done event in %+v", events)
	}
}

func TestServiceRunReactivelyCompactsWithinSingleLoopBudget(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.MaxLoops = 1
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := agentsession.New("reactive-single-loop")
	session.ID = "session-reactive-single-loop"
	session.TokenInputTotal = 160
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "older request"},
		{Role: providertypes.RoleAssistant, Content: "older answer"},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			if len(req.Messages) == 3 {
				return &provider.ProviderError{
					StatusCode: 400,
					Code:       provider.ErrorCodeContextTooLong,
					Message:    "maximum context length exceeded",
				}
			}
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("recovered within one loop"):
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case events <- providertypes.NewMessageDoneStreamEvent("stop", nil):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Content: "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue"},
				{Role: providertypes.RoleUser, Content: "continue"},
			},
			Applied: true,
			Metrics: contextcompact.Metrics{
				BeforeChars: 80,
				AfterChars:  30,
				SavedRatio:  0.625,
				TriggerMode: string(contextcompact.ModeReactive),
			},
		},
	}

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-reactive-single-loop",
		Content:   "continue",
	}); err != nil {
		t.Fatalf("Run() with MaxLoops=1 should recover, got %v", err)
	}

	if scripted.callCount != 2 {
		t.Fatalf("expected provider to be called twice within one loop budget, got %d", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactDone,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventError)
}

func TestServiceRunReactiveCompactRetriesOnlyOnce(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.ChatRequest, events chan<- providertypes.StreamEvent) error {
			return &provider.ProviderError{
				StatusCode: 400,
				Code:       provider.ErrorCodeContextTooLong,
				Message:    "prompt is too long",
				Retryable:  false,
			}
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	service.compactRunner = &stubCompactRunner{
		err: errors.New("compact failed"),
	}

	err := service.Run(context.Background(), UserInput{
		RunID:   "run-reactive-compact-once",
		Content: "continue",
	})
	if err == nil || !containsError(err, "prompt is too long") {
		t.Fatalf("expected final context-too-long error, got %v", err)
	}

	compactRunner := service.compactRunner.(*stubCompactRunner)
	if len(compactRunner.calls) != 1 {
		t.Fatalf("expected reactive compact to run once, got %d", len(compactRunner.calls))
	}
	if scripted.callCount != 2 {
		t.Fatalf("expected provider to be called exactly twice, got %d", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactError,
		EventError,
	})
	assertNoEventType(t, events, EventCompactDone)

	foundReactiveError := false
	for _, event := range events {
		if event.Type != EventCompactError {
			continue
		}
		payload, ok := event.Payload.(CompactErrorPayload)
		if !ok {
			t.Fatalf("expected CompactErrorPayload, got %T", event.Payload)
		}
		if payload.TriggerMode != string(contextcompact.ModeReactive) {
			t.Fatalf("expected trigger mode %q, got %q", contextcompact.ModeReactive, payload.TriggerMode)
		}
		foundReactiveError = true
	}
	if !foundReactiveError {
		t.Fatalf("expected reactive compact_error event in %+v", events)
	}
}

func TestRestoreSessionTokens(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	session := agentsession.Session{
		TokenInputTotal:  500,
		TokenOutputTotal: 200,
	}

	service.restoreSessionTokens(session)

	if service.sessionInputTokens != 500 {
		t.Fatalf("expected sessionInputTokens == 500, got %d", service.sessionInputTokens)
	}
	if service.sessionOutputTokens != 200 {
		t.Fatalf("expected sessionOutputTokens == 200, got %d", service.sessionOutputTokens)
	}
}

func TestRestoreSessionTokensNewSession(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	session := agentsession.Session{
		TokenInputTotal:  0,
		TokenOutputTotal: 0,
	}

	service.restoreSessionTokens(session)

	if service.sessionInputTokens != 0 {
		t.Fatalf("expected sessionInputTokens == 0, got %d", service.sessionInputTokens)
	}
	if service.sessionOutputTokens != 0 {
		t.Fatalf("expected sessionOutputTokens == 0, got %d", service.sessionOutputTokens)
	}
}

func TestAutoCompactThresholdEnabled(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             true,
				InputTokenThreshold: 50000,
			},
		},
	}

	threshold := service.autoCompactThreshold(cfg)
	if threshold != 50000 {
		t.Fatalf("expected threshold == 50000, got %d", threshold)
	}
}

func TestAutoCompactThresholdDisabled(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             false,
				InputTokenThreshold: 50000,
			},
		},
	}

	threshold := service.autoCompactThreshold(cfg)
	if threshold != 0 {
		t.Fatalf("expected threshold == 0, got %d", threshold)
	}
}

func TestAutoCompactThresholdZeroValue(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             true,
				InputTokenThreshold: 0,
			},
		},
	}

	threshold := service.autoCompactThreshold(cfg)
	if threshold != 0 {
		t.Fatalf("expected threshold == 0, got %d", threshold)
	}
}

func TestTokenUsageRecordedOnMessageDone(t *testing.T) {
	t.Parallel()

	service := &Service{
		events:              make(chan RuntimeEvent, 128),
		sessionInputTokens:  0,
		sessionOutputTokens: 0,
	}

	events := collectRuntimeEvents(service.Events())

	// Create a MessageDone stream event with token usage
	messageDoneEvent := providertypes.NewMessageDoneStreamEvent("stop", &providertypes.Usage{
		InputTokens:  100,
		OutputTokens: 50,
	})

	// Handle the event with an onMessageDone callback that mimics forwardProviderEvents
	err := handleProviderStreamEvent(
		messageDoneEvent,
		nil,
		nil,
		nil,
		func(payload providertypes.MessageDonePayload) {
			if payload.Usage != nil {
				service.sessionInputTokens += payload.Usage.InputTokens
				service.sessionOutputTokens += payload.Usage.OutputTokens
				service.emit(context.Background(), EventTokenUsage, "test-run-id", "test-session-id", TokenUsagePayload{
					InputTokens:         payload.Usage.InputTokens,
					OutputTokens:        payload.Usage.OutputTokens,
					SessionInputTokens:  service.sessionInputTokens,
					SessionOutputTokens: service.sessionOutputTokens,
				})
			}
		},
	)
	if err != nil {
		t.Fatalf("handleProviderStreamEvent error = %v", err)
	}

	// Verify the service counters are updated
	if service.sessionInputTokens != 100 {
		t.Fatalf("expected sessionInputTokens == 100, got %d", service.sessionInputTokens)
	}
	if service.sessionOutputTokens != 50 {
		t.Fatalf("expected sessionOutputTokens == 50, got %d", service.sessionOutputTokens)
	}

	// Verify EventTokenUsage was emitted with correct payload
	events = collectRuntimeEvents(service.Events())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventTokenUsage {
		t.Fatalf("expected EventTokenUsage, got %s", events[0].Type)
	}

	tokenUsagePayload, ok := events[0].Payload.(TokenUsagePayload)
	if !ok {
		t.Fatalf("expected TokenUsagePayload, got %T", events[0].Payload)
	}
	if tokenUsagePayload.InputTokens != 100 {
		t.Fatalf("expected InputTokens == 100, got %d", tokenUsagePayload.InputTokens)
	}
	if tokenUsagePayload.OutputTokens != 50 {
		t.Fatalf("expected OutputTokens == 50, got %d", tokenUsagePayload.OutputTokens)
	}
	if tokenUsagePayload.SessionInputTokens != 100 {
		t.Fatalf("expected SessionInputTokens == 100, got %d", tokenUsagePayload.SessionInputTokens)
	}
	if tokenUsagePayload.SessionOutputTokens != 50 {
		t.Fatalf("expected SessionOutputTokens == 50, got %d", tokenUsagePayload.SessionOutputTokens)
	}
}
