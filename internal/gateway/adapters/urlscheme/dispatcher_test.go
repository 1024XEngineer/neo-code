package urlscheme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/launcher"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
)

func TestDispatcherDispatchSuccess(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) {
			return "stub://gateway", nil
		},
		dialFn: func(string) (net.Conn, error) {
			return clientConn, nil
		},
		requestIDFn: func() string {
			return "wake-1"
		},
	}

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
		if rpcRequest.JSONRPC != protocol.JSONRPCVersion {
			t.Errorf("request jsonrpc = %q, want %q", rpcRequest.JSONRPC, protocol.JSONRPCVersion)
			return
		}
		if len(bytes.TrimSpace(rpcRequest.ID)) == 0 {
			t.Error("request id should not be empty")
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
		if got := params.Params["path"]; got != "README.md" {
			t.Errorf("request params[path] = %q, want %q", got, "README.md")
			return
		}

		if err := encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      rpcRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-1",
				Payload: map[string]any{
					"message": "wake intent accepted",
				},
			}),
		}); err != nil {
			t.Errorf("encode response rpc: %v", err)
		}
	}()

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "neocode://review?path=README.md",
	})
	if err != nil {
		t.Fatalf("dispatch url: %v", err)
	}
	if result.ListenAddress != "stub://gateway" {
		t.Fatalf("listen address = %q, want %q", result.ListenAddress, "stub://gateway")
	}
	if result.Response.Type != gateway.FrameTypeAck {
		t.Fatalf("response type = %q, want %q", result.Response.Type, gateway.FrameTypeAck)
	}

	<-done
}

func TestDispatcherDispatchReturnsGatewayError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn:            func() string { return "wake-2" },
	}

	go func() {
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var rpcRequest protocol.JSONRPCRequest
		_ = decoder.Decode(&rpcRequest)
		_ = encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      rpcRequest.ID,
			Error: protocol.NewJSONRPCError(
				protocol.JSONRPCCodeInvalidParams,
				"unsupported wake action",
				gateway.ErrorCodeInvalidAction.String(),
			),
		})
	}()

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "neocode://open?path=README.md",
	})
	if err == nil {
		t.Fatal("expected gateway error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != gateway.ErrorCodeInvalidAction.String() {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInvalidAction.String())
	}
}

func TestDispatcherDispatchReturnsUnexpectedResponseError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn:            func() string { return "wake-3" },
	}

	go func() {
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var rpcRequest protocol.JSONRPCRequest
		_ = decoder.Decode(&rpcRequest)
		_ = encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      rpcRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-3",
			}),
		})
	}()

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "neocode://review?path=README.md",
	})
	if err == nil {
		t.Fatal("expected unexpected response error")
	}
	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != ErrorCodeUnexpectedResponse {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
	}
}

func TestDispatcherDispatchReturnsCorrelationMismatchError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn:            func() string { return "wake-9" },
	}

	go func() {
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var rpcRequest protocol.JSONRPCRequest
		_ = decoder.Decode(&rpcRequest)
		_ = encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      rpcRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-mismatch",
			}),
		})
	}()

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "neocode://review?path=README.md",
	})
	if err == nil {
		t.Fatal("expected correlation mismatch error")
	}
	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != ErrorCodeUnexpectedResponse {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
	}
	if !strings.Contains(dispatchErr.Message, "frame correlation failed") {
		t.Fatalf("error message = %q, want correlation failure", dispatchErr.Message)
	}
}

func TestDispatcherDispatchInputAndDialErrors(t *testing.T) {
	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn: func(string) (net.Conn, error) {
			return nil, errors.New("dial failed")
		},
		requestIDFn: func() string { return "wake-4" },
	}

	_, parseErr := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "http://review?path=README.md",
	})
	if parseErr == nil {
		t.Fatal("expected parse error")
	}
	var parseDispatchErr *DispatchError
	if !errors.As(parseErr, &parseDispatchErr) {
		t.Fatalf("parse error type = %T, want *DispatchError", parseErr)
	}
	if parseDispatchErr.Code != "invalid_scheme" {
		t.Fatalf("parse error code = %q, want %q", parseDispatchErr.Code, "invalid_scheme")
	}

	_, dialErr := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL: "neocode://review?path=README.md",
	})
	if dialErr == nil {
		t.Fatal("expected dial error")
	}
	var dialDispatchErr *DispatchError
	if !errors.As(dialErr, &dialDispatchErr) {
		t.Fatalf("dial error type = %T, want *DispatchError", dialErr)
	}
	if dialDispatchErr.Code != ErrorCodeGatewayUnavailable {
		t.Fatalf("dial error code = %q, want %q", dialDispatchErr.Code, ErrorCodeGatewayUnavailable)
	}
}

