package ptyproxy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

// bindShellRoleStreamWithCaller 统一处理 shell 角色 bind_stream 调用与兼容回退。
func bindShellRoleStreamWithCaller(
	ctx context.Context,
	sessionID string,
	autoEnabled bool,
	caller func(context.Context, protocol.BindStreamParams, *gateway.MessageFrame) error,
) error {
	if caller == nil {
		return errors.New("bind stream caller is nil")
	}
	primaryParams := protocol.BindStreamParams{
		SessionID: strings.TrimSpace(sessionID),
		Channel:   "all",
		Role:      "shell",
		State: map[string]any{
			"auto_enabled": autoEnabled,
		},
	}
	legacyParams := primaryParams
	legacyParams.State = nil

	var ack gateway.MessageFrame
	if err := caller(ctx, primaryParams, &ack); err != nil {
		if !shouldFallbackBindStreamState(err) {
			return err
		}
		ack = gateway.MessageFrame{}
		if retryErr := caller(ctx, legacyParams, &ack); retryErr != nil {
			return retryErr
		}
		return validateBindStreamAckFrame(ack)
	}
	return validateBindStreamAckFrame(ack)
}

// shouldFallbackBindStreamState 判断 bind_stream 是否需要回退到无 state 版本。
func shouldFallbackBindStreamState(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *gatewayclient.GatewayRPCError
	if errors.As(err, &rpcErr) {
		if rpcErr != nil && rpcErr.Code == protocol.JSONRPCCodeInvalidParams {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "invalid params")
}

// validateBindStreamAckFrame 校验 bind_stream 的 ACK 返回。
func validateBindStreamAckFrame(ack gateway.MessageFrame) error {
	if ack.Type == gateway.FrameTypeError && ack.Error != nil {
		return fmt.Errorf(
			"gateway bind_stream failed (%s): %s",
			strings.TrimSpace(ack.Error.Code),
			strings.TrimSpace(ack.Error.Message),
		)
	}
	if ack.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", ack.Type)
	}
	return nil
}

// bindWindowsShellRoleStreamWithCaller 复用 Windows 端 bind_stream 兼容回退逻辑。
func bindWindowsShellRoleStreamWithCaller(
	ctx context.Context,
	sessionID string,
	autoEnabled bool,
	caller func(context.Context, protocol.BindStreamParams, *gateway.MessageFrame) error,
) error {
	return bindShellRoleStreamWithCaller(ctx, sessionID, autoEnabled, caller)
}

// shouldFallbackWindowsBindStreamState 判断 bind_stream 是否需要回退到无 state 形式。
func shouldFallbackWindowsBindStreamState(err error) bool {
	return shouldFallbackBindStreamState(err)
}

// validateWindowsBindStreamAckFrame 校验 bind_stream ACK 帧。
func validateWindowsBindStreamAckFrame(ack gateway.MessageFrame) error {
	return validateBindStreamAckFrame(ack)
}
