package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

type rpcRunCaptureRuntimeStub struct {
	runInput RunInput
}

func (s *rpcRunCaptureRuntimeStub) Run(_ context.Context, input RunInput) error {
	s.runInput = input
	return nil
}

func (s *rpcRunCaptureRuntimeStub) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (s *rpcRunCaptureRuntimeStub) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (s *rpcRunCaptureRuntimeStub) CancelActiveRun() bool {
	return false
}

func (s *rpcRunCaptureRuntimeStub) Events() <-chan RuntimeEvent {
	return nil
}

func (s *rpcRunCaptureRuntimeStub) ListSessions(_ context.Context) ([]SessionSummary, error) {
	return nil, nil
}

func (s *rpcRunCaptureRuntimeStub) LoadSession(_ context.Context, _ string) (Session, error) {
	return Session{}, nil
}

func TestDispatchRPCRequestResultEncodeError(t *testing.T) {
	originalHandlers := requestFrameHandlers
	requestFrameHandlers = map[FrameAction]requestFrameHandler{
		FrameActionPing: func(_ context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
			return MessageFrame{
				Type:      FrameTypeAck,
				Action:    FrameActionPing,
				RequestID: frame.RequestID,
				Payload: map[string]any{
					"bad": make(chan int),
				},
			}
		},
	}
	t.Cleanup(func() {
		requestFrameHandlers = originalHandlers
	})

	response := dispatchRPCRequest(context.Background(), protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"rpc-encode-1"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if response.Error == nil {
		t.Fatal("expected jsonrpc internal error")
	}
	if response.Error.Code != protocol.JSONRPCCodeInternalError {
		t.Fatalf("rpc error code = %d, want %d", response.Error.Code, protocol.JSONRPCCodeInternalError)
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error); gatewayCode != ErrorCodeInternalError.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeInternalError.String())
	}
}

func TestHydrateFrameSessionFromConnectionFallback(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	baseContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionContext := WithConnectionID(baseContext, connectionID)
	connectionContext = WithStreamRelay(connectionContext, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionContext,
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
		SessionID: "session-fallback",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	hydrated := hydrateFrameSessionFromConnection(connectionContext, MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionPing,
	})
	if hydrated.SessionID != "session-fallback" {
		t.Fatalf("session_id = %q, want %q", hydrated.SessionID, "session-fallback")
	}
}

func TestApplyAutomaticBindingPingRefreshesTTL(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		BindingTTL: 20 * time.Millisecond,
	})
	baseContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionContext := WithConnectionID(baseContext, connectionID)
	connectionContext = WithStreamRelay(connectionContext, relay)
	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionContext,
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
		SessionID: "session-ping",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	time.Sleep(10 * time.Millisecond)
	applyAutomaticBinding(connectionContext, MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionPing,
	})
	time.Sleep(15 * time.Millisecond)
	if !relay.RefreshConnectionBindings(connectionID) {
		t.Fatal("expected ping to refresh existing bindings")
	}
}

func TestDispatchFrameValidationBranches(t *testing.T) {
	response := dispatchFrame(context.Background(), MessageFrame{
		Type: FrameType("invalid"),
	}, nil)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("response error = %#v, want invalid_frame", response.Error)
	}

	response = dispatchFrame(context.Background(), MessageFrame{
		Type:   FrameTypeEvent,
		Action: FrameActionPing,
	}, nil)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("response error = %#v, want invalid_frame", response.Error)
	}
}

func TestDispatchRPCRequestUnauthorizedAndAccessDenied(t *testing.T) {
	authenticator := staticTokenAuthenticator{token: "t-1"}
	authState := NewConnectionAuthState()
	baseContext := WithRequestSource(context.Background(), RequestSourceHTTP)
	baseContext = WithTokenAuthenticator(baseContext, authenticator)
	baseContext = WithConnectionAuthState(baseContext, authState)
	baseContext = WithRequestACL(baseContext, NewStrictControlPlaneACL())

	unauthorizedResponse := dispatchRPCRequest(baseContext, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-unauthorized"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if unauthorizedResponse.Error == nil {
		t.Fatal("expected unauthorized response")
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(unauthorizedResponse.Error); gatewayCode != ErrorCodeUnauthorized.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeUnauthorized.String())
	}

	deniedACL := &ControlPlaneACL{
		mode:    ACLModeStrict,
		allow:   map[RequestSource]map[string]struct{}{},
		enabled: true,
	}
	deniedContext := WithRequestACL(baseContext, deniedACL)
	deniedContext = WithRequestToken(deniedContext, "t-1")
	deniedContext = WithConnectionAuthState(deniedContext, authState)
	authState.MarkAuthenticated()

	deniedResponse := dispatchRPCRequest(deniedContext, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-denied"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if deniedResponse.Error == nil {
		t.Fatal("expected access denied response")
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(deniedResponse.Error); gatewayCode != ErrorCodeAccessDenied.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeAccessDenied.String())
	}
}

