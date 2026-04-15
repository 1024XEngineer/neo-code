package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

type lockProbeStore struct {
	saveFn func(ctx context.Context, session *agentsession.Session) error
}

func (s *lockProbeStore) Save(ctx context.Context, session *agentsession.Session) error {
	if s.saveFn == nil {
		return nil
	}
	return s.saveFn(ctx, session)
}

func (s *lockProbeStore) Load(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, errors.New("not implemented")
}

func (s *lockProbeStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, errors.New("not implemented")
}

func (s *stubMemoExtractor) Schedule(_ string, messages []providertypes.Message) {
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
	state.recordUsage(3, 5)
	state.resetTokenTotals()
	state.touchSession()
}

func TestRunStateMutationsAndSync(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-state")
	state := newRunState("run-1", session)

	state.recordUsage(10, 20)
	if state.session.TokenInputTotal != 11 || state.session.TokenOutputTotal != 22 {
		t.Fatalf("unexpected token totals: in=%d out=%d", state.session.TokenInputTotal, state.session.TokenOutputTotal)
	}

	state.resetTokenTotals()
	if state.session.TokenInputTotal != 0 || state.session.TokenOutputTotal != 0 {
		t.Fatalf("expected reset totals to be zero, got in=%d out=%d", state.session.TokenInputTotal, state.session.TokenOutputTotal)
	}

	before := state.session.UpdatedAt
	state.recordUsage(1, 2)
	state.touchSession()
	if state.session.UpdatedAt.Before(before) {
		t.Fatalf("expected touchSession to update time")
	}
	if state.session.TokenInputTotal != 1 || state.session.TokenOutputTotal != 2 {
		t.Fatalf("expected recordUsage to sync totals")
	}
}

func TestRunStateMarkSkillMissingReportedBranches(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-mark-missing")
	state := newRunState("run-mark-missing", session)

	if !state.markSkillMissingReported("Go_Review") {
		t.Fatalf("expected first mark to succeed")
	}
	if state.markSkillMissingReported("go-review") {
		t.Fatalf("expected normalized duplicate to be rejected")
	}
	if state.markSkillMissingReported(" - ") {
		t.Fatalf("expected blank normalized id to be rejected")
	}

	var nilState *runState
	if !nilState.markSkillMissingReported("anything") {
		t.Fatalf("expected nil run state to allow reporting")
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

func TestAppendToolMessageAndSaveUnlocksStateBeforePersist(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-append-tool-lock")
	state := newRunState("run-append-tool-lock", session)

	store := &lockProbeStore{
		saveFn: func(_ context.Context, _ *agentsession.Session) error {
			locked := make(chan struct{})
			go func() {
				state.mu.Lock()
				state.mu.Unlock()
				close(locked)
			}()

			select {
			case <-locked:
				return nil
			case <-time.After(200 * time.Millisecond):
				return errors.New("state lock is still held during save")
			}
		},
	}

	service := &Service{sessionStore: store}
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{Name: "filesystem_read_file", Content: "ok"}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}
}

func TestAgentSessionCloneSkillActivationsCreatesDeepCopy(t *testing.T) {
	t.Parallel()

	original := []agentsession.SkillActivation{{SkillID: "go-review"}}
	cloned := agentsessionCloneSkillActivations(original)
	if len(cloned) != 1 || cloned[0].SkillID != "go-review" {
		t.Fatalf("unexpected cloned activations: %+v", cloned)
	}
	cloned[0].SkillID = "changed"
	if original[0].SkillID != "go-review" {
		t.Fatalf("expected source activation to remain unchanged, got %+v", original)
	}
	if agentsessionCloneSkillActivations(nil) != nil {
		t.Fatalf("expected nil activation input to return nil")
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

func TestDegradeKeepRecentMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    int
		attempt int
		want    int
	}{
		{
			name:    "首次尝试使用原值",
			base:    10,
			attempt: 1,
			want:    10,
		},
		{
			name:    "第二次尝试减半",
			base:    10,
			attempt: 2,
			want:    5,
		},
		{
			name:    "第三次尝试四分之一",
			base:    10,
			attempt: 3,
			want:    2,
		},
		{
			name:    "不会低于1",
			base:    1,
			attempt: 3,
			want:    1,
		},
		{
			name:    "大基数多次降级",
			base:    100,
			attempt: 3,
			want:    25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := degradeKeepRecentMessages(tt.base, tt.attempt)
			if got != tt.want {
				t.Fatalf("degradeKeepRecentMessages(%d, %d) = %d, want %d", tt.base, tt.attempt, got, tt.want)
			}
		})
	}
}