func TestDispatcherDialGatewayWithSingleLaunchFallback(t *testing.T) {
	t.Run("launch succeeds and second dial succeeds", func(t *testing.T) {
		dialCalls := 0
		dispatcher := &Dispatcher{
			dialFn: func(string) (net.Conn, error) {
				dialCalls++
				if dialCalls == 1 {
					return nil, errors.New("not ready")
				}
				return &stubDispatchConn{}, nil
			},
			autoLaunchGateway: true,
			resolveLaunchSpecFn: func() (launcher.LaunchSpec, error) {
				return launcher.LaunchSpec{
					LaunchMode: launcher.LaunchModePathBinary,
					Executable: "/usr/local/bin/neocode-gateway",
				}, nil
			},
			startGatewayFn: func(launcher.LaunchSpec) error { return nil },
			nowFn:          time.Now,
			sleepFn:        func(time.Duration) {},
		}

		connection, err := dispatcher.dialGatewayWithFallback(context.Background(), "stub://gateway", "wake-1", "")
		if err != nil {
			t.Fatalf("dialGatewayWithFallback() error = %v", err)
		}
		if connection == nil {
			t.Fatal("expected non-nil connection")
		}
		if dialCalls != 3 {
			t.Fatalf("dial calls = %d, want %d", dialCalls, 3)
		}
	})

	t.Run("single fallback and deterministic error", func(t *testing.T) {
		dialCalls := 0
		now := time.Unix(200, 0)
		dispatcher := &Dispatcher{
			dialFn: func(string) (net.Conn, error) {
				dialCalls++
				return nil, errors.New("still unreachable")
			},
			autoLaunchGateway: true,
			resolveLaunchSpecFn: func() (launcher.LaunchSpec, error) {
				return launcher.LaunchSpec{
					LaunchMode: launcher.LaunchModePathBinary,
					Executable: "/usr/local/bin/neocode-gateway",
				}, nil
			},
			startGatewayFn: func(launcher.LaunchSpec) error { return nil },
			nowFn: func() time.Time {
				current := now
				now = now.Add(4 * time.Second)
				return current
			},
			sleepFn: func(time.Duration) {},
		}

		_, err := dispatcher.dialGatewayWithFallback(context.Background(), "stub://gateway", "wake-2", "")
		if err == nil {
			t.Fatal("expected unreachable error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeGatewayUnavailable {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeGatewayUnavailable)
		}
		if !strings.Contains(dispatchErr.Message, "launch gateway failed") {
			t.Fatalf("error message = %q, want contains launch failure", dispatchErr.Message)
		}
		if dialCalls != 2 {
			t.Fatalf("dial calls = %d, want %d", dialCalls, 2)
		}
	})
}

func TestDispatcherLaunchDecisionLogWhitelistFields(t *testing.T) {
	assertPayload := func(t *testing.T, entry launchDecisionLogEntry, expected map[string]string) {
		t.Helper()
		buffer := &bytes.Buffer{}
		dispatcher := &Dispatcher{
			logger: log.New(buffer, "", 0),
		}
		dispatcher.emitLaunchDecisionLog(entry)

		var payload map[string]any
		if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
			t.Fatalf("decode launch log payload: %v", err)
		}
		for fieldName, expectedValue := range expected {
			value, ok := payload[fieldName]
			if !ok {
				t.Fatalf("missing field %q", fieldName)
			}
			textValue, ok := value.(string)
			if !ok {
				t.Fatalf("field %q type = %T, want string", fieldName, value)
			}
			if textValue != expectedValue {
				t.Fatalf("field %q = %q, want %q", fieldName, textValue, expectedValue)
			}
		}
	}

	assertPayload(t, launchDecisionLogEntry{
		RequestID:     "wake-123",
		Method:        protocol.MethodWakeOpenURL,
		Source:        "url-dispatch",
		Status:        "launch_attempt",
		GatewayCode:   "",
		ListenAddress: "127.0.0.1:8080",
		AuthMode:      "required",
		LaunchMode:    launcher.LaunchModePathBinary,
		ResolvedExec:  "/usr/local/bin/neocode-gateway",
	}, map[string]string{
		"request_id":     "wake-123",
		"method":         protocol.MethodWakeOpenURL,
		"source":         "url-dispatch",
		"status":         "launch_attempt",
		"gateway_code":   "",
		"listen_address": "127.0.0.1:8080",
		"auth_mode":      "required",
		"launch_mode":    launcher.LaunchModePathBinary,
		"resolved_exec":  "/usr/local/bin/neocode-gateway",
	})

	assertPayload(t, launchDecisionLogEntry{
		RequestID:     "wake-124",
		Method:        protocol.MethodWakeOpenURL,
		Source:        "url-dispatch",
		Status:        "launch_failed",
		GatewayCode:   ErrorCodeGatewayUnavailable,
		ListenAddress: "127.0.0.1:8080",
		AuthMode:      "disabled",
		LaunchMode:    launcher.LaunchModeFallbackSubcommand,
		ResolvedExec:  "/usr/local/bin/neocode",
		Message:       "launch failed",
	}, map[string]string{
		"request_id":     "wake-124",
		"method":         protocol.MethodWakeOpenURL,
		"source":         "url-dispatch",
		"status":         "launch_failed",
		"gateway_code":   ErrorCodeGatewayUnavailable,
		"listen_address": "127.0.0.1:8080",
		"auth_mode":      "disabled",
		"launch_mode":    launcher.LaunchModeFallbackSubcommand,
		"resolved_exec":  "/usr/local/bin/neocode",
	})
}

