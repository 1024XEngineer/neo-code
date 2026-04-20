package services

import (
	"encoding/json"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/tools"
)

func TestDecodeRuntimeEventFromGatewayNotificationRestoresStringPayload(t *testing.T) {
	timestamp := time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC)
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-1",
		RunID:     "run-1",
		Payload: map[string]any{
			"runtime_event_type": string(agentruntime.EventAgentChunk),
			"turn":               2,
			"phase":              "thinking",
			"timestamp":          timestamp.Format(time.RFC3339Nano),
			"payload_version":    1,
			"payload":            "hello",
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	if event.Type != agentruntime.EventAgentChunk {
		t.Fatalf("event.Type = %q, want %q", event.Type, agentruntime.EventAgentChunk)
	}
	if event.SessionID != "session-1" || event.RunID != "run-1" {
		t.Fatalf("unexpected ids: %#v", event)
	}
	if event.Turn != 2 || event.Phase != "thinking" {
		t.Fatalf("unexpected turn/phase: %#v", event)
	}
	if !event.Timestamp.Equal(timestamp) {
		t.Fatalf("event.Timestamp = %v, want %v", event.Timestamp, timestamp)
	}
	payload, ok := event.Payload.(string)
	if !ok || payload != "hello" {
		t.Fatalf("event.Payload = %#v, want %q", event.Payload, "hello")
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationRestoresToolResultPayload(t *testing.T) {
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-2",
		RunID:     "run-2",
		Payload: map[string]any{
			"runtime_event_type": string(agentruntime.EventToolResult),
			"payload": map[string]any{
				"ToolCallID": "call-1",
				"Name":       "bash",
				"Content":    "ok",
				"IsError":    false,
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	toolResult, ok := event.Payload.(tools.ToolResult)
	if !ok {
		t.Fatalf("event.Payload type = %T, want tools.ToolResult", event.Payload)
	}
	if toolResult.ToolCallID != "call-1" || toolResult.Name != "bash" || toolResult.Content != "ok" || toolResult.IsError {
		t.Fatalf("unexpected tool result payload: %#v", toolResult)
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationSupportsNestedEnvelope(t *testing.T) {
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-3",
		RunID:     "run-3",
		Payload: map[string]any{
			"type": "run_progress",
			"payload": map[string]any{
				"runtime_event_type": string(agentruntime.EventError),
				"payload":            "boom",
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	if event.Type != agentruntime.EventError {
		t.Fatalf("event.Type = %q, want %q", event.Type, agentruntime.EventError)
	}
	if payload, ok := event.Payload.(string); !ok || payload != "boom" {
		t.Fatalf("event.Payload = %#v, want %q", event.Payload, "boom")
	}
}

func TestGatewayStreamClientEmitsDecodeErrorAsRuntimeErrorEvent(t *testing.T) {
	source := make(chan gatewayRPCNotification, 1)
	client := NewGatewayStreamClient(source)
	t.Cleanup(func() { _ = client.Close() })

	source <- gatewayRPCNotification{
		Method: protocol.MethodGatewayEvent,
		Params: json.RawMessage(`{"bad":`),
	}

	select {
	case event := <-client.Events():
		if event.Type != agentruntime.EventError {
			t.Fatalf("event.Type = %q, want %q", event.Type, agentruntime.EventError)
		}
		payload, ok := event.Payload.(string)
		if !ok || payload == "" {
			t.Fatalf("event.Payload = %#v, want non-empty string", event.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for decode error event")
	}
}

func buildGatewayEventNotification(t *testing.T, frame gateway.MessageFrame) gatewayRPCNotification {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return gatewayRPCNotification{
		Method: protocol.MethodGatewayEvent,
		Params: raw,
	}
}
