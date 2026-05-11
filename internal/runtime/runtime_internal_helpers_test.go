package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/approval"
	"neo-code/internal/runtime/controlplane"
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
	appendFn func(ctx context.Context, input agentsession.AppendMessagesInput) error
}

func (s *lockProbeStore) CreateSession(ctx context.Context, input agentsession.CreateSessionInput) (agentsession.Session, error) {
	return agentsession.Session{}, errors.New("not implemented")
}

func (s *lockProbeStore) AppendMessages(ctx context.Context, input agentsession.AppendMessagesInput) error {
	if s.appendFn == nil {
		return nil
	}
	return s.appendFn(ctx, input)
}

func (s *lockProbeStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, errors.New("not implemented")
}

func (s *lockProbeStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, errors.New("not implemented")
}

// UpdateSessionWorkdir 仅为接口占位，当前测试不会走到该分支。
func (s *lockProbeStore) UpdateSessionWorkdir(ctx context.Context, input agentsession.UpdateSessionWorkdirInput) error {
	return errors.New("not implemented")
}

func (s *lockProbeStore) UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error {
	return errors.New("not implemented")
}

func (s *lockProbeStore) ReplaceTranscript(ctx context.Context, input agentsession.ReplaceTranscriptInput) error {
	return errors.New("not implemented")
}

func (s *lockProbeStore) CleanupExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	return 0, nil
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

func TestValidateUserInputPartsAcceptsPureImage(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewRemoteImagePart("https://example.com/image.png"),
	}
	if err := validateUserInputParts(parts); err != nil {
		t.Fatalf("validateUserInputParts() error = %v", err)
	}
}

func TestValidateUserInputPartsRejectsInvalidAndEmptyContent(t *testing.T) {
	t.Parallel()

	if err := validateUserInputParts(nil); err == nil || err.Error() != "runtime: input parts is empty" {
		t.Fatalf("expected empty parts error, got %v", err)
	}

	err := validateUserInputParts([]providertypes.ContentPart{{Kind: providertypes.ContentPartKind("unknown")}})
	if err == nil || !strings.Contains(err.Error(), "invalid input parts") {
		t.Fatalf("expected invalid parts error, got %v", err)
	}

	err = validateUserInputParts([]providertypes.ContentPart{providertypes.NewTextPart(" \t ")})
	if err == nil || err.Error() != "runtime: input content is empty" {
		t.Fatalf("expected empty content error, got %v", err)
	}
}