func TestDispatcherDispatchFailsFastOnCanceledContextBeforeIO(t *testing.T) {
	conn := &stubDispatchConn{}
	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return conn, nil },
		requestIDFn:            func() string { return "wake-ctx-1" },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := dispatcher.Dispatch(ctx, DispatchRequest{
		RawURL: "neocode://review?path=README.md",
	})
	if err == nil {
		t.Fatal("expected canceled context error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != ErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
	}
	if !strings.Contains(dispatchErr.Message, context.Canceled.Error()) {
		t.Fatalf("error message = %q, want contains %q", dispatchErr.Message, context.Canceled.Error())
	}
	if conn.writeCalls != 0 {
		t.Fatalf("write calls = %d, want %d", conn.writeCalls, 0)
	}
	if conn.readCalls != 0 {
		t.Fatalf("read calls = %d, want %d", conn.readCalls, 0)
	}
}

func TestDispatcherDispatchInterruptsBlockedReadOnContextCancel(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn:            func() string { return "wake-ctx-2" },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requestArrived := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		decoder := json.NewDecoder(serverConn)
		var rpcRequest protocol.JSONRPCRequest
		if err := decoder.Decode(&rpcRequest); err != nil {
			t.Errorf("decode request rpc: %v", err)
			return
		}
		close(requestArrived)
		<-ctx.Done()
	}()

	dispatchDone := make(chan error, 1)
	go func() {
		_, dispatchErr := dispatcher.Dispatch(ctx, DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		dispatchDone <- dispatchErr
	}()

	select {
	case <-requestArrived:
	case <-time.After(1 * time.Second):
		t.Fatal("request frame did not arrive in time")
	}

	cancel()

	select {
	case err := <-dispatchDone:
		if err == nil {
			t.Fatal("expected canceled dispatch error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeInternal {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
		}
		if !strings.Contains(dispatchErr.Message, context.Canceled.Error()) {
			t.Fatalf("error message = %q, want contains %q", dispatchErr.Message, context.Canceled.Error())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("dispatch did not fail fast after context cancellation")
	}

	select {
	case <-serverDone:
	case <-time.After(1 * time.Second):
		t.Fatal("server goroutine did not exit")
	}
}

func TestDispatcherResolveAddressUsesTransportResolver(t *testing.T) {
	dispatcher := NewDispatcher()
	got, err := dispatcher.resolveListenAddressFn("")
	if err != nil {
		t.Fatalf("resolve dispatcher address: %v", err)
	}
	want, err := transport.ResolveListenAddress("")
	if err != nil {
		t.Fatalf("resolve transport address: %v", err)
	}
	if got != want {
		t.Fatalf("resolved address = %q, want %q", got, want)
	}
}

func TestApplyDispatchDeadlineAndToDispatchError(t *testing.T) {
	stubConn := &stubDispatchConn{}
	before := time.Now()
	if err := applyDispatchDeadline(stubConn, nil); err != nil {
		t.Fatalf("apply dispatch deadline with nil context: %v", err)
	}
	if stubConn.setDeadlineCalls != 1 {
		t.Fatalf("set deadline calls = %d, want %d", stubConn.setDeadlineCalls, 1)
	}
	if stubConn.lastDeadline.Before(before) {
		t.Fatalf("last deadline = %v, want >= %v", stubConn.lastDeadline, before)
	}

	connA, connB := net.Pipe()
	t.Cleanup(func() {
		_ = connA.Close()
		_ = connB.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := applyDispatchDeadline(connA, ctx); err != nil {
		t.Fatalf("apply dispatch deadline: %v", err)
	}

	unknownErr := toDispatchError(errors.New("boom"))
	var dispatchErr *DispatchError
	if !errors.As(unknownErr, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", unknownErr)
	}
	if dispatchErr.Code != ErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
	}
	if toDispatchError(nil) != nil {
		t.Fatal("toDispatchError(nil) should return nil")
	}
	if toDispatchError(newDispatchError("x", "y")) == nil {
		t.Fatal("toDispatchError should keep dispatch error")
	}
	if (*DispatchError)(nil).Error() != "" {
		t.Fatal("nil dispatch error string should be empty")
	}

	if !strings.Contains(newDispatchError("x", "y").Error(), "x: y") {
		t.Fatal("dispatch error text should include code and message")
	}
}

func TestDispatchConvenienceAndRequestID(t *testing.T) {
	_, err := Dispatch(context.Background(), DispatchRequest{
		RawURL: "http://review?path=README.md",
	})
	if err == nil {
		t.Fatal("expected parse error from convenience dispatch")
	}
	if !strings.HasPrefix(nextDispatchRequestID(), "wake-") {
		t.Fatal("request id should use wake- prefix")
	}
}

func TestDispatcherDispatchAdditionalErrorBranches(t *testing.T) {
	t.Run("resolve listen address failed", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) {
				return "", errors.New("resolve failed")
			},
			dialFn:      func(string) (net.Conn, error) { return nil, nil },
			requestIDFn: func() string { return "wake-10" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected resolve error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeInternal {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
		}
	})

	t.Run("set deadline failed", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{setDeadlineErr: errors.New("set deadline failed")}, nil
			},
			requestIDFn: func() string { return "wake-11" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected deadline error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeInternal {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
		}
	})

	t.Run("encode request failed", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{writeErr: errors.New("write failed")}, nil
			},
			requestIDFn: func() string { return "wake-12" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected encode error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeInternal {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
		}
	})

	t.Run("encode request failed with nil context", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{writeErr: errors.New("write failed")}, nil
			},
			requestIDFn: func() string { return "wake-12-nil" },
		}

		_, err := dispatcher.Dispatch(nil, DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected encode error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeInternal {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
		}
	})

	t.Run("decode response failed", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{readBuffer: bytes.NewBufferString("not-json")}, nil
			},
			requestIDFn: func() string { return "wake-13" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected decode error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("decode response failed with nil context", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{readBuffer: bytes.NewBufferString("not-json")}, nil
			},
			requestIDFn: func() string { return "wake-13-nil" },
		}

		_, err := dispatcher.Dispatch(nil, DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected decode error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("gateway response missing result payload", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-14"}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-14" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected missing result payload branch")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("response rpc version mismatch", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"jsonrpc":"1.0","id":"wake-15","result":{}}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-15" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected rpc version mismatch error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("response rpc id mismatch", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-other","result":{}}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-16" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected rpc id mismatch error")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("response contains both result and error", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(
						`{"jsonrpc":"2.0","id":"wake-17","result":{},"error":{"code":-32603,"message":"boom"}}` + "\n",
					),
				}, nil
			},
			requestIDFn: func() string { return "wake-17" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected both result and error payload failure")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})

	t.Run("rpc error without gateway_code uses fallback code map", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(
						`{"jsonrpc":"2.0","id":"wake-18","error":{"code":-32601,"message":"method not found"}}` + "\n",
					),
				}, nil
			},
			requestIDFn: func() string { return "wake-18" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected rpc error mapping failure")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != gateway.ErrorCodeUnsupportedAction.String() {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeUnsupportedAction.String())
		}
	})

	t.Run("rpc error with empty message uses fallback text", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-19","error":{"code":-32603,"message":""}}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-19" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected rpc error mapping failure")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != gateway.ErrorCodeInternalError.String() {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInternalError.String())
		}
		if !strings.Contains(dispatchErr.Message, "empty rpc error message") {
			t.Fatalf("error message = %q, want fallback text", dispatchErr.Message)
		}
	})

	t.Run("decode response frame failed", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-20","result":"not-frame"}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-20" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{RawURL: "neocode://review?path=README.md"})
		if err == nil {
			t.Fatal("expected decode frame failure")
		}
		var dispatchErr *DispatchError
		if !errors.As(err, &dispatchErr) {
			t.Fatalf("error type = %T, want *DispatchError", err)
		}
		if dispatchErr.Code != ErrorCodeUnexpectedResponse {
			t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
		}
	})
}

