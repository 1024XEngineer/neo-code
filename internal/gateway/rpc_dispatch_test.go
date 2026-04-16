package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

func TestDispatchRPCRequestResultEncodeError(t *testing.T) {
	originalHandlers := requestFrameHandlers
	requestFrameHandlers = map[FrameAction]requestFrameHandler{
		FrameActionPing: func(_ context.Context, frame MessageFrame) MessageFrame {
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
