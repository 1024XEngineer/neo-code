package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

func TestDecodeBindStreamParamsAndReadStringValueBranches(t *testing.T) {
	t.Run("nil bind stream pointer", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams((*protocol.BindStreamParams)(nil))
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want invalid_frame", frameErr)
		}
	})

	t.Run("map payload and trim", func(t *testing.T) {
		params, frameErr := decodeBindStreamParams(map[string]any{
			"session_id": "  s-1  ",
			"run_id":     123,
			"channel":    "  ws  ",
		})
		if frameErr != nil {
			t.Fatalf("decode bind stream params: %v", frameErr)
		}
		if params.SessionID != "s-1" {
			t.Fatalf("session_id = %q, want %q", params.SessionID, "s-1")
		}
		if params.RunID != "" {
			t.Fatalf("run_id = %q, want empty", params.RunID)
		}
		if params.Channel != StreamChannelWS {
			t.Fatalf("channel = %q, want %q", params.Channel, StreamChannelWS)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		type badPayload struct {
			Bad chan int `json:"bad"`
		}
		_, frameErr := decodeBindStreamParams(badPayload{Bad: make(chan int)})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want invalid_frame", frameErr)
		}
	})

	t.Run("invalid bind stream channel", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams(protocol.BindStreamParams{SessionID: "s-1", Channel: "tcp"})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("frameErr = %#v, want invalid_action", frameErr)
		}
	})

	t.Run("read string helper", func(t *testing.T) {
		payload := map[string]any{
			"str": "  value  ",
			"num": 1,
		}
		if got := readStringValue(payload, "missing"); got != "" {
			t.Fatalf("missing value = %q, want empty", got)
		}
		if got := readStringValue(payload, "num"); got != "" {
			t.Fatalf("non-string value = %q, want empty", got)
		}
		if got := readStringValue(payload, "str"); got != "value" {
			t.Fatalf("string value = %q, want %q", got, "value")
		}
	})
}

func TestConnectionContextAdditionalBranches(t *testing.T) {
	ctx := WithConnectionID(nil, ConnectionID("   "))
	if _, exists := ConnectionIDFromContext(ctx); exists {
		t.Fatal("blank normalized connection id should not exist")
	}

	ctx = context.WithValue(context.Background(), connectionIDContextKey{}, "cid-raw")
	if _, exists := ConnectionIDFromContext(ctx); exists {
		t.Fatal("non-ConnectionID type should not exist")
	}

	relayCtx := WithStreamRelay(nil, nil)
	if _, exists := StreamRelayFromContext(relayCtx); exists {
		t.Fatal("nil relay should not be resolved")
	}

	if channel, ok := ParseStreamChannel("  ALL "); !ok || channel != StreamChannelAll {
		t.Fatalf("channel = %q ok=%v, want all true", channel, ok)
	}
}

