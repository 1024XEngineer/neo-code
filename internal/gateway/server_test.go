package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

func TestServerHandleConnectionPing(t *testing.T) {
	t.Parallel()

	server := &Server{logger: log.New(io.Discard, "", 0)}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	encoder := json.NewEncoder(clientConn)
	decoder := json.NewDecoder(clientConn)

	if err := encoder.Encode(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-1"`),
		Method:  protocol.MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var response protocol.JSONRPCResponse
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", response.Error)
	}
	resultFrame, err := decodeJSONRPCResultFrame(response)
	if err != nil {
		t.Fatalf("decode result frame: %v", err)
	}

	if resultFrame.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", resultFrame.Type, FrameTypeAck)
	}
	if resultFrame.Action != FrameActionPing {
		t.Fatalf("response action = %q, want %q", resultFrame.Action, FrameActionPing)
	}
	if resultFrame.RequestID != "req-1" {
		t.Fatalf("response request_id = %q, want %q", resultFrame.RequestID, "req-1")
	}

	payloadMap, ok := resultFrame.Payload.(map[string]any)
	if !ok {
		t.Fatalf("response payload type = %T, want map[string]any", resultFrame.Payload)
	}
	if got, _ := payloadMap["message"].(string); got != "pong" {
		t.Fatalf("response payload message = %q, want %q", got, "pong")
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

func TestServerHandleConnectionUnsupportedAction(t *testing.T) {
	t.Parallel()

	server := &Server{logger: log.New(io.Discard, "", 0)}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	encoder := json.NewEncoder(clientConn)
	decoder := json.NewDecoder(clientConn)

	if err := encoder.Encode(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-2"`),
		Method:  "gateway.run",
		Params:  json.RawMessage(`{"input_text":"hello"}`),
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var response protocol.JSONRPCResponse
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error == nil {
		t.Fatal("response rpc error is nil")
	}
	if response.Error.Code != protocol.JSONRPCCodeMethodNotFound {
		t.Fatalf("rpc error code = %d, want %d", response.Error.Code, protocol.JSONRPCCodeMethodNotFound)
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error); gatewayCode != ErrorCodeUnsupportedAction.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeUnsupportedAction.String())
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

func TestServerHandleConnectionRejectsOversizedFrame(t *testing.T) {
	t.Parallel()

	server := &Server{logger: log.New(io.Discard, "", 0)}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	decoder := json.NewDecoder(clientConn)
	oversizedPayload := strings.Repeat("a", int(MaxFrameSize)+128)
	requestFrame := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":"req-oversize","method":"gateway.ping","params":{"input_text":"%s"}}`+"\n",
		oversizedPayload,
	)

	writeDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(clientConn, requestFrame)
		writeDone <- err
	}()

	var response protocol.JSONRPCResponse
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode oversized response: %v", err)
	}
	if response.Error == nil {
		t.Fatal("response rpc error is nil")
	}
	if response.Error.Code != protocol.JSONRPCCodeInvalidRequest {
		t.Fatalf("rpc error code = %d, want %d", response.Error.Code, protocol.JSONRPCCodeInvalidRequest)
	}
	if gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error); gatewayCode != ErrorCodeInvalidFrame.String() {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, ErrorCodeInvalidFrame.String())
	}
	if !strings.Contains(response.Error.Message, "frame exceeds max size") {
		t.Fatalf("error message = %q, want contains %q", response.Error.Message, "frame exceeds max size")
	}

	select {
	case <-writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("write oversized frame timed out")
	}

	readDone := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := clientConn.Read(buf[:])
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && strings.Contains(err.Error(), "closed pipe") {
			break
		}
		t.Fatalf("expected connection close after oversized frame, got %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("connection was not closed after oversized frame")
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

func TestServerHandleConnectionRelaysRuntimeEventAfterBindStream(t *testing.T) {
	t.Parallel()

	eventCh := make(chan RuntimeEvent, 1)
	relay := NewStreamRelay(StreamRelayOptions{
		Logger: log.New(io.Discard, "", 0),
	})
	server := &Server{
		logger: log.New(io.Discard, "", 0),
		relay:  relay,
	}
	relay.Start(context.Background(), &runtimePortEventStub{events: eventCh})

	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	encoder := json.NewEncoder(clientConn)
	decoder := json.NewDecoder(clientConn)
	if err := encoder.Encode(protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"bind-1"`),
		Method:  protocol.MethodGatewayBindStream,
		Params: json.RawMessage(`{
			"session_id":"session-1",
			"run_id":"run-1",
			"channel":"ipc"
		}`),
	}); err != nil {
		t.Fatalf("encode bind request: %v", err)
	}

	var bindResponse protocol.JSONRPCResponse
	if err := decoder.Decode(&bindResponse); err != nil {
		t.Fatalf("decode bind response: %v", err)
	}
	if bindResponse.Error != nil {
		t.Fatalf("unexpected bind rpc error: %+v", bindResponse.Error)
	}

	eventCh <- RuntimeEvent{
		Type:      RuntimeEventTypeRunProgress,
		SessionID: "session-1",
		RunID:     "run-1",
		Payload: map[string]string{
			"chunk": "hello",
		},
	}

	type notificationPayload struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	var notification notificationPayload
	if err := decoder.Decode(&notification); err != nil {
		t.Fatalf("decode notification: %v", err)
	}
	if notification.Method != protocol.MethodGatewayEvent {
		t.Fatalf("notification method = %q, want %q", notification.Method, protocol.MethodGatewayEvent)
	}

	var eventFrame MessageFrame
	if err := json.Unmarshal(notification.Params, &eventFrame); err != nil {
		t.Fatalf("decode notification params frame: %v", err)
	}
	if eventFrame.SessionID != "session-1" || eventFrame.RunID != "run-1" {
		t.Fatalf("event frame session/run mismatch: %#v", eventFrame)
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

type runtimePortEventStub struct {
	events <-chan RuntimeEvent
}

func (s *runtimePortEventStub) Run(_ context.Context, _ RunInput) error {
	return nil
}

func (s *runtimePortEventStub) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (s *runtimePortEventStub) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (s *runtimePortEventStub) CancelActiveRun() bool {
	return false
}

func (s *runtimePortEventStub) Events() <-chan RuntimeEvent {
	return s.events
}

func (s *runtimePortEventStub) ListSessions(_ context.Context) ([]SessionSummary, error) {
	return nil, nil
}

func (s *runtimePortEventStub) LoadSession(_ context.Context, _ string) (Session, error) {
	return Session{}, nil
}

func decodeJSONRPCResultFrame(response protocol.JSONRPCResponse) (MessageFrame, error) {
	if response.Result == nil {
		return MessageFrame{}, errors.New("rpc result is nil")
	}
	var frame MessageFrame
	if err := json.Unmarshal(response.Result, &frame); err != nil {
		return MessageFrame{}, err
	}
	return frame, nil
}
