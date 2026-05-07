package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
	"neo-code/internal/security"
	"neo-code/internal/tools"
)

func TestRunnerRegistryLifecycle(t *testing.T) {
	registry := NewRunnerRegistry(log.New(io.Discard, "", 0))
	connectionID := ConnectionID("cid-runner")
	registry.Register(connectionID, "runner-1", "Runner One", "/tmp/work", []string{"local"})

	if !registry.IsOnline(connectionID) {
		t.Fatal("IsOnline() = false, want true")
	}
	if !registry.BindSession("session-1", connectionID) {
		t.Fatal("BindSession() = false, want true")
	}
	if got, ok := registry.LookupBySession("session-1"); !ok || got != connectionID {
		t.Fatalf("LookupBySession() = (%q,%v), want (%q,true)", got, ok, connectionID)
	}
	record, ok := registry.Record(connectionID)
	if !ok || record.RunnerID != "runner-1" {
		t.Fatalf("Record() = (%#v,%v)", record, ok)
	}
	before := record.LastSeenAt
	time.Sleep(time.Millisecond)
	registry.Heartbeat(connectionID)
	record, _ = registry.Record(connectionID)
	if !record.LastSeenAt.After(before) {
		t.Fatalf("LastSeenAt = %v, want after %v", record.LastSeenAt, before)
	}

	list := registry.List()
	if len(list) != 1 || list[0].RunnerName != "Runner One" {
		t.Fatalf("List() = %#v", list)
	}

	registry.UnbindSession("session-1")
	if _, ok := registry.LookupBySession("session-1"); ok {
		t.Fatal("LookupBySession() ok = true after UnbindSession")
	}
	registry.BindSession("session-2", connectionID)
	registry.OnConnectionDropped(connectionID)
	if registry.IsOnline(connectionID) {
		t.Fatal("IsOnline() = true after OnConnectionDropped")
	}
	if _, ok := registry.LookupBySession("session-2"); ok {
		t.Fatal("session binding still present after unregister")
	}
	if registry.BindSession("session-3", connectionID) {
		t.Fatal("BindSession() = true for offline runner")
	}
	registry.Unregister("missing")
	registry.UnbindSession("missing")
}

func TestRunnerToolManagerDispatchAndCompletion(t *testing.T) {
	registry := NewRunnerRegistry(log.New(io.Discard, "", 0))
	relay := NewStreamRelay(StreamRelayOptions{Logger: log.New(io.Discard, "", 0)})
	connectionID := ConnectionID("cid-runner")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectionCtx := WithStreamRelay(WithConnectionID(ctx, connectionID), relay)
	messageCh := make(chan RelayMessage, 1)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelWS,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			messageCh <- message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("RegisterConnection() error = %v", err)
	}
	registry.Register(connectionID, "runner-1", "Runner", "/tmp/work", nil)
	registry.BindSession("session-1", connectionID)

	signer, err := security.NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCapabilitySigner() error = %v", err)
	}
	manager := NewRunnerToolManager(registry, relay, signer, time.Second, log.New(io.Discard, "", 0))

	resultCh := make(chan struct {
		content string
		isError bool
		err     error
	}, 1)
	go func() {
		content, isError, dispatchErr := manager.DispatchToolRequest(
			context.Background(),
			"session-1",
			"run-1",
			"tool-1",
			"bash",
			json.RawMessage(`{"command":"pwd"}`),
		)
		resultCh <- struct {
			content string
			isError bool
			err     error
		}{content: content, isError: isError, err: dispatchErr}
	}()

	select {
	case message := <-messageCh:
		payload, ok := message.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", message.Payload)
		}
		if payload["method"] != protocol.MethodGatewayToolRequest {
			t.Fatalf("method = %v, want %q", payload["method"], protocol.MethodGatewayToolRequest)
		}
		params := payload["params"].(map[string]any)
		if params["tool_name"] != "bash" {
			t.Fatalf("tool_name = %v, want bash", params["tool_name"])
		}
		if params["capability_token"] == nil {
			t.Fatal("capability_token = nil, want signed token")
		}
		if err := manager.CompleteToolRequest(params["request_id"].(string), "done", false); err != nil {
			t.Fatalf("CompleteToolRequest() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool request")
	}

	result := <-resultCh
	if result.err != nil || result.isError || result.content != "done" {
		t.Fatalf("DispatchToolRequest() = (%q,%v,%v)", result.content, result.isError, result.err)
	}
}

