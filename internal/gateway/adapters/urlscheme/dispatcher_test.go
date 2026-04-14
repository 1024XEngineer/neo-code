package urlscheme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway"
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

		var requestFrame gateway.MessageFrame
		if err := decoder.Decode(&requestFrame); err != nil {
			t.Errorf("decode request frame: %v", err)
			return
		}
		if requestFrame.Action != gateway.FrameActionWakeOpenURL {
			t.Errorf("request action = %q, want %q", requestFrame.Action, gateway.FrameActionWakeOpenURL)
			return
		}

		if err := encoder.Encode(gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: requestFrame.RequestID,
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		}); err != nil {
			t.Errorf("encode response frame: %v", err)
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
		var requestFrame gateway.MessageFrame
		_ = decoder.Decode(&requestFrame)
		_ = encoder.Encode(gateway.MessageFrame{
			Type:      gateway.FrameTypeError,
			Action:    requestFrame.Action,
			RequestID: requestFrame.RequestID,
			Error: &gateway.FrameError{
				Code:    gateway.ErrorCodeInvalidAction.String(),
				Message: "unsupported wake action",
			},
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
		var requestFrame gateway.MessageFrame
		_ = decoder.Decode(&requestFrame)
		_ = encoder.Encode(gateway.MessageFrame{
			Type:      gateway.FrameTypeEvent,
			Action:    requestFrame.Action,
			RequestID: requestFrame.RequestID,
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

	t.Run("gateway error frame missing payload", func(t *testing.T) {
		dispatcher := &Dispatcher{
			resolveListenAddressFn: func(string) (string, error) { return "stub://gateway", nil },
			dialFn: func(string) (net.Conn, error) {
				return &stubDispatchConn{
					readBuffer: bytes.NewBufferString(`{"type":"error","action":"wake.openUrl","request_id":"wake-14"}` + "\n"),
				}, nil
			},
			requestIDFn: func() string { return "wake-14" },
		}

		_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
			RawURL: "neocode://review?path=README.md",
		})
		if err == nil {
			t.Fatal("expected missing error payload branch")
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

type stubDispatchConn struct {
	readBuffer     *bytes.Buffer
	writeErr       error
	setDeadlineErr error
}

func (c *stubDispatchConn) Read(p []byte) (int, error) {
	if c.readBuffer == nil {
		return 0, io.EOF
	}
	return c.readBuffer.Read(p)
}

func (c *stubDispatchConn) Write(p []byte) (int, error) {
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

func (c *stubDispatchConn) SetDeadline(_ time.Time) error {
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
