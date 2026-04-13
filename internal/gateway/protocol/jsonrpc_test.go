package protocol

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHandleRawPingSuccess(t *testing.T) {
	router := NewRouter()
	router.Register(MethodCorePing, func(_ context.Context, req Request) Response {
		return NewSuccessResponse(req.ID, "Pong")
	})

	payload := []byte(`{"jsonrpc":"2.0","id":"1","method":"core.ping"}`)
	response := router.HandleRaw(context.Background(), payload)

	var decoded Response
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatalf("HandleRaw() decode response error = %v", err)
	}
	if decoded.Error != nil {
		t.Fatalf("HandleRaw() error = %+v, want nil", decoded.Error)
	}
	if got := decoded.Result; got != "Pong" {
		t.Fatalf("HandleRaw() result = %v, want Pong", got)
	}
}

func TestHandleRawParseError(t *testing.T) {
	router := NewRouter()
	response := router.HandleRaw(context.Background(), []byte(`{"jsonrpc":"2.0","method":`))

	var decoded Response
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatalf("HandleRaw() decode response error = %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("HandleRaw() error = nil, want parse error")
	}
	if decoded.Error.Code != ErrorCodeParseError {
		t.Fatalf("HandleRaw() error code = %d, want %d", decoded.Error.Code, ErrorCodeParseError)
	}
}

func TestHandleRawMethodNotFound(t *testing.T) {
	router := NewRouter()
	payload := []byte(`{"jsonrpc":"2.0","id":"1","method":"core.unknown"}`)
	response := router.HandleRaw(context.Background(), payload)

	var decoded Response
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatalf("HandleRaw() decode response error = %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("HandleRaw() error = nil, want method not found")
	}
	if decoded.Error.Code != ErrorCodeMethodNotFound {
		t.Fatalf("HandleRaw() error code = %d, want %d", decoded.Error.Code, ErrorCodeMethodNotFound)
	}
}

func TestHandleRawInvalidRequest(t *testing.T) {
	router := NewRouter()
	payload := []byte(`{"jsonrpc":"1.0","id":"1","method":"core.ping"}`)
	response := router.HandleRaw(context.Background(), payload)

	var decoded Response
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatalf("HandleRaw() decode response error = %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("HandleRaw() error = nil, want invalid request")
	}
	if decoded.Error.Code != ErrorCodeInvalidRequest {
		t.Fatalf("HandleRaw() error code = %d, want %d", decoded.Error.Code, ErrorCodeInvalidRequest)
	}
}

func TestDecodeRequestDefaultVersion(t *testing.T) {
	decoded, err := DecodeRequest([]byte(`{"method":"core.ping"}`))
	if err != nil {
		t.Fatalf("DecodeRequest() error = %v", err)
	}
	if decoded.JSONRPC != Version {
		t.Fatalf("DecodeRequest() jsonrpc = %s, want %s", decoded.JSONRPC, Version)
	}
}