func TestRunnerToolManagerErrorPaths(t *testing.T) {
	registry := NewRunnerRegistry(log.New(io.Discard, "", 0))
	relay := NewStreamRelay(StreamRelayOptions{Logger: log.New(io.Discard, "", 0)})
	manager := NewRunnerToolManager(registry, relay, nil, 20*time.Millisecond, log.New(io.Discard, "", 0))

	if _, _, err := manager.DispatchToolRequest(context.Background(), "missing", "run-1", "tool-1", "bash", nil); err == nil {
		t.Fatal("DispatchToolRequest() error = nil, want offline error")
	}

	connectionID := ConnectionID("cid-runner")
	registry.Register(connectionID, "runner-1", "Runner", "/tmp/work", nil)
	registry.BindSession("session-1", connectionID)
	if _, _, err := manager.DispatchToolRequest(context.Background(), "session-1", "run-1", "tool-1", "bash", nil); err == nil || !strings.Contains(err.Error(), "failed to send") {
		t.Fatalf("DispatchToolRequest() error = %v", err)
	}

	pending := &PendingToolCall{
		RequestID:  "req-full",
		ResultChan: make(chan toolResultEnvelope, 1),
		Deadline:   time.Now().Add(time.Second),
	}
	pending.ResultChan <- toolResultEnvelope{}
	manager.pending[pending.RequestID] = pending
	if err := manager.CompleteToolRequest(pending.RequestID, "x", true); err == nil {
		t.Fatal("CompleteToolRequest() error = nil, want channel full or missing")
	}
	if err := manager.CompleteToolRequest("missing", "x", true); err == nil {
		t.Fatal("CompleteToolRequest() missing error = nil")
	}

	manager.pending["expired"] = &PendingToolCall{
		RequestID:  "expired",
		ResultChan: make(chan toolResultEnvelope, 1),
		Deadline:   time.Now().Add(-time.Second),
	}
	manager.cleanupExpired()
	if _, exists := manager.pending["expired"]; exists {
		t.Fatal("cleanupExpired() did not remove expired request")
	}
}

func TestRunnerToolManagerCapabilityTokenAndCleanupLoop(t *testing.T) {
	manager := NewRunnerToolManager(nil, nil, nil, 10*time.Millisecond, log.New(io.Discard, "", 0))
	token, err := manager.NewCapabilityToken("session-1", "run-1", "bash", "/tmp/work")
	if err != nil || token != nil {
		t.Fatalf("NewCapabilityToken() = (%v,%v), want (nil,nil)", token, err)
	}

	signer, err := security.NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCapabilitySigner() error = %v", err)
	}
	manager = NewRunnerToolManager(nil, nil, signer, 10*time.Millisecond, log.New(io.Discard, "", 0))
	token, err = manager.NewCapabilityToken("session-1", "run-1", "bash", "/tmp/work")
	if err != nil {
		t.Fatalf("NewCapabilityToken() error = %v", err)
	}
	if token == nil || token.AllowedTools[0] != "bash" {
		t.Fatalf("token = %#v", token)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	manager.pending["cleanup"] = &PendingToolCall{
		RequestID:  "cleanup",
		ResultChan: make(chan toolResultEnvelope, 1),
		Deadline:   time.Now().Add(-time.Second),
	}
	go func() {
		manager.CleanupLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CleanupLoop() did not stop after cancel")
	}

	defaultManager := NewRunnerToolManager(nil, nil, nil, 0, nil)
	if defaultManager.timeout != 30*time.Second {
		t.Fatalf("default timeout = %v", defaultManager.timeout)
	}
	if defaultManager.logger == nil {
		t.Fatal("default logger = nil")
	}
}

