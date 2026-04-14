package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

func TestExecuteAssistantToolCallsReturnsNilForEmptyCalls(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := &runState{}
	err := service.executeAssistantToolCalls(context.Background(), state, turnSnapshot{}, providertypes.Message{})
	if err != nil {
		t.Fatalf("executeAssistantToolCalls() error = %v", err)
	}
}

func TestExecuteOneToolCallStopsWhenContextCheckReturnsTrue(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := newRunState("run-stop", newRuntimeSession("session-stop"))
	called := false

	service.executeOneToolCall(
		context.Background(),
		&state,
		turnSnapshot{},
		providertypes.ToolCall{ID: "call-1", Name: "noop"},
		&sync.Mutex{},
		func() bool { return true },
		func(error) { called = true },
	)
	if called {
		t.Fatalf("rememberError should not be called when execution is short-circuited")
	}
}

func TestResolveToolParallelismBounds(t *testing.T) {
	t.Parallel()

	if got := resolveToolParallelism(0); got != 1 {
		t.Fatalf("resolveToolParallelism(0) = %d, want 1", got)
	}
	if got := resolveToolParallelism(2); got != 2 {
		t.Fatalf("resolveToolParallelism(2) = %d, want 2", got)
	}
	if got := resolveToolParallelism(defaultToolParallelism + 3); got != defaultToolParallelism {
		t.Fatalf("resolveToolParallelism(overflow) = %d, want %d", got, defaultToolParallelism)
	}
}

func TestRememberFirstErrorBranches(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var firstErr error
	errA := errors.New("err-a")
	errB := errors.New("err-b")

	if rememberFirstError(&mu, &firstErr, nil) {
		t.Fatalf("rememberFirstError(nil) should return false")
	}
	if firstErr != nil {
		t.Fatalf("firstErr should stay nil for nil input")
	}

	if !rememberFirstError(&mu, &firstErr, errA) {
		t.Fatalf("first non-nil error should be recorded")
	}
	if !errors.Is(firstErr, errA) {
		t.Fatalf("expected firstErr to be errA, got %v", firstErr)
	}

	if rememberFirstError(&mu, &firstErr, errB) {
		t.Fatalf("second error should not replace firstErr")
	}
	if !errors.Is(firstErr, errA) {
		t.Fatalf("expected firstErr to remain errA, got %v", firstErr)
	}
}

func TestTransitionRunPhaseNoopBranches(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 4)}
	service.transitionRunPhase(context.Background(), nil, controlplane.PhasePlan)

	state := newRunState("run-phase", newRuntimeSession("session-phase"))
	state.phase = controlplane.PhasePlan
	service.transitionRunPhase(context.Background(), &state, controlplane.PhasePlan)

	events := collectRuntimeEvents(service.Events())
	if len(events) != 0 {
		t.Fatalf("expected no phase event for noop transition, got %+v", events)
	}
}

func TestEmitRunTerminationMaxLoopsBranch(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 4)}
	state := newRunState("run-max-loops", newRuntimeSession("session-max-loops"))
	service.emitRunTermination(
		context.Background(),
		UserInput{RunID: "run-max-loops", SessionID: state.session.ID},
		&state,
		ErrMaxLoopReached,
	)

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventStopReasonDecided {
		t.Fatalf("expected one stop_reason_decided event, got %+v", events)
	}
	payload, ok := events[0].Payload.(StopReasonDecidedPayload)
	if !ok {
		t.Fatalf("expected StopReasonDecidedPayload, got %#v", events[0].Payload)
	}
	if payload.Reason != controlplane.StopReasonMaxLoops {
		t.Fatalf("reason = %q, want %q", payload.Reason, controlplane.StopReasonMaxLoops)
	}
}

func TestCloneMessagesReturnsNilForEmptyInput(t *testing.T) {
	t.Parallel()

	if cloned := cloneMessages(nil); cloned != nil {
		t.Fatalf("cloneMessages(nil) should return nil, got %#v", cloned)
	}
}

