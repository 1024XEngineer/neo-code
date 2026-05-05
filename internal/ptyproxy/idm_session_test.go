//go:build !windows

package ptyproxy

import (
	"encoding/json"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestGenerateAndParseIDMSessionID(t *testing.T) {
	sessionID := generateIDMSessionID(1234)
	pid, ok := parseIDMSessionPID(sessionID)
	if !ok {
		t.Fatalf("parseIDMSessionPID(%q) should succeed", sessionID)
	}
	if pid != 1234 {
		t.Fatalf("pid = %d, want 1234", pid)
	}
	if _, ok := parseIDMSessionPID("session-1"); ok {
		t.Fatal("non-idm session id should not be parsed")
	}
}

func TestCleanupZombieIDMSessions(t *testing.T) {
	currentPID := os.Getpid()
	sessions := []gateway.SessionSummary{
		{ID: "idm-" + strconv.Itoa(currentPID) + "-1"},
		{ID: "idm-99999999-2"},
		{ID: "normal-session"},
	}

	var (
		deleted []string
		mu      sync.Mutex
	)

	client, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
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
					case protocol.MethodGatewayListSessions:
						writeRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionListSessions,
							Payload: map[string]any{
								"sessions": sessions,
							},
						})
					case protocol.MethodGatewayDeleteSession:
						var params protocol.DeleteSessionParams
						_ = json.Unmarshal(request.Params, &params)
						mu.Lock()
						deleted = append(deleted, params.SessionID)
						mu.Unlock()
						writeRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionDeleteSession,
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
	t.Cleanup(func() { _ = client.Close() })

	cleanupZombieIDMSessions(client, nil)

	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		mu.Lock()
		count := len(deleted)
		mu.Unlock()
		if count >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 {
		t.Fatalf("deleted sessions = %v, want exactly one stale idm session", deleted)
	}
	if deleted[0] != "idm-99999999-2" {
		t.Fatalf("deleted session = %q, want stale idm session", deleted[0])
	}
}

func writeRPCResultFrame(
	t *testing.T,
	encoder *json.Encoder,
	id json.RawMessage,
	frame gateway.MessageFrame,
) {
	t.Helper()
	rawResult, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("json.Marshal(frame) error = %v", err)
	}
	if err := encoder.Encode(protocol.JSONRPCResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      id,
		Result:  rawResult,
	}); err != nil {
		t.Fatalf("encoder.Encode(response) error = %v", err)
	}
}
