package urlscheme

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
)

func TestBuildHTTPDaemonWakeIntentRun(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"http://neocode:18921/run?prompt=hello&workdir=/tmp/x&session_id=s-1",
		http.NoBody,
	)
	intent, err := buildHTTPDaemonWakeIntent(request)
	if err != nil {
		t.Fatalf("buildHTTPDaemonWakeIntent() error = %v", err)
	}
	if intent.Action != protocol.WakeActionRun {
		t.Fatalf("action = %q, want %q", intent.Action, protocol.WakeActionRun)
	}
	if intent.Params["prompt"] != "hello" {
		t.Fatalf("prompt = %q, want %q", intent.Params["prompt"], "hello")
	}
	if intent.Workdir != "/tmp/x" {
		t.Fatalf("workdir = %q, want %q", intent.Workdir, "/tmp/x")
	}
	if intent.SessionID != "s-1" {
		t.Fatalf("session_id = %q, want %q", intent.SessionID, "s-1")
	}
}

func TestBuildHTTPDaemonWakeIntentRejectsMissingPathForReview(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://neocode:18921/review", http.NoBody)
	_, err := buildHTTPDaemonWakeIntent(request)
	if err == nil {
		t.Fatal("expected missing path error")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Fatalf("error = %v, want contains path", err)
	}
}

func TestIsAllowedHTTPDaemonHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{host: "neocode:18921", want: true},
		{host: "localhost:18921", want: true},
		{host: "127.0.0.1:18921", want: true},
		{host: "evil.com:18921", want: false},
	}
	for _, testCase := range cases {
		if got := isAllowedHTTPDaemonHost(testCase.host); got != testCase.want {
			t.Fatalf("isAllowedHTTPDaemonHost(%q) = %v, want %v", testCase.host, got, testCase.want)
		}
	}
}

func TestHTTPDaemonHandlerDispatchesIntent(t *testing.T) {
	var captured daemonWakeDispatchRequest
	handler := newHTTPDaemonHandler(
		func(_ context.Context, request daemonWakeDispatchRequest) (daemonWakeDispatchResult, error) {
			captured = request
			return daemonWakeDispatchResult{
				Action:    request.Intent.Action,
				SessionID: "session-from-runtime",
			}, nil
		},
		"/tmp/gateway.sock",
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://neocode:18921/run?prompt=hello", http.NoBody)
	request.Host = "neocode:18921"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if captured.Intent.Action != protocol.WakeActionRun {
		t.Fatalf("captured action = %q, want %q", captured.Intent.Action, protocol.WakeActionRun)
	}
	if captured.Intent.Params["prompt"] != "hello" {
		t.Fatalf("captured prompt = %q, want %q", captured.Intent.Params["prompt"], "hello")
	}
	if captured.ListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("captured listen address = %q, want %q", captured.ListenAddress, "/tmp/gateway.sock")
	}
}

func TestHTTPDaemonHandlerRejectsForbiddenHost(t *testing.T) {
	handler := newHTTPDaemonHandler(
		func(context.Context, daemonWakeDispatchRequest) (daemonWakeDispatchResult, error) {
			return daemonWakeDispatchResult{}, nil
		},
		"",
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://evil.com:18921/run?prompt=hello", http.NoBody)
	request.Host = "evil.com:18921"
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestHasHostsAlias(t *testing.T) {
	text := []byte("127.0.0.1 localhost neocode\n")
	if !hasHostsAlias(text, "neocode") {
		t.Fatal("expected hosts alias present")
	}
	if hasHostsAlias(text, "other-host") {
		t.Fatal("expected missing alias")
	}
}

func TestDispatchWakeIntentSuccess(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := newStubDispatcher(func(dispatcher *Dispatcher) {
		dispatcher.dialFn = func(string) (net.Conn, error) { return clientConn, nil }
		dispatcher.requestIDFn = func() string { return "wake-intent-1" }
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var rpcRequest protocol.JSONRPCRequest
		if err := decoder.Decode(&rpcRequest); err != nil {
			t.Errorf("decode request rpc: %v", err)
			return
		}
		if rpcRequest.Method != protocol.MethodWakeOpenURL {
			t.Errorf("request method = %q, want %q", rpcRequest.Method, protocol.MethodWakeOpenURL)
			return
		}
		var params protocol.WakeIntent
		if err := json.Unmarshal(rpcRequest.Params, &params); err != nil {
			t.Errorf("decode request params: %v", err)
			return
		}
		if params.Action != protocol.WakeActionReview {
			t.Errorf("request params action = %q, want %q", params.Action, protocol.WakeActionReview)
			return
		}
		if err := encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      rpcRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-intent-1",
			}),
		}); err != nil {
			t.Errorf("encode response rpc: %v", err)
		}
	}()

	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: protocol.WakeActionReview,
			Params: map[string]string{"path": "README.md"},
		},
	})
	if err != nil {
		t.Fatalf("DispatchWakeIntent() error = %v", err)
	}
	<-done
}

func TestDispatchWakeIntentRejectsInvalidAction(t *testing.T) {
	dispatcher := newStubDispatcher(func(dispatcher *Dispatcher) {
		dispatcher.dialFn = func(string) (net.Conn, error) {
			return nil, errors.New("should not dial")
		}
	})
	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: "open",
			Params: map[string]string{"path": "README.md"},
		},
	})
	if err == nil {
		t.Fatal("expected invalid action error")
	}
	dispatchErr := assertDispatchErrorCode(t, err, gateway.ErrorCodeInvalidAction.String())
	if !strings.Contains(dispatchErr.Message, "invalid wake action") {
		t.Fatalf("error message = %q, want contains invalid wake action", dispatchErr.Message)
	}
}

func newStubDispatcher(customize func(*Dispatcher)) *Dispatcher {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(listenAddress string) (string, error) {
		if strings.TrimSpace(listenAddress) == "" {
			return "inmemory", nil
		}
		return listenAddress, nil
	}
	if customize != nil {
		customize(dispatcher)
	}
	return dispatcher
}

func mustMarshalRawJSON(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func assertDispatchErrorCode(t *testing.T, err error, code string) *DispatchError {
	t.Helper()
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != code {
		t.Fatalf("dispatch error code = %q, want %q", dispatchErr.Code, code)
	}
	return dispatchErr
}