func TestDispatcherAuthenticateBranches(t *testing.T) {
	t.Run("rpc returns error", func(t *testing.T) {
		dispatcher := &Dispatcher{
			requestIDFn: func() string { return "wake-auth-1" },
		}
		conn := &stubDispatchConn{
			readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-auth-1-auth","error":{"code":-32600,"message":"unauthorized","data":{"gateway_code":"unauthorized"}}}` + "\n"),
		}
		err := dispatcher.authenticate(context.Background(), conn, "token-1")
		if err == nil {
			t.Fatal("expected authenticate rpc error")
		}
	})

	t.Run("missing auth result payload", func(t *testing.T) {
		dispatcher := &Dispatcher{
			requestIDFn: func() string { return "wake-auth-2" },
		}
		conn := &stubDispatchConn{
			readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-auth-2-auth"}` + "\n"),
		}
		err := dispatcher.authenticate(context.Background(), conn, "token-1")
		if err == nil || !strings.Contains(err.Error(), "missing result payload") {
			t.Fatalf("expected missing result payload error, got %v", err)
		}
	})

	t.Run("unexpected auth frame", func(t *testing.T) {
		dispatcher := &Dispatcher{
			requestIDFn: func() string { return "wake-auth-3" },
		}
		conn := &stubDispatchConn{
			readBuffer: bytes.NewBufferString(`{"jsonrpc":"2.0","id":"wake-auth-3-auth","result":{"type":"ack","action":"gateway.ping","request_id":"wake-auth-3-auth"}}` + "\n"),
		}
		err := dispatcher.authenticate(context.Background(), conn, "token-1")
		if err == nil || !strings.Contains(err.Error(), "unexpected auth response frame") {
			t.Fatalf("expected unexpected auth frame error, got %v", err)
		}
	})
}

func TestDispatcherDispatchWithAuthHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn: func() string {
			return "wake-auth"
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var authRequest protocol.JSONRPCRequest
		if err := decoder.Decode(&authRequest); err != nil {
			t.Errorf("decode auth request: %v", err)
			return
		}
		if authRequest.Method != protocol.MethodGatewayAuthenticate {
			t.Errorf("auth method = %q, want %q", authRequest.Method, protocol.MethodGatewayAuthenticate)
			return
		}
		if err := encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      authRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionAuthenticate,
				RequestID: "wake-auth-auth",
				Payload:   map[string]string{"message": "authenticated"},
			}),
		}); err != nil {
			t.Errorf("encode auth response: %v", err)
			return
		}

		var wakeRequest protocol.JSONRPCRequest
		if err := decoder.Decode(&wakeRequest); err != nil {
			t.Errorf("decode wake request: %v", err)
			return
		}
		if wakeRequest.Method != protocol.MethodWakeOpenURL {
			t.Errorf("wake method = %q, want %q", wakeRequest.Method, protocol.MethodWakeOpenURL)
			return
		}
		if err := encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      wakeRequest.ID,
			Result: mustMarshalRawJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-auth",
				Payload:   map[string]string{"message": "wake intent accepted"},
			}),
		}); err != nil {
			t.Errorf("encode wake response: %v", err)
		}
	}()

	result, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL:    "neocode://review?path=README.md",
		AuthToken: "token-1",
	})
	if err != nil {
		t.Fatalf("dispatch with auth: %v", err)
	}
	if result.Response.Action != gateway.FrameActionWakeOpenURL {
		t.Fatalf("action = %q, want %q", result.Response.Action, gateway.FrameActionWakeOpenURL)
	}
	<-done
}

