package types

import (
	"encoding/json"
	"testing"
)

// --- Role 常量 ---

func TestRoleConstants(t *testing.T) {
	tests := []struct {
		name   string
		got    string
		expect string
	}{
		{"system", RoleSystem, "system"},
		{"user", RoleUser, "user"},
		{"assistant", RoleAssistant, "assistant"},
		{"tool", RoleTool, "tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expect {
				t.Fatalf("expected %q, got %q", tt.expect, tt.got)
			}
		})
	}
}

// --- StreamEventType 常量 ---

func TestStreamEventConstants(t *testing.T) {
	tests := []struct {
		name   string
		got    StreamEventType
		expect string
	}{
		{"text_delta", StreamEventTextDelta, "text_delta"},
		{"tool_call_start", StreamEventToolCallStart, "tool_call_start"},
		{"tool_call_delta", StreamEventToolCallDelta, "tool_call_delta"},
		{"message_done", StreamEventMessageDone, "message_done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.expect {
				t.Fatalf("expected %q, got %q", tt.expect, string(tt.got))
			}
		})
	}
}

// --- NewTextDeltaStreamEvent ---

func TestNewTextDeltaStreamEvent(t *testing.T) {
	t.Parallel()

	event := NewTextDeltaStreamEvent("hello")
	if event.Type != StreamEventTextDelta {
		t.Fatalf("expected type %q, got %q", StreamEventTextDelta, event.Type)
	}

	payload, err := event.TextDeltaValue()
	if err != nil {
		t.Fatalf("TextDeltaValue() error = %v", err)
	}
	if payload.Text != "hello" {
		t.Fatalf("expected text %q, got %q", "hello", payload.Text)
	}
}

// --- NewToolCallStartStreamEvent ---

func TestNewToolCallStartStreamEvent(t *testing.T) {
	t.Parallel()

	event := NewToolCallStartStreamEvent(3, "call_1", "edit_file")
	if event.Type != StreamEventToolCallStart {
		t.Fatalf("expected type %q, got %q", StreamEventToolCallStart, event.Type)
	}

	payload, err := event.ToolCallStartValue()
	if err != nil {
		t.Fatalf("ToolCallStartValue() error = %v", err)
	}
	if payload.Index != 3 || payload.ID != "call_1" || payload.Name != "edit_file" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

// --- NewToolCallDeltaStreamEvent ---

func TestNewToolCallDeltaStreamEvent(t *testing.T) {
	t.Parallel()

	event := NewToolCallDeltaStreamEvent(1, "call_2", `{"path":"main.go"}`)
	if event.Type != StreamEventToolCallDelta {
		t.Fatalf("expected type %q, got %q", StreamEventToolCallDelta, event.Type)
	}

	payload, err := event.ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("ToolCallDeltaValue() error = %v", err)
	}
	if payload.Index != 1 || payload.ID != "call_2" || payload.ArgumentsDelta != `{"path":"main.go"}` {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

// --- NewMessageDoneStreamEvent ---

func TestNewMessageDoneStreamEvent(t *testing.T) {
	t.Parallel()

	t.Run("with usage", func(t *testing.T) {
		usage := &Usage{TotalTokens: 42}
		event := NewMessageDoneStreamEvent("stop", usage)

		if event.Type != StreamEventMessageDone {
			t.Fatalf("expected type %q, got %q", StreamEventMessageDone, event.Type)
		}

		payload, err := event.MessageDoneValue()
		if err != nil {
			t.Fatalf("MessageDoneValue() error = %v", err)
		}
		if payload.FinishReason != "stop" {
			t.Fatalf("expected finish reason %q, got %q", "stop", payload.FinishReason)
		}
		if payload.Usage == nil || payload.Usage.TotalTokens != 42 {
			t.Fatalf("unexpected usage: %+v", payload.Usage)
		}
	})

	t.Run("nil usage", func(t *testing.T) {
		event := NewMessageDoneStreamEvent("tool_calls", nil)

		payload, err := event.MessageDoneValue()
		if err != nil {
			t.Fatalf("MessageDoneValue() error = %v", err)
		}
		if payload.FinishReason != "tool_calls" {
			t.Fatalf("expected finish reason %q, got %q", "tool_calls", payload.FinishReason)
		}
		if payload.Usage != nil {
			t.Fatal("expected nil usage")
		}
	})

	t.Run("empty finish reason", func(t *testing.T) {
		event := NewMessageDoneStreamEvent("", nil)

		payload, err := event.MessageDoneValue()
		if err != nil {
			t.Fatalf("MessageDoneValue() error = %v", err)
		}
		if payload.FinishReason != "" {
			t.Fatalf("expected empty finish reason, got %q", payload.FinishReason)
		}
	})
}

// --- 结构体字段覆盖验证 ---

func TestMessageStructFields(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:         RoleUser,
		Parts:        []ContentPart{NewTextPart("hello")},
		ToolCalls:    []ToolCall{{ID: "t1"}},
		ToolCallID:   "tc_1",
		IsError:      true,
		ToolMetadata: map[string]string{"path": "main.go"},
	}
	if msg.Role != RoleUser || renderPartsForTest(msg.Parts) != "hello" || len(msg.ToolCalls) != 1 ||
		msg.ToolCallID != "tc_1" || !msg.IsError || msg.ToolMetadata["path"] != "main.go" {
		t.Fatalf("message fields not as expected: %+v", msg)
	}
}

func TestToolCallStructFields(t *testing.T) {
	t.Parallel()

	tc := ToolCall{ID: "c1", Name: "fn", Arguments: "{}"}
	if tc.ID != "c1" || tc.Name != "fn" || tc.Arguments != "{}" {
		t.Fatalf("tool call fields not as expected: %+v", tc)
	}
}

func TestToolSpecStructFields(t *testing.T) {
	t.Parallel()

	spec := ToolSpec{Name: "read", Description: "read file", Schema: map[string]any{"type": "object"}}
	if spec.Name != "read" || spec.Description != "read file" || spec.Schema == nil {
		t.Fatalf("tool spec fields not as expected: %+v", spec)
	}
}

func TestGenerateRequestStructFields(t *testing.T) {
	t.Parallel()

	req := GenerateRequest{
		Model:        "gpt-4",
		SystemPrompt: "you are helpful",
		Messages:     []Message{{Role: RoleUser}},
		Tools:        []ToolSpec{{Name: "bash"}},
	}
	if req.Model != "gpt-4" || req.SystemPrompt != "you are helpful" ||
		len(req.Messages) != 1 || len(req.Tools) != 1 {
		t.Fatalf("chat request fields not as expected: %+v", req)
	}
}

func TestUsageStructFields(t *testing.T) {
	t.Parallel()

	usage := Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	if usage.InputTokens != 10 || usage.OutputTokens != 20 || usage.TotalTokens != 30 {
		t.Fatalf("usage fields not as expected: %+v", usage)
	}
}

func TestStreamEventStructFields(t *testing.T) {
	t.Parallel()

	event := StreamEvent{
		Type:      StreamEventTextDelta,
		TextDelta: &TextDeltaPayload{Text: "hi"},
	}
	if event.Type != StreamEventTextDelta {
		t.Fatalf("event type not as expected: %s", event.Type)
	}
	if event.TextDelta == nil || event.TextDelta.Text != "hi" {
		t.Fatalf("event text_delta not as expected: %+v", event.TextDelta)
	}
}

func TestStreamEventJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := NewToolCallDeltaStreamEvent(2, "call-7", `{"path":"main.go"}`)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	payload, err := decoded.ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("ToolCallDeltaValue() error = %v", err)
	}
	if payload.Index != 2 || payload.ID != "call-7" || payload.ArgumentsDelta != `{"path":"main.go"}` {
		t.Fatalf("unexpected round-trip payload: %+v", payload)
	}
}

