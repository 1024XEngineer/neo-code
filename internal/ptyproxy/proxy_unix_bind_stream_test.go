//go:build !windows

package ptyproxy

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestBindShellRoleStreamWithCallerFallbackOnInvalidParams(t *testing.T) {
	calls := make([]protocol.BindStreamParams, 0, 2)
	err := bindShellRoleStreamWithCaller(context.Background(), "shell-session-1", true, func(
		_ context.Context,
		params protocol.BindStreamParams,
		ack *gateway.MessageFrame,
	) error {
		calls = append(calls, params)
		if len(calls) == 1 {
			return &gatewayclient.GatewayRPCError{
				Method:      protocol.MethodGatewayBindStream,
				Code:        protocol.JSONRPCCodeInvalidParams,
				GatewayCode: protocol.GatewayCodeInvalidAction,
				Message:     "invalid params for gateway.bindStream",
			}
		}
		*ack = gateway.MessageFrame{Type: gateway.FrameTypeAck, Action: gateway.FrameActionBindStream}
		return nil
	})
	if err != nil {
		t.Fatalf("bindShellRoleStreamWithCaller() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("bind call count = %d, want 2", len(calls))
	}
	if calls[0].State == nil || calls[0].State["auto_enabled"] != true {
		t.Fatalf("first bind state = %#v, want auto_enabled=true", calls[0].State)
	}
	if len(calls[1].State) != 0 {
		t.Fatalf("fallback bind state = %#v, want empty", calls[1].State)
	}
}

func TestBindShellRoleStreamWithCallerNoFallbackOnNonInvalidParams(t *testing.T) {
	expectedErr := errors.New("transport unavailable")
	err := bindShellRoleStreamWithCaller(context.Background(), "shell-session-2", false, func(
		_ context.Context,
		_ protocol.BindStreamParams,
		_ *gateway.MessageFrame,
	) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("bindShellRoleStreamWithCaller() error = %v, want %v", err, expectedErr)
	}
}

func TestValidateBindStreamAckFrame(t *testing.T) {
	if err := validateBindStreamAckFrame(gateway.MessageFrame{Type: gateway.FrameTypeAck}); err != nil {
		t.Fatalf("validateBindStreamAckFrame(ack) error = %v", err)
	}

	errFrame := gateway.MessageFrame{
		Type: gateway.FrameTypeError,
		Error: &gateway.FrameError{
			Code:    gateway.ErrorCodeInvalidAction.String(),
			Message: "invalid action",
		},
	}
	if err := validateBindStreamAckFrame(errFrame); err == nil {
		t.Fatal("expected validateBindStreamAckFrame to reject error frame")
	}

	if err := validateBindStreamAckFrame(gateway.MessageFrame{Type: gateway.FrameTypeEvent}); err == nil {
		t.Fatal("expected validateBindStreamAckFrame to reject non-ack frame")
	}
}
