package gateway

import (
	"context"

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
		Workdir:   normalized.Workdir,
		Payload:   normalized.Payload,
	}

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