func TestStreamRelayRegisterConnectionValidationBranches(t *testing.T) {
	var nilRelay *StreamRelay
	if err := nilRelay.RegisterConnection(ConnectionRegistration{}); err == nil {
		t.Fatal("expected nil relay register error")
	}

	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cases := []ConnectionRegistration{
		{Context: ctx, Cancel: cancel, Write: func(message RelayMessage) error { return nil }, Close: func() {}},
		{ConnectionID: "cid", Cancel: cancel, Write: func(message RelayMessage) error { return nil }, Close: func() {}},
		{ConnectionID: "cid", Context: ctx, Write: func(message RelayMessage) error { return nil }, Close: func() {}},
		{ConnectionID: "cid", Context: ctx, Cancel: cancel, Close: func() {}},
		{ConnectionID: "cid", Context: ctx, Cancel: cancel, Write: func(message RelayMessage) error { return nil }},
		{ConnectionID: "cid", Channel: StreamChannelAll, Context: ctx, Cancel: cancel, Write: func(message RelayMessage) error { return nil }, Close: func() {}},
	}
	for i, registration := range cases {
		if err := relay.RegisterConnection(registration); err == nil {
			t.Fatalf("case %d: expected register error", i)
		}
	}

	registration := ConnectionRegistration{
		ConnectionID: " cid-dup ",
		Channel:      StreamChannelIPC,
		Context:      ctx,
		Cancel:       cancel,
		Write:        func(message RelayMessage) error { return nil },
		Close:        func() {},
	}
	if err := relay.RegisterConnection(registration); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection("cid-dup") })
	if err := relay.RegisterConnection(registration); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestStreamRelayBindingAndRefreshBranches(t *testing.T) {
	var nilRelay *StreamRelay
	if frameErr := nilRelay.BindConnection("cid", StreamBinding{SessionID: "s"}); frameErr == nil {
		t.Fatal("expected nil relay bind error")
	}
	if nilRelay.ResolveFallbackSessionID("cid") != "" {
		t.Fatal("nil relay fallback should be empty")
	}
	if nilRelay.RefreshConnectionBindings("cid") {
		t.Fatal("nil relay refresh should be false")
	}

	relay := NewStreamRelay(StreamRelayOptions{BindingTTL: 20 * time.Millisecond, MaxBindingsPerConnection: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := ConnectionID("cid-binding")
	if frameErr := relay.BindConnection("", StreamBinding{SessionID: "s"}); frameErr == nil || frameErr.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("blank connection id error = %#v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{}); frameErr == nil || frameErr.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("missing session error = %#v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s", Channel: "tcp"}); frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("invalid channel error = %#v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s"}); frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("unregistered connection error = %#v", frameErr)
	}

	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelWS,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write:        func(message RelayMessage) error { return nil },
		Close:        func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection(connectionID) })

	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s-1", Channel: StreamChannelIPC}); frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("channel mismatch error = %#v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s-1", RunID: "r-1", Channel: StreamChannelAll}); frameErr != nil {
		t.Fatalf("first bind error: %v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s-1", RunID: "r-1", Channel: StreamChannelAll}); frameErr != nil {
		t.Fatalf("upsert bind should pass: %v", frameErr)
	}
	if frameErr := relay.BindConnection(connectionID, StreamBinding{SessionID: "s-1", RunID: "r-2", Channel: StreamChannelAll}); frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("max bindings error = %#v", frameErr)
	}

	relay.mu.Lock()
	bindings := relay.connectionBindings[connectionID]
	bindings[bindingKey{sessionID: "s-2", runID: "r-expired"}] = &bindingState{sessionID: "s-2", runID: "r-expired", expireAt: time.Now().Add(-time.Second)}
	bindings[bindingKey{sessionID: "s-3", runID: "r-nil"}] = nil
	relay.addConnectionToSessionIndexLocked("s-2", connectionID)
	relay.addConnectionToSessionRunIndexLocked("s-2", "r-expired", connectionID)
	relay.mu.Unlock()

	if !relay.RefreshConnectionBindings(connectionID) {
		t.Fatal("expected refresh to succeed with active bindings")
	}
	if fallback := relay.ResolveFallbackSessionID(connectionID); fallback == "" {
		t.Fatal("fallback session should not be empty")
	}

	relay.cleanupExpiredBindings()
	relay.mu.RLock()
	_, hasExpired := relay.connectionBindings[connectionID][bindingKey{sessionID: "s-2", runID: "r-expired"}]
	relay.mu.RUnlock()
	if hasExpired {
		t.Fatal("expired binding should be removed after cleanup")
	}
}

