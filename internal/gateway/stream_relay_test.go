package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

func TestStreamRelayBindAndFallbackSession(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-1",
		RunID:     "run-1",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	fallbackSessionID := relay.ResolveFallbackSessionID(connectionID)
	if fallbackSessionID != "session-1" {
		t.Fatalf("fallback session id = %q, want %q", fallbackSessionID, "session-1")
	}
}

func TestStreamRelayPublishRuntimeEventNoCrossSession(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messageChA := make(chan RelayMessage, 2)
	messageChB := make(chan RelayMessage, 2)
	registerConnectionForRelayTest(t, relay, ctx, StreamChannelWS, "session-a", "run-a", messageChA)
	registerConnectionForRelayTest(t, relay, ctx, StreamChannelWS, "session-b", "run-b", messageChB)

	relay.PublishRuntimeEvent(RuntimeEvent{
		Type:      RuntimeEventTypeRunProgress,
		SessionID: "session-a",
		RunID:     "run-a",
		Payload: map[string]string{
			"chunk": "hello",
		},
	})

	select {
	case message := <-messageChA:
		if message.Kind != relayMessageKindJSON {
			t.Fatalf("message kind = %q, want %q", message.Kind, relayMessageKindJSON)
		}
		notification, ok := message.Payload.(protocol.JSONRPCNotification)
		if !ok {
			t.Fatalf("payload type = %T, want protocol.JSONRPCNotification", message.Payload)
		}
		if notification.Method != protocol.MethodGatewayEvent {
			t.Fatalf("method = %q, want %q", notification.Method, protocol.MethodGatewayEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("expected session-a to receive runtime event")
	}

	select {
	case <-messageChB:
		t.Fatal("session-b should not receive session-a event")
	default:
	}
}

func TestStreamRelayQueueOverflowDropsSlowConnection(t *testing.T) {
	blockWrite := make(chan struct{})
	relay := NewStreamRelay(StreamRelayOptions{
		QueueSize: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			<-blockWrite
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer close(blockWrite)

	response := protocol.NewJSONRPCErrorResponse(
		json.RawMessage(`"queue-1"`),
		protocol.NewJSONRPCError(protocol.JSONRPCCodeInternalError, "boom", protocol.GatewayCodeInternalError),
	)
	if !relay.SendJSONRPCResponse(connectionID, response) {
		t.Fatal("first enqueue should succeed")
	}

	dropped := false
	for attempt := 0; attempt < 8; attempt++ {
		if !relay.SendJSONRPCResponse(connectionID, response) {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatal("expected slow connection to be dropped when queue overflows")
	}
}

func TestStreamRelayCleanupExpiredBindings(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		BindingTTL:      20 * time.Millisecond,
		CleanupInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	relay.Start(ctx, nil)

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-expire",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	time.Sleep(60 * time.Millisecond)
	if sessionID := relay.ResolveFallbackSessionID(connectionID); sessionID != "" {
		t.Fatalf("expired fallback session id = %q, want empty", sessionID)
	}
}

func TestStreamRelayRestartCleanupLoopAfterContextDone(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		CleanupInterval: 5 * time.Millisecond,
	})

	firstCtx, firstCancel := context.WithCancel(context.Background())
	relay.Start(firstCtx, nil)
	firstCancel()

	waitForStreamRelayState(t, relay, false)

	secondCtx, secondCancel := context.WithCancel(context.Background())
	relay.Start(secondCtx, nil)
	waitForStreamRelayState(t, relay, true)

	secondCancel()
	waitForStreamRelayState(t, relay, false)
}

func TestStreamRelayBindConnectionLimit(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		MaxBindingsPerConnection: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-1",
		RunID:     "run-1",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("first bind: %v", bindErr)
	}

	bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-1",
		RunID:     "run-2",
		Channel:   StreamChannelAll,
		Explicit:  true,
	})
	if bindErr == nil || bindErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("second bind error = %#v, want invalid_action", bindErr)
	}
}