func TestDispatcherDispatchWithAuthHandshakeError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	dispatcher := &Dispatcher{
		resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
		dialFn:                 func(string) (net.Conn, error) { return clientConn, nil },
		requestIDFn: func() string {
			return "wake-auth-err"
		},
	}

	go func() {
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var authRequest protocol.JSONRPCRequest
		_ = decoder.Decode(&authRequest)
		_ = encoder.Encode(protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      authRequest.ID,
			Error: protocol.NewJSONRPCError(
				protocol.JSONRPCCodeInvalidParams,
				"invalid token",
				protocol.GatewayCodeUnauthorized,
			),
		})
	}()

	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		RawURL:    "neocode://review?path=README.md",
		AuthToken: "bad-token",
	})
	if err == nil {
		t.Fatal("expected auth handshake error")
	}
	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != protocol.GatewayCodeUnauthorized {
		t.Fatalf("code = %q, want %q", dispatchErr.Code, protocol.GatewayCodeUnauthorized)
	}
}

func TestDispatcherJSONRPCHelpers(t *testing.T) {
	marshalErr := toDispatchErrorFromJSONRPC(&protocol.JSONRPCError{
		Code:    protocol.JSONRPCCodeInternalError,
		Message: "boom",
	})
	var dispatchErr *DispatchError
	if !errors.As(marshalErr, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", marshalErr)
	}
	if dispatchErr.Code != gateway.ErrorCodeInternalError.String() {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInternalError.String())
	}

	emptyErr := toDispatchErrorFromJSONRPC(nil)
	if !errors.As(emptyErr, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", emptyErr)
	}
	if dispatchErr.Code != ErrorCodeUnexpectedResponse {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
	}

	if mapJSONRPCCodeToDispatchCode(protocol.JSONRPCCodeMethodNotFound) != gateway.ErrorCodeUnsupportedAction.String() {
		t.Fatal("method_not_found should map to unsupported_action")
	}
	if mapJSONRPCCodeToDispatchCode(protocol.JSONRPCCodeInvalidParams) != gateway.ErrorCodeInvalidFrame.String() {
		t.Fatal("invalid_params should map to invalid_frame")
	}
	if mapJSONRPCCodeToDispatchCode(123456) != ErrorCodeInternal {
		t.Fatal("unknown rpc code should map to internal_error")
	}

	if _, err := decodeResponseFrameResult(json.RawMessage(`"not-frame"`)); err == nil {
		t.Fatal("expected decodeResponseFrameResult unmarshal failure")
	}
	if _, err := decodeResponseFrameResult(json.RawMessage(`{"type":"ack","action":"wake.openUrl","request_id":"x"`)); err == nil {
		t.Fatal("expected decodeResponseFrameResult malformed json failure")
	}

	if _, err := marshalJSONRawMessage(make(chan int)); err == nil {
		t.Fatal("expected marshalJSONRawMessage failure")
	}
}

type stubDispatchConn struct {
	readBuffer       *bytes.Buffer
	writeErr         error
	setDeadlineErr   error
	readCalls        int
	writeCalls       int
	setDeadlineCalls int
	lastDeadline     time.Time
}

func (c *stubDispatchConn) Read(p []byte) (int, error) {
	c.readCalls++
	if c.readBuffer == nil {
		return 0, io.EOF
	}
	return c.readBuffer.Read(p)
}

func (c *stubDispatchConn) Write(p []byte) (int, error) {
	c.writeCalls++
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}

func (c *stubDispatchConn) Close() error {
	return nil
}

func (c *stubDispatchConn) LocalAddr() net.Addr {
	return stubDispatchAddr("local")
}

func (c *stubDispatchConn) RemoteAddr() net.Addr {
	return stubDispatchAddr("remote")
}

func (c *stubDispatchConn) SetDeadline(deadline time.Time) error {
	c.setDeadlineCalls++
	c.lastDeadline = deadline
	return c.setDeadlineErr
}

func (c *stubDispatchConn) SetReadDeadline(_ time.Time) error {
	return c.setDeadlineErr
}

func (c *stubDispatchConn) SetWriteDeadline(_ time.Time) error {
	return c.setDeadlineErr
}

type stubDispatchAddr string

func (a stubDispatchAddr) Network() string {
	return "stub"
}

func (a stubDispatchAddr) String() string {
	return string(a)
}

func mustMarshalRawJSON(t *testing.T, payload any) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return json.RawMessage(raw)
}