func TestStreamEventValueAccessorsRejectMissingPayload(t *testing.T) {
	t.Parallel()

	event := StreamEvent{Type: StreamEventTextDelta}
	if _, err := event.TextDeltaValue(); err == nil {
		t.Fatal("expected TextDeltaValue() to reject missing payload")
	}
}

func TestStreamEventValueAccessorsRejectWrongType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "text delta accessor",
			run: func() error {
				_, err := NewToolCallStartStreamEvent(0, "call-1", "edit").TextDeltaValue()
				return err
			},
		},
		{
			name: "tool call start accessor",
			run: func() error {
				_, err := NewTextDeltaStreamEvent("hello").ToolCallStartValue()
				return err
			},
		},
		{
			name: "tool call delta accessor",
			run: func() error {
				_, err := NewMessageDoneStreamEvent("stop", nil).ToolCallDeltaValue()
				return err
			},
		},
		{
			name: "message done accessor",
			run: func() error {
				_, err := NewToolCallDeltaStreamEvent(1, "call-2", "{}").MessageDoneValue()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected wrong-type accessor to return error")
			}
		})
	}
}

func TestStreamEventValueAccessorsRejectMissingPayloadForAllTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "tool call start payload missing",
			run: func() error {
				_, err := (StreamEvent{Type: StreamEventToolCallStart}).ToolCallStartValue()
				return err
			},
		},
		{
			name: "tool call delta payload missing",
			run: func() error {
				_, err := (StreamEvent{Type: StreamEventToolCallDelta}).ToolCallDeltaValue()
				return err
			},
		},
		{
			name: "message done payload missing",
			run: func() error {
				_, err := (StreamEvent{Type: StreamEventMessageDone}).MessageDoneValue()
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("expected missing payload to return error")
			}
		})
	}
}
