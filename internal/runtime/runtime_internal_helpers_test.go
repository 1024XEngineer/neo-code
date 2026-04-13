package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/approval"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type stubMemoExtractor struct {
	mu       sync.Mutex
	calls    int
	lastMsgs []providertypes.Message
	doneCh   chan struct{}
}

func (s *stubMemoExtractor) ExtractAndStore(_ context.Context, messages []providertypes.Message) {
	s.mu.Lock()
	s.calls++
	s.lastMsgs = append([]providertypes.Message(nil), messages...)
	doneCh := s.doneCh
	s.mu.Unlock()
	if doneCh != nil {
		select {
		case doneCh <- struct{}{}:
		default:
		}
	}
}

func TestRunStateNilReceiverNoops(t *testing.T) {
	t.Parallel()

	var state *runState
	state.syncSessionTokenTotals()
	state.recordUsage(3, 5)
	state.resetTokenTotals()
	state.touchSession()
}

func TestRunStateMutationsAndSync(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-state")
	state := newRunState("run-1", session)

	state.recordUsage(10, 20)
	if state.tokenInputTotal != 11 || state.tokenOutputTotal != 22 {
		t.Fatalf("unexpected token totals: in=%d out=%d", state.tokenInputTotal, state.tokenOutputTotal)
	}

	state.syncSessionTokenTotals()
	if state.session.TokenInputTotal != 11 || state.session.TokenOutputTotal != 22 {
		t.Fatalf("session totals not synced: %+v", state.session)
	}

	state.resetTokenTotals()
	if state.tokenInputTotal != 0 || state.tokenOutputTotal != 0 {
		t.Fatalf("expected reset totals to be zero, got in=%d out=%d", state.tokenInputTotal, state.tokenOutputTotal)
	}

	before := state.session.UpdatedAt
	state.recordUsage(1, 2)
	state.touchSession()
	if state.session.UpdatedAt.Before(before) {
		t.Fatalf("expected touchSession to update time")
	}
	if state.session.TokenInputTotal != 1 || state.session.TokenOutputTotal != 2 {
		t.Fatalf("expected touchSession to sync totals")
	}
}

func TestAppendAssistantMessageAndSaveMetadataBranches(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-assistant")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-assistant", session)
	snapshot := turnSnapshot{
		providerConfig: providerRuntimeConfigForTest("openai"),
		model:          "gpt-4.1",
	}

	if err := service.appendAssistantMessageAndSave(context.Background(), &state, snapshot, providertypes.Message{Role: providertypes.RoleAssistant}); err != nil {
		t.Fatalf("appendAssistantMessageAndSave() error = %v", err)
	}
	if store.saves != 1 {
		t.Fatalf("expected metadata change to persist once, saves=%d", store.saves)
	}

	store.saves = 0
	state.session.Provider = snapshot.providerConfig.Name
	state.session.Model = snapshot.model
	if err := service.appendAssistantMessageAndSave(context.Background(), &state, snapshot, providertypes.Message{Role: providertypes.RoleAssistant}); err != nil {
		t.Fatalf("appendAssistantMessageAndSave() error = %v", err)
	}
	if store.saves != 0 {
		t.Fatalf("expected empty assistant without metadata change to skip save, saves=%d", store.saves)
	}
}

func TestAppendToolMessageAndSaveSanitizesMetadata(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{
		Name:    "filesystem_read_file",
		Content: "ok",
		Metadata: map[string]any{
			"stderr":    "warn",
			"sensitive": "drop-me",
		},
	}
	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}
	if len(state.session.Messages) != 1 {
		t.Fatalf("expected one persisted tool message, got %d", len(state.session.Messages))
	}
	msg := state.session.Messages[0]
	if msg.ToolMetadata["tool_name"] != "filesystem_read_file" {
		t.Fatalf("expected tool_name metadata, got %+v", msg.ToolMetadata)
	}
	if _, exists := msg.ToolMetadata["sensitive"]; exists {
		t.Fatalf("expected sensitive metadata key to be removed, got %+v", msg.ToolMetadata)
	}
}

func TestResolveMaxLoopsBranches(t *testing.T) {
	t.Parallel()

	if got := resolveMaxLoops(config.Config{MaxLoops: 0}); got != defaultMaxLoops {
		t.Fatalf("expected default max loops for zero, got %d", got)
	}
	if got := resolveMaxLoops(config.Config{MaxLoops: -3}); got != defaultMaxLoops {
		t.Fatalf("expected default max loops for negative, got %d", got)
	}
	if got := resolveMaxLoops(config.Config{MaxLoops: 12}); got != 12 {
		t.Fatalf("expected explicit max loops, got %d", got)
	}
}

