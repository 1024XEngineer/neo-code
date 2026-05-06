package protocol

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSONRPCRequestCreateSession(t *testing.T) {
	request := JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"req-1"`),
		Method:  MethodGatewayCreateSession,
		Params:  json.RawMessage(`{"session_id":" s-1 "}`),
	}

	normalized, rpcErr := NormalizeJSONRPCRequest(request)
	if rpcErr != nil {
		t.Fatalf("NormalizeJSONRPCRequest() error = %#v", rpcErr)
	}
	if normalized.Action != "create_session" {
		t.Fatalf("Action = %q, want create_session", normalized.Action)
	}
	if normalized.SessionID != "s-1" {
		t.Fatalf("SessionID = %q, want s-1", normalized.SessionID)
	}

	params, ok := normalized.Payload.(CreateSessionParams)
	if !ok {
		t.Fatalf("payload type = %T, want CreateSessionParams", normalized.Payload)
	}
	if params.SessionID != "s-1" {
		t.Fatalf("payload.SessionID = %q, want s-1", params.SessionID)
	}
}

func TestDecodeCreateSessionParamsBranches(t *testing.T) {
	params, rpcErr := decodeCreateSessionParams(nil)
	if rpcErr != nil {
		t.Fatalf("decodeCreateSessionParams(nil) error = %#v", rpcErr)
	}
	if params.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty", params.SessionID)
	}

	_, rpcErr = decodeCreateSessionParams(json.RawMessage(`{"session_id":1}`))
	if rpcErr == nil {
		t.Fatal("expected type error for invalid session_id")
	}
}
