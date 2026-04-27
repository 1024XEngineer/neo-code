package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"
)

type rpcRunCaptureRuntimeStub struct {
	runInput            RunInput
	runCh               chan RunInput
	executeSystemToolIn ExecuteSystemToolInput
	executeSystemToolFn func(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error)
	activateSkillFn     func(ctx context.Context, input SessionSkillMutationInput) error
	deactivateSkillFn   func(ctx context.Context, input SessionSkillMutationInput) error
	listSessionSkillsFn func(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error)
	listAvailableFn     func(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error)
	loadSessionFn       func(ctx context.Context, input LoadSessionInput) (Session, error)
}

func (s *rpcRunCaptureRuntimeStub) Run(_ context.Context, input RunInput) error {
	s.runInput = input
	if s.runCh != nil {
		s.runCh <- input
	}
	return nil
}

func (s *rpcRunCaptureRuntimeStub) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (s *rpcRunCaptureRuntimeStub) ExecuteSystemTool(
	ctx context.Context,
	input ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	s.executeSystemToolIn = input
	if s.executeSystemToolFn != nil {
		return s.executeSystemToolFn(ctx, input)
	}
	return tools.ToolResult{}, nil
}

func (s *rpcRunCaptureRuntimeStub) ActivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	if s.activateSkillFn != nil {
		return s.activateSkillFn(ctx, input)
	}
	return nil
}

func (s *rpcRunCaptureRuntimeStub) DeactivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	if s.deactivateSkillFn != nil {
		return s.deactivateSkillFn(ctx, input)
	}
	return nil
}

func (s *rpcRunCaptureRuntimeStub) ListSessionSkills(
	ctx context.Context,
	input ListSessionSkillsInput,
) ([]SessionSkillState, error) {
	if s.listSessionSkillsFn != nil {
		return s.listSessionSkillsFn(ctx, input)
	}
	return nil, nil
}

func (s *rpcRunCaptureRuntimeStub) ListAvailableSkills(
	ctx context.Context,
	input ListAvailableSkillsInput,
) ([]AvailableSkillState, error) {
	if s.listAvailableFn != nil {
		return s.listAvailableFn(ctx, input)
	}
	return nil, nil
}

func (s *rpcRunCaptureRuntimeStub) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (s *rpcRunCaptureRuntimeStub) CancelRun(_ context.Context, _ CancelInput) (bool, error) {
	return false, nil
}

func (s *rpcRunCaptureRuntimeStub) Events() <-chan RuntimeEvent {
	return nil
}

func (s *rpcRunCaptureRuntimeStub) ListSessions(_ context.Context) ([]SessionSummary, error) {
	return nil, nil
}

func (s *rpcRunCaptureRuntimeStub) LoadSession(ctx context.Context, input LoadSessionInput) (Session, error) {
	if s.loadSessionFn != nil {
		return s.loadSessionFn(ctx, input)
	}
	return Session{}, nil
}