func TestEmitTokenUsageSkipsZeroUsage(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 8)}
	state := &runState{runID: "run-token", session: newRuntimeSession("session-token")}

	service.emitTokenUsage(context.Background(), state, providerTurnResult{})
	events := collectRuntimeEvents(service.Events())
	if len(events) != 0 {
		t.Fatalf("expected no token event for zero usage, got %+v", events)
	}

	state.recordUsage(5, 7)
	service.emitTokenUsage(context.Background(), state, providerTurnResult{inputTokens: 5, outputTokens: 7})
	events = collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventTokenUsage {
		t.Fatalf("expected one token usage event, got %+v", events)
	}
}

func TestExecuteAssistantToolCallsFillsErrorContent(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-exec-tool-error-fill")
	store.sessions[session.ID] = cloneSession(session)

	toolErr := errors.New("tool exploded")
	manager := &stubToolManager{err: toolErr}
	service := &Service{
		sessionStore:   store,
		toolManager:    manager,
		approvalBroker: approval.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-exec-tool-error-fill", session)
	assistant := providertypes.Message{
		Role: providertypes.RoleAssistant,
		ToolCalls: []providertypes.ToolCall{
			{ID: "call-err", Name: "filesystem_read_file", Arguments: `{"path":"a.txt"}`},
		},
	}
	snapshot := turnSnapshot{workdir: t.TempDir(), toolTimeout: time.Second}

	if err := service.executeAssistantToolCalls(context.Background(), &state, snapshot, assistant); err != nil {
		t.Fatalf("executeAssistantToolCalls() error = %v", err)
	}
	if len(state.session.Messages) != 1 {
		t.Fatalf("expected one tool message, got %d", len(state.session.Messages))
	}
	if state.session.Messages[0].Content != toolErr.Error() {
		t.Fatalf("expected tool error content fallback, got %q", state.session.Messages[0].Content)
	}
}

func TestExecuteAssistantToolCallsCanceledSaveStillEmitsResultWhenExecErr(t *testing.T) {
	t.Parallel()

	baseStore := newMemoryStore()
	session := newRuntimeSession("session-exec-tool-cancel-save")
	baseStore.sessions[session.ID] = cloneSession(session)
	store := &failingStore{
		Store:            baseStore,
		saveErr:          context.Canceled,
		failOnSave:       1,
		ignoreContextErr: true,
	}

	manager := &stubToolManager{err: errors.New("tool failed")}
	service := &Service{
		sessionStore:   store,
		toolManager:    manager,
		approvalBroker: approval.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-exec-tool-cancel-save", session)
	assistant := providertypes.Message{
		Role: providertypes.RoleAssistant,
		ToolCalls: []providertypes.ToolCall{
			{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"a.txt"}`},
		},
	}
	snapshot := turnSnapshot{workdir: t.TempDir(), toolTimeout: time.Second}

	err := service.executeAssistantToolCalls(context.Background(), &state, snapshot, assistant)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from save failure, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventToolStart, EventToolResult})
}

func TestSetMemoExtractorAndRunTriggersExtraction(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	providerStub := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewTextDeltaStreamEvent("memo ready"),
				providertypes.NewMessageDoneStreamEvent("", nil),
			},
		},
	}
	factory := &scriptedProviderFactory{provider: providerStub}
	toolManager := &stubToolManager{}
	service := NewWithFactory(
		newRuntimeConfigManagerWithProviderEnvs(t, nil),
		toolManager,
		store,
		factory,
		&stubContextBuilder{},
	)
	extractor := &stubMemoExtractor{doneCh: make(chan struct{}, 1)}
	service.SetMemoExtractor(extractor)

	if err := service.Run(context.Background(), UserInput{RunID: "run-memo-extract", Content: "hello"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	select {
	case <-extractor.doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("memo extractor was not triggered")
	}

	extractor.mu.Lock()
	defer extractor.mu.Unlock()
	if extractor.calls != 1 {
		t.Fatalf("expected memo extractor to be called once, got %d", extractor.calls)
	}
	if len(extractor.lastMsgs) < 2 {
		t.Fatalf("expected user+assistant messages, got %d", len(extractor.lastMsgs))
	}
}

func newRuntimeSession(id string) agentsession.Session {
	session := agentsession.New("runtime test")
	session.ID = id
	session.TokenInputTotal = 1
	session.TokenOutputTotal = 2
	return session
}

func providerRuntimeConfigForTest(name string) provider.RuntimeConfig {
	return provider.RuntimeConfig{Name: name}
}
