package hooks

import (
	"context"
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
