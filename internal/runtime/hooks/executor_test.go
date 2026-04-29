package hooks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingEmitter struct {
	mu     sync.Mutex
	events []HookEvent
	err    error
}

func (r *recordingEmitter) EmitHookEvent(ctx context.Context, event HookEvent) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return r.err
}

func (r *recordingEmitter) snapshot() []HookEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]HookEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestExecutorRunPass(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:      "hook-pass",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{
		RunID:     "run-1",
		SessionID: "session-1",
	})
	if output.Blocked {
		t.Fatalf("Blocked = true, want false")
	}
	if got := len(output.Results); got != 1 {
		t.Fatalf("len(Results) = %d, want 1", got)
	}
	if output.Results[0].Status != HookResultPass {
		t.Fatalf("Results[0].Status = %q, want pass", output.Results[0].Status)
	}

	events := emitter.snapshot()
	if got := len(events); got != 2 {
		t.Fatalf("len(events) = %d, want 2", got)
	}
	if events[0].Type != HookEventStarted || events[1].Type != HookEventFinished {
		t.Fatalf("event types = [%s, %s], want [hook_started, hook_finished]", events[0].Type, events[1].Type)
	}
}

func TestExecutorRunBlockShortCircuit(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)
	var calledSecond atomic.Int32

	if err := registry.Register(HookSpec{
		ID:       "hook-block",
		Point:    HookPointBeforeToolCall,
		Priority: 10,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultBlock, Message: "blocked"}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:       "hook-second",
		Point:    HookPointBeforeToolCall,
		Priority: 1,
		Handler: func(context.Context, HookContext) HookResult {
			calledSecond.Add(1)
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if !output.Blocked {
		t.Fatalf("Blocked = false, want true")
	}
	if output.BlockedBy != "hook-block" {
		t.Fatalf("BlockedBy = %q, want hook-block", output.BlockedBy)
	}
	if calledSecond.Load() != 0 {
		t.Fatalf("second hook called = %d, want 0", calledSecond.Load())
	}
}

func TestExecutorRunDowngradesBlockForObserveOnlyPoint(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 200*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-observe-only",
		Point: HookPointAfterToolFailure,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultBlock, Message: "should downgrade"}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointAfterToolFailure, HookContext{})
	if output.Blocked {
		t.Fatalf("blocked = true, want false for observe-only point")
	}
	if len(output.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(output.Results))
	}
	if output.Results[0].Status != HookResultPass {
		t.Fatalf("status = %q, want pass after downgrade", output.Results[0].Status)
	}
	if output.Results[0].Message == "" {
		t.Fatal("expected downgraded result to retain message for observability")
	}
}

