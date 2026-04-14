package gateway

import (
	"context"
	"testing"

	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
)

func TestDispatchRequestFramePing(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionPing,
		RequestID: "req-ping",
	}, nil)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionPing {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionPing)
	}
}

func TestDispatchRequestFrameWakeOpenURLSuccess(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "review",
			"params": map[string]string{
				"path": "README.md",
			},
		},
		RequestID: "req-wake",
	}, nil)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionWakeOpenURL {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionWakeOpenURL)
	}
}

func TestDispatchRequestFrameWakeOpenURLInvalidAction(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "open",
			"params": map[string]string{
				"path": "README.md",
			},
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeInvalidAction.String())
	}
}

func TestDispatchRequestFrameWakeOpenURLMissingPath(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "review",
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeMissingRequiredField.String())
	}
}

func TestDispatchRequestFrameUnsupportedAction(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionRun,
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeUnsupportedAction.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeUnsupportedAction.String())
	}
}

func TestDecodeWakeIntentAdditionalBranches(t *testing.T) {
	t.Run("nil payload", func(t *testing.T) {
		_, err := decodeWakeIntent(nil)
		if err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("pointer payload", func(t *testing.T) {
		intent, err := decodeWakeIntent(&protocol.WakeIntent{
			Action: "review",
			Params: map[string]string{"path": "README.md"},
		})
		if err != nil {
			t.Fatalf("decode wake intent: %v", err)
		}
		if intent.Action != "review" {
			t.Fatalf("action = %q, want %q", intent.Action, "review")
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeWakeIntent(map[string]any{"bad": make(chan int)})
		if err == nil {
			t.Fatal("expected marshal error")
		}
	})
}

func TestToFrameError(t *testing.T) {
	stable := toFrameError(&handlers.WakeError{
		Code:    ErrorCodeInvalidAction.String(),
		Message: "invalid",
	})
	if stable.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("stable code = %q, want %q", stable.Code, ErrorCodeInvalidAction.String())
	}

	fallback := toFrameError(&handlers.WakeError{
		Code:    "custom",
		Message: "custom error",
	})
	if fallback.Code != ErrorCodeInternalError.String() {
		t.Fatalf("fallback code = %q, want %q", fallback.Code, ErrorCodeInternalError.String())
	}
}
