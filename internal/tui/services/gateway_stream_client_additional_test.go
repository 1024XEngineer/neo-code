package services

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/runtime/controlplane"
)

type streamInvalidJSONMarshaler struct {
	raw []byte
}

func (m streamInvalidJSONMarshaler) MarshalJSON() ([]byte, error) {
	return m.raw, nil
}

func TestDecodeRuntimeEventFromGatewayNotificationErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := decodeRuntimeEventFromGatewayNotification(gatewayRPCNotification{Method: protocol.MethodGatewayEvent}); err == nil {
		t.Fatalf("expected empty params error")
	}

	if _, err := decodeRuntimeEventFromGatewayNotification(gatewayRPCNotification{
		Method: protocol.MethodGatewayEvent,
		Params: json.RawMessage(`{"payload":{}}`),
	}); err == nil {
		t.Fatalf("expected missing envelope error")
	}

	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:   gateway.FrameTypeEvent,
		Action: gateway.FrameActionRun,
		Payload: map[string]any{
			"payload": map[string]any{},
		},
	})
	if _, err := decodeRuntimeEventFromGatewayNotification(notification); err == nil {
		t.Fatalf("expected missing runtime_event_type error")
	}
}

func TestDecodeRuntimeEventFromGatewayNotificationUsesCurrentTimeWhenTimestampMissing(t *testing.T) {
	t.Parallel()

	notification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:   gateway.FrameTypeEvent,
		Action: gateway.FrameActionRun,
		Payload: map[string]any{
			"runtime_event_type": string(agentruntime.EventError),
			"payload":            "boom",
		},
	})

	before := time.Now().UTC().Add(-time.Second)
	event, err := decodeRuntimeEventFromGatewayNotification(notification)
	if err != nil {
		t.Fatalf("decodeRuntimeEventFromGatewayNotification() error = %v", err)
	}
	if event.Timestamp.Before(before) {
		t.Fatalf("event timestamp should fallback to now, got %v", event.Timestamp)
	}
}

func TestExtractRuntimeEnvelopeFallbackMarshalling(t *testing.T) {
	t.Parallel()

	type payloadEnvelope struct {
		Payload map[string]any `json:"payload"`
	}
	envelope, ok := extractRuntimeEnvelope(payloadEnvelope{Payload: map[string]any{
		"RuntimeEventType": string(agentruntime.EventError),
		"payload":          "x",
	}})
	if !ok {
		t.Fatalf("expected envelope to be detected")
	}
	if got := streamReadMapString(envelope, "runtime_event_type"); got != string(agentruntime.EventError) {
		t.Fatalf("runtime_event_type = %q", got)
	}

	if _, ok := extractRuntimeEnvelope(nil); ok {
		t.Fatalf("nil payload should not decode")
	}
}

func TestRestoreRuntimePayloadCoversSpecializedTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventType agentruntime.EventType
		payload   any
		assertFn  func(t *testing.T, got any)
	}{
		{
			name:      "user message",
			eventType: agentruntime.EventUserMessage,
			payload:   map[string]any{"Role": string(providertypes.RoleAssistant)},
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				if _, ok := got.(providertypes.Message); !ok {
					t.Fatalf("payload type = %T", got)
				}
			},
		},
		{
			name:      "permission request",
			eventType: agentruntime.EventPermissionRequested,
			payload:   map[string]any{"RequestID": "req-1"},
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				if v, ok := got.(agentruntime.PermissionRequestPayload); !ok || v.RequestID != "req-1" {
					t.Fatalf("payload = %#v", got)
				}
			},
		},
		{
			name:      "stop reason",
			eventType: agentruntime.EventStopReasonDecided,
			payload:   map[string]any{"reason": "  max_rounds  "},
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				value, ok := got.(agentruntime.StopReasonDecidedPayload)
				if !ok {
					t.Fatalf("payload type = %T", got)
				}
				if value.Reason != controlplane.StopReason("max_rounds") {
					t.Fatalf("reason = %q", value.Reason)
				}
			},
		},
		{
			name:      "runtime usage payload",
			eventType: agentruntime.EventType(RuntimeEventUsage),
			payload:   map[string]any{"delta": map[string]any{"inputtokens": 1}},
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				if _, ok := got.(RuntimeUsagePayload); !ok {
					t.Fatalf("payload type = %T", got)
				}
			},
		},
		{
			name:      "string payload",
			eventType: agentruntime.EventToolChunk,
			payload:   42,
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				if got != "42" {
					t.Fatalf("payload = %#v", got)
				}
			},
		},
		{
			name:      "default passthrough",
			eventType: agentruntime.EventType("unknown"),
			payload:   map[string]any{"k": "v"},
			assertFn: func(t *testing.T, got any) {
				t.Helper()
				if !reflect.DeepEqual(got, map[string]any{"k": "v"}) {
					t.Fatalf("payload = %#v", got)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := restoreRuntimePayload(tc.eventType, tc.payload)
			if err != nil {
				t.Fatalf("restoreRuntimePayload() error = %v", err)
			}
			tc.assertFn(t, got)
		})
	}
}