func TestDispatchRPCRequestAuthenticateThenPing(t *testing.T) {
	authenticator := staticTokenAuthenticator{token: "token-2"}
	authState := NewConnectionAuthState()
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithTokenAuthenticator(ctx, authenticator)
	ctx = WithConnectionAuthState(ctx, authState)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	authResponse := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-auth"`),
		Method:  protocol.MethodGatewayAuthenticate,
		Params:  json.RawMessage(`{"token":"token-2"}`),
	}, nil)
	if authResponse.Error != nil {
		t.Fatalf("authenticate response error: %+v", authResponse.Error)
	}
	authFrame, err := decodeJSONRPCResultFrame(authResponse)
	if err != nil {
		t.Fatalf("decode auth frame: %v", err)
	}
	if authFrame.Action != FrameActionAuthenticate {
		t.Fatalf("auth action = %q, want %q", authFrame.Action, FrameActionAuthenticate)
	}

	pingResponse := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-ping"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if pingResponse.Error != nil {
		t.Fatalf("ping response error: %+v", pingResponse.Error)
	}
	pingFrame, err := decodeJSONRPCResultFrame(pingResponse)
	if err != nil {
		t.Fatalf("decode ping frame: %v", err)
	}
	if pingFrame.Action != FrameActionPing {
		t.Fatalf("ping action = %q, want %q", pingFrame.Action, FrameActionPing)
	}
	payloadMap, ok := pingFrame.Payload.(map[string]any)
	if !ok {
		t.Fatalf("ping payload type = %T, want map[string]any", pingFrame.Payload)
	}
	version, _ := payloadMap["version"].(string)
	if strings.TrimSpace(version) == "" {
		t.Fatal("ping payload should include version")
	}
}

func TestDispatchRPCRequestMissingSessionAndAuthHelpers(t *testing.T) {
	metrics := NewGatewayMetrics()
	ctx := WithRequestSource(context.Background(), RequestSourceHTTP)
	ctx = WithGatewayMetrics(ctx, metrics)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	ctx = WithConnectionAuthState(ctx, NewConnectionAuthState())

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-missing-session"`),
		Method:  protocol.MethodGatewayBindStream,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if response.Error == nil {
		t.Fatal("expected missing session error")
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error); gatewayCode != protocol.GatewayCodeMissingRequiredField {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, protocol.GatewayCodeMissingRequiredField)
	}
}

func TestDispatchRPCRequestResolvePermissionDoesNotRequireSession(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-resolve-no-session"`),
		Method:  protocol.MethodGatewayResolvePermission,
		Params:  json.RawMessage(`{"request_id":"perm-1","decision":"reject"}`),
	}, &runtimePortCompileStub{})
	if response.Error != nil {
		t.Fatalf("resolve permission should pass without session_id, got error: %+v", response.Error)
	}

	frame, err := decodeJSONRPCResultFrame(response)
	if err != nil {
		t.Fatalf("decode resolve permission result frame: %v", err)
	}
	if frame.Action != FrameActionResolvePermission {
		t.Fatalf("response action = %q, want %q", frame.Action, FrameActionResolvePermission)
	}
}

func TestDispatchRPCRequestRunHydratesInputPartsAndFallbackRunID(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	runtimeStub := &rpcRunCaptureRuntimeStub{}

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-run-hydrate"`),
		Method:  protocol.MethodGatewayRun,
		Params: json.RawMessage(`{
			"session_id":"session-run-1",
			"input_parts":[
				{"type":"text","text":"hello world"},
				{"type":"image","media":{"uri":"C:/tmp/pic.png","mime_type":"image/png"}}
			]
		}`),
	}, runtimeStub)
	if response.Error != nil {
		t.Fatalf("run response error: %+v", response.Error)
	}

	if runtimeStub.runInput.SessionID != "session-run-1" {
		t.Fatalf("runtime run session_id = %q, want %q", runtimeStub.runInput.SessionID, "session-run-1")
	}
	if runtimeStub.runInput.RunID != "req-run-hydrate" {
		t.Fatalf("runtime run run_id = %q, want %q", runtimeStub.runInput.RunID, "req-run-hydrate")
	}
	if len(runtimeStub.runInput.InputParts) != 2 {
		t.Fatalf("runtime run input_parts len = %d, want %d", len(runtimeStub.runInput.InputParts), 2)
	}
	if runtimeStub.runInput.InputParts[0].Type != InputPartTypeText {
		t.Fatalf("runtime text part type = %q, want %q", runtimeStub.runInput.InputParts[0].Type, InputPartTypeText)
	}
	if runtimeStub.runInput.InputParts[1].Type != InputPartTypeImage {
		t.Fatalf("runtime image part type = %q, want %q", runtimeStub.runInput.InputParts[1].Type, InputPartTypeImage)
	}
	if runtimeStub.runInput.InputParts[1].Media == nil || runtimeStub.runInput.InputParts[1].Media.URI != "C:/tmp/pic.png" {
		t.Fatalf("runtime image media = %#v, want uri %q", runtimeStub.runInput.InputParts[1].Media, "C:/tmp/pic.png")
	}
}

