package chatcompletions

import (
	"context"
	"errors"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestEmitStreamEvent_NilEventsChannel(t *testing.T) {
	t.Parallel()

	event := providertypes.NewTextDeltaStreamEvent("test")
	if err := emitStreamEvent(context.Background(), nil, event); err != nil {
		t.Fatalf("expected nil channel to return nil, got %v", err)
	}
}

func TestEmitStreamEvent_NormalSend(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 1)
	event := providertypes.NewTextDeltaStreamEvent("hello")
	if err := emitStreamEvent(context.Background(), events, event); err != nil {
		t.Fatalf("emitStreamEvent() error = %v", err)
	}

	got := <-events
	if got.Type != providertypes.StreamEventTextDelta {
		t.Fatalf("unexpected event type: %s", got.Type)
	}
}

func TestEmitStreamEvent_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	events := make(chan providertypes.StreamEvent)
	event := providertypes.NewTextDeltaStreamEvent("test")

	err := emitStreamEvent(ctx, events, event)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