func TestRunnerToolDispatcherBridge(t *testing.T) {
	registry := NewRunnerRegistry(log.New(io.Discard, "", 0))
	relay := NewStreamRelay(StreamRelayOptions{Logger: log.New(io.Discard, "", 0)})
	manager := NewRunnerToolManager(registry, relay, nil, time.Second, log.New(io.Discard, "", 0))
	bridge := NewRunnerToolDispatcher(manager)
	if bridge == nil {
		t.Fatal("NewRunnerToolDispatcher(manager) = nil")
	}
	if NewRunnerToolDispatcher(nil) != nil {
		t.Fatal("NewRunnerToolDispatcher(nil manager) != nil")
	}

	connectionID := ConnectionID("cid-runner")
	registry.Register(connectionID, "runner-1", "Runner", "/tmp/work", nil)
	registry.BindSession("session-1", connectionID)

	result, handled, err := bridge.TryDispatch(context.Background(), "session-1", "run-1", tools.ToolCallInput{
		ID:   "tool-1",
		Name: "bash",
	})
	if err != nil || !handled || !result.IsError {
		t.Fatalf("TryDispatch(send fail) = (%#v,%v,%v)", result, handled, err)
	}

	result, handled, err = bridge.TryDispatch(context.Background(), "missing", "run-1", tools.ToolCallInput{
		ID:   "tool-1",
		Name: "bash",
	})
	if err != nil || handled {
		t.Fatalf("TryDispatch(offline) = (%#v,%v,%v)", result, handled, err)
	}

	messageCh := make(chan RelayMessage, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectionCtx := WithStreamRelay(WithConnectionID(ctx, connectionID), relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelWS,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			messageCh <- message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("RegisterConnection() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, handled, err = bridge.TryDispatch(context.Background(), "session-1", "run-2", tools.ToolCallInput{
			ID:   "tool-2",
			Name: "bash",
		})
	}()
	message := <-messageCh
	payload := message.Payload.(map[string]any)
	params := payload["params"].(map[string]any)
	if err := manager.CompleteToolRequest(params["request_id"].(string), "remote-ok", false); err != nil {
		t.Fatalf("CompleteToolRequest() error = %v", err)
	}
	<-done
	if err != nil || !handled || result.Content != "remote-ok" || result.IsError {
		t.Fatalf("TryDispatch(success) = (%#v,%v,%v)", result, handled, err)
	}
}

func TestRunnerContextHelpersAndACL(t *testing.T) {
	ctx := context.Background()
	registry := NewRunnerRegistry(nil)
	manager := NewRunnerToolManager(registry, NewStreamRelay(StreamRelayOptions{}), nil, time.Second, nil)

	if RunnerRegistryFromContext(nil) != nil || RunnerToolManagerFromContext(nil) != nil {
		t.Fatal("nil context should not return runner helpers")
	}
	ctx = WithRunnerRegistry(ctx, registry)
	ctx = WithRunnerToolManager(ctx, manager)
	if RunnerRegistryFromContext(ctx) != registry {
		t.Fatal("RunnerRegistryFromContext() mismatch")
	}
	if RunnerToolManagerFromContext(ctx) != manager {
		t.Fatal("RunnerToolManagerFromContext() mismatch")
	}

	acl := NewStrictControlPlaneACL()
	if !acl.IsAllowed(RequestSourceRunner, protocol.MethodGatewayRegisterRunner) {
		t.Fatal("runner source should allow registerRunner")
	}
	if acl.IsAllowed(RequestSourceRunner, protocol.MethodGatewayRun) {
		t.Fatal("runner source should not allow gateway.run")
	}
	if NormalizeRequestSource(RequestSource(" RUNNER ")) != RequestSourceRunner {
		t.Fatal("NormalizeRequestSource() did not normalize runner")
	}

	ctx = WithRunnerRegistry(nil, registry)
	if RunnerRegistryFromContext(ctx) != registry {
		t.Fatal("WithRunnerRegistry(nil) did not create background context")
	}
	ctx = WithRunnerToolManager(nil, manager)
	if RunnerToolManagerFromContext(ctx) != manager {
		t.Fatal("WithRunnerToolManager(nil) did not create background context")
	}
}