func TestDispatchRPCRequestResultEncodeError(t *testing.T) {
	installHandlerRegistryForTest(t, map[FrameAction]requestFrameHandler{
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
		BindingTTL: 100 * time.Millisecond,
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

	key := bindingKey{sessionID: "session-ping", runID: ""}
	relay.mu.RLock()
	beforeState := relay.connectionBindings[connectionID][key]
	relay.mu.RUnlock()
	if beforeState == nil {
		t.Fatal("expected binding state to exist before ping")
	}
	expireBefore := beforeState.expireAt

	time.Sleep(20 * time.Millisecond)
	applyAutomaticBinding(connectionContext, MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionPing,
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		relay.mu.RLock()
		afterState := relay.connectionBindings[connectionID][key]
		relay.mu.RUnlock()
		if afterState != nil && afterState.expireAt.After(expireBefore) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected ping to refresh binding ttl")
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
	authState.MarkAuthenticated("local_admin")

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

func TestDispatchRPCRequestRunMissingSessionAtDispatchLayer(t *testing.T) {
	metrics := NewGatewayMetrics()
	ctx := WithRequestSource(context.Background(), RequestSourceHTTP)
	ctx = WithGatewayMetrics(ctx, metrics)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-run-missing-session"`),
		Method:  protocol.MethodGatewayRun,
		Params:  json.RawMessage(`{"input_text":"hello"}`),
	}, &runtimePortCompileStub{})
	if response.Error == nil {
		t.Fatal("expected missing session error at dispatch layer")
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

func TestDispatchRPCRequestExecuteSystemToolDoesNotRequireSession(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	runtimeStub := &rpcRunCaptureRuntimeStub{
		executeSystemToolFn: func(_ context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error) {
			if input.ToolName != "memo_list" {
				t.Fatalf("tool_name = %q, want %q", input.ToolName, "memo_list")
			}
			if string(input.Arguments) != "{}" {
				t.Fatalf("arguments = %s, want {}", string(input.Arguments))
			}
			if input.Workdir != "/repo" {
				t.Fatalf("workdir = %q, want %q", input.Workdir, "/repo")
			}
			return tools.ToolResult{
				Name:    "memo_list",
				Content: "ok",
			}, nil
		},
	}

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-exec-no-session"`),
		Method:  protocol.MethodGatewayExecuteSystemTool,
		Params: json.RawMessage(`{
			"tool_name":"memo_list",
			"workdir":" /repo ",
			"arguments":null
		}`),
	}, runtimeStub)
	if response.Error != nil {
		t.Fatalf("execute system tool should pass without session_id, got error: %+v", response.Error)
	}

	frame, err := decodeJSONRPCResultFrame(response)
	if err != nil {
		t.Fatalf("decode execute_system_tool result frame: %v", err)
	}
	if frame.Action != FrameActionExecuteSystemTool {
		t.Fatalf("response action = %q, want %q", frame.Action, FrameActionExecuteSystemTool)
	}
}

func TestDispatchRPCRequestSkillMethods(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	runtimeStub := &rpcRunCaptureRuntimeStub{
		activateSkillFn: func(_ context.Context, input SessionSkillMutationInput) error {
			if input.SessionID != "session-skills" {
				t.Fatalf("activate session_id = %q, want %q", input.SessionID, "session-skills")
			}
			if input.SkillID != "go-review" {
				t.Fatalf("activate skill_id = %q, want %q", input.SkillID, "go-review")
			}
			return nil
		},
		deactivateSkillFn: func(_ context.Context, input SessionSkillMutationInput) error {
			if input.SessionID != "session-skills" {
				t.Fatalf("deactivate session_id = %q, want %q", input.SessionID, "session-skills")
			}
			if input.SkillID != "go-review" {
				t.Fatalf("deactivate skill_id = %q, want %q", input.SkillID, "go-review")
			}
			return nil
		},
		listSessionSkillsFn: func(_ context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error) {
			if input.SessionID != "session-skills" {
				t.Fatalf("listSessionSkills session_id = %q, want %q", input.SessionID, "session-skills")
			}
			return []SessionSkillState{
				{
					SkillID: "go-review",
				},
			}, nil
		},
		listAvailableFn: func(_ context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error) {
			if input.SessionID != "session-skills" {
				t.Fatalf("listAvailableSkills session_id = %q, want %q", input.SessionID, "session-skills")
			}
			return []AvailableSkillState{
				{
					Descriptor: SkillDescriptor{ID: "go-review"},
					Active:     true,
				},
			}, nil
		},
	}

	activate := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-activate-skill"`),
		Method:  protocol.MethodGatewayActivateSessionSkill,
		Params:  json.RawMessage(`{"session_id":"session-skills","skill_id":"go-review"}`),
	}, runtimeStub)
	if activate.Error != nil {
		t.Fatalf("activateSessionSkill response error: %+v", activate.Error)
	}
	activateFrame, err := decodeJSONRPCResultFrame(activate)
	if err != nil {
		t.Fatalf("decode activateSessionSkill frame: %v", err)
	}
	if activateFrame.Action != FrameActionActivateSessionSkill {
		t.Fatalf("activateSessionSkill action = %q, want %q", activateFrame.Action, FrameActionActivateSessionSkill)
	}

	deactivate := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-deactivate-skill"`),
		Method:  protocol.MethodGatewayDeactivateSessionSkill,
		Params:  json.RawMessage(`{"session_id":"session-skills","skill_id":"go-review"}`),
	}, runtimeStub)
	if deactivate.Error != nil {
		t.Fatalf("deactivateSessionSkill response error: %+v", deactivate.Error)
	}
	deactivateFrame, err := decodeJSONRPCResultFrame(deactivate)
	if err != nil {
		t.Fatalf("decode deactivateSessionSkill frame: %v", err)
	}
	if deactivateFrame.Action != FrameActionDeactivateSessionSkill {
		t.Fatalf("deactivateSessionSkill action = %q, want %q", deactivateFrame.Action, FrameActionDeactivateSessionSkill)
	}

	listSession := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-list-session-skills"`),
		Method:  protocol.MethodGatewayListSessionSkills,
		Params:  json.RawMessage(`{"session_id":"session-skills"}`),
	}, runtimeStub)
	if listSession.Error != nil {
		t.Fatalf("listSessionSkills response error: %+v", listSession.Error)
	}
	listSessionFrame, err := decodeJSONRPCResultFrame(listSession)
	if err != nil {
		t.Fatalf("decode listSessionSkills frame: %v", err)
	}
	if listSessionFrame.Action != FrameActionListSessionSkills {
		t.Fatalf("listSessionSkills action = %q, want %q", listSessionFrame.Action, FrameActionListSessionSkills)
	}

	listAvailable := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-list-available-skills"`),
		Method:  protocol.MethodGatewayListAvailableSkills,
		Params:  json.RawMessage(`{"session_id":"session-skills"}`),
	}, runtimeStub)
	if listAvailable.Error != nil {
		t.Fatalf("listAvailableSkills response error: %+v", listAvailable.Error)
	}
	listAvailableFrame, err := decodeJSONRPCResultFrame(listAvailable)
	if err != nil {
		t.Fatalf("decode listAvailableSkills frame: %v", err)
	}
	if listAvailableFrame.Action != FrameActionListAvailableSkills {
		t.Fatalf("listAvailableSkills action = %q, want %q", listAvailableFrame.Action, FrameActionListAvailableSkills)
	}
}

