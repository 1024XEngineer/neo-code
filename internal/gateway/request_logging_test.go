package gateway

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

func TestEmitRequestLogAuthStateAndSourceFallback(t *testing.T) {
	t.Run("authenticated state", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := log.New(buffer, "", 0)
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("local_admin")
		ctx := WithConnectionAuthState(context.Background(), authState)
		ctx = WithConnectionID(ctx, ConnectionID("conn-1"))

		emitRequestLog(ctx, logger, RequestLogEntry{
			RequestID: " req-1 ",
			SessionID: " session-1 ",
			Method:    " gateway.run ",
			Status:    "ok",
		})
		output := buffer.String()
		if !strings.Contains(output, `"source":"unknown"`) {
			t.Fatalf("output = %q, want unknown source", output)
		}
		if !strings.Contains(output, `"connection_id":"conn-1"`) {
			t.Fatalf("output = %q, want connection_id", output)
		}
		if !strings.Contains(output, `"auth_state":"authenticated"`) {
			t.Fatalf("output = %q, want authenticated state", output)
		}
	})

	t.Run("required auth state", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := log.New(buffer, "", 0)
		ctx := WithTokenAuthenticator(context.Background(), staticTokenAuthenticator{token: "token-1"})

		emitRequestLog(ctx, logger, RequestLogEntry{
			RequestID: "req-2",
			Method:    "gateway.run",
			Source:    string(RequestSourceHTTP),
			Status:    "error",
		})
		if !strings.Contains(buffer.String(), `"auth_state":"required"`) {
			t.Fatalf("output = %q, want required auth state", buffer.String())
		}
	})

	t.Run("disabled auth state", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := log.New(buffer, "", 0)
		emitRequestLog(context.Background(), logger, RequestLogEntry{
			RequestID: "req-3",
			Method:    "gateway.run",
			Source:    string(RequestSourceIPC),
			Status:    "ok",
		})
		if !strings.Contains(buffer.String(), `"auth_state":"disabled"`) {
			t.Fatalf("output = %q, want disabled auth state", buffer.String())
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		emitRequestLog(context.Background(), nil, RequestLogEntry{
			RequestID: "req-noop",
		})
	})
}

func TestEmitRequestLogMutesGatewayPing(t *testing.T) {
	buffer := &bytes.Buffer{}
	logger := log.New(buffer, "", 0)

	emitRequestLog(context.Background(), logger, RequestLogEntry{
		RequestID: "req-ping",
		Method:    protocol.MethodGatewayPing,
		Source:    string(RequestSourceIPC),
		Status:    "ok",
	})
	if buffer.Len() != 0 {
		t.Fatalf("gateway.ping log should be muted, got %q", buffer.String())
	}
}

func TestRequestLatencyMS(t *testing.T) {
	if requestLatencyMS(time.Time{}) != 0 {
		t.Fatal("zero start time should return 0 latency")
	}
	if requestStartTime().IsZero() {
		t.Fatal("requestStartTime should not return zero time")
	}
}