func TestDecodeRuntimePayloadAndMapHelpers(t *testing.T) {
	t.Parallel()

	typed, err := decodeRuntimePayload[agentruntime.InputNormalizedPayload](agentruntime.InputNormalizedPayload{TextLength: 1})
	if err != nil || typed.TextLength != 1 {
		t.Fatalf("typed decode mismatch, got (%#v, %v)", typed, err)
	}

	ptrValue := &agentruntime.InputNormalizedPayload{ImageCount: 3}
	decodedPtr, err := decodeRuntimePayload[agentruntime.InputNormalizedPayload](ptrValue)
	if err != nil || decodedPtr.ImageCount != 3 {
		t.Fatalf("pointer decode mismatch, got (%#v, %v)", decodedPtr, err)
	}

	var nilPtr *agentruntime.InputNormalizedPayload
	if _, err := decodeRuntimePayload[agentruntime.InputNormalizedPayload](nilPtr); err == nil {
		t.Fatalf("expected nil pointer decode error")
	}
	if _, err := decodeRuntimePayload[agentruntime.InputNormalizedPayload](nil); err == nil {
		t.Fatalf("expected nil payload decode error")
	}

	m := map[string]any{
		"runtimeEventType": "agent_chunk",
		"turn":             json.Number("7"),
		"payloadVersion":   "5",
		"time_stamp":       "2026-04-20T12:00:00Z",
	}
	if value, ok := streamReadMapValue(m, "runtime_event_type"); !ok || value != "agent_chunk" {
		t.Fatalf("streamReadMapValue mismatch: (%v, %v)", value, ok)
	}
	if got := streamReadMapInt(m, "turn"); got != 7 {
		t.Fatalf("streamReadMapInt(turn) = %d", got)
	}
	if got := streamReadMapInt(m, "payload_version"); got != 5 {
		t.Fatalf("streamReadMapInt(payload_version) = %d", got)
	}
	if got := streamReadMapString(m, "runtime_event_type"); got != "agent_chunk" {
		t.Fatalf("streamReadMapString = %q", got)
	}
	if got := streamReadMapTime(m, "time_stamp"); got.IsZero() {
		t.Fatalf("streamReadMapTime should parse timestamp")
	}

	if normalizeMapLookupKey(" Runtime_Event-Type ") != "runtimeeventtype" {
		t.Fatalf("normalizeMapLookupKey mismatch")
	}
	if toSnakeCase("RuntimeEventType") != "runtime_event_type" {
		t.Fatalf("toSnakeCase mismatch")
	}
	if toLowerCamelCase(" RuntimeEventType ") != "runtimeEventType" {
		t.Fatalf("toLowerCamelCase mismatch")
	}
}

func TestGatewayStreamClientRunSkipsNonGatewayEventsAndStopsOnClose(t *testing.T) {
	t.Parallel()

	source := make(chan gatewayRPCNotification, 4)
	client := NewGatewayStreamClient(source)

	source <- gatewayRPCNotification{Method: "gateway.ping", Params: json.RawMessage(`{"foo":"bar"}`)}
	source <- buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:   gateway.FrameTypeEvent,
		Action: gateway.FrameActionRun,
		Payload: map[string]any{
			"runtime_event_type": string(agentruntime.EventAgentChunk),
			"payload":            "ok",
		},
	})

	select {
	case event := <-client.Events():
		if event.Type != agentruntime.EventAgentChunk {
			t.Fatalf("event.Type = %q", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for stream event")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() second call error = %v", err)
	}
}

