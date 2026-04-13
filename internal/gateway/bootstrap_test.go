package gateway

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBootstrapPingPongOverIPC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uds integration test runs on non-windows")
	}

	socketPath := filepath.Join(t.TempDir(), "gateway.sock")
	bootstrap, err := NewBootstrap(BootstrapConfig{
		Transport: "uds",
		Endpoint:  socketPath,
	}, nil)
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	defer func() {
		_ = bootstrap.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- bootstrap.Run(ctx)
	}()

	conn := waitForGatewayDial(t, bootstrap.Endpoint())
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	if err := encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "core.ping",
	}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if response["error"] != nil {
		t.Fatalf("response error = %v, want nil", response["error"])
	}
	if got := response["result"]; got != "Pong" {
		t.Fatalf("response result = %v, want Pong", got)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not exit after cancel")
	}
}

func waitForGatewayDial(t *testing.T, endpoint string) net.Conn {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", endpoint)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial gateway endpoint %s timeout", endpoint)
	return nil
}
