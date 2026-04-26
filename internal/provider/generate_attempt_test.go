package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestRunGenerateWithRetryUsingRetriesBeforePayloadStarts(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   2,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			attempts++
			if attempts < 3 {
				return NewProviderErrorFromStatus(500, "temporary")
			}
			if emitErr := EmitTextDelta(ctx, attemptEvents, "ok"); emitErr != nil {
				return emitErr
			}
			return EmitMessageDone(ctx, attemptEvents, "stop", nil)
		},
	)
	if err != nil {
		t.Fatalf("RunGenerateWithRetryUsing() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}

	drained := drainAttemptEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected success events to be forwarded once, got %+v", drained)
	}
}

func TestRunGenerateWithRetryUsingDoesNotRetryAfterPayloadStarts(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   3,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			attempts++
			if emitErr := EmitTextDelta(ctx, attemptEvents, "partial"); emitErr != nil {
				return emitErr
			}
			return NewProviderErrorFromStatus(500, "temporary")
		},
	)
	if err == nil {
		t.Fatal("expected error after payload-started failure")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", attempts)
	}
}

func TestRunGenerateWithRetryUsingRetriesOnStartTimeout(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   1,
		GenerateStartTimeout: 20 * time.Millisecond,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			attempts++
			if attempts == 1 {
				<-ctx.Done()
				return ctx.Err()
			}
			if emitErr := EmitTextDelta(ctx, attemptEvents, "ok"); emitErr != nil {
				return emitErr
			}
			return EmitMessageDone(ctx, attemptEvents, "stop", nil)
		},
	)
	if err != nil {
		t.Fatalf("RunGenerateWithRetryUsing() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected retry after start timeout, got %d attempts", attempts)
	}
}

func TestRunGenerateWithRetryUsingStopsOnIdleTimeout(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   3,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  20 * time.Millisecond,
	}
	events := make(chan providertypes.StreamEvent, 8)
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			attempts++
			if emitErr := EmitTextDelta(ctx, attemptEvents, "partial"); emitErr != nil {
				return emitErr
			}
			<-ctx.Done()
			return ctx.Err()
		},
	)
	if !errors.Is(err, ErrGenerateIdleTimeout) {
		t.Fatalf("expected idle timeout error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected idle timeout to stop retries, got %d attempts", attempts)
	}
}

func TestRunGenerateWithRetryUsingDoesNotStartTimeoutAfterMessageDone(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   1,
		GenerateStartTimeout: 20 * time.Millisecond,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			if emitErr := EmitMessageDone(ctx, attemptEvents, "stop", nil); emitErr != nil {
				return emitErr
			}
			time.Sleep(40 * time.Millisecond)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("RunGenerateWithRetryUsing() error = %v", err)
	}
}

func TestRunGenerateWithRetryUsingDrainsEventsAfterTimeoutCancel(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   0,
		GenerateStartTimeout: 20 * time.Millisecond,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)

	done := make(chan error, 1)
	go func() {
		done <- RunGenerateWithRetryUsing(
			context.Background(),
			cfg,
			events,
			func(int) time.Duration { return 0 },
			func(context.Context, time.Duration) error { return nil },
			func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
				<-ctx.Done()
				for i := 0; i < 128; i++ {
					if err := EmitTextDelta(ctx, attemptEvents, "ignored"); err != nil {
						return err
					}
				}
				return ctx.Err()
			},
		)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrGenerateStartTimeout) {
			t.Fatalf("expected start timeout, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected generate attempt to return without deadlock")
	}
}

func TestRunGenerateWithRetryUsingTreatsMessageDoneAsCompletedState(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   1,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			if emitErr := EmitMessageDone(ctx, attemptEvents, "stop", nil); emitErr != nil {
				return emitErr
			}
			<-ctx.Done()
			return ctx.Err()
		},
	)
	if err != nil {
		t.Fatalf("expected completed attempt to ignore trailing cancellation, got %v", err)
	}

	drained := drainAttemptEvents(events)
	if len(drained) != 1 || drained[0].Type != providertypes.StreamEventMessageDone {
		t.Fatalf("expected only message_done to be forwarded, got %+v", drained)
	}
}

func TestRunGenerateWithRetryUsesDefaultRunner(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   0,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 8)

	err := RunGenerateWithRetry(
		context.Background(),
		cfg,
		events,
		func(ctx context.Context, attemptEvents chan<- providertypes.StreamEvent) error {
			if emitErr := EmitTextDelta(ctx, attemptEvents, "ok"); emitErr != nil {
				return emitErr
			}
			return EmitMessageDone(ctx, attemptEvents, "stop", nil)
		},
	)
	if err != nil {
		t.Fatalf("RunGenerateWithRetry() error = %v", err)
	}

	drained := drainAttemptEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected two forwarded events, got %d", len(drained))
	}
}