func TestDispatchRPCRequestListAvailableSkillsDoesNotRequireSession(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	runtimeStub := &rpcRunCaptureRuntimeStub{
		listAvailableFn: func(_ context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error) {
			if input.SessionID != "" {
				t.Fatalf("listAvailableSkills session_id = %q, want empty", input.SessionID)
			}
			return []AvailableSkillState{
				{
					Descriptor: SkillDescriptor{ID: "go-review"},
					Active:     false,
				},
			}, nil
		},
	}

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-list-available-no-session"`),
		Method:  protocol.MethodGatewayListAvailableSkills,
	}, runtimeStub)
	if response.Error != nil {
		t.Fatalf("listAvailableSkills should pass without session_id, got error: %+v", response.Error)
	}
	frame, err := decodeJSONRPCResultFrame(response)
	if err != nil {
		t.Fatalf("decode listAvailableSkills frame: %v", err)
	}
	if frame.Action != FrameActionListAvailableSkills {
		t.Fatalf("response action = %q, want %q", frame.Action, FrameActionListAvailableSkills)
	}
}

func TestDispatchRPCRequestRunHydratesInputPartsAndFallbackRunID(t *testing.T) {
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	runtimeStub := &rpcRunCaptureRuntimeStub{runCh: make(chan RunInput, 1)}

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

	var captured RunInput
	select {
	case captured = <-runtimeStub.runCh:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime run input was not captured")
	}

	if captured.SessionID != "session-run-1" {
		t.Fatalf("runtime run session_id = %q, want %q", captured.SessionID, "session-run-1")
	}
	if captured.RunID != "req-run-hydrate" {
		t.Fatalf("runtime run run_id = %q, want %q", captured.RunID, "req-run-hydrate")
	}
	if len(captured.InputParts) != 2 {
		t.Fatalf("runtime run input_parts len = %d, want %d", len(captured.InputParts), 2)
	}
	if captured.InputParts[0].Type != InputPartTypeText {
		t.Fatalf("runtime text part type = %q, want %q", captured.InputParts[0].Type, InputPartTypeText)
	}
	if captured.InputParts[1].Type != InputPartTypeImage {
		t.Fatalf("runtime image part type = %q, want %q", captured.InputParts[1].Type, InputPartTypeImage)
	}
	if captured.InputParts[1].Media == nil || captured.InputParts[1].Media.URI != "C:/tmp/pic.png" {
		t.Fatalf("runtime image media = %#v, want uri %q", captured.InputParts[1].Media, "C:/tmp/pic.png")
	}
}

func TestDispatchRPCRequest_DenyCrossSubjectLoadSession(t *testing.T) {
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject_intruder")

	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())

	runtimeStub := &rpcRunCaptureRuntimeStub{
		loadSessionFn: func(_ context.Context, input LoadSessionInput) (Session, error) {
			if input.SubjectID != "subject_owner" {
				return Session{}, ErrRuntimeAccessDenied
			}
			return Session{ID: input.SessionID}, nil
		},
	}

	response := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-load-deny"`),
		Method:  protocol.MethodGatewayLoadSession,
		Params:  json.RawMessage(`{"session_id":"session-1"}`),
	}, runtimeStub)
	if response.Error == nil {
		t.Fatal("expected access denied error")
	}
	if response.Error.Code != protocol.JSONRPCCodeInvalidParams {
		t.Fatalf("rpc error code = %d, want %d", response.Error.Code, protocol.JSONRPCCodeInvalidParams)
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error); gatewayCode != ErrorCodeAccessDenied.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeAccessDenied.String())
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