func TestSessionTitleFromParts(t *testing.T) {
	t.Parallel()

	title := sessionTitleFromParts([]providertypes.ContentPart{
		providertypes.NewTextPart("   "),
		providertypes.NewTextPart("  First line  "),
	})
	if title != "First line" {
		t.Fatalf("sessionTitleFromParts() = %q, want %q", title, "First line")
	}

	title = sessionTitleFromParts([]providertypes.ContentPart{
		providertypes.NewRemoteImagePart("https://example.com/image.png"),
	})
	if title != "Image Message" {
		t.Fatalf("sessionTitleFromParts(image) = %q", title)
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
	state.session.HasUnknownUsage = true
	state.hasUnknownUsage = true
	if state.session.TokenInputTotal != 11 || state.session.TokenOutputTotal != 22 {
		t.Fatalf("unexpected token totals: in=%d out=%d", state.session.TokenInputTotal, state.session.TokenOutputTotal)
	}

	state.resetTokenTotals()
	if state.session.TokenInputTotal != 0 || state.session.TokenOutputTotal != 0 {
		t.Fatalf("expected reset totals to be zero, got in=%d out=%d", state.session.TokenInputTotal, state.session.TokenOutputTotal)
	}
	if state.session.HasUnknownUsage || state.hasUnknownUsage {
		t.Fatalf("expected resetTokenTotals to clear unknown usage flags")
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
	snapshot := TurnBudgetSnapshot{
		ProviderConfig: providerRuntimeConfigForTest("openai"),
		Model:          "gpt-4.1",
	}

	if err := service.appendAssistantMessageAndSave(
		context.Background(),
		&state,
		snapshot,
		providertypes.Message{Role: providertypes.RoleAssistant},
		0,
		0,
	); err != nil {
		t.Fatalf("appendAssistantMessageAndSave() error = %v", err)
	}
	if store.saves != 1 {
		t.Fatalf("expected metadata change to persist once, saves=%d", store.saves)
	}

	store.saves = 0
	state.session.Provider = snapshot.ProviderConfig.Name
	state.session.Model = snapshot.Model
	if err := service.appendAssistantMessageAndSave(
		context.Background(),
		&state,
		snapshot,
		providertypes.Message{Role: providertypes.RoleAssistant},
		0,
		0,
	); err != nil {
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

func TestAppendToolMessageAndSavePreservesMetadataOnlySuccessResult(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-metadata-only")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-metadata-only", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{
		Name:    "filesystem_read_file",
		Content: "",
		Metadata: map[string]any{
			"path": "README.md",
		},
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if renderPartsForTest(msg.Parts) != "" {
		t.Fatalf("expected metadata-only success result to keep empty content, got %q", renderPartsForTest(msg.Parts))
	}
	if msg.ToolMetadata["tool_name"] != "filesystem_read_file" || msg.ToolMetadata["path"] != "README.md" {
		t.Fatalf("expected metadata-only success result to keep sanitized metadata, got %+v", msg.ToolMetadata)
	}
}

func TestAppendToolMessageAndSaveNormalizesSemanticallyEmptySuccessResult(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-empty-success")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-empty-success", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{
		Name:    "filesystem_read_file",
		Content: "   ",
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if renderPartsForTest(msg.Parts) != "ok" {
		t.Fatalf("expected empty success result to be normalized to ok, got %q", renderPartsForTest(msg.Parts))
	}
	if msg.ToolMetadata["tool_name"] != "filesystem_read_file" {
		t.Fatalf("expected tool_name metadata to be preserved after normalization, got %+v", msg.ToolMetadata)
	}
}

func TestAppendToolMessageAndSaveNormalizesToolNameOnlyMetadataSuccessResult(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-name-only-metadata-success")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-name-only-metadata-success", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{
		Name:    "filesystem_read_file",
		Content: "   ",
		Metadata: map[string]any{
			"unsupported_key": "ignored",
		},
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if renderPartsForTest(msg.Parts) != "ok" {
		t.Fatalf("expected tool_name-only metadata success to normalize content to ok, got %q", renderPartsForTest(msg.Parts))
	}
	if len(msg.ToolMetadata) != 1 || msg.ToolMetadata["tool_name"] != "filesystem_read_file" {
		t.Fatalf("expected only tool_name metadata to remain, got %+v", msg.ToolMetadata)
	}
}

func TestAppendToolMessageAndSaveFallsBackToCallNameForToolMetadata(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-name-fallback")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-name-fallback", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file"}
	result := tools.ToolResult{
		Content: "ok",
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if msg.ToolMetadata["tool_name"] != "filesystem_read_file" {
		t.Fatalf("expected tool_name fallback from call name, got %+v", msg.ToolMetadata)
	}
}

func TestAppendToolMessageAndSaveMarksMetadataOkFalseAsError(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-ok-false")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-ok-false", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "bash"}
	result := tools.ToolResult{
		Name:    "bash",
		Content: "",
		Metadata: map[string]any{
			"ok": false,
		},
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if !msg.IsError {
		t.Fatalf("expected message to be marked as error when metadata ok=false")
	}
	if got := renderPartsForTest(msg.Parts); got != "tool execution failed (ok=false)" {
		t.Fatalf("expected fallback error content, got %q", got)
	}
}

func TestAppendToolMessageAndSaveMarksStringAndNumericOkFalseAsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]any
	}{
		{
			name: "string false",
			metadata: map[string]any{
				"ok": "false",
			},
		},
		{
			name: "string zero",
			metadata: map[string]any{
				"ok": "0",
			},
		},
		{
			name: "numeric zero",
			metadata: map[string]any{
				"ok": 0,
			},
		},
		{
			name: "float zero",
			metadata: map[string]any{
				"ok": 0.0,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMemoryStore()
			session := newRuntimeSession("session-append-tool-ok-false-" + strings.ReplaceAll(tt.name, " ", "-"))
			store.sessions[session.ID] = cloneSession(session)

			service := &Service{sessionStore: store}
			state := newRunState("run-append-tool-ok-false-"+strings.ReplaceAll(tt.name, " ", "-"), session)
			call := providertypes.ToolCall{ID: "call-1", Name: "bash"}
			result := tools.ToolResult{
				Name:     "bash",
				Content:  "",
				Metadata: tt.metadata,
			}

			if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
				t.Fatalf("appendToolMessageAndSave() error = %v", err)
			}

			msg := state.session.Messages[0]
			if !msg.IsError {
				t.Fatalf("expected message to be marked as error when metadata ok=%v", tt.metadata["ok"])
			}
			if got := renderPartsForTest(msg.Parts); got != "tool execution failed (ok=false)" {
				t.Fatalf("expected fallback error content, got %q", got)
			}
		})
	}
}

func TestAppendToolMessageAndSaveMarksExitCodeNonZeroAsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exitCode any
	}{
		{name: "int", exitCode: 1},
		{name: "string", exitCode: "2"},
		{name: "float", exitCode: 3.0},
		{name: "float fractional", exitCode: 0.5},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMemoryStore()
			session := newRuntimeSession("session-append-tool-exit-code-" + tt.name)
			store.sessions[session.ID] = cloneSession(session)

			service := &Service{sessionStore: store}
			state := newRunState("run-append-tool-exit-code-"+tt.name, session)
			call := providertypes.ToolCall{ID: "call-1", Name: "bash"}
			result := tools.ToolResult{
				Name:    "bash",
				Content: "",
				Metadata: map[string]any{
					"exit_code": tt.exitCode,
				},
			}

			if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
				t.Fatalf("appendToolMessageAndSave() error = %v", err)
			}

			msg := state.session.Messages[0]
			if !msg.IsError {
				t.Fatalf("expected message to be marked as error when metadata exit_code=%v", tt.exitCode)
			}
		})
	}
}