func TestRunnerBootstrapHandlers(t *testing.T) {
	registry := NewRunnerRegistry(nil)
	ctx := WithRunnerRegistry(WithConnectionID(context.Background(), "cid-runner"), registry)
	frame := MessageFrame{
		RequestID: "req-1",
		Payload: protocol.RegisterRunnerParams{
			RunnerID:   "runner-1",
			RunnerName: "Runner",
			Workdir:    "/tmp/work",
		},
	}
	response := handleRegisterRunnerFrame(ctx, frame, nil)
	if response.Type != FrameTypeAck || response.Action != FrameActionRegisterRunner {
		t.Fatalf("handleRegisterRunnerFrame() = %#v", response)
	}
	if _, ok := registry.Record("cid-runner"); !ok {
		t.Fatal("runner not registered")
	}

	manager := NewRunnerToolManager(registry, NewStreamRelay(StreamRelayOptions{}), nil, time.Second, nil)
	manager.pending["pending-1"] = &PendingToolCall{
		RequestID:  "pending-1",
		ResultChan: make(chan toolResultEnvelope, 1),
		Deadline:   time.Now().Add(time.Second),
	}
	ctx = WithRunnerToolManager(context.Background(), manager)
	resultFrame := handleExecuteToolResultFrame(ctx, MessageFrame{
		RequestID: "req-2",
		Payload: protocol.ExecuteToolResultParams{
			RequestID:  "pending-1",
			SessionID:  "session-1",
			RunID:      "run-1",
			ToolCallID: "tool-1",
			Content:    "ok",
		},
	}, nil)
	if resultFrame.Type != FrameTypeAck || resultFrame.Action != FrameActionExecuteToolResult {
		t.Fatalf("handleExecuteToolResultFrame() = %#v", resultFrame)
	}

	if frame := handleRegisterRunnerFrame(context.Background(), MessageFrame{}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(nil registry) = %#v", frame)
	}
	if frame := handleRegisterRunnerFrame(WithRunnerRegistry(context.Background(), registry), MessageFrame{
		Payload: map[string]any{"runner_id": "", "workdir": "/tmp/work"},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(missing runner_id) = %#v", frame)
	}
	if frame := handleRegisterRunnerFrame(WithRunnerRegistry(context.Background(), registry), MessageFrame{
		Payload: map[string]any{"runner_id": "runner-1", "workdir": "/tmp/work"},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(missing connection id) = %#v", frame)
	}
	if frame := handleExecuteToolResultFrame(context.Background(), MessageFrame{}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleExecuteToolResultFrame(nil manager) = %#v", frame)
	}
	if frame := handleExecuteToolResultFrame(WithRunnerToolManager(context.Background(), manager), MessageFrame{
		Payload: map[string]any{"request_id": ""},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleExecuteToolResultFrame(missing request id) = %#v", frame)
	}
	if frame := handleExecuteToolResultFrame(WithRunnerToolManager(context.Background(), manager), MessageFrame{
		Payload: protocol.ExecuteToolResultParams{RequestID: "missing"},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleExecuteToolResultFrame(missing pending) = %#v", frame)
	}
}

func TestRunnerJSONRPCNormalizationAndInjection(t *testing.T) {
	registerNormalized, rpcErr := protocol.NormalizeJSONRPCRequest(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"runner-1"`),
		Method:  protocol.MethodGatewayRegisterRunner,
		Params:  json.RawMessage(`{"runner_id":"r-1","workdir":"/tmp/work"}`),
	})
	if rpcErr != nil || registerNormalized.Action != "register_runner" {
		t.Fatalf("NormalizeJSONRPCRequest(register) = (%#v,%v)", registerNormalized, rpcErr)
	}

	resultNormalized, rpcErr := protocol.NormalizeJSONRPCRequest(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"runner-2"`),
		Method:  protocol.MethodGatewayExecuteToolResult,
		Params:  json.RawMessage(`{"request_id":"req-1","session_id":"s-1","run_id":"r-1","tool_call_id":"tool-1"}`),
	})
	if rpcErr != nil || resultNormalized.Action != "execute_tool_result" {
		t.Fatalf("NormalizeJSONRPCRequest(result) = (%#v,%v)", resultNormalized, rpcErr)
	}

	if _, rpcErr := protocol.NormalizeJSONRPCRequest(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"runner-3"`),
		Method:  protocol.MethodGatewayRegisterRunner,
		Params:  json.RawMessage(`{"runner_id":"","workdir":"/tmp/work"}`),
	}); rpcErr == nil {
		t.Fatal("NormalizeJSONRPCRequest(register invalid) error = nil")
	}

	port := &bootstrapRuntimeStub{}
	multi := &MultiWorkspaceRuntime{
		bundles: map[string]*workspaceBundle{
			"default": {port: port, cleanup: func() error { return nil }},
		},
	}
	called := false
	multi.InjectRunnerDispatcher(func(runtimePort RuntimePort) {
		if runtimePort == port {
			called = true
		}
	})
	if !called {
		t.Fatal("InjectRunnerDispatcher() did not inject existing bundle")
	}

	if _, rpcErr := protocol.NormalizeJSONRPCRequest(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"runner-4"`),
		Method:  protocol.MethodGatewayExecuteToolResult,
		Params:  json.RawMessage(`{"request_id":"req-1","session_id":"","run_id":"r-1","tool_call_id":"tool-1"}`),
	}); rpcErr == nil {
		t.Fatal("NormalizeJSONRPCRequest(result invalid) error = nil")
	}
}

