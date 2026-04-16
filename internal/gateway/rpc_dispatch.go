package gateway

import (
	"context"
	"strings"

	"neo-code/internal/gateway/protocol"
)

// dispatchRPCRequest 统一将 JSON-RPC 请求归一化并分发到网关内部 MessageFrame 处理链路。
func dispatchRPCRequest(ctx context.Context, request protocol.JSONRPCRequest, runtimePort RuntimePort) protocol.JSONRPCResponse {
	normalized, rpcErr := protocol.NormalizeJSONRPCRequest(request)
	if rpcErr != nil {
		return protocol.NewJSONRPCErrorResponse(normalized.ID, rpcErr)
	}

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameAction(normalized.Action),
		RequestID: normalized.RequestID,
		SessionID: normalized.SessionID,
		RunID:     normalized.RunID,
		Workdir:   normalized.Workdir,
		Payload:   normalized.Payload,
	}

	frame = hydrateFrameSessionFromConnection(ctx, frame)
	if requiresSession(frame.Action) && strings.TrimSpace(frame.SessionID) == "" {
		return protocol.NewJSONRPCErrorResponse(
			normalized.ID,
			protocol.NewJSONRPCError(
				protocol.JSONRPCCodeInvalidParams,
				"missing required field: session_id",
				protocol.GatewayCodeMissingRequiredField,
			),
		)
	}
	applyAutomaticBinding(ctx, frame)

	responseFrame := dispatchFrame(ctx, frame, runtimePort)
	if responseFrame.Type != FrameTypeError {
		rpcResponse, encodeErr := protocol.NewJSONRPCResultResponse(normalized.ID, responseFrame)
		if encodeErr != nil {
			return protocol.NewJSONRPCErrorResponse(normalized.ID, encodeErr)
		}
		return rpcResponse
	}

	frameErr := responseFrame.Error
	if frameErr == nil {
		frameErr = NewFrameError(ErrorCodeInternalError, "gateway response missing error payload")
	}
	return protocol.NewJSONRPCErrorResponse(
		normalized.ID,
		protocol.NewJSONRPCError(
			protocol.MapGatewayCodeToJSONRPCCode(frameErr.Code),
			frameErr.Message,
			frameErr.Code,
		),
	)
}

// dispatchFrame 统一校验并分发网关 MessageFrame，请求动作会进入注册处理器。
func dispatchFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if validationErr := ValidateFrame(frame); validationErr != nil {
		return errorFrame(frame, validationErr)
	}

	if frame.Type != FrameTypeRequest {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidFrame, "only request frames are supported"))
	}

	return dispatchRequestFrame(ctx, frame, runtimePort)
}

// hydrateFrameSessionFromConnection 根据统一优先级为请求帧补齐 session_id：显式字段 > payload 参数 > 连接绑定兜底。
func hydrateFrameSessionFromConnection(ctx context.Context, frame MessageFrame) MessageFrame {
	if strings.TrimSpace(frame.SessionID) != "" {
		return frame
	}

	payloadSessionID := strings.TrimSpace(extractSessionIDFromPayload(frame.Payload))
	if payloadSessionID != "" {
		frame.SessionID = payloadSessionID
		return frame
	}

	relay, relayExists := StreamRelayFromContext(ctx)
	connectionID, connectionExists := ConnectionIDFromContext(ctx)
	if !relayExists || !connectionExists {
		return frame
	}

	frame.SessionID = strings.TrimSpace(relay.ResolveFallbackSessionID(connectionID))
	return frame
}

// requiresSession 判断指定动作在分发阶段是否必须携带 session_id。
func requiresSession(action FrameAction) bool {
	switch action {
	case FrameActionBindStream, FrameActionRun, FrameActionCompact, FrameActionLoadSession, FrameActionResolvePermission:
		return true
	default:
		return false
	}
}

// applyAutomaticBinding 在请求分发前执行自动续绑与 ping 续期逻辑。
func applyAutomaticBinding(ctx context.Context, frame MessageFrame) {
	relay, relayExists := StreamRelayFromContext(ctx)
	connectionID, connectionExists := ConnectionIDFromContext(ctx)
	if !relayExists || !connectionExists {
		return
	}

	if frame.Action == FrameActionPing {
		relay.RefreshConnectionBindings(connectionID)
		return
	}

	if frame.Action == FrameActionBindStream {
		return
	}

	relay.AutoBindFromFrame(connectionID, frame)
}
