package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
)

type requestFrameHandler func(frame MessageFrame) MessageFrame

var wakeOpenURLHandler = handlers.NewWakeOpenURLHandler()

var requestFrameHandlers = map[FrameAction]requestFrameHandler{
	FrameActionPing:        handlePingFrame,
	FrameActionWakeOpenURL: handleWakeOpenURLFrame,
}

// dispatchRequestFrame 统一分发 request 帧到对应动作处理器。
func dispatchRequestFrame(_ context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
	handler, ok := requestFrameHandlers[frame.Action]
	if !ok {
		return errorFrame(frame, NewFrameError(ErrorCodeUnsupportedAction, "action is not implemented in gateway step 2"))
	}
	return handler(frame)
}

// handlePingFrame 处理 ping 探活请求并返回 pong 响应。
func handlePingFrame(frame MessageFrame) MessageFrame {
	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionPing,
		RequestID: frame.RequestID,
		Payload: map[string]string{
			"message": "pong",
		},
	}
}

// handleWakeOpenURLFrame 解析并处理 wake.openUrl 请求。
func handleWakeOpenURLFrame(frame MessageFrame) MessageFrame {
	intent, err := decodeWakeIntent(frame.Payload)
	if err != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidFrame, "invalid wake payload"))
	}

	result, wakeErr := wakeOpenURLHandler.Handle(intent)
	if wakeErr != nil {
		return errorFrame(frame, toFrameError(wakeErr))
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWakeOpenURL,
		RequestID: frame.RequestID,
		SessionID: intent.SessionID,
		Payload:   result,
	}
}

// decodeWakeIntent 将任意 payload 解码为 WakeIntent 结构。
func decodeWakeIntent(payload any) (protocol.WakeIntent, error) {
	if payload == nil {
		return protocol.WakeIntent{}, fmt.Errorf("payload is nil")
	}

	if direct, ok := payload.(protocol.WakeIntent); ok {
		return normalizeWakeIntent(direct), nil
	}
	if pointer, ok := payload.(*protocol.WakeIntent); ok {
		if pointer == nil {
			return protocol.WakeIntent{}, fmt.Errorf("payload pointer is nil")
		}
		return normalizeWakeIntent(*pointer), nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return protocol.WakeIntent{}, err
	}

	var decoded protocol.WakeIntent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return protocol.WakeIntent{}, err
	}
	return normalizeWakeIntent(decoded), nil
}

// normalizeWakeIntent 对 WakeIntent 中关键字段执行归一化，保证后续处理一致。
func normalizeWakeIntent(intent protocol.WakeIntent) protocol.WakeIntent {
	intent.Action = strings.ToLower(strings.TrimSpace(intent.Action))
	intent.SessionID = strings.TrimSpace(intent.SessionID)
	intent.Workdir = strings.TrimSpace(intent.Workdir)
	if len(intent.Params) == 0 {
		intent.Params = nil
	}
	return intent
}

// toFrameError 将 wake handler 错误映射为网关稳定错误帧。
func toFrameError(err *handlers.WakeError) *FrameError {
	if err == nil {
		return NewFrameError(ErrorCodeInternalError, "unknown wake handler error")
	}
	if IsStableErrorCode(err.Code) {
		return &FrameError{
			Code:    err.Code,
			Message: err.Message,
		}
	}
	return NewFrameError(ErrorCodeInternalError, err.Message)
}