func TestMultiWorkspaceRuntimeInjectRunnerDispatcherForFutureBundle(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	injected := make(chan RuntimePort, 1)
	mw.InjectRunnerDispatcher(func(port RuntimePort) {
		injected <- port
	})

	port, err := mw.getPortForHash(alpha.Hash)
	if err != nil {
		t.Fatalf("getPortForHash() error = %v", err)
	}
	select {
	case got := <-injected:
		if got != port {
			t.Fatalf("injected port = %#v, want %#v", got, port)
		}
	case <-time.After(time.Second):
		t.Fatal("runner dispatcher injector was not called for future bundle")
	}
}

func TestRunnerToolManagerDispatchCancellationAndCleanup(t *testing.T) {
	registry := NewRunnerRegistry(nil)
	relay := NewStreamRelay(StreamRelayOptions{})
	manager := NewRunnerToolManager(registry, relay, nil, 20*time.Millisecond, log.New(io.Discard, "", 0))

	connectionID := ConnectionID("cid-cancel")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	registered := make(chan struct{}, 1)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelWS,
		Context:      context.Background(),
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			select {
			case registered <- struct{}{}:
			default:
			}
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("RegisterConnection() error = %v", err)
	}
	registry.Register(connectionID, "runner-1", "Runner", "/tmp/work", nil)
	registry.BindSession("session-1", connectionID)

	cancelCtx, cancelDispatch := context.WithCancel(context.Background())
	go func() {
		<-registered
		cancelDispatch()
	}()
	if _, _, err := manager.DispatchToolRequest(cancelCtx, "session-1", "run-1", "tool-1", "bash", nil); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("DispatchToolRequest(cancel) error = %v", err)
	}
	if len(manager.pending) != 0 {
		t.Fatalf("pending after cancel = %d, want 0", len(manager.pending))
	}

	manager.pending["cleanup"] = &PendingToolCall{RequestID: "cleanup"}
	manager.cleanupPending("cleanup")
	if len(manager.pending) != 0 {
		t.Fatalf("pending after cleanupPending = %d, want 0", len(manager.pending))
	}

	timeoutRegistry := NewRunnerRegistry(nil)
	timeoutRelay := NewStreamRelay(StreamRelayOptions{})
	timeoutManager := NewRunnerToolManager(timeoutRegistry, timeoutRelay, nil, 5*time.Millisecond, log.New(io.Discard, "", 0))
	timeoutConnectionID := ConnectionID("cid-timeout")
	if err := timeoutRelay.RegisterConnection(ConnectionRegistration{
		ConnectionID: timeoutConnectionID,
		Channel:      StreamChannelWS,
		Context:      context.Background(),
		Cancel:       func() {},
		Write: func(message RelayMessage) error {
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("RegisterConnection(timeout) error = %v", err)
	}
	timeoutRegistry.Register(timeoutConnectionID, "runner-timeout", "Runner", "/tmp/work", nil)
	timeoutRegistry.BindSession("session-timeout", timeoutConnectionID)
	if _, _, err := timeoutManager.DispatchToolRequest(context.Background(), "session-timeout", "run-1", "tool-1", "bash", nil); err == nil || !strings.Contains(err.Error(), "timed out waiting for runner") {
		t.Fatalf("DispatchToolRequest(timeout) error = %v", err)
	}

	expiredResultCh := make(chan toolResultEnvelope, 1)
	timeoutManager.pending["expired-send"] = &PendingToolCall{
		RequestID:  "expired-send",
		ResultChan: expiredResultCh,
		Deadline:   time.Now().Add(-time.Second),
	}
	timeoutManager.cleanupExpired()
	if _, exists := timeoutManager.pending["expired-send"]; exists {
		t.Fatal("cleanupExpired() should delete expired-send entry")
	}
	select {
	case result := <-expiredResultCh:
		if !result.IsError || result.Content == "" {
			t.Fatalf("cleanupExpired() result = %#v", result)
		}
	default:
		t.Fatal("cleanupExpired() did not emit timeout result")
	}

}

func TestRunnerBootstrapHandlersRejectMalformedPayloads(t *testing.T) {
	registry := NewRunnerRegistry(nil)
	manager := NewRunnerToolManager(registry, NewStreamRelay(StreamRelayOptions{}), nil, time.Second, nil)

	registerCtx := WithRunnerRegistry(WithConnectionID(context.Background(), "cid"), registry)
	if frame := handleRegisterRunnerFrame(registerCtx, MessageFrame{
		Payload: map[string]any{"runner_id": 1, "workdir": "/tmp/work"},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(type mismatch) = %#v", frame)
	}
	if frame := handleRegisterRunnerFrame(registerCtx, MessageFrame{
		Payload: make(chan int),
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(marshal failure) = %#v", frame)
	}
	if frame := handleExecuteToolResultFrame(WithRunnerToolManager(context.Background(), manager), MessageFrame{
		Payload: map[string]any{"request_id": 1},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleExecuteToolResultFrame(type mismatch) = %#v", frame)
	}
	if frame := handleExecuteToolResultFrame(WithRunnerToolManager(context.Background(), manager), MessageFrame{
		Payload: make(chan int),
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleExecuteToolResultFrame(marshal failure) = %#v", frame)
	}
	if frame := handleRegisterRunnerFrame(registerCtx, MessageFrame{
		Payload: map[string]any{"runner_id": "runner-1", "workdir": ""},
	}, nil); frame.Type != FrameTypeError {
		t.Fatalf("handleRegisterRunnerFrame(missing workdir) = %#v", frame)
	}
}