func TestRunGenerateWithRetryUsingReturnsRetryWaitError(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   1,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 4)
	waitErr := errors.New("wait failed")
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return time.Millisecond },
		func(context.Context, time.Duration) error { return waitErr },
		func(context.Context, chan<- providertypes.StreamEvent) error {
			attempts++
			return ErrStreamInterrupted
		},
	)
	if !errors.Is(err, waitErr) {
		t.Fatalf("expected retry wait error, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected only first attempt before wait failure, got %d", attempts)
	}
}

func TestRunGenerateWithRetryUsingReturnsLastErrorAfterExhaustedRetries(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{
		GenerateMaxRetries:   1,
		GenerateStartTimeout: time.Second,
		GenerateIdleTimeout:  time.Second,
	}
	events := make(chan providertypes.StreamEvent, 4)
	firstErr := NewProviderErrorFromStatus(500, "first")
	lastErr := NewProviderErrorFromStatus(500, "last")
	attempts := 0

	err := RunGenerateWithRetryUsing(
		context.Background(),
		cfg,
		events,
		func(int) time.Duration { return 0 },
		func(context.Context, time.Duration) error { return nil },
		func(context.Context, chan<- providertypes.StreamEvent) error {
			attempts++
			if attempts == 1 {
				return firstErr
			}
			return lastErr
		},
	)
	if !errors.Is(err, lastErr) {
		t.Fatalf("expected last retryable error, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts after exhausting retries, got %d", attempts)
	}
}

func TestWaitForRetryHonorsContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRetry(ctx, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForRetry() error = %v, want context canceled", err)
	}
}

func TestWaitForRetryReturnsNilOnNonPositiveWait(t *testing.T) {
	t.Parallel()

	if err := waitForRetry(context.Background(), 0); err != nil {
		t.Fatalf("waitForRetry() error = %v", err)
	}
}

func TestStopAndResetTimerHelpers(t *testing.T) {
	t.Parallel()

	stopTimer(nil)
	resetTimer(nil, time.Millisecond)

	timer := time.NewTimer(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	stopTimer(timer)
	select {
	case <-timer.C:
		t.Fatal("expected stopTimer to drain timer channel")
	default:
	}

	resetTimer(timer, 30*time.Millisecond)
	select {
	case <-timer.C:
		t.Fatal("expected resetTimer to apply new wait")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-timer.C:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected reset timer to fire")
	}
}

func TestUpdateGenerateAttemptPhaseTransitions(t *testing.T) {
	t.Parallel()

	var phase atomic.Uint32
	if got := updateGenerateAttemptPhase(
		providertypes.StreamEvent{Type: providertypes.StreamEventType("unknown")},
		&phase,
	); got != generateAttemptPhaseWaitingFirstPayload {
		t.Fatalf("unexpected initial phase = %v", got)
	}
	if got := updateGenerateAttemptPhase(
		providertypes.StreamEvent{Type: providertypes.StreamEventToolCallStart},
		&phase,
	); got != generateAttemptPhaseStreaming {
		t.Fatalf("expected streaming phase, got %v", got)
	}
	if got := updateGenerateAttemptPhase(
		providertypes.StreamEvent{Type: providertypes.StreamEventMessageDone},
		&phase,
	); got != generateAttemptPhaseCompleted {
		t.Fatalf("expected completed phase, got %v", got)
	}
	if got := updateGenerateAttemptPhase(
		providertypes.StreamEvent{Type: providertypes.StreamEventTextDelta},
		&phase,
	); got != generateAttemptPhaseCompleted {
		t.Fatalf("expected completed phase to remain terminal, got %v", got)
	}
}

func TestIsEffectiveGeneratePayloadEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType providertypes.StreamEventType
		want      bool
	}{
		{eventType: providertypes.StreamEventTextDelta, want: true},
		{eventType: providertypes.StreamEventToolCallStart, want: true},
		{eventType: providertypes.StreamEventToolCallDelta, want: true},
		{eventType: providertypes.StreamEventMessageDone, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.eventType), func(t *testing.T) {
			t.Parallel()
			if got := IsEffectiveGeneratePayloadEvent(providertypes.StreamEvent{Type: tc.eventType}); got != tc.want {
				t.Fatalf("IsEffectiveGeneratePayloadEvent(%s) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}

func drainAttemptEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	out := make([]providertypes.StreamEvent, 0, len(events))
	for {
		select {
		case event := <-events:
			out = append(out, event)
		default:
			return out
		}
	}
}