func TestIsRequestAuthenticatedBranches(t *testing.T) {
	authenticator := staticTokenAuthenticator{token: "token-ok"}

	if !isRequestAuthenticated(context.Background()) {
		t.Fatal("request without authenticator should be treated as authenticated")
	}

	ctx := WithTokenAuthenticator(context.Background(), authenticator)
	if isRequestAuthenticated(ctx) {
		t.Fatal("empty request token should fail authentication")
	}

	ctx = WithRequestToken(ctx, "token-ok")
	if !isRequestAuthenticated(ctx) {
		t.Fatal("matching token should pass authentication")
	}

	ctx = WithRequestToken(ctx, "token-bad")
	if isRequestAuthenticated(ctx) {
		t.Fatal("mismatched token should fail authentication")
	}
}

func TestAuthorizeRPCRequestBranches(t *testing.T) {
	denyACL := &ControlPlaneACL{
		mode:    ACLModeStrict,
		allow:   map[RequestSource]map[string]struct{}{},
		enabled: true,
	}

	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, denyACL)
	err := authorizeRPCRequest(ctx, protocol.MethodGatewayAuthenticate, string(FrameActionAuthenticate))
	if err == nil || protocol.GatewayCodeFromJSONRPCError(err) != ErrorCodeAccessDenied.String() {
		t.Fatalf("authenticate acl error = %#v, want access_denied", err)
	}

	ctx = WithTokenAuthenticator(ctx, staticTokenAuthenticator{token: "token-1"})
	err = authorizeRPCRequest(ctx, protocol.MethodGatewayPing, string(FrameActionPing))
	if err == nil || protocol.GatewayCodeFromJSONRPCError(err) != ErrorCodeUnauthorized.String() {
		t.Fatalf("unauthenticated request error = %#v, want unauthorized", err)
	}
}

func TestDispatchRPCRequestMetricsBranches(t *testing.T) {
	metrics := NewGatewayMetrics()
	authenticator := staticTokenAuthenticator{token: "token-m"}
	ctx := WithRequestSource(context.Background(), RequestSourceHTTP)
	ctx = WithTokenAuthenticator(ctx, authenticator)
	ctx = WithConnectionAuthState(ctx, NewConnectionAuthState())
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	ctx = WithGatewayMetrics(ctx, metrics)

	unauthorized := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-m1"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if unauthorized.Error == nil {
		t.Fatal("expected unauthorized error response")
	}

	okCtx := WithRequestToken(ctx, "token-m")
	okCtx = WithConnectionAuthState(okCtx, NewConnectionAuthState())
	ack := dispatchRPCRequest(okCtx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-m2"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if ack.Error != nil {
		t.Fatalf("expected success response, got %+v", ack.Error)
	}

	snapshot := metrics.Snapshot()
	if snapshot["gateway_requests_total"]["http|gateway.ping|error"] == 0 {
		t.Fatalf("expected error request metric, snapshot=%#v", snapshot["gateway_requests_total"])
	}
	if snapshot["gateway_requests_total"]["http|gateway.ping|ok"] == 0 {
		t.Fatalf("expected ok request metric, snapshot=%#v", snapshot["gateway_requests_total"])
	}
}

func TestDispatchRPCRequestMetricsUnknownMethodCollapsed(t *testing.T) {
	metrics := NewGatewayMetrics()
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithGatewayMetrics(ctx, metrics)

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-unknown-method"`),
		Method:  "random.method.user.input",
		Params:  json.RawMessage(`{}`),
	}, nil)
	if response.Error == nil {
		t.Fatal("expected method-not-found error for unknown method")
	}

	snapshot := metrics.Snapshot()
	if snapshot["gateway_requests_total"]["ipc|unknown_method|error"] == 0 {
		t.Fatalf("expected unknown_method metric label, snapshot=%#v", snapshot["gateway_requests_total"])
	}
}