func TestAppendToolMessageAndSaveDoesNotMarkInvalidOkStringAsError(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-append-tool-invalid-ok")
	store.sessions[session.ID] = cloneSession(session)

	service := &Service{sessionStore: store}
	state := newRunState("run-append-tool-invalid-ok", session)
	call := providertypes.ToolCall{ID: "call-1", Name: "bash"}
	result := tools.ToolResult{
		Name:    "bash",
		Content: "",
		Metadata: map[string]any{
			"ok": "not-bool",
		},
	}

	if err := service.appendToolMessageAndSave(context.Background(), &state, call, result); err != nil {
		t.Fatalf("appendToolMessageAndSave() error = %v", err)
	}

	msg := state.session.Messages[0]
	if msg.IsError {
		t.Fatalf("expected invalid ok string to keep non-error result")
	}
}

func TestAppendToolMessageAndSaveUnlocksStateBeforePersist(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-append-tool-lock")
	state := newRunState("run-append-tool-lock", session)

	store := &lockProbeStore{
		appendFn: func(_ context.Context, _ agentsession.AppendMessagesInput) error {
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

	service.emitTokenUsage(context.Background(), state, ledgerReconcileResult{})
	events := collectRuntimeEvents(service.Events())
	if len(events) != 0 {
		t.Fatalf("expected no token event for zero usage, got %+v", events)
	}

	state.recordUsage(5, 7)
	service.emitTokenUsage(context.Background(), state, ledgerReconcileResult{
		inputTokens:  5,
		inputSource:  usageSourceObserved,
		outputTokens: 7,
		outputSource: usageSourceObserved,
	})
	events = collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventTokenUsage {
		t.Fatalf("expected one token usage event, got %+v", events)
	}
}

func TestReconcileLedgerSupportsPartialObservation(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := &runState{session: newRuntimeSession("session-partial-observed")}
	id := controlplane.TurnBudgetID{AttemptSeq: 2, RequestHash: "hash-partial-observed"}
	decision := controlplane.TurnBudgetDecision{
		ID:                   id,
		EstimatedInputTokens: 37,
	}
	observation := TurnBudgetUsageObservation{
		ID:             id,
		InputTokens:    13,
		OutputTokens:   0,
		InputObserved:  true,
		OutputObserved: false,
	}

	result, err := service.reconcileLedger(state, decision, observation)
	if err != nil {
		t.Fatalf("reconcileLedger() error = %v", err)
	}
	if result.inputTokens != 13 || result.inputSource != usageSourceObserved {
		t.Fatalf("expected observed input reconciliation, got %+v", result)
	}
	if result.outputTokens != 0 || result.outputSource != usageSourceUnknown {
		t.Fatalf("expected unknown output reconciliation, got %+v", result)
	}
	if !result.hasUnknownUsage {
		t.Fatalf("expected hasUnknownUsage=true for partial observation")
	}
	if !state.session.HasUnknownUsage || !state.hasUnknownUsage {
		t.Fatalf("expected unknown usage flag to propagate to run state")
	}
}

func TestReconcileLedgerUsesEstimateWhenInputNotObserved(t *testing.T) {
	t.Parallel()

	service := &Service{}
	id := controlplane.TurnBudgetID{AttemptSeq: 3, RequestHash: "hash-no-input-observed"}
	decision := controlplane.TurnBudgetDecision{
		ID:                   id,
		EstimatedInputTokens: 41,
	}
	observation := TurnBudgetUsageObservation{
		ID:             id,
		InputTokens:    0,
		OutputTokens:   7,
		InputObserved:  false,
		OutputObserved: true,
	}

	result, err := service.reconcileLedger(nil, decision, observation)
	if err != nil {
		t.Fatalf("reconcileLedger() error = %v", err)
	}
	if result.inputTokens != 41 || result.inputSource != usageSourceEstimated {
		t.Fatalf("expected estimated input reconciliation, got %+v", result)
	}
	if result.outputTokens != 7 || result.outputSource != usageSourceObserved {
		t.Fatalf("expected observed output reconciliation, got %+v", result)
	}
	if !result.hasUnknownUsage {
		t.Fatalf("expected hasUnknownUsage=true when any side is unobserved")
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
	snapshot := TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second}

	if _, err := service.executeAssistantToolCalls(context.Background(), &state, snapshot, assistant); err != nil {
		t.Fatalf("executeAssistantToolCalls() error = %v", err)
	}
	if len(state.session.Messages) != 1 {
		t.Fatalf("expected one tool message, got %d", len(state.session.Messages))
	}
	if renderPartsForTest(state.session.Messages[0].Parts) != toolErr.Error() {
		t.Fatalf("expected tool error content fallback, got %q", renderPartsForTest(state.session.Messages[0].Parts))
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
	snapshot := TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second}

	_, err := service.executeAssistantToolCalls(context.Background(), &state, snapshot, assistant)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from save failure, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventToolStart, EventToolResult})
}

