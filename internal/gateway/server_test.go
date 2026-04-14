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

	if err := encoder.Encode(MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionPing,
		RequestID: "req-1",
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var response MessageFrame
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionPing {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionPing)
	}
	if response.RequestID != "req-1" {
		t.Fatalf("response request_id = %q, want %q", response.RequestID, "req-1")
	}

	payloadMap, ok := response.Payload.(map[string]any)
	if !ok {
		t.Fatalf("response payload type = %T, want map[string]any", response.Payload)
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

	if err := encoder.Encode(MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRun,
		RequestID: "req-2",
		InputText: "hello",
	}); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	var response MessageFrame
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil {
		t.Fatal("response error is nil")
	}
	if response.Error.Code != ErrorCodeUnsupportedAction.String() {
		t.Fatalf("error code = %q, want %q", response.Error.Code, ErrorCodeUnsupportedAction.String())
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
		`{"type":"request","action":"ping","request_id":"req-oversize","input_text":"%s"}`+"\n",
		oversizedPayload,
	)

	writeDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(clientConn, requestFrame)
		writeDone <- err
	}()

	var response MessageFrame
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode oversized response: %v", err)
	}
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil {
		t.Fatal("response error is nil")
	}
	if response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("error code = %q, want %q", response.Error.Code, ErrorCodeInvalidFrame.String())
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
