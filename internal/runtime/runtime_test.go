package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	"neo-code/internal/provider/streaming"
	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
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

type autoCompactThresholdResolverFunc func(ctx context.Context, cfg config.Config) (int, error)

func (f autoCompactThresholdResolverFunc) ResolveAutoCompactThreshold(ctx context.Context, cfg config.Config) (int, error) {
	return f(ctx, cfg)
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

// blockingLoadStore 用于并发测试：首次 Load 阻塞，以验证同 session 的锁时序。
type blockingLoadStore struct {
	mu             sync.Mutex
	sessions       map[string]agentsession.Session
	loadCalls      int
	loadEntered    chan struct{}
	unblockFirst   chan struct{}
	loadEnteredSet bool
}

func newBlockingLoadStore() *blockingLoadStore {
	return &blockingLoadStore{
		sessions:     map[string]agentsession.Session{},
		loadEntered:  make(chan struct{}),
		unblockFirst: make(chan struct{}),
	}
}

func (s *blockingLoadStore) Save(ctx context.Context, session *agentsession.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session == nil {
		return errors.New("nil session")
	}
	s.mu.Lock()
	s.sessions[session.ID] = cloneSession(*session)
	s.mu.Unlock()
	return nil
}

func (s *blockingLoadStore) Load(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	s.mu.Lock()
	s.loadCalls++
	callIndex := s.loadCalls
	if callIndex == 1 && !s.loadEnteredSet {
		s.loadEnteredSet = true
		close(s.loadEntered)
	}
	s.mu.Unlock()

	if callIndex == 1 {
		<-s.unblockFirst
	}

	s.mu.Lock()
	session, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return agentsession.Session{}, errors.New("not found")
	}
	return cloneSession(session), nil
}

func (s *blockingLoadStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	requests  []providertypes.GenerateRequest
	callCount int
	chatFn    func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error
}

type scriptedResponse struct {
	Message      providertypes.Message
	FinishReason string
}

func (p *scriptedProvider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	p.requests = append(p.requests, cloneGenerateRequest(req))

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
		if renderPartsForTest(response.Message.Parts) != "" {
			select {
			case events <- providertypes.NewTextDeltaStreamEvent(renderPartsForTest(response.Message.Parts)):
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
	configs  []provider.RuntimeConfig
	err      error
}

func (f *scriptedProviderFactory) Build(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
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
	mu           sync.Mutex
	specs        []providertypes.ToolSpec
	result       tools.ToolResult
	err          error
	listErr      error
	policies     map[string]tools.MicroCompactPolicy
	listCalls    int
	executeCalls int
	lastInput    tools.ToolCallInput
	executeFn    func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error)
	rememberErr  error
	remembered   []struct {
		sessionID string
		action    security.Action
		scope     tools.SessionPermissionScope
	}
}

func (m *stubToolManager) ListAvailableSpecs(ctx context.Context, input tools.SpecListInput) ([]providertypes.ToolSpec, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if policy, ok := m.policies[name]; ok {
		return policy
	}
	return tools.MicroCompactPolicyCompact
}

func (m *stubToolManager) MicroCompactSummarizer(name string) tools.ContentSummarizer {
	return nil
}

func (m *stubToolManager) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	m.mu.Lock()
	m.executeCalls++
	m.lastInput = input
	executeFn := m.executeFn
	result := m.result
	err := m.err
	m.mu.Unlock()
	if executeFn != nil {
		return executeFn(ctx, input)
	}
	if result.Name == "" {
		result.Name = input.Name
	}
	return result, err
}