func TestStreamRelayAutoBindAndExtractSessionBranches(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := ConnectionID("cid-auto-bind")
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write:        func(message RelayMessage) error { return nil },
		Close:        func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection(connectionID) })

	relay.AutoBindFromFrame(connectionID, MessageFrame{SessionID: "session-direct", RunID: "run-direct"})
	relay.AutoBindFromFrame(connectionID, MessageFrame{Payload: protocol.BindStreamParams{SessionID: "session-bind"}})
	relay.AutoBindFromFrame(connectionID, MessageFrame{Payload: &protocol.WakeIntent{SessionID: "session-wake"}})
	relay.AutoBindFromFrame(connectionID, MessageFrame{Payload: map[string]any{"session_id": " session-map "}})
	relay.AutoBindFromFrame(connectionID, MessageFrame{Payload: map[string]any{"session_id": 1}})
	relay.AutoBindFromFrame(connectionID, MessageFrame{})

	relay.mu.RLock()
	bindingCount := len(relay.connectionBindings[connectionID])
	relay.mu.RUnlock()
	if bindingCount < 4 {
		t.Fatalf("binding count = %d, want >= 4", bindingCount)
	}

	if got := extractSessionIDFromPayload(protocol.WakeIntent{SessionID: "s1"}); got != "s1" {
		t.Fatalf("session from wake intent = %q, want s1", got)
	}
	if got := extractSessionIDFromPayload((*protocol.WakeIntent)(nil)); got != "" {
		t.Fatalf("session from nil wake ptr = %q, want empty", got)
	}
	if got := extractSessionIDFromPayload(protocol.BindStreamParams{SessionID: "s2"}); got != "s2" {
		t.Fatalf("session from bind params = %q, want s2", got)
	}
	if got := extractSessionIDFromPayload((*protocol.BindStreamParams)(nil)); got != "" {
		t.Fatalf("session from nil bind ptr = %q, want empty", got)
	}
	if got := extractSessionIDFromPayload(map[string]any{"session_id": " s3 "}); got != "s3" {
		t.Fatalf("session from map = %q, want s3", got)
	}
}

func TestStreamRelayRuntimeAndWriterBranches(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{QueueSize: 1, BindingTTL: 20 * time.Millisecond, CleanupInterval: 5 * time.Millisecond})
	baseCtx := context.Background()

	if relay.SendJSONRPCPayload("", map[string]string{"x": "y"}) {
		t.Fatal("blank connection send should fail")
	}

	closedCount := int32(0)
	writeErrConnID := ConnectionID("cid-write-err")
	writeErrCtx, writeErrCancel := context.WithCancel(baseCtx)
	t.Cleanup(writeErrCancel)
	writeErrCtx = WithConnectionID(writeErrCtx, writeErrConnID)
	writeErrCtx = WithStreamRelay(writeErrCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: writeErrConnID,
		Channel:      StreamChannelWS,
		Context:      writeErrCtx,
		Cancel:       writeErrCancel,
		Write: func(message RelayMessage) error {
			return io.ErrClosedPipe
		},
		Close: func() {
			atomic.AddInt32(&closedCount, 1)
		},
	}); err != nil {
		t.Fatalf("register write error connection: %v", err)
	}
	if !relay.SendJSONRPCPayload(writeErrConnID, map[string]string{"trigger": "drop"}) {
		t.Fatal("send payload should enqueue")
	}
	time.Sleep(60 * time.Millisecond)
	if atomic.LoadInt32(&closedCount) == 0 {
		t.Fatal("connection close should be called after write failure")
	}

	sseMessageCh := make(chan RelayMessage, 4)
	sseConnID := ConnectionID("cid-sse")
	sseCtx, sseRootCancel := context.WithCancel(baseCtx)
	t.Cleanup(sseRootCancel)
	sseCtx = WithConnectionID(sseCtx, sseConnID)
	sseCtx = WithStreamRelay(sseCtx, relay)
	sseCancelCtx, sseCancel := context.WithCancel(sseCtx)
	t.Cleanup(sseCancel)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: sseConnID,
		Channel:      StreamChannelSSE,
		Context:      sseCancelCtx,
		Cancel:       sseCancel,
		Write: func(message RelayMessage) error {
			sseMessageCh <- message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register sse connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection(sseConnID) })

	if bindErr := relay.BindConnection(sseConnID, StreamBinding{SessionID: "sse-session", Channel: StreamChannelSSE, Explicit: true}); bindErr != nil {
		t.Fatalf("bind sse connection: %v", bindErr)
	}

	relay.PublishRuntimeEvent(RuntimeEvent{Type: RuntimeEventTypeRunProgress, SessionID: "sse-session", Payload: map[string]string{"chunk": "ok"}})
	select {
	case message := <-sseMessageCh:
		if message.Kind != relayMessageKindSSE {
			t.Fatalf("message kind = %q, want %q", message.Kind, relayMessageKindSSE)
		}
		if message.Event != protocol.MethodGatewayEvent {
			t.Fatalf("event = %q, want %q", message.Event, protocol.MethodGatewayEvent)
		}
	case <-time.After(time.Second):
		t.Fatal("expected sse relay event")
	}

	var nilRelay *StreamRelay
	nilRelay.PublishRuntimeEvent(RuntimeEvent{})
	nilRelay.AutoBindFromFrame("cid", MessageFrame{SessionID: "s"})
	if channel, ok := nilRelay.connectionChannel("cid"); ok || channel != "" {
		t.Fatal("nil relay channel should not exist")
	}

	if channel, ok := relay.connectionChannel("missing"); ok || channel != "" {
		t.Fatal("missing connection should not exist")
	}
}