func TestGatewayStreamClientRunStopsWhenSourceClosed(t *testing.T) {
	t.Parallel()

	source := make(chan gatewayRPCNotification)
	client := NewGatewayStreamClient(source)
	close(source)

	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatalf("events channel should be closed after source channel is closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for events channel close")
	}
}

func TestRestoreRuntimePayloadAdditionalBranches(t *testing.T) {
	t.Parallel()

	payloadCases := []struct {
		eventType agentruntime.EventType
		payload   any
	}{
		{eventType: agentruntime.EventAgentDone, payload: map[string]any{"Role": string(providertypes.RoleAssistant)}},
		{eventType: agentruntime.EventToolStart, payload: map[string]any{"Name": "bash"}},
		{eventType: agentruntime.EventPermissionResolved, payload: map[string]any{"RequestID": "req-1"}},
		{eventType: agentruntime.EventCompactApplied, payload: map[string]any{"Applied": true}},
		{eventType: agentruntime.EventCompactError, payload: map[string]any{"message": "boom"}},
		{eventType: agentruntime.EventPhaseChanged, payload: map[string]any{"from": "a", "to": "b"}},
		{eventType: agentruntime.EventInputNormalized, payload: map[string]any{"text_length": 3}},
		{eventType: agentruntime.EventAssetSaved, payload: map[string]any{"asset_id": "asset-1"}},
		{eventType: agentruntime.EventAssetSaveFailed, payload: map[string]any{"message": "x"}},
		{eventType: agentruntime.EventTodoUpdated, payload: map[string]any{"action": "replace"}},
		{eventType: agentruntime.EventTodoConflict, payload: map[string]any{"action": "conflict"}},
		{eventType: agentruntime.EventType(RuntimeEventRunContext), payload: map[string]any{"provider": "openai"}},
		{eventType: agentruntime.EventType(RuntimeEventToolStatus), payload: map[string]any{"status": "running"}},
	}

	for _, tc := range payloadCases {
		if _, err := restoreRuntimePayload(tc.eventType, tc.payload); err != nil {
			t.Fatalf("restoreRuntimePayload(%q) error = %v", tc.eventType, err)
		}
	}

	if _, err := restoreRuntimePayload(agentruntime.EventStopReasonDecided, map[string]any{"reason": func() {}}); err == nil {
		t.Fatalf("stop reason payload should return decode error for non-serializable field")
	}
}

