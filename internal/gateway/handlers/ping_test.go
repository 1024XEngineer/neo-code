package handlers

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/gateway/protocol"
)

type coreClientStub struct {
	pong string
	err  error
}

func (s coreClientStub) Ping(_ context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.pong, nil
}

func TestPingHandlerHandleSuccess(t *testing.T) {
	handler := NewPingHandler(coreClientStub{pong: "Pong"})
	response := handler.Handle(context.Background(), protocol.Request{ID: []byte(`"1"`)})

	if response.Error != nil {
		t.Fatalf("Handle() error = %+v, want nil", response.Error)
	}
	if response.Result != "Pong" {
		t.Fatalf("Handle() result = %v, want Pong", response.Result)
	}
}

func TestPingHandlerHandleCoreError(t *testing.T) {
	handler := NewPingHandler(coreClientStub{err: errors.New("boom")})
	response := handler.Handle(context.Background(), protocol.Request{ID: []byte(`"1"`)})

	if response.Error == nil {
		t.Fatal("Handle() error = nil, want internal error")
	}
	if response.Error.Code != protocol.ErrorCodeInternalError {
		t.Fatalf("Handle() error code = %d, want %d", response.Error.Code, protocol.ErrorCodeInternalError)
	}
}

func TestPingHandlerHandleNilCore(t *testing.T) {
	handler := NewPingHandler(nil)
	response := handler.Handle(context.Background(), protocol.Request{})

	if response.Error == nil {
		t.Fatal("Handle() error = nil, want internal error")
	}
	if response.Error.Code != protocol.ErrorCodeInternalError {
		t.Fatalf("Handle() error code = %d, want %d", response.Error.Code, protocol.ErrorCodeInternalError)
	}
}
