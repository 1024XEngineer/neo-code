//go:build !windows

package urlscheme

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/transport"
)

func TestDispatchEndToEndWithGatewayServer(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "run", "gateway.sock")
	server, err := gateway.NewServer(gateway.ServerOptions{
		ListenAddress: socketPath,
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("new gateway server: %v", err)
	}

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(serverCtx, nil)
	}()

	if err := waitGatewayReady(socketPath, 2*time.Second); err != nil {
		t.Fatalf("wait gateway ready: %v", err)
	}

	successResult, err := Dispatch(context.Background(), DispatchRequest{
		RawURL:        "neocode://review?path=README.md",
		ListenAddress: socketPath,
	})
	if err != nil {
		t.Fatalf("dispatch review url: %v", err)
	}
	if successResult.Response.Type != gateway.FrameTypeAck {
		t.Fatalf("response type = %q, want %q", successResult.Response.Type, gateway.FrameTypeAck)
	}
	if successResult.Response.Action != gateway.FrameActionWakeOpenURL {
		t.Fatalf("response action = %q, want %q", successResult.Response.Action, gateway.FrameActionWakeOpenURL)
	}

	_, err = Dispatch(context.Background(), DispatchRequest{
		RawURL:        "neocode://open?path=README.md",
		ListenAddress: socketPath,
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

	cancelServer()
	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("serve returned error: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gateway server did not stop in time")
	}
}

func waitGatewayReady(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := transport.Dial(address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("gateway did not become ready before timeout")
}