func TestExecutorRunTimeout(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 10*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:      "hook-timeout",
		Point:   HookPointBeforeToolCall,
		Timeout: 10 * time.Millisecond,
		Handler: func(context.Context, HookContext) HookResult {
			time.Sleep(50 * time.Millisecond)
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if got := len(output.Results); got != 1 {
		t.Fatalf("len(Results) = %d, want 1", got)
	}
	if output.Results[0].Status != HookResultFailed {
		t.Fatalf("status = %q, want failed", output.Results[0].Status)
	}
	if !strings.Contains(output.Results[0].Error, "timed out") {
		t.Fatalf("error = %q, want timeout message", output.Results[0].Error)
	}

	events := emitter.snapshot()
	if got := len(events); got != 2 {
		t.Fatalf("len(events) = %d, want 2", got)
	}
	if events[1].Type != HookEventFailed {
		t.Fatalf("events[1].Type = %q, want hook_failed", events[1].Type)
	}
}

func TestExecutorRunPanicRecover(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-panic",
		Point: HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult {
			panic("boom")
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if got := len(output.Results); got != 1 {
		t.Fatalf("len(Results) = %d, want 1", got)
	}
	if output.Results[0].Status != HookResultFailed {
		t.Fatalf("status = %q, want failed", output.Results[0].Status)
	}
	if !strings.Contains(output.Results[0].Error, "panicked") {
		t.Fatalf("error = %q, want panic message", output.Results[0].Error)
	}
}

func TestExecutorRunFailOpenContinues(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)

	if err := registry.Register(HookSpec{
		ID:            "hook-fail-open",
		Point:         HookPointBeforeToolCall,
		Priority:      10,
		FailurePolicy: FailurePolicyFailOpen,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultFailed, Error: "failed-by-design"}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:       "hook-pass",
		Point:    HookPointBeforeToolCall,
		Priority: 1,
		Handler:  func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if output.Blocked {
		t.Fatalf("Blocked = true, want false")
	}
	if got := len(output.Results); got != 2 {
		t.Fatalf("len(Results) = %d, want 2", got)
	}
	if output.Results[0].Status != HookResultFailed || output.Results[1].Status != HookResultPass {
		t.Fatalf("statuses = [%q, %q], want [failed, pass]", output.Results[0].Status, output.Results[1].Status)
	}
}

func TestExecutorRunFailClosedBlocks(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)
	var calledSecond atomic.Int32

	if err := registry.Register(HookSpec{
		ID:            "hook-fail-closed",
		Point:         HookPointBeforeToolCall,
		Priority:      10,
		FailurePolicy: FailurePolicyFailClosed,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultFailed, Error: "hard-stop"}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:       "hook-second",
		Point:    HookPointBeforeToolCall,
		Priority: 1,
		Handler: func(context.Context, HookContext) HookResult {
			calledSecond.Add(1)
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if !output.Blocked {
		t.Fatalf("Blocked = false, want true")
	}
	if output.BlockedBy != "hook-fail-closed" {
		t.Fatalf("BlockedBy = %q, want hook-fail-closed", output.BlockedBy)
	}
	if calledSecond.Load() != 0 {
		t.Fatalf("second hook called = %d, want 0", calledSecond.Load())
	}
	if got := len(output.Results); got != 1 {
		t.Fatalf("len(Results) = %d, want 1", got)
	}
}

func TestExecutorRunNoHooks(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if output.Blocked {
		t.Fatalf("Blocked = true, want false")
	}
	if len(output.Results) != 0 {
		t.Fatalf("len(Results) = %d, want 0", len(output.Results))
	}
	if len(emitter.snapshot()) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(emitter.snapshot()))
	}
}

func TestExecutorEventPayloadCompleteness(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)

	if err := registry.Register(HookSpec{
		ID:      "hook-pass",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_ = executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	events := emitter.snapshot()
	if got := len(events); got != 2 {
		t.Fatalf("len(events) = %d, want 2", got)
	}
	finished := events[1]
	if finished.HookID == "" {
		t.Fatalf("HookID is empty")
	}
	if finished.Point == "" {
		t.Fatalf("Point is empty")
	}
	if finished.Status != HookResultPass {
		t.Fatalf("Status = %q, want pass", finished.Status)
	}
	if finished.StartedAt.IsZero() {
		t.Fatalf("StartedAt is zero")
	}
	if finished.DurationMS < 0 {
		t.Fatalf("DurationMS = %d, want >= 0", finished.DurationMS)
	}
}

func TestExecutorEventEmitterFailureIgnored(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{err: context.DeadlineExceeded}
	executor := NewExecutor(registry, emitter, 200*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:      "hook-pass",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if got := len(output.Results); got != 1 {
		t.Fatalf("len(Results) = %d, want 1", got)
	}
	if output.Results[0].Status != HookResultPass {
		t.Fatalf("status = %q, want pass", output.Results[0].Status)
	}
}

func TestExecutorRunSaturationProtection(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{}
	executor := NewExecutor(registry, emitter, 10*time.Millisecond)
	executor.maxInFlight = 1

	releaseCh := make(chan struct{})
	if err := registry.Register(HookSpec{
		ID:      "hook-blocking",
		Point:   HookPointBeforeToolCall,
		Timeout: 10 * time.Millisecond,
		Handler: func(context.Context, HookContext) HookResult {
			<-releaseCh
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	first := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if len(first.Results) != 1 || first.Results[0].Status != HookResultFailed {
		t.Fatalf("first run result = %+v, want single failed result", first.Results)
	}

	second := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if len(second.Results) != 1 {
		t.Fatalf("second len(Results) = %d, want 1", len(second.Results))
	}
	if second.Results[0].Status != HookResultFailed {
		t.Fatalf("second status = %q, want failed", second.Results[0].Status)
	}
	if !strings.Contains(second.Results[0].Error, "saturated") {
		t.Fatalf("second error = %q, want saturation message", second.Results[0].Error)
	}

	close(releaseCh)
	time.Sleep(20 * time.Millisecond)
	if got := executor.inFlight.Load(); got != 0 {
		t.Fatalf("inFlight = %d, want 0 after release", got)
	}
}

func TestNewExecutorDefaults(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(nil, nil, 0)
	if executor == nil {
		t.Fatalf("NewExecutor() = nil, want non-nil")
	}
	if executor.registry == nil {
		t.Fatalf("registry = nil, want auto-created registry")
	}
	if executor.defaultTimeout != DefaultHookTimeout {
		t.Fatalf("defaultTimeout = %v, want %v", executor.defaultTimeout, DefaultHookTimeout)
	}
	if executor.maxInFlight != DefaultMaxInFlightHooks {
		t.Fatalf("maxInFlight = %d, want %d", executor.maxInFlight, DefaultMaxInFlightHooks)
	}
}

func TestExecutorRunNilReceiverOrRegistry(t *testing.T) {
	t.Parallel()

	var nilExecutor *Executor
	if out := nilExecutor.Run(nil, HookPointBeforeToolCall, HookContext{}); len(out.Results) != 0 || out.Blocked {
		t.Fatalf("nil executor Run() = %+v, want zero output", out)
	}

	executor := &Executor{}
	if out := executor.Run(nil, HookPointBeforeToolCall, HookContext{}); len(out.Results) != 0 || out.Blocked {
		t.Fatalf("nil registry Run() = %+v, want zero output", out)
	}
}

func TestExecutorRunWithNilContextAndEmitter(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-pass",
		Point: HookPointBeforeToolCall,
		Handler: func(ctx context.Context, input HookContext) HookResult {
			if ctx == nil {
				t.Fatalf("handler ctx is nil")
			}
			_ = input
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	out := executor.Run(nil, HookPointBeforeToolCall, HookContext{})
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	if out.Results[0].Status != HookResultPass {
		t.Fatalf("status = %q, want pass", out.Results[0].Status)
	}
}

func TestExecutorRunInvalidStatus(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-invalid-status",
		Point: HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultStatus("unknown")}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	out := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	if out.Results[0].Status != HookResultFailed {
		t.Fatalf("status = %q, want failed", out.Results[0].Status)
	}
	if !strings.Contains(out.Results[0].Error, "invalid status") {
		t.Fatalf("error = %q, want invalid status", out.Results[0].Error)
	}
}

func TestExecutorRunFailedResultBackfill(t *testing.T) {
	t.Parallel()

	t.Run("failed with message only", func(t *testing.T) {
		t.Parallel()
		registry := NewRegistry()
		executor := NewExecutor(registry, nil, 100*time.Millisecond)
		if err := registry.Register(HookSpec{
			ID:    "hook-message-only",
			Point: HookPointBeforeToolCall,
			Handler: func(context.Context, HookContext) HookResult {
				return HookResult{Status: HookResultFailed, Message: "failed-message-only"}
			},
		}); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		out := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
		got := out.Results[0]
		if got.Error != "failed-message-only" {
			t.Fatalf("Error = %q, want failed-message-only", got.Error)
		}
	})

	t.Run("failed with empty message and error", func(t *testing.T) {
		t.Parallel()
		registry := NewRegistry()
		executor := NewExecutor(registry, nil, 100*time.Millisecond)
		if err := registry.Register(HookSpec{
			ID:    "hook-empty-failed",
			Point: HookPointBeforeToolCall,
			Handler: func(context.Context, HookContext) HookResult {
				return HookResult{Status: HookResultFailed}
			},
		}); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		out := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
		got := out.Results[0]
		if got.Error != "hook returned failed status" {
			t.Fatalf("Error = %q, want hook returned failed status", got.Error)
		}
		if got.Message != "hook returned failed status" {
			t.Fatalf("Message = %q, want hook returned failed status", got.Message)
		}
	})
}

func TestExecutorRunDefaultStatusAndTimingFallback(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	first := time.Unix(100, 0)
	second := first.Add(-10 * time.Millisecond)
	var nowCalls atomic.Int32
	executor.now = func() time.Time {
		if nowCalls.Add(1) == 1 {
			return first
		}
		return second
	}

	if err := registry.Register(HookSpec{
		ID:    "hook-empty-result",
		Point: HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	out := executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	got := out.Results[0]
	if got.Status != HookResultPass {
		t.Fatalf("Status = %q, want pass", got.Status)
	}
	if got.StartedAt != first {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, first)
	}
	if got.DurationMS != 0 {
		t.Fatalf("DurationMS = %d, want 0", got.DurationMS)
	}
	if got.HookID != "hook-empty-result" {
		t.Fatalf("HookID = %q, want hook-empty-result", got.HookID)
	}
	if got.Point != HookPointBeforeToolCall {
		t.Fatalf("Point = %q, want %q", got.Point, HookPointBeforeToolCall)
	}
}

func TestExecutorRunCanceledContext(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-canceled",
		Point: HookPointBeforeToolCall,
		Handler: func(ctx context.Context, input HookContext) HookResult {
			<-ctx.Done()
			_ = input
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := executor.Run(ctx, HookPointBeforeToolCall, HookContext{})
	if len(out.Results) != 1 {
		t.Fatalf("len(out.Results) = %d, want 1", len(out.Results))
	}
	if out.Results[0].Status != HookResultFailed {
		t.Fatalf("Status = %q, want failed", out.Results[0].Status)
	}
	if !strings.Contains(out.Results[0].Error, "canceled") {
		t.Fatalf("Error = %q, want canceled", out.Results[0].Error)
	}
}

func TestExecutorWithHookTimeoutNoTimeoutPath(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(NewRegistry(), nil, 100*time.Millisecond)
	executor.defaultTimeout = 0

	ctx, cancel := executor.withHookTimeout(context.Background(), 0)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("context done unexpectedly: %v", ctx.Err())
	default:
	}
}

func TestExecutorSlotAndEmitterHelpers(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(NewRegistry(), nil, 100*time.Millisecond)
	executor.maxInFlight = 0
	if !executor.tryAcquireSlot() {
		t.Fatalf("tryAcquireSlot() = false, want true when limit <= 0")
	}

	executor.maxInFlight = -1
	executor.releaseSlot()

	executor.emitBestEffort(context.Background(), HookEvent{})

	var nilExecutor *Executor
	nilExecutor.releaseSlot()
	nilExecutor.emitBestEffort(context.Background(), HookEvent{})
}

func TestExecutorRunSetsFailedEventErrorOnInvalidStatus(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	emitter := &recordingEmitter{err: errors.New("emit failed")}
	executor := NewExecutor(registry, emitter, 100*time.Millisecond)
	if err := registry.Register(HookSpec{
		ID:    "hook-invalid",
		Point: HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult {
			return HookResult{Status: HookResultStatus("bad")}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_ = executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{})
	events := emitter.snapshot()
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[1].Type != HookEventFailed {
		t.Fatalf("events[1].Type = %q, want hook_failed", events[1].Type)
	}
	if events[1].Error == "" {
		t.Fatalf("events[1].Error is empty")
	}
}

func TestExecutorSanitizeUserHookContext(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	var captured HookContext
	if err := registry.Register(HookSpec{
		ID:    "user-hook",
		Point: HookPointBeforeToolCall,
		Scope: HookScopeUser,
		Handler: func(_ context.Context, input HookContext) HookResult {
			captured = input
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_ = executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{
		RunID:     "run-1",
		SessionID: "session-1",
		Metadata: map[string]any{
			"tool_name":        "bash",
			"tool_arguments":   "--secret-token=abc",
			"capability_token": "should-not-leak",
			"workdir":          "/tmp/work",
		},
	})

	if strings.TrimSpace(captured.RunID) != "run-1" || strings.TrimSpace(captured.SessionID) != "session-1" {
		t.Fatalf("unexpected run/session: %+v", captured)
	}
	if got := captured.Metadata["tool_name"]; got != "bash" {
		t.Fatalf("tool_name = %v, want bash", got)
	}
	if _, exists := captured.Metadata["tool_arguments"]; exists {
		t.Fatal("tool_arguments should be stripped for user hook context")
	}
	if _, exists := captured.Metadata["capability_token"]; exists {
		t.Fatal("capability_token should be stripped for user hook context")
	}
}

func TestExecutorSanitizeRepoHookContext(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, 100*time.Millisecond)
	var captured HookContext
	if err := registry.Register(HookSpec{
		ID:     "repo-hook",
		Point:  HookPointBeforeToolCall,
		Scope:  HookScopeRepo,
		Source: HookSourceRepo,
		Handler: func(_ context.Context, input HookContext) HookResult {
			captured = input
			return HookResult{Status: HookResultPass}
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_ = executor.Run(context.Background(), HookPointBeforeToolCall, HookContext{
		RunID:     "run-1",
		SessionID: "session-1",
		Metadata: map[string]any{
			"tool_name":        "bash",
			"tool_arguments":   "--secret-token=abc",
			"capability_token": "should-not-leak",
			"workdir":          "/tmp/work",
		},
	})

	if got := captured.Metadata["tool_name"]; got != "bash" {
		t.Fatalf("tool_name = %v, want bash", got)
	}
	if _, exists := captured.Metadata["tool_arguments"]; exists {
		t.Fatal("tool_arguments should be stripped for repo hook context")
	}
	if _, exists := captured.Metadata["capability_token"]; exists {
		t.Fatal("capability_token should be stripped for repo hook context")
	}
}
