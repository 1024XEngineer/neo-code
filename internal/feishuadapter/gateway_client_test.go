package feishuadapter

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

type rpcTestServer struct {
	t        *testing.T
	conn     net.Conn
	mu       sync.Mutex
	methods  []string
	requests []protocol.JSONRPCRequest
	encoder  *json.Encoder
	done     chan struct{}
}

func newRPCTestClient(t *testing.T) (*gatewayRPCClient, *rpcTestServer) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	tokenFile := filepath.Join(t.TempDir(), "gateway.token")
	tokenBody := `{"version":1,"token":"token","created_at":"2026-05-05T00:00:00Z","updated_at":"2026-05-05T00:00:00Z"}`
	if err := os.WriteFile(tokenFile, []byte(tokenBody), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	raw, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress:     "ignored",
		TokenFile:         tokenFile,
		RequestTimeout:    500 * time.Millisecond,
		HeartbeatInterval: time.Hour,
		HeartbeatTimeout:  500 * time.Millisecond,
		RetryCount:        0,
		DisableAutoSpawn:  true,
		ResolveListenAddress: func(override string) (string, error) {
			return override, nil
		},
		Dial: func(address string) (net.Conn, error) {
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("new raw gateway client: %v", err)
	}
	server := &rpcTestServer{
		t:       t,
		conn:    serverConn,
		encoder: json.NewEncoder(serverConn),
		done:    make(chan struct{}),
	}
	go server.serve()
	t.Cleanup(func() {
		_ = raw.Close()
		_ = serverConn.Close()
		select {
		case <-server.done:
		case <-time.After(time.Second):
			t.Fatal("rpc test server did not stop")
		}
	})
	return &gatewayRPCClient{client: raw}, server
}

func (s *rpcTestServer) serve() {
	defer close(s.done)
	decoder := json.NewDecoder(s.conn)
	for {
		var request protocol.JSONRPCRequest
		if err := decoder.Decode(&request); err != nil {
			return
		}
		s.mu.Lock()
		s.methods = append(s.methods, request.Method)
		s.requests = append(s.requests, request)
		s.mu.Unlock()
		response := protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      request.ID,
			Result:  json.RawMessage(`{}`),
		}
		if err := s.encoder.Encode(response); err != nil {
			return
		}
	}
}

func (s *rpcTestServer) snapshot() ([]string, []protocol.JSONRPCRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	methods := append([]string(nil), s.methods...)
	requests := append([]protocol.JSONRPCRequest(nil), s.requests...)
	return methods, requests
}

func (s *rpcTestServer) sendNotification(t *testing.T, method string, params any) {
	t.Helper()
	if err := s.encoder.Encode(protocol.JSONRPCNotification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  params,
	}); err != nil {
		t.Fatalf("send notification: %v", err)
	}
}

func TestGatewayRPCClientDelegatesRPCMethods(t *testing.T) {
	client, server := newRPCTestClient(t)
	ctx := context.Background()

	if err := client.Authenticate(ctx); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if err := client.BindStream(ctx, "session-1", "run-1"); err != nil {
		t.Fatalf("bind stream: %v", err)
	}
	if err := client.Run(ctx, "session-1", "run-1", "hello"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := client.ResolvePermission(ctx, "perm-1", "allow_once"); err != nil {
		t.Fatalf("resolve permission: %v", err)
	}
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	server.sendNotification(t, protocol.MethodGatewayEvent, map[string]any{
		"session_id": "session-1",
	})
	select {
	case notification := <-client.Notifications():
		if notification.Method != protocol.MethodGatewayEvent {
			t.Fatalf("notification method = %q, want %q", notification.Method, protocol.MethodGatewayEvent)
		}
		if string(notification.Params) == "" {
			t.Fatal("expected notification params")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}

	methods, requests := server.snapshot()
	wantMethods := []string{
		protocol.MethodGatewayAuthenticate,
		protocol.MethodGatewayBindStream,
		protocol.MethodGatewayRun,
		protocol.MethodGatewayResolvePermission,
		protocol.MethodGatewayPing,
	}
	if len(methods) < len(wantMethods) {
		t.Fatalf("methods = %v, want prefix %v", methods, wantMethods)
	}
	for index, want := range wantMethods {
		if methods[index] != want {
			t.Fatalf("method[%d] = %q, want %q", index, methods[index], want)
		}
	}

	var bindParams protocol.BindStreamParams
	if err := json.Unmarshal(requests[1].Params, &bindParams); err != nil {
		t.Fatalf("decode bind params: %v", err)
	}
	if bindParams.Channel != "all" || bindParams.SessionID != "session-1" || bindParams.RunID != "run-1" {
		t.Fatalf("unexpected bind params: %#v", bindParams)
	}

	var runParams protocol.RunParams
	if err := json.Unmarshal(requests[2].Params, &runParams); err != nil {
		t.Fatalf("decode run params: %v", err)
	}
	if runParams.InputText != "hello" {
		t.Fatalf("run params = %#v, want input text", runParams)
	}
}

func TestGatewayRPCClientCloseStopsNotificationBridge(t *testing.T) {
	client, _ := newRPCTestClient(t)
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case _, ok := <-client.Notifications():
		if ok {
			t.Fatal("expected notifications channel to close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notifications to close")
	}
}

func TestNewGatewayRPCClientConstructsWrapper(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "gateway.sock")
	client, err := NewGatewayRPCClient(GatewayClientConfig{
		ListenAddress:  socketPath,
		TokenFile:      filepath.Join(t.TempDir(), "gateway.token"),
		RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new gateway rpc client: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close wrapper: %v", err)
	}
}

func TestParseGatewayRuntimeEventHandlesEdgeCases(t *testing.T) {
	eventType, sessionID, runID, envelope, err := parseGatewayRuntimeEvent(json.RawMessage(`{"session_id":"s","run_id":"r","payload":{}}`))
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}
	if eventType != "" || sessionID != "s" || runID != "r" || envelope != nil {
		t.Fatalf("unexpected parsed event: type=%q session=%q run=%q payload=%#v", eventType, sessionID, runID, envelope)
	}
	if _, _, _, _, err := parseGatewayRuntimeEvent(json.RawMessage(`{`)); err == nil {
		t.Fatal("expected invalid json error")
	}
	if cloned := cloneRawMessage(nil); cloned != nil {
		t.Fatalf("clone nil = %#v, want nil", cloned)
	}
	if value := readString(map[string]any{"answer": 42}, "answer"); value != "" {
		t.Fatalf("readString = %q, want empty for non-string", value)
	}
}
