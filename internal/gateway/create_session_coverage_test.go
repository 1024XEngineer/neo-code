package gateway

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/gateway/protocol"
)

func TestHandleCreateSessionFrameBranches(t *testing.T) {
	t.Run("creates session with explicit payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)

		var captured CreateSessionInput
		runtimePort := &bootstrapRuntimeStub{
			createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
				captured = input
				return "created-1", nil
			},
		}

		frame := handleCreateSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateSession,
			RequestID: "req-1",
			Payload:   protocol.CreateSessionParams{SessionID: "  s-1  "},
		}, runtimePort)

		if frame.Type != FrameTypeAck || frame.SessionID != "created-1" {
			t.Fatalf("frame = %#v, want ack with created session", frame)
		}
		if captured.SubjectID != "subject-1" || captured.SessionID != "s-1" {
			t.Fatalf("captured input = %#v", captured)
		}
	})

	t.Run("falls back to frame session id", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-2")
		ctx := WithConnectionAuthState(context.Background(), authState)

		runtimePort := &bootstrapRuntimeStub{
			createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
				if input.SessionID != "frame-session" {
					t.Fatalf("input.SessionID = %q, want frame-session", input.SessionID)
				}
				return "frame-session", nil
			},
		}

		frame := handleCreateSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateSession,
			RequestID: "req-2",
			SessionID: " frame-session ",
		}, runtimePort)

		if frame.Type != FrameTypeAck || frame.SessionID != "frame-session" {
			t.Fatalf("frame = %#v, want ack for frame session", frame)
		}
	})

	t.Run("maps runtime failure", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-3")
		ctx := WithConnectionAuthState(context.Background(), authState)

		frame := handleCreateSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateSession,
			RequestID: "req-3",
		}, &bootstrapRuntimeStub{
			createSessionFn: func(context.Context, CreateSessionInput) (string, error) {
				return "", errors.New("boom")
			},
		})

		if frame.Type != FrameTypeError || frame.Error == nil {
			t.Fatalf("frame = %#v, want error frame", frame)
		}
	})
}

func TestDecodeCreateSessionPayloadBranches(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		wantID  string
		wantErr bool
	}{
		{name: "nil", payload: nil, wantID: ""},
		{name: "value params", payload: protocol.CreateSessionParams{SessionID: " s-1 "}, wantID: "s-1"},
		{name: "pointer params", payload: &protocol.CreateSessionParams{SessionID: " s-2 "}, wantID: "s-2"},
		{name: "nil pointer", payload: (*protocol.CreateSessionParams)(nil), wantID: ""},
		{name: "map payload", payload: map[string]any{"session_id": " s-3 "}, wantID: "s-3"},
		{name: "marshal error", payload: func() {}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, frameErr := decodeCreateSessionPayload(tc.payload)
			if tc.wantErr {
				if frameErr == nil {
					t.Fatal("expected frame error")
				}
				return
			}
			if frameErr != nil {
				t.Fatalf("unexpected frame error: %#v", frameErr)
			}
			if got.SessionID != tc.wantID {
				t.Fatalf("SessionID = %q, want %q", got.SessionID, tc.wantID)
			}
		})
	}
}

func TestExtractSessionIDFromCreateSessionPayload(t *testing.T) {
	if got := extractSessionIDFromPayload(protocol.CreateSessionParams{SessionID: " s-1 "}); got != "s-1" {
		t.Fatalf("value params = %q, want s-1", got)
	}
	if got := extractSessionIDFromPayload(&protocol.CreateSessionParams{SessionID: " s-2 "}); got != "s-2" {
		t.Fatalf("pointer params = %q, want s-2", got)
	}
	if got := extractSessionIDFromPayload((*protocol.CreateSessionParams)(nil)); got != "" {
		t.Fatalf("nil pointer params = %q, want empty", got)
	}
}
