package services

import (
	"encoding/json"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
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
			"runtime_event_type": string(EventAgentChunk),
			"turn":               2,
			"phase":              "thinking",
			"timestamp":          timestamp.Format(time.RFC3339Nano),
			"payload_version":    runtimeEventPayloadVersion,
			"payload":            "hello",
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	if event.Type != EventAgentChunk {
		t.Fatalf("event.Type = %q, want %q", event.Type, EventAgentChunk)
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
			"runtime_event_type": string(EventToolResult),
			"payload_version":    runtimeEventPayloadVersion,
			"payload": map[string]any{
				"ToolCallID": "call-1",
				"Name":       "bash",
				"Content":    "ok",
				"IsError":    false,
				"ErrorClass": "hook_blocked",
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
	if toolResult.ErrorClass != "hook_blocked" {
		t.Fatalf("toolResult.ErrorClass = %q, want %q", toolResult.ErrorClass, "hook_blocked")
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationRestoresHookBlockedPayload(t *testing.T) {
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-hook",
		RunID:     "run-hook",
		Payload: map[string]any{
			"runtime_event_type": string(EventHookBlocked),
			"payload_version":    runtimeEventPayloadVersion,
			"payload": map[string]any{
				"hook_id":      "block-before-tool",
				"point":        "before_tool_call",
				"tool_call_id": "call-2",
				"tool_name":    "bash",
				"reason":       "blocked by policy",
				"enforced":     true,
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	payload, ok := event.Payload.(HookBlockedPayload)
	if !ok {
		t.Fatalf("event.Payload type = %T, want HookBlockedPayload", event.Payload)
	}
	if payload.HookID != "block-before-tool" || payload.Point != "before_tool_call" || payload.ToolName != "bash" || !payload.Enforced {
		t.Fatalf("unexpected hook blocked payload: %#v", payload)
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationRestoresHookLifecyclePayload(t *testing.T) {
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-hook",
		RunID:     "run-hook",
		Payload: map[string]any{
			"runtime_event_type": string(EventHookStarted),
			"payload_version":    runtimeEventPayloadVersion,
			"payload": map[string]any{
				"hook_id":     "observe-after-tool",
				"point":       "after_tool_result",
				"status":      "pass",
				"duration_ms": 9,
				"scope":       "internal",
				"kind":        "function",
				"mode":        "sync",
				"started_at":  "2026-04-20T10:30:00Z",
				"error":       "",
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	payload, ok := event.Payload.(HookEventPayload)
	if !ok {
		t.Fatalf("event.Payload type = %T, want HookEventPayload", event.Payload)
	}
	if payload.HookID != "observe-after-tool" || payload.Point != "after_tool_result" || payload.Status != "pass" || payload.DurationMS != 9 {
		t.Fatalf("unexpected hook lifecycle payload: %#v", payload)
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationRestoresHookEventPayloadMessage(t *testing.T) {
	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: "session-hook-msg",
		RunID:     "run-hook-msg",
		Payload: map[string]any{
			"runtime_event_type": string(EventHookFinished),
			"payload_version":    runtimeEventPayloadVersion,
			"payload": map[string]any{
				"hook_id":     "warn-before-tool",
				"point":       "before_tool_call",
				"scope":       "user",
				"kind":        "function",
				"mode":        "sync",
				"status":      "pass",
				"message":     "tool call warning",
				"duration_ms": 1,
				"started_at":  time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	payload, ok := event.Payload.(HookEventPayload)
	if !ok {
		t.Fatalf("event.Payload type = %T, want HookEventPayload", event.Payload)
	}
	if payload.Scope != "user" || payload.Message != "tool call warning" {
		t.Fatalf("unexpected hook event payload: %#v", payload)
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
				"runtime_event_type": string(EventError),
				"payload_version":    runtimeEventPayloadVersion,
				"payload":            "boom",
			},
		},
	})

	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	if event.Type != EventError {
		t.Fatalf("event.Type = %q, want %q", event.Type, EventError)
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
		if event.Type != EventError {
			t.Fatalf("event.Type = %q, want %q", event.Type, EventError)
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