func TestStreamHelperBranches(t *testing.T) {
	t.Parallel()

	if decodeStringPayload(nil) != "" {
		t.Fatalf("decodeStringPayload(nil) should return empty string")
	}
	if decodeStringPayload("x") != "x" {
		t.Fatalf("decodeStringPayload(string) mismatch")
	}

	if _, err := decodeRuntimePayload[agentruntime.PhaseChangedPayload](func() {}); err == nil {
		t.Fatalf("decodeRuntimePayload should fail on marshal error")
	}
	if _, err := decodeRuntimePayload[agentruntime.PhaseChangedPayload](map[string]any{"from": map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatalf("decodeRuntimePayload should fail on invalid nested payload")
	}

	if value, ok := streamReadMapValue(map[string]any{"RUNTIMEEVENTTYPE": "v"}, "runtime_event_type"); !ok || value != "v" {
		t.Fatalf("streamReadMapValue normalized scan mismatch")
	}
	if _, ok := streamReadMapValue(nil, "key"); ok {
		t.Fatalf("nil map lookup should fail")
	}
	if _, ok := streamReadMapValue(map[string]any{"k": 1}, " "); ok {
		t.Fatalf("blank key lookup should fail")
	}

	intCases := map[string]any{
		"i":      1,
		"i64":    int64(2),
		"i32":    int32(3),
		"f64":    float64(4),
		"f32":    float32(5),
		"num":    json.Number("6"),
		"badnum": json.Number("x"),
		"str":    "7",
		"badstr": "x",
	}
	if streamReadMapInt(intCases, "i") != 1 ||
		streamReadMapInt(intCases, "i64") != 2 ||
		streamReadMapInt(intCases, "i32") != 3 ||
		streamReadMapInt(intCases, "f64") != 4 ||
		streamReadMapInt(intCases, "f32") != 5 ||
		streamReadMapInt(intCases, "num") != 6 ||
		streamReadMapInt(intCases, "str") != 7 {
		t.Fatalf("streamReadMapInt numeric coercion mismatch")
	}
	if streamReadMapInt(intCases, "badnum") != 0 || streamReadMapInt(intCases, "badstr") != 0 || streamReadMapInt(intCases, "missing") != 0 {
		t.Fatalf("streamReadMapInt invalid values should return zero")
	}

	now := time.Now().UTC()
	timeCases := map[string]any{
		"as_time":  now,
		"as_text":  now.Format(time.RFC3339Nano),
		"invalid":  "not-time",
		"blanktxt": " ",
	}
	if !streamReadMapTime(timeCases, "as_time").Equal(now) {
		t.Fatalf("streamReadMapTime(time.Time) mismatch")
	}
	if streamReadMapTime(timeCases, "as_text").IsZero() {
		t.Fatalf("streamReadMapTime(string) should parse")
	}
	if !streamReadMapTime(timeCases, "invalid").IsZero() ||
		!streamReadMapTime(timeCases, "blanktxt").IsZero() ||
		!streamReadMapTime(timeCases, "missing").IsZero() {
		t.Fatalf("streamReadMapTime invalid values should return zero time")
	}

	if toSnakeCase("") != "" || toLowerCamelCase("") != "" {
		t.Fatalf("empty case conversion should return empty string")
	}
}

func TestGatewayStreamDecodeAndEnvelopeExtraBranches(t *testing.T) {
	t.Parallel()

	missingTypeNotification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:   gateway.FrameTypeEvent,
		Action: gateway.FrameActionRun,
		Payload: map[string]any{
			"runtime_event_type": " ",
		},
	})
	if _, err := decodeRuntimeEventFromGatewayNotification(missingTypeNotification); err == nil {
		t.Fatalf("expected missing runtime_event_type error")
	}

	invalidPayloadNotification := buildGatewayEventNotification(t, gateway.MessageFrame{
		Type:   gateway.FrameTypeEvent,
		Action: gateway.FrameActionRun,
		Payload: map[string]any{
			"runtime_event_type": string(agentruntime.EventToolResult),
			"payload":            "not-an-object",
		},
	})
	if _, err := decodeRuntimeEventFromGatewayNotification(invalidPayloadNotification); err == nil {
		t.Fatalf("expected restore payload decode error")
	}

	if _, ok := extractRuntimeEnvelope(streamInvalidJSONMarshaler{raw: []byte("{")}); ok {
		t.Fatalf("expected marshal error path to fail envelope extraction")
	}
	if _, ok := extractRuntimeEnvelope(streamInvalidJSONMarshaler{raw: []byte("[]")}); ok {
		t.Fatalf("expected unmarshal-to-map error path to fail envelope extraction")
	}
	if envelope, ok := extractRuntimeEnvelope(struct {
		RuntimeEventType string `json:"runtime_event_type"`
	}{RuntimeEventType: string(agentruntime.EventError)}); !ok || streamReadMapString(envelope, "runtime_event_type") == "" {
		t.Fatalf("expected runtime_event_type detection after marshal/unmarshal")
	}

	if got := streamReadMapString(map[string]any{"v": 123}, "v"); got != "123" {
		t.Fatalf("streamReadMapString default conversion mismatch: %q", got)
	}
	if got := streamReadMapInt(map[string]any{"v": true}, "v"); got != 0 {
		t.Fatalf("streamReadMapInt unsupported type should return 0, got %d", got)
	}
	if got := streamReadMapTime(map[string]any{"v": 1}, "v"); !got.IsZero() {
		t.Fatalf("streamReadMapTime unsupported type should return zero, got %v", got)
	}
}