func TestExecuteAssistantToolCallsPersistsResultsInCallOrder(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-exec-tool-order")
	store.sessions[session.ID] = cloneSession(session)

	var mu sync.Mutex
	active := 0
	maxActive := 0
	manager := &stubToolManager{
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			if input.Name == "tool_slow" {
				time.Sleep(50 * time.Millisecond)
			} else {
				time.Sleep(10 * time.Millisecond)
			}
			mu.Lock()
			active--
			mu.Unlock()
			return tools.ToolResult{Name: input.Name, Content: input.Name + " done"}, nil
		},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    manager,
		approvalBroker: approval.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-exec-tool-order", session)
	assistant := providertypes.Message{
		Role: providertypes.RoleAssistant,
		ToolCalls: []providertypes.ToolCall{
			{ID: "call-slow", Name: "tool_slow", Arguments: `{}`},
			{ID: "call-fast", Name: "tool_fast", Arguments: `{}`},
		},
	}

	if _, err := service.executeAssistantToolCalls(context.Background(), &state, TurnBudgetSnapshot{}, assistant); err != nil {
		t.Fatalf("executeAssistantToolCalls() error = %v", err)
	}
	if maxActive < 2 {
		t.Fatalf("expected tools to execute concurrently, maxActive=%d", maxActive)
	}
	if len(state.session.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(state.session.Messages))
	}
	got := []string{
		renderPartsForTest(state.session.Messages[0].Parts),
		renderPartsForTest(state.session.Messages[1].Parts),
	}
	want := []string{"tool_slow done", "tool_fast done"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("persisted tool messages = %#v, want %#v", got, want)
	}
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

	if err := service.Run(context.Background(), UserInput{RunID: "run-memo-extract", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
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
	session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
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
