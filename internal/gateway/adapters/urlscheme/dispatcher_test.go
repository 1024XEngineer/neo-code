package urlscheme

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
)

func TestDispatchWakeIntentRejectsUnsupportedAction(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false

	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: "open",
			RawURL: "http://neocode:18921/open?path=README.md",
		},
	})
	if err == nil {
		t.Fatal("expected invalid action error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != gateway.ErrorCodeInvalidAction.String() {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInvalidAction.String())
	}
}

func TestDispatchWakeIntentRunLaunchesTerminal(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	var launchedCommand string
	dispatcher.launchTerminalFn = func(command string) error {
		launchedCommand = command
		return nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			SessionID: "session_123",
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	result, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: protocol.WakeActionRun,
			Params: map[string]string{"prompt": "hello"},
			RawURL: "http://neocode:18921/run?prompt=hello",
		},
		ListenAddress: "inmemory",
	})
	if err != nil {
		t.Fatalf("DispatchWakeIntent() error = %v", err)
	}
	<-serverDone

	if !result.TerminalLaunched {
		t.Fatal("expected terminal to be launched for wake.run")
	}
	const commandPrefix = "neocode --session session_123 --wake-input-b64 "
	if !strings.HasPrefix(launchedCommand, commandPrefix) {
		t.Fatalf("launch command = %q, want prefix %q", launchedCommand, commandPrefix)
	}
	decoded, decodeErr := protocol.DecodeWakeStartupInput(strings.TrimPrefix(launchedCommand, commandPrefix))
	if decodeErr != nil {
		t.Fatalf("DecodeWakeStartupInput() error = %v", decodeErr)
	}
	if decoded.Text != "hello" {
		t.Fatalf("startup input text = %q, want %q", decoded.Text, "hello")
	}
	if decoded.Workdir != "" {
		t.Fatalf("startup input workdir = %q, want empty", decoded.Workdir)
	}
}

func TestDispatchWakeIntentReviewLaunchesTerminal(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	var launchedCommand string
	dispatcher.launchTerminalFn = func(command string) error {
		launchedCommand = command
		return nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			SessionID: "session_review_1",
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	result, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action:  protocol.WakeActionReview,
			Params:  map[string]string{"path": "README.md"},
			Workdir: "/workspace/repo",
			RawURL:  "http://neocode:18921/review?path=README.md&workdir=%2Fworkspace%2Frepo",
		},
		ListenAddress: "inmemory",
	})
	if err != nil {
		t.Fatalf("DispatchWakeIntent() error = %v", err)
	}
	<-serverDone

	if !result.TerminalLaunched {
		t.Fatal("expected terminal to be launched for wake.review")
	}
	const commandPrefix = "neocode --session session_review_1 --wake-input-b64 "
	if !strings.HasPrefix(launchedCommand, commandPrefix) {
		t.Fatalf("launch command = %q, want prefix %q", launchedCommand, commandPrefix)
	}
	decoded, decodeErr := protocol.DecodeWakeStartupInput(strings.TrimPrefix(launchedCommand, commandPrefix))
	if decodeErr != nil {
		t.Fatalf("DecodeWakeStartupInput() error = %v", decodeErr)
	}
	if decoded.Text != "请审查文件 README.md" {
		t.Fatalf("startup input text = %q, want %q", decoded.Text, "请审查文件 README.md")
	}
	if decoded.Workdir != "/workspace/repo" {
		t.Fatalf("startup input workdir = %q, want %q", decoded.Workdir, "/workspace/repo")
	}
}

func TestDispatchWakeIntentRunWithSessionIDLaunchesSessionOnly(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	var launchedCommand string
	dispatcher.launchTerminalFn = func(command string) error {
		launchedCommand = command
		return nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			SessionID: "session_123",
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	result, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action:    protocol.WakeActionRun,
			SessionID: "session_123",
			Params: map[string]string{
				"prompt": "should-not-submit",
			},
			RawURL: "http://neocode:18921/run?session_id=session_123&prompt=should-not-submit",
		},
		ListenAddress: "inmemory",
	})
	if err != nil {
		t.Fatalf("DispatchWakeIntent() error = %v", err)
	}
	<-serverDone

	if !result.TerminalLaunched {
		t.Fatal("expected terminal to be launched for wake.run with session_id")
	}
	if launchedCommand != "neocode --session session_123" {
		t.Fatalf("launch command = %q, want %q", launchedCommand, "neocode --session session_123")
	}
}

func TestDispatchWakeIntentRunRequiresSessionID(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	dispatcher.launchTerminalFn = func(string) error {
		t.Fatal("launch terminal should not be called when session_id is missing")
		return nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: protocol.WakeActionRun,
			Params: map[string]string{"prompt": "hello"},
			RawURL: "http://neocode:18921/run?prompt=hello",
		},
		ListenAddress: "inmemory",
	})
	<-serverDone
	if err == nil {
		t.Fatal("expected missing session_id error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != ErrorCodeUnexpectedResponse {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
	}
}

func TestDispatchWakeIntentReviewRequiresSessionID(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	dispatcher.launchTerminalFn = func(string) error {
		t.Fatal("launch terminal should not be called when session_id is missing")
		return nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action:  protocol.WakeActionReview,
			Params:  map[string]string{"path": "README.md"},
			Workdir: "/workspace/repo",
			RawURL:  "http://neocode:18921/review?path=README.md&workdir=%2Fworkspace%2Frepo",
		},
		ListenAddress: "inmemory",
	})
	<-serverDone
	if err == nil {
		t.Fatal("expected missing session_id error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != ErrorCodeUnexpectedResponse {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeUnexpectedResponse)
	}
}

func TestDispatchWakeIntentReturnsGatewayFrameError(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.autoLaunchGateway = false
	dispatcher.resolveListenAddressFn = func(value string) (string, error) { return value, nil }

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })
	t.Cleanup(func() { _ = serverConn.Close() })
	dispatcher.dialFn = func(string) (net.Conn, error) {
		return clientConn, nil
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)

		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		requestID := ""
		if err := json.Unmarshal(request.ID, &requestID); err != nil {
			t.Errorf("decode request id: %v", err)
			return
		}

		response, err := protocol.NewJSONRPCResultResponse(request.ID, gateway.MessageFrame{
			Type:      gateway.FrameTypeError,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestID,
			Error: &gateway.FrameError{
				Code:    gateway.ErrorCodeInvalidAction.String(),
				Message: "invalid wake action",
			},
		})
		if err != nil {
			t.Errorf("build response: %v", err)
			return
		}
		if err := encoder.Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
	}()

	_, err := dispatcher.DispatchWakeIntent(context.Background(), WakeDispatchRequest{
		Intent: protocol.WakeIntent{
			Action: protocol.WakeActionReview,
			Params: map[string]string{"path": "README.md"},
			RawURL: "http://neocode:18921/review?path=README.md",
		},
		ListenAddress: "inmemory",
	})
	<-serverDone
	if err == nil {
		t.Fatal("expected gateway frame error")
	}

	var dispatchErr *DispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", err)
	}
	if dispatchErr.Code != gateway.ErrorCodeInvalidAction.String() {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, gateway.ErrorCodeInvalidAction.String())
	}
}
