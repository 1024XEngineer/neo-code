package ptyproxy

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestIDMControllerAskDoesNotInjectDiagnosisSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	notifications := make(chan gatewayclient.Notification, 4)
	var (
		mu         sync.Mutex
		askCalls   []protocol.AskParams
		bindCalls  []protocol.BindStreamParams
		deleteCall []string
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
					case protocol.MethodGatewayBindStream:
						var params protocol.BindStreamParams
						_ = json.Unmarshal(request.Params, &params)
						mu.Lock()
						bindCalls = append(bindCalls, params)
						mu.Unlock()
						writeIDMRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionBindStream,
						})
					case protocol.MethodGatewayAsk:
						var params protocol.AskParams
						_ = json.Unmarshal(request.Params, &params)
						mu.Lock()
						askCalls = append(askCalls, params)
						mu.Unlock()
						writeIDMRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionAsk,
						})
						go sendIDMAskDoneNotification(t, notifications, params.SessionID)
					case protocol.MethodGatewayDeleteAskSession:
						var params protocol.DeleteAskSessionParams
						_ = json.Unmarshal(request.Params, &params)
						mu.Lock()
						deleteCall = append(deleteCall, strings.TrimSpace(params.SessionID))
						mu.Unlock()
						writeIDMRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionDeleteAskSession,
						})
					default:
						writeIDMRPCResultFrame(t, encoder, request.ID, gateway.MessageFrame{
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

	controller := newIDMController(idmControllerOptions{
		PTYWriter:          &bytes.Buffer{},
		Output:             &bytes.Buffer{},
		Stderr:             &bytes.Buffer{},
		RPCClient:          client,
		NotificationStream: notifications,
		AutoState:          &autoRuntimeState{},
		LogBuffer:          NewUTF8RingBuffer(DefaultRingBufferCapacity),
		DefaultCap:         DefaultRingBufferCapacity,
		Workdir:            "/tmp/project",
		ShellSessionID:     "shell-idm-test",
	})
	controller.mu.Lock()
	controller.active = true
	controller.mode = idmModeIdle
	controller.sessionID = "idm-test-1"
	controller.sessionReady = true
	controller.mu.Unlock()

	if err := controller.sendAIMessage("what is ls"); err != nil {
		t.Fatalf("sendAIMessage() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bindCalls) != 1 {
		t.Fatalf("bind stream calls = %d, want 1", len(bindCalls))
	}
	if len(askCalls) != 1 {
		t.Fatalf("ask calls = %d, want 1", len(askCalls))
	}
	if askCalls[0].SessionID != "idm-test-1" {
		t.Fatalf("ask session id = %q, want idm-test-1", askCalls[0].SessionID)
	}
	if askCalls[0].UserQuery != "what is ls" {
		t.Fatalf("ask query = %q, want %q", askCalls[0].UserQuery, "what is ls")
	}
	if len(askCalls[0].Skills) != 0 {
		t.Fatalf("idm @ai should not inject diagnosis skills, got %#v", askCalls[0].Skills)
	}
	if askCalls[0].Workdir != "/tmp/project" {
		t.Fatalf("ask workdir = %q, want /tmp/project", askCalls[0].Workdir)
	}
	if len(deleteCall) != 0 {
		t.Fatalf("sendAIMessage should not delete ask session, got %v", deleteCall)
	}
}

func TestExtractIDMAskTextPreservesDeltaWhitespace(t *testing.T) {
	payload := map[string]any{"delta": " have "}
	if got := extractIDMAskText(payload); got != " have " {
		t.Fatalf("delta text = %q, want preserved whitespace", got)
	}
}

func sendIDMAskDoneNotification(
	t *testing.T,
	notifications chan<- gatewayclient.Notification,
	sessionID string,
) {
	t.Helper()
	notifications <- gatewayclient.Notification{
		Method: protocol.MethodGatewayEvent,
		Params: mustMarshalJSONRawMessage(t, gateway.MessageFrame{
			Type:      gateway.FrameTypeEvent,
			SessionID: sessionID,
			RunID:     "run-idm-1",
			Payload: map[string]any{
				"event_type": string(gateway.RuntimeEventTypeAskDone),
				"payload": map[string]any{
					"full_response": "ls 是列出目录内容的命令。",
				},
			},
		}),
	}
}

func mustMarshalJSONRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func writeIDMRPCResultFrame(
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