func TestStreamRelayMatchingAndLifecycleBranches(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{BindingTTL: 20 * time.Millisecond, CleanupInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connID := ConnectionID("cid-match")
	connCtx := WithConnectionID(ctx, connID)
	connCtx = WithStreamRelay(connCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connID,
		Channel:      StreamChannelWS,
		Context:      connCtx,
		Cancel:       cancel,
		Write:        func(message RelayMessage) error { return nil },
		Close:        func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection(connID) })

	if bindErr := relay.BindConnection(connID, StreamBinding{SessionID: "session-x", RunID: "run-x", Channel: StreamChannelWS}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	now := time.Now()
	relay.mu.RLock()
	if !relay.connectionMatchesEventLocked(connID, StreamChannelWS, "session-x", "run-x", now) {
		t.Fatal("expected exact run to match")
	}
	if relay.connectionMatchesEventLocked(connID, StreamChannelWS, "session-x", "", now) {
		t.Fatal("event with empty run_id should not match run-scoped binding")
	}
	relay.mu.RUnlock()

	relay.mu.Lock()
	relay.connectionBindings[connID][bindingKey{sessionID: "session-x", runID: "run-x"}].channel = StreamChannelSSE
	relay.mu.Unlock()
	relay.mu.RLock()
	if relay.connectionMatchesEventLocked(connID, StreamChannelWS, "session-x", "run-x", now) {
		t.Fatal("channel mismatch should not match")
	}
	relay.mu.RUnlock()

	if matches := relay.matchConnectionsForEvent("", "run-x"); len(matches) != 0 {
		t.Fatalf("empty session should not match, got %d", len(matches))
	}
	if key := buildSessionRunKey(" s ", " r "); key != "s\x00r" {
		t.Fatalf("session run key = %q, want %q", key, "s\\x00r")
	}

	var nilRelay *StreamRelay
	nilRelay.Start(nil, nil)
	nilRelay.Stop()

	stubPort := &runtimePortEventStub{events: make(chan RuntimeEvent)}
	relay.Start(ctx, stubPort)
	cancel()
	waitForStreamRelayState(t, relay, false)

	relay.runRuntimeEventLoop(context.Background(), nil, 0)
	relay.runRuntimeEventLoop(context.Background(), &runtimePortEventStub{events: nil}, 0)
}

func TestRPCDispatchAdditionalBranches(t *testing.T) {
	if !requiresSession(FrameActionRun) {
		t.Fatal("run should require session")
	}
	if requiresSession(FrameActionPing) {
		t.Fatal("ping should not require session")
	}

	ctx := context.Background()
	frame := MessageFrame{Type: FrameTypeRequest, Action: FrameActionPing}
	applyAutomaticBinding(ctx, frame)

	relay := NewStreamRelay(StreamRelayOptions{})
	connCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectionID := ConnectionID("cid-rpc")
	connCtx = WithConnectionID(connCtx, connectionID)
	connCtx = WithStreamRelay(connCtx, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connCtx,
		Cancel:       cancel,
		Write:        func(message RelayMessage) error { return nil },
		Close:        func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	t.Cleanup(func() { relay.dropConnection(connectionID) })

	applyAutomaticBinding(connCtx, MessageFrame{Action: FrameActionBindStream, SessionID: "session-skip"})
	applyAutomaticBinding(connCtx, MessageFrame{Action: FrameActionRun, Payload: map[string]any{"session_id": "session-auto"}})
	if fallback := relay.ResolveFallbackSessionID(connectionID); fallback != "session-auto" {
		t.Fatalf("fallback session = %q, want %q", fallback, "session-auto")
	}
}

func TestNetworkServerHelperBranches(t *testing.T) {
	request := httptest.NewRequest("GET", "/sse?method=gateway.ping&id=%20custom-id%20", nil)
	trigger := buildSSETriggerRequest(request)
	if trigger.Method != protocol.MethodGatewayPing {
		t.Fatalf("trigger method = %q, want %q", trigger.Method, protocol.MethodGatewayPing)
	}
	if string(trigger.ID) != `"custom-id"` {
		t.Fatalf("trigger id = %s, want %s", trigger.ID, `"custom-id"`)
	}

	raw := []byte(`{"jsonrpc":"2.0","id":"x","method":"gateway.ping","params":{}}`)
	if _, rpcErr := decodeJSONRPCRequestFromBytes(raw); rpcErr != nil {
		t.Fatalf("decode jsonrpc bytes: %v", rpcErr)
	}

	if _, rpcErr := decodeJSONRPCRequestFromBytes([]byte(`{"jsonrpc":"2.0"} {"extra":1}`)); rpcErr == nil {
		t.Fatal("expected trailing json parse error")
	}

	recorder := httptest.NewRecorder()
	writeJSONRPCHTTPResponse(recorder, protocol.NewJSONRPCErrorResponse(json.RawMessage(`"id-1"`), protocol.NewJSONRPCError(
		protocol.JSONRPCCodeInternalError,
		"boom",
		protocol.GatewayCodeInternalError,
	)))
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"jsonrpc":"2.0"`)) {
		t.Fatalf("response body = %s, want jsonrpc payload", recorder.Body.String())
	}
}

func TestStreamRelayInternalBranchCoverage(t *testing.T) {
	t.Run("resolve fallback skips expired and nil", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		connectionID := ConnectionID("cid-fallback")
		now := time.Now()

		relay.mu.Lock()
		relay.connectionBindings[connectionID] = map[bindingKey]*bindingState{
			{sessionID: "s-expired", runID: "r"}: {
				sessionID: "s-expired",
				expireAt:  now.Add(-time.Second),
				lastSeen:  now.Add(time.Second),
			},
			{sessionID: "s-old", runID: "r"}: {
				sessionID: "s-old",
				expireAt:  now.Add(time.Second),
				lastSeen:  now.Add(-2 * time.Second),
			},
			{sessionID: "s-new", runID: "r"}: {
				sessionID: "s-new",
				expireAt:  now.Add(time.Second),
				lastSeen:  now,
			},
			{sessionID: "s-nil", runID: "r"}: nil,
		}
		relay.mu.Unlock()

		if got := relay.ResolveFallbackSessionID(connectionID); got != "s-new" {
			t.Fatalf("fallback session = %q, want %q", got, "s-new")
		}
	})

	t.Run("run connection writer exits on closed queue", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		connection := &relayConnection{
			id:      "cid-queue-close",
			channel: StreamChannelIPC,
			ctx:     ctx,
			cancel:  cancel,
			writeFn: func(message RelayMessage) error { return nil },
			closeFn: func() {},
			queue:   make(chan RelayMessage, 1),
		}
		relay.mu.Lock()
		relay.connections[connection.id] = connection
		relay.mu.Unlock()

		done := make(chan struct{})
		go func() {
			defer close(done)
			relay.runConnectionWriter(connection)
		}()
		close(connection.queue)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("writer did not exit after queue closed")
		}
	})

	t.Run("unregister connection without close callback", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		connectionID := ConnectionID("cid-unregister")
		relay.mu.Lock()
		relay.connections[connectionID] = &relayConnection{
			id:      connectionID,
			channel: StreamChannelIPC,
			ctx:     ctx,
			cancel:  cancel,
			writeFn: func(message RelayMessage) error { return nil },
			closeFn: func() {},
			queue:   make(chan RelayMessage, 1),
		}
		relay.connectionBindings[connectionID] = map[bindingKey]*bindingState{
			{sessionID: "s", runID: "r"}: {
				sessionID: "s",
				runID:     "r",
				expireAt:  time.Now().Add(time.Minute),
			},
		}
		relay.addConnectionToSessionIndexLocked("s", connectionID)
		relay.addConnectionToSessionRunIndexLocked("s", "r", connectionID)
		relay.mu.Unlock()

		if connection := relay.unregisterConnection(connectionID, false); connection == nil {
			t.Fatal("expected unregister to return connection")
		}
		relay.mu.RLock()
		_, hasSession := relay.sessionIndex["s"]
		_, hasRun := relay.sessionRunIndex[buildSessionRunKey("s", "r")]
		relay.mu.RUnlock()
		if hasSession || hasRun {
			t.Fatal("indexes should be cleaned after unregister")
		}

		if connection := relay.unregisterConnection(connectionID, true); connection != nil {
			t.Fatal("second unregister should return nil")
		}
	})

	t.Run("connection match branch matrix", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		connectionID := ConnectionID("cid-match-matrix")
		channelOnlyConnectionID := ConnectionID("cid-match-channel-only")
		now := time.Now()

		relay.mu.Lock()
		relay.connectionBindings[connectionID] = map[bindingKey]*bindingState{
			{sessionID: "s", runID: ""}: {
				sessionID: "s",
				runID:     "",
				channel:   StreamChannelAll,
				expireAt:  now.Add(time.Minute),
			},
			{sessionID: "other", runID: "r"}: {
				sessionID: "other",
				runID:     "r",
				channel:   StreamChannelWS,
				expireAt:  now.Add(time.Minute),
			},
			{sessionID: "s", runID: "expired"}: {
				sessionID: "s",
				runID:     "expired",
				channel:   StreamChannelWS,
				expireAt:  now.Add(-time.Minute),
			},
			{sessionID: "s", runID: "channel-mismatch"}: {
				sessionID: "s",
				runID:     "channel-mismatch",
				channel:   StreamChannelSSE,
				expireAt:  now.Add(time.Minute),
			},
		}
		relay.connectionBindings[channelOnlyConnectionID] = map[bindingKey]*bindingState{
			{sessionID: "s", runID: "channel-mismatch"}: {
				sessionID: "s",
				runID:     "channel-mismatch",
				channel:   StreamChannelSSE,
				expireAt:  now.Add(time.Minute),
			},
		}
		relay.mu.Unlock()

		relay.mu.RLock()
		if !relay.connectionMatchesEventLocked(connectionID, StreamChannelWS, "s", "", now) {
			t.Fatal("session-only binding should match event with empty run_id")
		}
		if !relay.connectionMatchesEventLocked(connectionID, StreamChannelWS, "s", "run-1", now) {
			t.Fatal("session-only binding should match event with concrete run_id")
		}
		if relay.connectionMatchesEventLocked(channelOnlyConnectionID, StreamChannelWS, "s", "channel-mismatch", now) {
			t.Fatal("channel-mismatched binding should not match")
		}
		relay.mu.RUnlock()
	})

	t.Run("send event notification skips missing connection", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		relay.sendEventNotification("missing", protocol.NewJSONRPCNotification(protocol.MethodGatewayEvent, map[string]string{"x": "y"}))
	})
}