func TestStreamRelayBindConnectionPrunesExpiredBindingsBeforeLimitCheck(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		MaxBindingsPerConnection: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-old",
		RunID:     "run-1",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("first bind: %v", bindErr)
	}

	relay.mu.Lock()
	connectionBindings := relay.connectionBindings[connectionID]
	state := connectionBindings[bindingKey{sessionID: "session-old", runID: "run-1"}]
	if state == nil {
		relay.mu.Unlock()
		t.Fatal("expected old binding state")
	}
	state.expireAt = time.Now().Add(-time.Second)
	relay.mu.Unlock()

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-new",
		RunID:     "run-2",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("second bind after pruning expired binding: %v", bindErr)
	}

	relay.mu.RLock()
	defer relay.mu.RUnlock()
	if _, exists := relay.sessionIndex["session-old"]; exists {
		t.Fatalf("expired session index should be removed: %#v", relay.sessionIndex["session-old"])
	}
	if _, exists := relay.sessionIndex["session-new"][connectionID]; !exists {
		t.Fatal("new session index should include current connection")
	}
}

func TestStreamRelayRefreshConnectionBindingsPreservesLastSeen(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-a",
		RunID:     "run-a",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind session-a: %v", bindErr)
	}
	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: "session-b",
		RunID:     "run-b",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind session-b: %v", bindErr)
	}

	expectedLastSeenA := time.Now().Add(-2 * time.Minute)
	expectedLastSeenB := time.Now().Add(-1 * time.Minute)

	relay.mu.Lock()
	for key, state := range relay.connectionBindings[connectionID] {
		if state == nil {
			continue
		}
		state.expireAt = time.Now().Add(2 * time.Minute)
		switch key.sessionID {
		case "session-a":
			state.lastSeen = expectedLastSeenA
		case "session-b":
			state.lastSeen = expectedLastSeenB
		}
	}
	relay.mu.Unlock()

	if refreshed := relay.RefreshConnectionBindings(connectionID); !refreshed {
		t.Fatal("expected refresh to succeed")
	}

	relay.mu.RLock()
	lastSeenA := relay.connectionBindings[connectionID][bindingKey{sessionID: "session-a", runID: "run-a"}].lastSeen
	lastSeenB := relay.connectionBindings[connectionID][bindingKey{sessionID: "session-b", runID: "run-b"}].lastSeen
	relay.mu.RUnlock()

	if !lastSeenA.Equal(expectedLastSeenA) {
		t.Fatalf("session-a lastSeen changed: got %v, want %v", lastSeenA, expectedLastSeenA)
	}
	if !lastSeenB.Equal(expectedLastSeenB) {
		t.Fatalf("session-b lastSeen changed: got %v, want %v", lastSeenB, expectedLastSeenB)
	}

	if fallbackSessionID := relay.ResolveFallbackSessionID(connectionID); fallbackSessionID != "session-b" {
		t.Fatalf("fallback session id = %q, want %q", fallbackSessionID, "session-b")
	}
}

func TestStreamRelaySendJSONRPCResponseSync(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	messageCh := make(chan RelayMessage, 1)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			messageCh <- message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	response, rpcErr := protocol.NewJSONRPCResultResponse(json.RawMessage(`"sync-1"`), MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionPing,
		RequestID: "sync-1",
	})
	if rpcErr != nil {
		t.Fatalf("build rpc response: %v", rpcErr)
	}
	if ok := relay.SendJSONRPCResponseSync(connectionID, response); !ok {
		t.Fatal("expected sync response send to succeed")
	}

	select {
	case message := <-messageCh:
		if message.Kind != relayMessageKindJSON {
			t.Fatalf("message kind = %q, want %q", message.Kind, relayMessageKindJSON)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive sync response message")
	}
}

func waitForStreamRelayState(t *testing.T, relay *StreamRelay, wantStarted bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		relay.mu.RLock()
		started := relay.cleanupStarted
		relay.mu.RUnlock()
		if started == wantStarted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cleanupStarted state mismatch: want %v", wantStarted)
}

func registerConnectionForRelayTest(
	t *testing.T,
	relay *StreamRelay,
	ctx context.Context,
	channel StreamChannel,
	sessionID string,
	runID string,
	messageCh chan RelayMessage,
) {
	t.Helper()

	connectionID := NewConnectionID()
	connectionCtx, cancelConnection := context.WithCancel(ctx)
	connectionCtx = WithConnectionID(connectionCtx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      channel,
		Context:      connectionCtx,
		Cancel:       cancelConnection,
		Write: func(message RelayMessage) error {
			messageCh <- message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() {
		relay.dropConnection(connectionID)
	})

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: sessionID,
		RunID:     runID,
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}
}