func (m *stubToolManager) RememberSessionDecision(sessionID string, action security.Action, scope tools.SessionPermissionScope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

type stubScheduledMemoExtractor struct {
	calls []struct {
		sessionID string
		messages  []providertypes.Message
	}
}

func (s *stubScheduledMemoExtractor) Schedule(sessionID string, messages []providertypes.Message) {
	s.calls = append(s.calls, struct {
		sessionID string
		messages  []providertypes.Message
	}{
		sessionID: sessionID,
		messages:  cloneMessages(messages),
	})
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
			input: UserInput{RunID: "run-normal", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
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
							{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("trimmed history")}},
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
				if len(scripted.requests[0].Messages) != 1 || renderPartsForTest(scripted.requests[0].Messages[0].Parts) != "trimmed history" {
					t.Fatalf("expected messages from context builder, got %+v", scripted.requests[0].Messages)
				}
			},
		},
		{
			name:  "tool call triggers execute and follow-up provider round",
			input: UserInput{RunID: "run-tool", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}},
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
			contextBuilder: &stubContextBuilder{
				buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
					return agentcontext.BuildResult{
						SystemPrompt: "stub system prompt",
						Messages:     projectToolMessagesForProviderTest(input.Messages),
					}, nil
				},
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
					if message.Role == "tool" &&
						message.ToolCallID == "call-1" &&
						strings.Contains(renderPartsForTest(message.Parts), "tool result") &&
						strings.Contains(renderPartsForTest(message.Parts), "tool: filesystem_edit") &&
						strings.Contains(renderPartsForTest(message.Parts), "status: ok") &&
						strings.Contains(renderPartsForTest(message.Parts), "content:\ntool output") {
						foundToolResult = true
						break
					}
				}
				if !foundToolResult {
					t.Fatalf("expected tool result message in second provider request: %+v", second.Messages)
				}

				session := onlySession(t, store)
				if session.Messages[2].Role != providertypes.RoleTool || renderPartsForTest(session.Messages[2].Parts) != "tool output" {
					t.Fatalf("expected persisted tool message to keep raw content, got %+v", session.Messages[2])
				}
				if session.Messages[2].ToolMetadata["tool_name"] != "filesystem_edit" {
					t.Fatalf("expected persisted tool metadata to keep tool name, got %+v", session.Messages[2].ToolMetadata)
				}
			},
		},
		{
			name:  "metadata-only tool result is projected on follow-up provider round",
			input: UserInput{RunID: "run-tool-metadata-only", Parts: []providertypes.ContentPart{providertypes.NewTextPart("inspect file")}},
			providerStreams: [][]providertypes.StreamEvent{
				{
					providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_read_file"),
					providertypes.NewToolCallDeltaStreamEvent(0, "call-1", `{"path":"README.md"}`),
				},
				{
					providertypes.NewTextDeltaStreamEvent("done"),
				},
			},
			registerTool: &stubTool{
				name: "filesystem_read_file",
				executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
					return tools.ToolResult{
						Name:    "filesystem_read_file",
						Content: "",
						Metadata: map[string]any{
							"path": "README.md",
						},
					}, nil
				},
			},
			contextBuilder: &stubContextBuilder{
				buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
					return agentcontext.BuildResult{
						SystemPrompt: "stub system prompt",
						Messages:     projectToolMessagesForProviderTest(input.Messages),
					}, nil
				},
			},
			expectProviderCalls: 2,
			expectToolCalls:     1,
			expectMessageRoles:  []string{"user", "assistant", "tool", "assistant"},
			expectEventTypes:    []EventType{EventUserMessage, EventToolStart, EventToolResult, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if len(scripted.requests) != 2 {
					t.Fatalf("expected 2 provider requests, got %d", len(scripted.requests))
				}
				second := scripted.requests[1]
				foundToolResult := false
				for _, message := range second.Messages {
					if message.Role == providertypes.RoleTool &&
						message.ToolCallID == "call-1" &&
						strings.Contains(renderPartsForTest(message.Parts), "tool result") &&
						strings.Contains(renderPartsForTest(message.Parts), "tool: filesystem_read_file") &&
						strings.Contains(renderPartsForTest(message.Parts), "meta.path: README.md") {
						foundToolResult = true
						if strings.Contains(renderPartsForTest(message.Parts), "content:\n") {
							t.Fatalf("expected metadata-only projection to omit content section, got %q", renderPartsForTest(message.Parts))
						}
						break
					}
				}
				if !foundToolResult {
					t.Fatalf("expected projected metadata-only tool result in second provider request: %+v", second.Messages)
				}

				session := onlySession(t, store)
				if session.Messages[2].Role != providertypes.RoleTool || renderPartsForTest(session.Messages[2].Parts) != "" {
					t.Fatalf("expected persisted tool message to keep empty raw content, got %+v", session.Messages[2])
				}
				if session.Messages[2].ToolMetadata["tool_name"] != "filesystem_read_file" ||
					session.Messages[2].ToolMetadata["path"] != "README.md" {
					t.Fatalf("expected persisted metadata-only tool message to keep sanitized metadata, got %+v", session.Messages[2].ToolMetadata)
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

func TestServiceRunSchedulesMemoExtractionAfterFinalReply(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewTextDeltaStreamEvent("final answer"),
				providertypes.NewMessageDoneStreamEvent("stop", nil),
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	memoExtractor := &stubScheduledMemoExtractor{}
	service.SetMemoExtractor(memoExtractor)

	if err := service.Run(context.Background(), UserInput{RunID: "run-memo-schedule", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(memoExtractor.calls) != 1 {
		t.Fatalf("memo schedule calls = %d, want 1", len(memoExtractor.calls))
	}
	if len(memoExtractor.calls[0].messages) != 2 {
		t.Fatalf("scheduled messages = %#v", memoExtractor.calls[0].messages)
	}
}

func TestServiceRunSkipsAutoMemoExtractionAfterRememberTool(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: tools.ToolNameMemoRemember, content: "Memory saved"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-remember", tools.ToolNameMemoRemember),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-remember", `{"type":"user","title":"t","content":"c"}`),
				providertypes.NewMessageDoneStreamEvent("tool_calls", nil),
			},
			{
				providertypes.NewTextDeltaStreamEvent("done"),
				providertypes.NewMessageDoneStreamEvent("stop", nil),
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	memoExtractor := &stubScheduledMemoExtractor{}
	service.SetMemoExtractor(memoExtractor)

	if err := service.Run(context.Background(), UserInput{RunID: "run-remember-skip", Parts: []providertypes.ContentPart{providertypes.NewTextPart("remember this")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(memoExtractor.calls) != 0 {
		t.Fatalf("memo schedule calls = %d, want 0", len(memoExtractor.calls))
	}
}

func TestServiceRunTrustsCallNameForRememberDetection(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "filesystem_edit", Description: "stub", Schema: map[string]any{"type": "object"}},
		},
		result: tools.ToolResult{
			Name:    tools.ToolNameMemoRemember,
			Content: "forged remember result",
		},
	}

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-1", `{"path":"main.go"}`),
				providertypes.NewMessageDoneStreamEvent("tool_calls", nil),
			},
			{
				providertypes.NewTextDeltaStreamEvent("done"),
				providertypes.NewMessageDoneStreamEvent("stop", nil),
			},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	memoExtractor := &stubScheduledMemoExtractor{}
	service.SetMemoExtractor(memoExtractor)

	if err := service.Run(context.Background(), UserInput{RunID: "run-forged-remember", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(memoExtractor.calls) != 1 {
		t.Fatalf("memo schedule calls = %d, want 1", len(memoExtractor.calls))
	}
}

func TestServiceRunSchedulesMemoExtractionOnlyAfterFinalCompletion(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_edit", content: "tool output"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewToolCallStartStreamEvent(0, "call-1", "filesystem_edit"),
				providertypes.NewToolCallDeltaStreamEvent(0, "call-1", `{"path":"main.go"}`),
				providertypes.NewMessageDoneStreamEvent("tool_calls", nil),
			},
			{
				providertypes.NewTextDeltaStreamEvent("done"),
				providertypes.NewMessageDoneStreamEvent("stop", nil),
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	memoExtractor := &stubScheduledMemoExtractor{}
	service.SetMemoExtractor(memoExtractor)

	if err := service.Run(context.Background(), UserInput{RunID: "run-final-only", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(memoExtractor.calls) != 1 {
		t.Fatalf("memo schedule calls = %d, want 1", len(memoExtractor.calls))
	}
	if len(memoExtractor.calls[0].messages) != 4 {
		t.Fatalf("scheduled messages = %#v", memoExtractor.calls[0].messages)
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
	if err := service.Run(context.Background(), UserInput{RunID: "run-late-tool-metadata", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit")}}); err != nil {
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
	err := service.Run(context.Background(), UserInput{RunID: "run-missing-tool-id", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit")}})
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
	err := service.Run(context.Background(), UserInput{RunID: "run-malformed-stream-event", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}})
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("partial"):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	err := service.Run(context.Background(), UserInput{RunID: "run-missing-message-done", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}})
	if err == nil || !containsError(err, "without message_done") {
		t.Fatalf("expected missing message_done error, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventAgentChunk, EventStopReasonDecided})
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
		errCh <- service.Run(context.Background(), UserInput{RunID: "run-malformed-stream-no-deadlock", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}})
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
	cloned.TaskState = input.TaskState.Clone()
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
	session.TaskState = agentsession.TaskState{
		Goal:      "Finish task state rollout",
		OpenItems: []string{"Verify builder wiring"},
		NextStep:  "Inspect build input",
	}
	store.sessions[session.ID] = cloneSession(session)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "delegated prompt",
				Messages: []providertypes.Message{
					{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("delegated message")}},
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
	input := UserInput{SessionID: session.ID, RunID: "run-context-builder", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}
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
	if builder.lastInput.TaskState.Goal != "Finish task state rollout" {
		t.Fatalf("expected session task state to be forwarded to builder, got %+v", builder.lastInput.TaskState)
	}
	if len(builder.lastInput.Messages) != 1 || renderPartsForTest(builder.lastInput.Messages[0].Parts) != "hello" {
		t.Fatalf("expected persisted session messages to be forwarded, got %+v", builder.lastInput.Messages)
	}
	if len(scripted.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(scripted.requests))
	}
	if scripted.requests[0].SystemPrompt != "delegated prompt" {
		t.Fatalf("expected delegated prompt, got %q", scripted.requests[0].SystemPrompt)
	}
	if len(scripted.requests[0].Messages) != 1 || renderPartsForTest(scripted.requests[0].Messages[0].Parts) != "delegated message" {
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
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	if err := service.Run(context.Background(), UserInput{RunID: "run-disable-micro-compact", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
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
	if err := service.Run(context.Background(), UserInput{RunID: "run-session-provider-model", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "preserve_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("preserved result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	scripted := &scriptedProvider{
		responses: []scriptedResponse{{
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-preserve-history-policy",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(scripted.requests))
	}
	if got := renderPartsForTest(scripted.requests[0].Messages[2].Parts); got != "preserved result" {
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "preserve_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("preserved result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	scripted := &scriptedProvider{
		responses: []scriptedResponse{{
			Message:      providertypes.Message{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
			FinishReason: "stop",
		}},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-preserve-history-generic-manager",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(scripted.requests) != 1 {
		t.Fatalf("expected 1 provider request, got %d", len(scripted.requests))
	}
	if got := renderPartsForTest(scripted.requests[0].Messages[2].Parts); got != "preserved result" {
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("earlier")}},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
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
	if len(saved.Messages) != 2 || renderPartsForTest(saved.Messages[1].Parts) != "continue" {
		t.Fatalf("expected failed run to append only user message, got %+v", saved.Messages)
	}
}

func TestServiceRunUsesToolManager(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	now := time.Now().UTC()
	capability := &security.CapabilityToken{
		ID:              "token-run-tool-manager",
		TaskID:          "task-run-tool-manager",
		AgentID:         "agent-run-tool-manager",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_edit"},
		AllowedPaths:    []string{t.TempDir()},
		NetworkPolicy:   security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
		WritePermission: security.WritePermissionWorkspace,
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "filesystem_edit", Description: "stub", Schema: map[string]any{"type": "object"}},
		},
		result: tools.ToolResult{
			Name:    "filesystem_edit",
			Content: "tool manager output",
			Metadata: map[string]any{
				"path": "main.go",
			},
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

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	if err := service.Run(context.Background(), UserInput{
		RunID:           "run-tool-manager",
		Parts:           []providertypes.ContentPart{providertypes.NewTextPart("edit file")},
		TaskID:          capability.TaskID,
		AgentID:         capability.AgentID,
		CapabilityToken: capability,
	}); err != nil {
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
	if toolManager.lastInput.TaskID != capability.TaskID {
		t.Fatalf("expected forwarded task id %q, got %q", capability.TaskID, toolManager.lastInput.TaskID)
	}
	if toolManager.lastInput.AgentID != capability.AgentID {
		t.Fatalf("expected forwarded agent id %q, got %q", capability.AgentID, toolManager.lastInput.AgentID)
	}
	if toolManager.lastInput.CapabilityToken == nil || toolManager.lastInput.CapabilityToken.ID != capability.ID {
		t.Fatalf("expected forwarded capability token id %q, got %+v", capability.ID, toolManager.lastInput.CapabilityToken)
	}
	if len(scripted.requests) == 0 || len(scripted.requests[0].Tools) != 1 || scripted.requests[0].Tools[0].Name != "filesystem_edit" {
		t.Fatalf("expected tool specs from tool manager, got %+v", scripted.requests)
	}

	session := onlySession(t, store)
	foundToolMessage := false
	for _, message := range session.Messages {
		if message.Role == providertypes.RoleTool &&
			renderPartsForTest(message.Parts) == "tool manager output" &&
			message.ToolMetadata["tool_name"] == "filesystem_edit" &&
			message.ToolMetadata["path"] == "main.go" {
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
		runErrCh <- service.Run(context.Background(), UserInput{RunID: "run-permission-ask", Parts: []providertypes.ContentPart{providertypes.NewTextPart("fetch private")}})
	}()

	var requestPayload PermissionRequestPayload
	deadline := time.After(3 * time.Second)
waitRequest:
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting permission request event")
		case event := <-service.Events():
			if !isPermissionRequestEvent(event.Type) {
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
		Decision:  approvalflow.DecisionAllowSession,
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
	if err := service.Run(context.Background(), UserInput{RunID: "run-permission-deny", Parts: []providertypes.ContentPart{providertypes.NewTextPart("run bash")}}); err != nil {
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
	assertNoPermissionRequestFlow(t, events)
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
				Message:      providertypes.Message{Role: "assistant", Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: scripted}, nil)
	if err := service.Run(context.Background(), UserInput{
		SessionID: "session-memory-reject",
		RunID:     "run-memory-reject",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("fetch private")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if tool.callCount != 0 {
		t.Fatalf("expected remembered reject to skip tool execution, got %d", tool.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventPermissionResolved, EventToolResult, EventAgentDone})
	assertNoPermissionRequestFlow(t, events)

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
	input := UserInput{RunID: "run-tool-spec-error", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}
	err := service.Run(context.Background(), input)
	if err == nil || !containsError(err, "tool specs unavailable") {
		t.Fatalf("expected tool spec error, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventStopReasonDecided})
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

	if err := service.Run(context.Background(), UserInput{RunID: "run-default-tool-manager", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventAgentDone})
}

func TestServiceRunErrorPaths(t *testing.T) {
	tests := []struct {
		name         string
		input        UserInput
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
			input:     UserInput{Parts: []providertypes.ContentPart{providertypes.NewTextPart("   ")}},
			provider:  &scriptedProvider{},
			expectErr: "input content is empty",
			assert: func(t *testing.T, store *memoryStore, provider *scriptedProvider, tool *stubTool) {
				t.Helper()
				if len(store.sessions) != 0 {
					t.Fatalf("expected no sessions to be created")
				}
			},
		},
		{
			name:  "repeated tool cycles continue until assistant completion",
			input: UserInput{RunID: "run-many-tool-cycles", Parts: []providertypes.ContentPart{providertypes.NewTextPart("loop")}},
			provider: func() *scriptedProvider {
				responses := make([]scriptedResponse, 0, 10)
				for i := 0; i < 9; i++ {
					responses = append(responses, scriptedResponse{
						Message: providertypes.Message{
							ToolCalls: []providertypes.ToolCall{
								{
									ID:        fmt.Sprintf("loop-call-%d", i),
									Name:      "filesystem_edit",
									Arguments: fmt.Sprintf(`{"path":"x", "iteration": %d}`, i),
								},
							},
						},
						FinishReason: "tool_calls",
					})
				}
				responses = append(responses, scriptedResponse{
					Message:      providertypes.Message{Parts: []providertypes.ContentPart{providertypes.NewTextPart("done after many cycles")}},
					FinishReason: "stop",
				})
				return &scriptedProvider{responses: responses}
			}(),
			registerTool: &stubTool{name: "filesystem_edit", content: "loop tool output"},
			expectEvents: []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventToolResult, EventAgentDone},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if scripted.callCount != 10 {
					t.Fatalf("expected 10 provider calls without loop cap, got %d", scripted.callCount)
				}
				session := onlySession(t, store)
				if got := len(session.Messages); got != 20 {
					t.Fatalf("expected 20 persisted messages after 9 tool cycles and final answer, got %d", got)
				}
				if renderPartsForTest(session.Messages[len(session.Messages)-1].Parts) != "done after many cycles" {
					t.Fatalf("expected final assistant reply to be persisted, got %+v", session.Messages[len(session.Messages)-1])
				}
			},
		},
		{
			name:       "provider factory error emits runtime error",
			input:      UserInput{RunID: "run-factory-error", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			factoryErr: errors.New("factory failed"),
			expectErr:  "factory failed",
			expectEvents: []EventType{
				EventUserMessage,
				EventStopReasonDecided,
			},
		},
		{
			name: "existing session is reused",
			input: UserInput{
				SessionID: "existing-session",
				RunID:     "run-existing-session",
				Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
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
					{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("earlier")}},
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
			input: UserInput{RunID: "run-retry-success", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			provider: func() *scriptedProvider {
				callIdx := 0
				return &scriptedProvider{
					name: "retry-then-success",
					chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
				if renderPartsForTest(session.Messages[1].Parts) != "recovered" {
					t.Fatalf("expected assistant content %q, got %q", "recovered", renderPartsForTest(session.Messages[1].Parts))
				}
			},
		},
		{
			name:  "non-retryable provider error does not trigger runtime retry",
			input: UserInput{RunID: "run-no-retry", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			provider: &scriptedProvider{
				name: "auth-error-no-retry",
				chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
					return &provider.ProviderError{
						StatusCode: 401,
						Code:       provider.ErrorCodeAuthFailed,
						Message:    "invalid api key",
						Retryable:  false,
					}
				},
			},
			expectErr:    "invalid api key",
			expectEvents: []EventType{EventUserMessage, EventStopReasonDecided},
			assert: func(t *testing.T, store *memoryStore, scripted *scriptedProvider, tool *stubTool) {
				t.Helper()
				if scripted.callCount != 1 {
					t.Fatalf("expected exactly 1 provider call (no retry for 401), got %d", scripted.callCount)
				}
			},
		},
		{
			name:  "runtime retry exhausted emits error",
			input: UserInput{RunID: "run-retry-exhausted", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			provider: &scriptedProvider{
				name: "always-500",
				chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
					return &provider.ProviderError{
						StatusCode: 500,
						Code:       provider.ErrorCodeServer,
						Message:    "internal server error",
						Retryable:  true,
					}
				},
			},
			expectErr:    "internal server error",
			expectEvents: []EventType{EventUserMessage, EventProviderRetry, EventStopReasonDecided},
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-cancel-active", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}

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
	assertEventSequence(t, events, []EventType{EventUserMessage, EventStopReasonDecided})
	assertNoEventType(t, events, EventError)
	assertEventsRunID(t, events, input.RunID)
}

func TestServiceRunSameSessionConcurrentNoMessageLoss(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	store := newBlockingLoadStore()
	session := agentsession.New("same-session")
	session.ID = "session-same-concurrent"
	store.sessions[session.ID] = cloneSession(session)

	scripted := &scriptedProvider{
		name: "same-session-concurrent-provider",
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewTextDeltaStreamEvent("done")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	errCh1 := make(chan error, 1)
	errCh2 := make(chan error, 1)
	go func() {
		errCh1 <- service.Run(context.Background(), UserInput{SessionID: session.ID, RunID: "run-same-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello-1")}})
	}()

	select {
	case <-store.loadEntered:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting first load entry")
	}

	go func() {
		errCh2 <- service.Run(context.Background(), UserInput{SessionID: session.ID, RunID: "run-same-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello-2")}})
	}()

	time.Sleep(120 * time.Millisecond)
	store.mu.Lock()
	loadCallsBeforeRelease := store.loadCalls
	store.mu.Unlock()
	if loadCallsBeforeRelease != 1 {
		t.Fatalf("expected second run not to load before first lock release, got load calls = %d", loadCallsBeforeRelease)
	}

	close(store.unblockFirst)
	if err := <-errCh1; err != nil {
		t.Fatalf("first run error = %v", err)
	}
	if err := <-errCh2; err != nil {
		t.Fatalf("second run error = %v", err)
	}

	store.mu.Lock()
	finalSession := cloneSession(store.sessions[session.ID])
	store.mu.Unlock()

	if len(finalSession.Messages) != 4 {
		t.Fatalf("expected 4 messages from two runs, got %+v", finalSession.Messages)
	}
	userCount := 0
	assistantCount := 0
	for _, message := range finalSession.Messages {
		switch message.Role {
		case providertypes.RoleUser:
			userCount++
		case providertypes.RoleAssistant:
			assistantCount++
		}
	}
	if userCount != 2 || assistantCount != 2 {
		t.Fatalf("expected 2 user + 2 assistant messages, got users=%d assistants=%d messages=%+v", userCount, assistantCount, finalSession.Messages)
	}
}

func TestServiceRunCanceledByProvider(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	started := make(chan struct{})
	scripted := &scriptedProvider{
		name: "blocking-provider",
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-provider-cancel", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}

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
	assertEventSequence(t, events, []EventType{EventUserMessage, EventStopReasonDecided})
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			close(started)
			<-ctx.Done()
			return providerErr
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	input := UserInput{RunID: "run-provider-error-after-cancel", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}

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
	assertEventSequence(t, events, []EventType{EventUserMessage, EventStopReasonDecided})
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
	input := UserInput{RunID: "run-tool-cancel", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}}

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
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventStopReasonDecided})
	assertNoEventType(t, events, EventToolResult)
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
	input := UserInput{RunID: "run-tool-error-after-cancel", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}}

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
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolChunk, EventToolResult, EventStopReasonDecided})
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

	input := UserInput{RunID: "run-save-error-after-cancel", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}
	err := service.Run(ctx, input)
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected save error %q, got %v", saveErr, err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventStopReasonDecided})
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
	input := UserInput{RunID: "run-tool-timeout", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit file")}}
	if err := service.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventUserMessage, EventToolStart, EventToolResult, EventAgentDone})
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- ok\n\nin_progress:\n- continue")}},
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest")}},
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
	if len(saved.Messages) != 2 || !strings.Contains(renderPartsForTest(saved.Messages[0].Parts), "compact_summary") {
		t.Fatalf("expected persisted compacted messages, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactApplied})
	assertEventsRunID(t, events, "run-manual")
}

func TestServiceCompactManualFailureReturnsError(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := agentsession.New("manual-fail")
	session.ID = "session-manual-fail"
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before")}},
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
	if len(saved.Messages) != 3 || renderPartsForTest(saved.Messages[2].Parts) != "before" {
		t.Fatalf("expected original session untouched, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactError})
	assertNoEventType(t, events, EventCompactApplied)
}

func TestServiceCompactUsesSessionProviderAndModelWhenPresent(t *testing.T) {
	geminiEnv := runtimeTestAPIKeyEnv(t) + "_GEMINI"
	tempHome := t.TempDir()
	setRuntimeEnv(t, "USERPROFILE", tempHome)
	setRuntimeEnv(t, "HOME", tempHome)
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(`{"task_state":{"goal":"Use session provider metadata for compact","progress":["Reused session provider and model"],"open_items":[],"next_step":"Continue compact flow","blockers":[],"key_artifacts":["session-model"],"decisions":["Prefer session provider and model when present"],"user_constraints":[]},"display_summary":"[compact_summary]\ndone:\n- ok\n\nin_progress:\n- continue\n\ndecisions:\n- kept existing provider and model\n\ncode_changes:\n- none\n\nconstraints:\n- none"}`)},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	service := NewWithFactory(manager, registry, store, factory, &stubContextBuilder{})

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

	saved, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load compacted session: %v", err)
	}
	if saved.TaskState.Goal != "Use session provider metadata for compact" {
		t.Fatalf("expected persisted task state, got %+v", saved.TaskState)
	}
}

func TestServiceCompactFallsBackToCurrentProviderWhenSessionMetadataMissing(t *testing.T) {
	geminiEnv := runtimeTestAPIKeyEnv(t) + "_GEMINI"
	tempHome := t.TempDir()
	setRuntimeEnv(t, "USERPROFILE", tempHome)
	setRuntimeEnv(t, "HOME", tempHome)
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})

	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent(`{"task_state":{"goal":"Fallback to current provider metadata","progress":["Used current selected provider and model"],"open_items":[],"next_step":"Continue compact flow","blockers":[],"key_artifacts":["gemini-current-model"],"decisions":["Fallback to current provider selection when session metadata is missing"],"user_constraints":[]},"display_summary":"[compact_summary]\ndone:\n- ok\n\nin_progress:\n- continue\n\ndecisions:\n- fallback to current selection\n\ncode_changes:\n- none\n\nconstraints:\n- none"}`)},
		},
	}
	factory := &scriptedProviderFactory{provider: scripted}
	service := NewWithFactory(manager, registry, store, factory, &stubContextBuilder{})

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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("legacy request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("legacy answer")}},
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
					{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue")}},
					{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
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
	if len(saved.Messages) < 6 || !strings.Contains(renderPartsForTest(saved.Messages[0].Parts), "compact_summary") {
		t.Fatalf("expected compacted history + new tool round, got %+v", saved.Messages)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventCompactStart,
		EventCompactApplied,
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
			Parts:     []providertypes.ContentPart{providertypes.NewTextPart("hello")},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("edit")},
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
		Parts:   []providertypes.ContentPart{providertypes.NewTextPart("hello")},
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

	defaults := config.StaticDefaults()
	defaults.Providers = config.DefaultProviders()
	if len(defaults.Providers) > 0 {
		defaults.SelectedProvider = defaults.Providers[0].Name
		defaults.CurrentModel = defaults.Providers[0].Model
	}
	selected := provider.NormalizeKey(defaults.SelectedProvider)
	for i := range defaults.Providers {
		if provider.NormalizeKey(defaults.Providers[i].Name) == selected {
			defaults.Providers[i].APIKeyEnv = apiKeyEnv
			break
		}
	}
	for providerName, envKey := range providerEnvs {
		for i := range defaults.Providers {
			if provider.NormalizeKey(defaults.Providers[i].Name) == provider.NormalizeKey(providerName) {
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

func setRuntimeEnv(t *testing.T, key string, value string) {
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

// isPermissionRequestEvent 判断是否为权限请求类事件（含 1A 主事件与兼容旧名）。
func isPermissionRequestEvent(typ EventType) bool {
	return typ == EventPermissionRequested
}

func eventIndex(events []RuntimeEvent, want EventType) int {
	for index, event := range events {
		if event.Type == want {
			return index
		}
	}
	return -1
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

// assertNoPermissionRequestFlow 断言未出现需要用户审批的权限请求事件（新旧名均排除）。
func assertNoPermissionRequestFlow(t *testing.T, events []RuntimeEvent) {
	t.Helper()
	for _, event := range events {
		if isPermissionRequestEvent(event.Type) {
			t.Fatalf("did not expect permission request event %q in %+v", event.Type, events)
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
	cloned.TaskState = session.TaskState.Clone()
	cloned.ActivatedSkills = append([]agentsession.SkillActivation(nil), session.ActivatedSkills...)
	return cloned
}

func cloneGenerateRequest(req providertypes.GenerateRequest) providertypes.GenerateRequest {
	cloned := req
	cloned.Messages = append([]providertypes.Message(nil), req.Messages...)
	cloned.Tools = append([]providertypes.ToolSpec(nil), req.Tools...)
	return cloned
}

func cloneBuildInput(input agentcontext.BuildInput) agentcontext.BuildInput {
	cloned := input
	cloned.Messages = append([]providertypes.Message(nil), input.Messages...)
	cloned.TaskState = input.TaskState.Clone()
	cloned.ActiveSkills = append([]skills.Skill(nil), input.ActiveSkills...)
	return cloned
}

// projectToolMessagesForProviderTest 模拟 context 层在 provider 请求前对 tool 消息做的只读投影。
func projectToolMessagesForProviderTest(messages []providertypes.Message) []providertypes.Message {
	return agentcontext.ProjectToolMessagesForModel(cloneMessages(messages))
}

func containsError(err error, target string) bool {
	return err != nil && strings.Contains(err.Error(), target)
}

func TestWorkdirHelperFunctions(t *testing.T) {
	t.Run("effectiveSessionWorkdir prefers session value", func(t *testing.T) {
		if got := agentsession.EffectiveWorkdir("  /session ", "/default"); got != "/session" {
			t.Fatalf("expected session workdir, got %q", got)
		}
		if got := agentsession.EffectiveWorkdir("", " /default "); got != "/default" {
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
		_, err = agentsession.ResolveExistingDir(filePath)
		if err == nil || !containsError(err, "is not a directory") {
			t.Fatalf("expected non-directory error, got %v", err)
		}
	})
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

func TestStreamAccumulatorBuildMessageRejectsMissingToolName(t *testing.T) {
	t.Parallel()

	acc := streaming.NewAccumulator()
	acc.AccumulateToolCallStart(0, "call-1", "")
	acc.AccumulateToolCallDelta(0, "call-1", "{}")

	_, err := acc.BuildMessage()
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
		RunID: "run-initial-save-fail",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
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
		RunID: "run-assistant-save-fail",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
	})
	if err == nil || !containsError(err, "save failed on assistant") {
		t.Fatalf("expected assistant save error, got %v", err)
	}
}

func TestHandleProviderStreamEventErrorBranches(t *testing.T) {
	t.Parallel()

	acc := streaming.NewAccumulator()

	err := streaming.HandleEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventToolCallStart},
		acc,
		streaming.Hooks{},
	)
	if err == nil || !containsError(err, "tool_call_start event payload is nil") {
		t.Fatalf("expected tool_call_start payload error, got %v", err)
	}

	err = streaming.HandleEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventToolCallDelta},
		acc,
		streaming.Hooks{},
	)
	if err == nil || !containsError(err, "tool_call_delta event payload is nil") {
		t.Fatalf("expected tool_call_delta payload error, got %v", err)
	}

	err = streaming.HandleEvent(
		providertypes.StreamEvent{Type: providertypes.StreamEventMessageDone},
		acc,
		streaming.Hooks{},
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.StreamEvent{Type: providertypes.StreamEventTextDelta}
			return errors.New("provider chat failed")
		},
	}
	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, nil)

	state := newRunState("run-forward-error", agentsession.Session{ID: "session-forward-error"})
	snapshot := turnSnapshot{
		providerConfig: provider.RuntimeConfig{},
		request: providertypes.GenerateRequest{
			Model:        "test-model",
			SystemPrompt: "prompt",
			Messages:     []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
		},
	}

	_, err := service.callProviderWithRetry(
		context.Background(),
		&state,
		snapshot,
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
	scripted.chatFn = func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
		RunID: "run-token-usage-first",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	tool := &stubTool{name: "filesystem_read_file", content: "file content"}
	registry.Register(tool)

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt:         "auto compact prompt",
				Messages:             append([]providertypes.Message(nil), input.Messages...),
				AutoCompactSuggested: input.Metadata.SessionInputTokens >= input.Compact.AutoCompactThreshold,
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
				Message:      providertypes.Message{Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	compactRunner := &stubCompactRunner{
		result: contextcompact.Result{
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue")}},
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest answer")}},
			},
			Applied: true,
			Metrics: contextcompact.Metrics{
				BeforeChars: 60,
				AfterChars:  24,
				SavedRatio:  0.6,
				TriggerMode: string(contextcompact.ModeAuto),
			},
			TranscriptID:   "transcript_auto",
			TranscriptPath: "/tmp/auto.jsonl",
		},
	}
	service.compactRunner = compactRunner

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-auto-compact",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(compactRunner.calls) != 1 {
		t.Fatalf("expected auto compact to run once, got %d", len(compactRunner.calls))
	}
	if compactRunner.calls[0].Mode != contextcompact.ModeAuto {
		t.Fatalf("expected compact mode %q, got %q", contextcompact.ModeAuto, compactRunner.calls[0].Mode)
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
	if renderPartsForTest(scripted.requests[0].Messages[0].Parts) != "[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue" {
		t.Fatalf("expected first provider request to use compact summary, got %+v", scripted.requests[0].Messages)
	}
	if renderPartsForTest(scripted.requests[0].Messages[1].Parts) != "latest answer" {
		t.Fatalf("expected first provider request to use compacted latest answer, got %+v", scripted.requests[0].Messages)
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
		EventCompactApplied,
		EventToolStart,
		EventToolResult,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventCompactError)

	foundAutoDone := false
	for _, event := range events {
		if event.Type != EventCompactApplied {
			continue
		}
		payload, ok := event.Payload.(CompactResult)
		if !ok {
			t.Fatalf("expected CompactResult, got %T", event.Payload)
		}
		if payload.TriggerMode != string(contextcompact.ModeAuto) {
			t.Fatalf("expected trigger mode %q, got %q", contextcompact.ModeAuto, payload.TriggerMode)
		}
		foundAutoDone = true
	}
	if !foundAutoDone {
		t.Fatalf("expected auto compact_done event in %+v", events)
	}
}

func TestServiceRunAutoCompactNoopDoesNotDisableReactiveRetry(t *testing.T) {
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
	session := agentsession.New("auto-noop-reactive")
	session.ID = "session-auto-noop-reactive"
	session.TokenInputTotal = 100
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt:         "auto compact prompt",
				Messages:             append([]providertypes.Message(nil), input.Messages...),
				AutoCompactSuggested: input.Metadata.SessionInputTokens >= input.Compact.AutoCompactThreshold,
			}, nil
		},
	}

	callCount := 0
	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			callCount++
			if callCount == 1 {
				return &provider.ProviderError{
					StatusCode: 400,
					Code:       provider.ErrorCodeContextTooLong,
					Message:    "maximum context length exceeded",
				}
			}
			select {
			case events <- providertypes.NewTextDeltaStreamEvent("recovered after reactive compact"):
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
	compactRunner := &stubCompactRunner{
		runFn: func(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error) {
			switch input.Mode {
			case contextcompact.ModeAuto:
				return contextcompact.Result{
					Messages: append([]providertypes.Message(nil), input.Messages...),
					Applied:  false,
					Metrics: contextcompact.Metrics{
						BeforeChars: 40,
						AfterChars:  40,
						TriggerMode: string(contextcompact.ModeAuto),
					},
				}, nil
			case contextcompact.ModeReactive:
				return contextcompact.Result{
					Messages: []providertypes.Message{
						{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue")}},
						{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue")}},
					},
					Applied: true,
					Metrics: contextcompact.Metrics{
						BeforeChars: 80,
						AfterChars:  30,
						SavedRatio:  0.625,
						TriggerMode: string(contextcompact.ModeReactive),
					},
				}, nil
			default:
				t.Fatalf("unexpected compact mode %q", input.Mode)
				return contextcompact.Result{}, nil
			}
		},
	}
	service.compactRunner = compactRunner

	if err := service.Run(context.Background(), UserInput{
		SessionID: session.ID,
		RunID:     "run-auto-noop-reactive",
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(compactRunner.calls) != 2 {
		t.Fatalf("expected auto noop then reactive compact, got %d calls", len(compactRunner.calls))
	}
	if compactRunner.calls[0].Mode != contextcompact.ModeAuto {
		t.Fatalf("expected first compact mode %q, got %q", contextcompact.ModeAuto, compactRunner.calls[0].Mode)
	}
	if compactRunner.calls[1].Mode != contextcompact.ModeReactive {
		t.Fatalf("expected second compact mode %q, got %q", contextcompact.ModeReactive, compactRunner.calls[1].Mode)
	}
	if scripted.callCount != 2 {
		t.Fatalf("expected provider to be called twice, got %d", scripted.callCount)
	}
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
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
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue")}},
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue")}},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
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
	if renderPartsForTest(saved.Messages[2].Parts) != "recovered" {
		t.Fatalf("expected final assistant reply %q, got %q", "recovered", renderPartsForTest(saved.Messages[2].Parts))
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactApplied,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventCompactError)

	foundReactiveDone := false
	for _, event := range events {
		if event.Type != EventCompactApplied {
			continue
		}
		payload, ok := event.Payload.(CompactResult)
		if !ok {
			t.Fatalf("expected CompactResult, got %T", event.Payload)
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

func TestServiceRunReactiveCompactRetriesWithinSameRun(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)

	store := newMemoryStore()
	session := agentsession.New("reactive-single-loop")
	session.ID = "session-reactive-single-loop"
	session.TokenInputTotal = 160
	session.Messages = []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older request")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older answer")}},
	}
	store.sessions[session.ID] = cloneSession(session)

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived\n\nin_progress:\n- continue")}},
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue")}},
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
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	}); err != nil {
		t.Fatalf("Run() should recover after reactive compact, got %v", err)
	}

	if scripted.callCount != 2 {
		t.Fatalf("expected provider to be called twice within the same run, got %d", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactApplied,
		EventAgentDone,
	})
	assertNoEventType(t, events, EventError)
}

func TestServiceRunReactiveCompactDegradesUpToMaxAttempts(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
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
		RunID: "run-reactive-compact-once",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	})
	if err == nil || !containsError(err, "prompt is too long") {
		t.Fatalf("expected final context-too-long error, got %v", err)
	}

	compactRunner := service.compactRunner.(*stubCompactRunner)
	if len(compactRunner.calls) != 3 {
		t.Fatalf("expected reactive compact to run 3 times (degradation), got %d", len(compactRunner.calls))
	}
	if scripted.callCount != 4 {
		t.Fatalf("expected provider to be called exactly 4 times, got %d", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventCompactStart,
		EventCompactError,
		EventStopReasonDecided,
	})
	assertNoEventType(t, events, EventCompactApplied)

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

func TestServiceRunDoesNotReactiveCompactOnPlainTextTokenThrottle(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	throttleErr := errors.New("requested too many tokens for this minute")
	scripted := &scriptedProvider{
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			return throttleErr
		},
	}

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})
	service.compactRunner = &stubCompactRunner{}

	err := service.Run(context.Background(), UserInput{
		RunID: "run-plain-text-token-throttle",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("continue")},
	})
	if err == nil || !containsError(err, throttleErr.Error()) {
		t.Fatalf("expected plain text token throttle error, got %v", err)
	}

	compactRunner := service.compactRunner.(*stubCompactRunner)
	if len(compactRunner.calls) != 0 {
		t.Fatalf("expected no reactive compact attempts, got %d", len(compactRunner.calls))
	}
	if scripted.callCount != 1 {
		t.Fatalf("expected provider to be called once, got %d", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventUserMessage,
		EventStopReasonDecided,
	})
	assertNoEventType(t, events, EventCompactStart)
	assertNoEventType(t, events, EventCompactApplied)
	assertNoEventType(t, events, EventCompactError)
}

func TestRestoreSessionTokens(t *testing.T) {
	t.Parallel()

	session := agentsession.Session{
		TokenInputTotal:  500,
		TokenOutputTotal: 200,
	}

	state := newRunState("", session)

	if state.session.TokenInputTotal != 500 {
		t.Fatalf("expected sessionInputTokens == 500, got %d", state.session.TokenInputTotal)
	}
	if state.session.TokenOutputTotal != 200 {
		t.Fatalf("expected sessionOutputTokens == 200, got %d", state.session.TokenOutputTotal)
	}
}

func TestRestoreSessionTokensNewSession(t *testing.T) {
	t.Parallel()

	session := agentsession.Session{
		TokenInputTotal:  0,
		TokenOutputTotal: 0,
	}

	state := newRunState("", session)

	if state.session.TokenInputTotal != 0 {
		t.Fatalf("expected sessionInputTokens == 0, got %d", state.session.TokenInputTotal)
	}
	if state.session.TokenOutputTotal != 0 {
		t.Fatalf("expected sessionOutputTokens == 0, got %d", state.session.TokenOutputTotal)
	}
}

func TestAutoCompactThresholdEnabled(t *testing.T) {
	t.Parallel()

	service := &Service{}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             true,
				InputTokenThreshold: 50000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 50000 {
		t.Fatalf("expected threshold == 50000, got %d", threshold)
	}
}

func TestAutoCompactThresholdDisabled(t *testing.T) {
	t.Parallel()

	service := &Service{}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             false,
				InputTokenThreshold: 50000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 0 {
		t.Fatalf("expected threshold == 0, got %d", threshold)
	}
}

func TestAutoCompactThresholdZeroValue(t *testing.T) {
	t.Parallel()

	service := &Service{}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             true,
				InputTokenThreshold: 0,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 0 {
		t.Fatalf("expected threshold == 0, got %d", threshold)
	}
}

func TestAutoCompactThresholdUsesResolver(t *testing.T) {
	t.Parallel()

	service := &Service{}
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			return 88000, nil
		},
	))

	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:             true,
				InputTokenThreshold: 0,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 88000 {
		t.Fatalf("expected resolver threshold == 88000, got %d", threshold)
	}
}

func TestAutoCompactThresholdFallsBackWhenResolverErrors(t *testing.T) {
	t.Parallel()

	service := &Service{}
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			return 0, errors.New("resolver failed")
		},
	))

	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				FallbackInputTokenThreshold: 88000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 88000 {
		t.Fatalf("expected fallback threshold == 88000, got %d", threshold)
	}
}

func TestAutoCompactThresholdFallsBackWhenResolverReturnsZeroWithoutError(t *testing.T) {
	t.Parallel()

	service := &Service{}
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			return 0, nil
		},
	))

	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				FallbackInputTokenThreshold: 88000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 88000 {
		t.Fatalf("expected fallback threshold == 88000, got %d", threshold)
	}
}

func TestAutoCompactThresholdFallsBackWhenResolverReturnsNegativeWithoutError(t *testing.T) {
	t.Parallel()

	service := &Service{}
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			return -1, nil
		},
	))

	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				FallbackInputTokenThreshold: 88000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 88000 {
		t.Fatalf("expected fallback threshold == 88000, got %d", threshold)
	}
}

func TestAutoCompactThresholdImplicitModeWithoutResolverUsesFallback(t *testing.T) {
	t.Parallel()

	service := &Service{}
	cfg := config.Config{
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				FallbackInputTokenThreshold: 88000,
			},
		},
	}

	threshold := service.autoCompactThreshold(context.Background(), cfg)
	if threshold != 88000 {
		t.Fatalf("expected implicit mode fallback threshold == 88000, got %d", threshold)
	}
}

