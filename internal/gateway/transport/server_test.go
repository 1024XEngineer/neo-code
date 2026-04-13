package transport

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNewServerInvalidMode(t *testing.T) {
	_, err := NewServer(Config{Mode: "invalid"}, func(_ context.Context, _ []byte) []byte { return nil })
	if err == nil {
		t.Fatal("NewServer() error = nil, want invalid mode error")
	}
}

func TestNewServerUnsupportedNPipeOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-windows only")
	}

	_, err := NewServer(Config{Mode: ModeNPipe}, func(_ context.Context, _ []byte) []byte { return nil })
	if err == nil {
		t.Fatal("NewServer() error = nil, want unsupported transport")
	}
}

func TestServerServeAndHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uds dialing is unix-only in this test")
	}

	socketPath := filepath.Join(t.TempDir(), "gateway.sock")
	server, err := NewServer(Config{Mode: ModeUDS, Endpoint: socketPath}, func(_ context.Context, payload []byte) []byte {
		return []byte(`{"jsonrpc":"2.0","id":"1","result":"Pong"}`)
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer func() {
		_ = server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx)
	}()

	conn := waitForUnixDial(t, server.Endpoint())
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "core.ping"}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := response["result"]; got != "Pong" {
		t.Fatalf("response result = %v, want Pong", got)
	}

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not exit after cancel")
	}
}

func waitForUnixDial(t *testing.T, endpoint string) net.Conn {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", endpoint)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial unix %s timeout", endpoint)
	return nil
}
