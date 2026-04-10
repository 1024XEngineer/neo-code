package gateway

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageFrameJSONRoundTrip(t *testing.T) {
	original := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRun,
		RequestID: "req_001",
		RunID:     "run_123",
		SessionID: "sess_abc",
		InputText: "请分析这张图",
		InputParts: []InputPart{
			{
				Type: InputPartTypeText,
				Text: "请先读取图片中的文字",
			},
			{
				Type: InputPartTypeImage,
				Media: &Media{
					URI:      "file:///workspace/assets/screen.png",
					MimeType: "image/png",
					FileName: "screen.png",
				},
			},
		},
		Workdir: "/workspace/project",
		Error: &FrameError{
			Code:    ErrorCodeInvalidFrame.String(),
			Message: "invalid frame",
		},
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded MessageFrame
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Type != original.Type {
		t.Fatalf("type mismatch: got %q want %q", decoded.Type, original.Type)
	}
	if decoded.Action != original.Action {
		t.Fatalf("action mismatch: got %q want %q", decoded.Action, original.Action)
	}
	if decoded.RequestID != original.RequestID {
		t.Fatalf("request_id mismatch: got %q want %q", decoded.RequestID, original.RequestID)
	}
	if decoded.SessionID != original.SessionID {
		t.Fatalf("session_id mismatch: got %q want %q", decoded.SessionID, original.SessionID)
	}
	if len(decoded.InputParts) != 2 {
		t.Fatalf("input_parts mismatch: got %d want %d", len(decoded.InputParts), 2)
	}
	if decoded.InputParts[1].Media == nil || decoded.InputParts[1].Media.MimeType != "image/png" {
		t.Fatalf("media mime_type mismatch in image part")
	}
	if decoded.Error == nil || decoded.Error.Code != original.Error.Code {
		t.Fatalf("error code mismatch")
	}
}

func TestSessionMessageToolCallsRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := Session{
		ID:        "sess_1",
		Title:     "demo",
		CreatedAt: now,
		UpdatedAt: now,
		Messages: []SessionMessage{
			{
				Role:    "assistant",
				Content: "我准备调用工具",
				ToolCalls: []ToolCall{
					{
						ID:        "call_1",
						Name:      "filesystem_read",
						Arguments: `{"path":"README.md"}`,
					},
				},
			},
			{
				Role:       "tool",
				Content:    "tool result",
				ToolCallID: "call_1",
			},
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal session failed: %v", err)
	}

	var decoded Session
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal session failed: %v", err)
	}

	if len(decoded.Messages) != 2 {
		t.Fatalf("message count mismatch: got %d want %d", len(decoded.Messages), 2)
	}
	if len(decoded.Messages[0].ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls count mismatch: got %d want %d", len(decoded.Messages[0].ToolCalls), 1)
	}
	if decoded.Messages[0].ToolCalls[0].Name != "filesystem_read" {
		t.Fatalf("tool call name mismatch: got %q", decoded.Messages[0].ToolCalls[0].Name)
	}
	if decoded.Messages[0].ToolCalls[0].Arguments != `{"path":"README.md"}` {
		t.Fatalf("tool call arguments mismatch: got %q", decoded.Messages[0].ToolCalls[0].Arguments)
	}
}