func TestEmitRunScopedNilStateFallsBackToBaseEnvelope(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 4)}
	if err := service.emitRunScoped(context.Background(), EventAgentChunk, nil, "chunk"); err != nil {
		t.Fatalf("emitRunScoped() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	event := events[0]
	if event.RunID != "" || event.SessionID != "" || event.Turn != turnUnspecified {
		t.Fatalf("unexpected fallback envelope: %+v", event)
	}
}

func TestEmitWithEnvelopeBackfillsVersionAndTimestamp(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 4)}
	if err := service.emitWithEnvelope(context.Background(), RuntimeEvent{
		Type:    EventAgentChunk,
		Payload: "chunk",
	}); err != nil {
		t.Fatalf("emitWithEnvelope() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	event := events[0]
	if event.PayloadVersion != controlplane.PayloadVersion {
		t.Fatalf("payload_version = %d, want %d", event.PayloadVersion, controlplane.PayloadVersion)
	}
	if event.Timestamp.IsZero() {
		t.Fatalf("expected timestamp to be set")
	}
}

func TestFinishRunPromotesLatestRemainingToken(t *testing.T) {
	t.Parallel()

	service := &Service{activeRunCancels: make(map[uint64]context.CancelFunc)}
	token1 := service.startRun(func() {})
	token2 := service.startRun(func() {})

	service.finishRun(token2)
	if service.activeRunToken != token1 {
		t.Fatalf("activeRunToken = %d, want %d", service.activeRunToken, token1)
	}
	if _, exists := service.activeRunCancels[token1]; !exists {
		t.Fatalf("expected token1 cancel handle to remain")
	}
}

func TestAcquireSessionLockReleaseGuards(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, releaseMissing := service.acquireSessionLock("session-missing")

	service.sessionMu.Lock()
	delete(service.sessionLocks, "session-missing")
	service.sessionMu.Unlock()
	releaseMissing()
	releaseMissing()

	_, releaseMismatch := service.acquireSessionLock("session-mismatch")
	service.sessionMu.Lock()
	service.sessionLocks["session-mismatch"] = &sessionLockEntry{}
	service.sessionMu.Unlock()
	releaseMismatch()
}

func TestPermissionAskLockKeyFallbacks(t *testing.T) {
	t.Parallel()

	if got := permissionAskLockKey("run-1", "session-1"); got != "run:run-1" {
		t.Fatalf("expected run scope key, got %q", got)
	}
	if got := permissionAskLockKey("", "session-1"); got != "session:session-1" {
		t.Fatalf("expected session scope key, got %q", got)
	}
	if got := permissionAskLockKey(" ", " "); got != "global" {
		t.Fatalf("expected global key fallback, got %q", got)
	}
}

func TestAcquirePermissionAskLockReleaseGuards(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, releaseMissing := service.acquirePermissionAskLock("run-missing", "")
	service.permissionAskMapMu.Lock()
	delete(service.permissionAskLocks, "run:run-missing")
	service.permissionAskMapMu.Unlock()
	releaseMissing()

	_, releaseMismatch := service.acquirePermissionAskLock("run-mismatch", "")
	service.permissionAskMapMu.Lock()
	service.permissionAskLocks["run:run-mismatch"] = &permissionAskLockEntry{}
	service.permissionAskMapMu.Unlock()
	releaseMismatch()
}

func TestClonePersistenceHelpersBranches(t *testing.T) {
	t.Parallel()

	if cloned := cloneMessagesForPersistence(nil); cloned != nil {
		t.Fatalf("cloneMessagesForPersistence(nil) should return nil")
	}
	message := providertypes.Message{Role: providertypes.RoleTool}
	clonedMessages := cloneMessagesForPersistence([]providertypes.Message{message})
	if len(clonedMessages) != 1 {
		t.Fatalf("expected one cloned message, got %d", len(clonedMessages))
	}
	if clonedMessages[0].ToolCalls != nil {
		t.Fatalf("expected nil tool calls for empty source")
	}
	if clonedMessages[0].ToolMetadata != nil {
		t.Fatalf("expected nil tool metadata for empty source")
	}

	if cloned := cloneTodosForPersistence(nil); cloned != nil {
		t.Fatalf("cloneTodosForPersistence(nil) should return nil")
	}
	todos := []agentsession.TodoItem{
		{ID: "t1", Content: "task", Dependencies: []string{"dep-1"}},
	}
	clonedTodos := cloneTodosForPersistence(todos)
	if len(clonedTodos) != 1 {
		t.Fatalf("expected one cloned todo, got %d", len(clonedTodos))
	}
	todos[0].Dependencies[0] = "changed"
	if clonedTodos[0].Dependencies[0] != "dep-1" {
		t.Fatalf("expected todo dependencies to be deep-copied")
	}
}
