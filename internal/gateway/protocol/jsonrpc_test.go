package protocol

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSONRPCRequestPing(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"ping-1"`),
		Method:  MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize ping request: %v", rpcErr)
	}
	if normalized.RequestID != "ping-1" {
		t.Fatalf("request_id = %q, want %q", normalized.RequestID, "ping-1")
	}
	if normalized.Action != "ping" {
		t.Fatalf("action = %q, want %q", normalized.Action, "ping")
	}
}

func TestNormalizeJSONRPCRequestPingWithNumericID(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`123`),
		Method:  MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize ping request with numeric id: %v", rpcErr)
	}
	if normalized.RequestID != "123" {
		t.Fatalf("request_id = %q, want %q", normalized.RequestID, "123")
	}
}

func TestNormalizeJSONRPCRequestWakeOpenURL(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"wake-1"`),
		Method:  MethodWakeOpenURL,
		Params: json.RawMessage(`{
			"action":"review",
			"session_id":"session-1",
			"workdir":"/tmp/repo",
			"params":{"path":"README.md"}
		}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize wake request: %v", rpcErr)
	}
	if normalized.Action != MethodWakeOpenURL {
		t.Fatalf("action = %q, want %q", normalized.Action, MethodWakeOpenURL)
	}
	if normalized.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", normalized.SessionID, "session-1")
	}
	if normalized.Workdir != "/tmp/repo" {
		t.Fatalf("workdir = %q, want %q", normalized.Workdir, "/tmp/repo")
	}
	intent, ok := normalized.Payload.(WakeIntent)
	if !ok {
		t.Fatalf("payload type = %T, want WakeIntent", normalized.Payload)
	}
	if intent.Params["path"] != "README.md" {
		t.Fatalf("intent.params[path] = %q, want %q", intent.Params["path"], "README.md")
	}
}

func TestNormalizeJSONRPCRequestBindStream(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"bind-1"`),
		Method:  MethodGatewayBindStream,
		Params: json.RawMessage(`{
			"session_id":"session-1",
			"run_id":"run-1",
			"channel":"ws"
		}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize bindStream request: %v", rpcErr)
	}
	if normalized.Action != "bind_stream" {
		t.Fatalf("action = %q, want %q", normalized.Action, "bind_stream")
	}
	if normalized.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", normalized.SessionID, "session-1")
	}
	if normalized.RunID != "run-1" {
		t.Fatalf("run_id = %q, want %q", normalized.RunID, "run-1")
	}
	params, ok := normalized.Payload.(BindStreamParams)
	if !ok {
		t.Fatalf("payload type = %T, want BindStreamParams", normalized.Payload)
	}
	if params.Channel != "ws" {
		t.Fatalf("channel = %q, want %q", params.Channel, "ws")
	}
}

func TestNormalizeJSONRPCRequestErrors(t *testing.T) {
	testCases := []struct {
		name            string
		request         JSONRPCRequest
		wantCode        int
		wantGatewayCode string
	}{
		{
			name: "missing id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "invalid version",
			request: JSONRPCRequest{
				JSONRPC: "1.0",
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id object",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`{}`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id array",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`[]`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id boolean",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`true`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "missing method",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "method not found",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  "gateway.unknown",
			},
			wantCode:        JSONRPCCodeMethodNotFound,
			wantGatewayCode: GatewayCodeUnsupportedAction,
		},
		{
			name: "wake missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodWakeOpenURL,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "wake invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodWakeOpenURL,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "bindStream missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "bindStream missing session",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
				Params:  json.RawMessage(`{"channel":"all"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "bindStream invalid channel",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
				Params:  json.RawMessage(`{"session_id":"s-1","channel":"tcp"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidAction,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := NormalizeJSONRPCRequest(tc.request)
			if rpcErr == nil {
				t.Fatal("expected rpc error")
			}
			if rpcErr.Code != tc.wantCode {
				t.Fatalf("rpc code = %d, want %d", rpcErr.Code, tc.wantCode)
			}
			if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != tc.wantGatewayCode {
				t.Fatalf("gateway_code = %q, want %q", gatewayCode, tc.wantGatewayCode)
			}
		})
	}
}

func TestNormalizeJSONRPCRequestInvalidIDReturnsNullResponseID(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`{}`),
		Method:  MethodGatewayPing,
	})
	if rpcErr == nil {
		t.Fatal("expected rpc error")
	}
	if rpcErr.Code != JSONRPCCodeInvalidRequest {
		t.Fatalf("rpc code = %d, want %d", rpcErr.Code, JSONRPCCodeInvalidRequest)
	}
	if normalized.ID != nil {
		t.Fatalf("normalized id = %s, want nil", string(normalized.ID))
	}
}

func TestJSONRPCHelpers(t *testing.T) {
	response, rpcErr := NewJSONRPCResultResponse(json.RawMessage(`"req-1"`), map[string]string{"message": "ok"})
	if rpcErr != nil {
		t.Fatalf("new jsonrpc result response: %v", rpcErr)
	}
	if response.JSONRPC != JSONRPCVersion {
		t.Fatalf("jsonrpc = %q, want %q", response.JSONRPC, JSONRPCVersion)
	}
	if string(response.ID) != `"req-1"` {
		t.Fatalf("id = %s, want %s", response.ID, `"req-1"`)
	}
	var result map[string]string
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode result raw message: %v", err)
	}
	if result["message"] != "ok" {
		t.Fatalf(`result["message"] = %q, want %q`, result["message"], "ok")
	}

	_, rpcErr = NewJSONRPCResultResponse(json.RawMessage(`"req-chan"`), map[string]any{"bad": make(chan int)})
	if rpcErr == nil {
		t.Fatal("expected result encode error")
	}
	if rpcErr.Code != JSONRPCCodeInternalError {
		t.Fatalf("rpc code = %d, want %d", rpcErr.Code, JSONRPCCodeInternalError)
	}
	if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != GatewayCodeInternalError {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, GatewayCodeInternalError)
	}

	rpcErr = NewJSONRPCError(JSONRPCCodeInternalError, "boom", GatewayCodeInternalError)
	errorResponse := NewJSONRPCErrorResponse(json.RawMessage(`"req-2"`), rpcErr)
	if errorResponse.Error == nil {
		t.Fatal("error response should include rpc error payload")
	}
	if GatewayCodeFromJSONRPCError(errorResponse.Error) != GatewayCodeInternalError {
		t.Fatalf("gateway_code = %q, want %q", GatewayCodeFromJSONRPCError(errorResponse.Error), GatewayCodeInternalError)
	}
	if GatewayCodeFromJSONRPCError(nil) != "" {
		t.Fatal("gateway_code for nil rpc error should be empty")
	}

	if MapGatewayCodeToJSONRPCCode(GatewayCodeUnsupportedAction) != JSONRPCCodeMethodNotFound {
		t.Fatal("unsupported_action should map to method_not_found")
	}
	if MapGatewayCodeToJSONRPCCode(GatewayCodeInvalidAction) != JSONRPCCodeInvalidParams {
		t.Fatal("invalid_action should map to invalid_params")
	}
	if MapGatewayCodeToJSONRPCCode("unknown") != JSONRPCCodeInternalError {
		t.Fatal("unknown code should map to internal_error")
	}

	notification := NewJSONRPCNotification(MethodGatewayEvent, map[string]any{"message": "ok"})
	if notification.JSONRPC != JSONRPCVersion {
		t.Fatalf("notification jsonrpc = %q, want %q", notification.JSONRPC, JSONRPCVersion)
	}
	if notification.Method != MethodGatewayEvent {
		t.Fatalf("notification method = %q, want %q", notification.Method, MethodGatewayEvent)
	}
}

func TestNewJSONRPCErrorResponseWithNilIDEncodesNull(t *testing.T) {
	response := NewJSONRPCErrorResponse(nil, NewJSONRPCError(JSONRPCCodeParseError, "parse error", GatewayCodeInvalidFrame))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal encoded response: %v", err)
	}
	if _, ok := payload["id"]; !ok {
		t.Fatal("encoded response should contain id field")
	}
	if payload["id"] != nil {
		t.Fatalf("encoded response id = %#v, want nil", payload["id"])
	}
}