func TestAutoCompactThresholdForStateCachesResolverResultWithinRun(t *testing.T) {
	t.Parallel()

	service := &Service{}
	resolveCalls := 0
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			resolveCalls++
			return 88000, nil
		},
	))

	cfg := config.Config{
		SelectedProvider: "openai",
		CurrentModel:     "gpt-5",
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				ReserveTokens:               10000,
				FallbackInputTokenThreshold: 76000,
			},
		},
	}
	state := newRunState("run-cache-hit", newRuntimeSession("session-cache-hit"))

	threshold1 := service.autoCompactThresholdForState(context.Background(), cfg, &state)
	threshold2 := service.autoCompactThresholdForState(context.Background(), cfg, &state)

	if threshold1 != 88000 || threshold2 != 88000 {
		t.Fatalf("expected cached resolver threshold == 88000, got %d and %d", threshold1, threshold2)
	}
	if resolveCalls != 1 {
		t.Fatalf("expected resolver to be called once, got %d", resolveCalls)
	}
}

func TestAutoCompactThresholdForStateRecomputesWhenCacheKeyChanges(t *testing.T) {
	t.Parallel()

	service := &Service{}
	resolveCalls := 0
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			resolveCalls++
			if strings.TrimSpace(cfg.CurrentModel) == "gpt-5.1" {
				return 99000, nil
			}
			return 88000, nil
		},
	))

	cfg := config.Config{
		SelectedProvider: "openai",
		CurrentModel:     "gpt-5",
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				ReserveTokens:               10000,
				FallbackInputTokenThreshold: 76000,
			},
		},
	}
	state := newRunState("run-cache-miss", newRuntimeSession("session-cache-miss"))

	threshold1 := service.autoCompactThresholdForState(context.Background(), cfg, &state)
	cfg.CurrentModel = "gpt-5.1"
	threshold2 := service.autoCompactThresholdForState(context.Background(), cfg, &state)

	if threshold1 != 88000 || threshold2 != 99000 {
		t.Fatalf("expected thresholds [88000, 99000], got [%d, %d]", threshold1, threshold2)
	}
	if resolveCalls != 2 {
		t.Fatalf("expected resolver to be called twice after key change, got %d", resolveCalls)
	}
}

