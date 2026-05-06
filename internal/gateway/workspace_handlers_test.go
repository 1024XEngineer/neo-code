package gateway

import (
	"context"
	"testing"

	"neo-code/internal/gateway/protocol"
)

func TestHandleWorkspaceDeleteFrameClearsActiveWorkspaceStateAndBindings(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	relay := NewStreamRelay(StreamRelayOptions{})
	connID := NewConnectionID()
	registerErr := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connID,
		Channel:      StreamChannelIPC,
		Context:      context.Background(),
		Cancel:       func() {},
		Write: func(message RelayMessage) error {
			return nil
		},
		Close: func() {},
	})
	if registerErr != nil {
		t.Fatalf("register connection: %v", registerErr)
	}
	t.Cleanup(func() { relay.dropConnection(connID) })

	if bindErr := relay.BindConnection(connID, StreamBinding{
		SessionID: "session-delete-check",
		Channel:   StreamChannelAll,
		Explicit:  true,
	}); bindErr != nil {
		t.Fatalf("bind connection: %v", bindErr)
	}

	wsState := NewConnectionWorkspaceState()
	wsState.SetWorkspaceHash(alpha.Hash)
	ctx := WithConnectionID(
		WithStreamRelay(
			WithConnectionWorkspaceState(context.Background(), wsState),
			relay,
		),
		connID,
	)

	response := handleWorkspaceDeleteFrame(ctx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWorkspaceDelete,
		RequestID: "workspace-delete-active",
		Payload: protocol.DeleteWorkspaceParams{
			WorkspaceHash: alpha.Hash,
		},
	}, mw)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}

	if got := wsState.GetWorkspaceHash(); got != "" {
		t.Fatalf("workspace hash should be cleared after deleting active workspace, got %q", got)
	}

	relay.mu.RLock()
	_, exists := relay.connectionBindings[NormalizeConnectionID(connID)]
	relay.mu.RUnlock()
	if exists {
		t.Fatalf("connection bindings should be cleared after deleting active workspace")
	}
}
