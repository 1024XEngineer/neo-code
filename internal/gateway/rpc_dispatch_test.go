package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"neo-code/internal/gateway/protocol"
)

func TestDispatchRPCRequestResultEncodeError(t *testing.T) {
	originalHandlers := requestFrameHandlers
	requestFrameHandlers = map[FrameAction]requestFrameHandler{
		FrameActionPing: func(frame MessageFrame) MessageFrame {
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