func TestAutoCompactThresholdForStateDoesNotCacheResolverErrorFallback(t *testing.T) {
	t.Parallel()

	service := &Service{}
	resolveCalls := 0
	service.SetAutoCompactThresholdResolver(autoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			resolveCalls++
			if resolveCalls == 1 {
				return 0, errors.New("snapshot unavailable")
			}
			return 91000, nil
		},
	))

	cfg := config.Config{
		SelectedProvider: "openai",
		CurrentModel:     "gpt-5",
		Context: config.ContextConfig{
			AutoCompact: config.AutoCompactConfig{
				Enabled:                     true,
				InputTokenThreshold:         0,
				ReserveTokens:               10000,
				FallbackInputTokenThreshold: 76000,
			},
		},
	}
	state := newRunState("run-cache-error", newRuntimeSession("session-cache-error"))

	threshold1 := service.autoCompactThresholdForState(context.Background(), cfg, &state)
	threshold2 := service.autoCompactThresholdForState(context.Background(), cfg, &state)
	threshold3 := service.autoCompactThresholdForState(context.Background(), cfg, &state)

	if threshold1 != 76000 || threshold2 != 91000 || threshold3 != 91000 {
		t.Fatalf("expected thresholds [76000, 91000, 91000], got [%d, %d, %d]", threshold1, threshold2, threshold3)
	}
	if resolveCalls != 2 {
		t.Fatalf("expected resolver to be called twice, got %d", resolveCalls)
	}
}