func TestDispatchRPCRequestMetricsGrowForTUIMethodSequence(t *testing.T) {
	metrics := NewGatewayMetrics()
	authState := NewConnectionAuthState()
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithGatewayMetrics(ctx, metrics)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	ctx = WithConnectionAuthState(ctx, authState)
	ctx = WithTokenAuthenticator(ctx, staticTokenAuthenticator{token: "token-tui"})

	authenticate := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-auth-tui"`),
		Method:  protocol.MethodGatewayAuthenticate,
		Params:  json.RawMessage(`{"token":"token-tui"}`),
	}, &runtimePortCompileStub{})
	if authenticate.Error != nil {
		t.Fatalf("authenticate response error: %+v", authenticate.Error)
	}

	run := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-run-tui"`),
		Method:  protocol.MethodGatewayRun,
		Params:  json.RawMessage(`{"session_id":"session-tui","input_text":"hello"}`),
	}, &runtimePortCompileStub{})
	if run.Error != nil {
		t.Fatalf("run response error: %+v", run.Error)
	}

	compact := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-compact-tui"`),
		Method:  protocol.MethodGatewayCompact,
		Params:  json.RawMessage(`{"session_id":"session-tui"}`),
	}, &runtimePortCompileStub{})
	if compact.Error != nil {
		t.Fatalf("compact response error: %+v", compact.Error)
	}

	listSessions := dispatchRPCRequest(ctx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-list-tui"`),
		Method:  protocol.MethodGatewayListSessions,
		Params:  json.RawMessage(`{}`),
	}, &runtimePortCompileStub{})
	if listSessions.Error != nil {
		t.Fatalf("listSessions response error: %+v", listSessions.Error)
	}

	snapshot := metrics.Snapshot()["gateway_requests_total"]
	if snapshot["ipc|gateway.authenticate|ok"] == 0 {
		t.Fatalf("expected authenticate metric to grow, snapshot=%#v", snapshot)
	}
	if snapshot["ipc|gateway.run|ok"] == 0 {
		t.Fatalf("expected run metric to grow, snapshot=%#v", snapshot)
	}
	if snapshot["ipc|gateway.compact|ok"] == 0 {
		t.Fatalf("expected compact metric to grow, snapshot=%#v", snapshot)
	}
	if snapshot["ipc|gateway.listsessions|ok"] == 0 {
		t.Fatalf("expected listSessions metric to grow, snapshot=%#v", snapshot)
	}
}

func TestDispatchRPCRequestMetricsACLDeniedAndFrameErrorLabels(t *testing.T) {
	metrics := NewGatewayMetrics()
	denyACL := &ControlPlaneACL{
		mode:    ACLModeStrict,
		allow:   map[RequestSource]map[string]struct{}{},
		enabled: true,
	}
	deniedCtx := WithRequestSource(context.Background(), RequestSourceHTTP)
	deniedCtx = WithGatewayMetrics(deniedCtx, metrics)
	deniedCtx = WithRequestACL(deniedCtx, denyACL)
	deniedCtx = WithConnectionAuthState(deniedCtx, NewConnectionAuthState())
	deniedCtx = WithRequestToken(deniedCtx, "token-a")
	deniedCtx = WithTokenAuthenticator(deniedCtx, staticTokenAuthenticator{token: "token-a"})

	denied := dispatchRPCRequest(deniedCtx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-denied-metric"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if denied.Error == nil {
		t.Fatal("expected acl denied response")
	}

	installHandlerRegistryForTest(t, map[FrameAction]requestFrameHandler{
		FrameActionPing: func(_ context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
			return MessageFrame{
				Type:      FrameTypeError,
				Action:    frame.Action,
				RequestID: frame.RequestID,
				Error:     NewFrameError(ErrorCodeAccessDenied, "denied by handler"),
			}
		},
	})

	frameErrCtx := WithRequestSource(context.Background(), RequestSourceHTTP)
	frameErrCtx = WithGatewayMetrics(frameErrCtx, metrics)
	frameErrCtx = WithRequestACL(frameErrCtx, NewStrictControlPlaneACL())
	frameErrCtx = WithConnectionAuthState(frameErrCtx, NewConnectionAuthState())
	frameErrCtx = WithRequestToken(frameErrCtx, "token-b")
	frameErrCtx = WithTokenAuthenticator(frameErrCtx, staticTokenAuthenticator{token: "token-b"})

	frameErrResponse := dispatchRPCRequest(frameErrCtx, protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-frame-err"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}, nil)
	if frameErrResponse.Error == nil {
		t.Fatal("expected frame error response")
	}

	snapshot := metrics.Snapshot()
	if snapshot["gateway_acl_denied_total"]["http|gateway.ping"] < 2 {
		t.Fatalf("expected acl denied metric >= 2, snapshot=%#v", snapshot["gateway_acl_denied_total"])
	}
}
