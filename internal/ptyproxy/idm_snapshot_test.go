//go:build !windows

package ptyproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway"
	gatewayauth "neo-code/internal/gateway/auth"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestIDMControllerEnterExitRingBufferLifecycle(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	authManager, err := gatewayauth.NewManager("")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	var (
		mu                  sync.Mutex
		deleteAskSessionIDs []string
	)

	client, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     authManager.Path(),
		ResolveListenAddress: func(_ string) (string, error) {
			return "test://gateway", nil
		},
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				for {
					var request protocol.JSONRPCRequest
					if err := decoder.Decode(&request); err != nil {
						return
					}
					switch request.Method {
					case protocol.MethodGatewayAuthenticate:
						writeRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionAuthenticate,
						})
					case protocol.MethodGatewayDeleteAskSession:
						var params protocol.DeleteAskSessionParams
						_ = json.Unmarshal(request.Params, &params)
						mu.Lock()
						deleteAskSessionIDs = append(deleteAskSessionIDs, strings.TrimSpace(params.SessionID))
						mu.Unlock()
						writeRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionDeleteAskSession,
						})
					default:
						writeRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type: gateway.FrameTypeAck,
						})
					}
				}
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	defer client.Close()

	authCtx, authCancel := context.WithTimeout(context.Background(), time.Second)
	if err := client.Authenticate(authCtx); err != nil {
		authCancel()
		t.Fatalf("Authenticate() error = %v", err)
	}
	authCancel()

	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	_, _ = logBuffer.Write([]byte("initial text"))

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(true)

	output := &bytes.Buffer{}
	controller := newIDMController(idmControllerOptions{
		PTYWriter:          &bytes.Buffer{},
		Output:             output,
		Stderr:             output,
		RPCClient:          client,
		NotificationStream: nil,
		AutoState:          autoState,
		LogBuffer:          logBuffer,
		DefaultCap:         DefaultRingBufferCapacity,
		Workdir:            "/tmp",
		ShellSessionID:     "shell-test",
	})

	if err := controller.Enter(); err != nil {
		t.Fatalf("Enter() error = %v", err)
	}
	if got := logBuffer.Capacity(); got != idmExpandedRingBufferCapacity {
		t.Fatalf("capacity after Enter = %d, want %d", got, idmExpandedRingBufferCapacity)
	}
	if autoState.Enabled.Load() {
		t.Fatal("auto mode should be disabled in IDM")
	}

	controller.Exit()
	if got := logBuffer.Capacity(); got != DefaultRingBufferCapacity {
		t.Fatalf("capacity after Exit = %d, want %d", got, DefaultRingBufferCapacity)
	}
	if snapshot := logBuffer.SnapshotString(); snapshot != "" {
		t.Fatalf("snapshot after Exit = %q, want empty", snapshot)
	}
	if !autoState.Enabled.Load() {
		t.Fatal("auto mode should be restored after IDM exit")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		mu.Lock()
		calls := len(deleteAskSessionIDs)
		mu.Unlock()
		if calls > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deleteAskSessionIDs) != 1 {
		t.Fatalf("delete ask session calls = %v, want exactly one call", deleteAskSessionIDs)
	}
	if deleteAskSessionIDs[0] == "" {
		t.Fatalf("delete ask session id should not be empty: %v", deleteAskSessionIDs)
	}
}