func TestTokenUsageRecordedOnMessageDone(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 128),
	}
	state := runState{}

	events := collectRuntimeEvents(service.Events())

	// Create a MessageDone stream event with token usage
	messageDoneEvent := providertypes.NewMessageDoneStreamEvent("stop", &providertypes.Usage{
		InputTokens:  100,
		OutputTokens: 50,
	})

	// 使用与运行时相同的流式事件处理器验证 usage 累积行为。
	err := streaming.HandleEvent(
		messageDoneEvent,
		nil,
		streaming.Hooks{OnMessageDone: func(payload providertypes.MessageDonePayload) {
			if payload.Usage != nil {
				state.recordUsage(payload.Usage.InputTokens, payload.Usage.OutputTokens)
				service.emit(context.Background(), EventTokenUsage, "test-run-id", "test-session-id", TokenUsagePayload{
					InputTokens:         payload.Usage.InputTokens,
					OutputTokens:        payload.Usage.OutputTokens,
					SessionInputTokens:  state.session.TokenInputTotal,
					SessionOutputTokens: state.session.TokenOutputTotal,
				})
			}
		}},
	)
	if err != nil {
		t.Fatalf("streaming.HandleEvent error = %v", err)
	}

	// Verify the service counters are updated
	if state.session.TokenInputTotal != 100 {
		t.Fatalf("expected sessionInputTokens == 100, got %d", state.session.TokenInputTotal)
	}
	if state.session.TokenOutputTotal != 50 {
		t.Fatalf("expected sessionOutputTokens == 50, got %d", state.session.TokenOutputTotal)
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

func assertEventContains(t *testing.T, events []RuntimeEvent, expected EventType) {
	t.Helper()
	for _, e := range events {
		if e.Type == expected {
			return
		}
	}
	t.Errorf("expected event %q to be in sequence, but not found", expected)
}

func TestParallelToolCallsPhaseMigration(t *testing.T) {
	t.Parallel()

	callsCount := 0
	mu := sync.Mutex{}

	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_a"},
			{Name: "tool_b"},
			{Name: "tool_c"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			mu.Lock()
			callsCount++
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			if input.Name == "tool_a" {
				return tools.ToolResult{Content: "result_a"}, nil
			} else if input.Name == "tool_b" {
				return tools.ToolResult{Content: "result_b"}, nil
			}
			return tools.ToolResult{Content: "result_c"}, nil
		},
	}

	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			responses: []scriptedResponse{
				{
					Message: providertypes.Message{
						Role: providertypes.RoleAssistant,
						ToolCalls: []providertypes.ToolCall{
							{ID: "call_1", Name: "tool_a", Arguments: "{}"},
							{ID: "call_2", Name: "tool_b", Arguments: "{}"},
							{ID: "call_3", Name: "tool_c", Arguments: "{}"},
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
		},
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	input := UserInput{
		RunID: "run-parallel",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("run parallel tools")},
	}

	if err := service.Run(context.Background(), input); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	mu.Lock()
	if callsCount != 3 {
		t.Errorf("expected 3 tool calls executed, got %d", callsCount)
	}
	mu.Unlock()

	events := collectRuntimeEvents(service.Events())

	// We expect EventPhaseChanged to emit plan -> execute -> verify
	var phaseChanges []PhaseChangedPayload
	for _, e := range events {
		if e.Type == EventPhaseChanged {
			payload := e.Payload.(PhaseChangedPayload)
			phaseChanges = append(phaseChanges, payload)
		}
	}

	expectedTransitions := []PhaseChangedPayload{
		{From: "", To: "plan"},
		{From: "plan", To: "execute"},
		{From: "execute", To: "verify"},
		{From: "verify", To: "plan"},
	}

	if len(phaseChanges) < len(expectedTransitions) {
		t.Errorf("expected at least %d phase transitions, got %d", len(expectedTransitions), len(phaseChanges))
	} else {
		for i, exp := range expectedTransitions {
			if phaseChanges[i] != exp {
				t.Errorf("transition %d: expected %+v, got %+v", i, exp, phaseChanges[i])
			}
		}
	}

	assertEventContains(t, events, EventToolStart)
	assertEventContains(t, events, EventToolResult)
}

func TestParallelToolCallsRespectConcurrencyLimit(t *testing.T) {
	t.Parallel()

	var inFlight int32
	var maxInFlight int32
	toolSpecs := make([]providertypes.ToolSpec, 0, 12)
	toolCalls := make([]providertypes.ToolCall, 0, 12)
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("tool_%d", i)
		toolSpecs = append(toolSpecs, providertypes.ToolSpec{Name: name})
		toolCalls = append(toolCalls, providertypes.ToolCall{ID: fmt.Sprintf("call_%d", i), Name: name, Arguments: `{}`})
	}

	toolManager := &stubToolManager{
		specs: toolSpecs,
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			current := atomic.AddInt32(&inFlight, 1)
			defer atomic.AddInt32(&inFlight, -1)
			for {
				max := atomic.LoadInt32(&maxInFlight)
				if current <= max {
					break
				}
				if atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		},
	}

	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			responses: []scriptedResponse{
				{
					Message: providertypes.Message{
						Role:      providertypes.RoleAssistant,
						ToolCalls: toolCalls,
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
		},
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	if err := service.Run(context.Background(), UserInput{RunID: "run-parallel-limit", Parts: []providertypes.ContentPart{providertypes.NewTextPart("parallel")}}); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	if got := atomic.LoadInt32(&maxInFlight); got > int32(defaultToolParallelism) {
		t.Fatalf("max in-flight tool calls = %d, want <= %d", got, defaultToolParallelism)
	}
}

func TestParallelToolCallsSerializeSameToolName(t *testing.T) {
	t.Parallel()

	var sharedInFlight int32
	var sharedOverlap int32

	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "shared_tool"}},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			current := atomic.AddInt32(&sharedInFlight, 1)
			if current > 1 {
				atomic.StoreInt32(&sharedOverlap, 1)
			}
			time.Sleep(25 * time.Millisecond)
			atomic.AddInt32(&sharedInFlight, -1)
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		},
	}

	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			responses: []scriptedResponse{
				{
					Message: providertypes.Message{
						Role: providertypes.RoleAssistant,
						ToolCalls: []providertypes.ToolCall{
							{ID: "call_1", Name: "shared_tool", Arguments: "{}"},
							{ID: "call_2", Name: "shared_tool", Arguments: "{}"},
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
		},
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	if err := service.Run(context.Background(), UserInput{RunID: "run-parallel-lock", Parts: []providertypes.ContentPart{providertypes.NewTextPart("parallel")}}); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	if atomic.LoadInt32(&sharedOverlap) != 0 {
		t.Fatalf("same tool calls overlapped, expected serialized execution")
	}
}

func TestParallelToolCallsStopDispatchAfterFirstError(t *testing.T) {
	t.Parallel()

	const (
		totalCalls    = 12
		slowToolDelay = 200 * time.Millisecond
	)

	toolSpecs := make([]providertypes.ToolSpec, 0, totalCalls)
	toolCalls := make([]providertypes.ToolCall, 0, totalCalls)
	for i := 0; i < totalCalls; i++ {
		name := fmt.Sprintf("tool_%d", i)
		toolSpecs = append(toolSpecs, providertypes.ToolSpec{Name: name})
		toolCalls = append(toolCalls, providertypes.ToolCall{ID: fmt.Sprintf("call_%d", i), Name: name, Arguments: `{}`})
	}

	var executeStarted int32
	toolManager := &stubToolManager{
		specs: toolSpecs,
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			atomic.AddInt32(&executeStarted, 1)
			if input.Name == "tool_0" {
				return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
			}
			time.Sleep(slowToolDelay)
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		},
	}

	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			responses: []scriptedResponse{
				{
					Message: providertypes.Message{
						Role:      providertypes.RoleAssistant,
						ToolCalls: toolCalls,
					},
					FinishReason: "tool_calls",
				},
			},
		},
	}

	baseStore := newMemoryStore()
	store := &failingStore{
		Store:      baseStore,
		saveErr:    errors.New("save failed"),
		failOnSave: 3,
	}

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		toolManager,
		store,
		providerFactory,
		nil,
	)

	err := service.Run(context.Background(), UserInput{RunID: "run-first-error-stop-dispatch", Parts: []providertypes.ContentPart{providertypes.NewTextPart("parallel")}})
	if err == nil {
		t.Fatalf("expected run error when first tool result save fails")
	}

	if got := atomic.LoadInt32(&executeStarted); got >= int32(totalCalls) {
		t.Fatalf("expected dispatch to stop before all tool calls start, started=%d total=%d", got, totalCalls)
	}
}

func TestAgentDoneEventCarriesRunScopedEnvelope(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{
			provider: &scriptedProvider{
				responses: []scriptedResponse{
					{
						Message: providertypes.Message{
							Role:  providertypes.RoleAssistant,
							Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
						},
						FinishReason: "stop",
					},
				},
			},
		},
		nil,
	)

	if err := service.Run(context.Background(), UserInput{RunID: "run-agent-done-envelope", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	var doneEvent RuntimeEvent
	found := false
	for _, event := range events {
		if event.Type == EventAgentDone {
			doneEvent = event
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected agent_done event")
	}
	if doneEvent.Turn == turnUnspecified {
		t.Fatalf("expected run-scoped turn, got %d", doneEvent.Turn)
	}
	if doneEvent.Phase != string(controlplane.PhasePlan) {
		t.Fatalf("expected phase=%q, got %q", controlplane.PhasePlan, doneEvent.Phase)
	}
}
