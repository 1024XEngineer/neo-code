package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
)

type requestFrameHandler func(ctx context.Context, frame MessageFrame) MessageFrame

var wakeOpenURLHandler = handlers.NewWakeOpenURLHandler()

var requestFrameHandlers = map[FrameAction]requestFrameHandler{
	FrameActionPing:        handlePingFrame,
	FrameActionBindStream:  handleBindStreamFrame,
	FrameActionWakeOpenURL: handleWakeOpenURLFrame,
}

// dispatchRequestFrame 统一分发 request 帧到对应动作处理器。
func dispatchRequestFrame(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
	handler, ok := requestFrameHandlers[frame.Action]
	if !ok {
		return errorFrame(frame, NewFrameError(ErrorCodeUnsupportedAction, "action is not implemented in gateway step 2"))
	}
	return handler(ctx, frame)
}

// handlePingFrame 处理 ping 探活请求并返回 pong 响应。
func handlePingFrame(_ context.Context, frame MessageFrame) MessageFrame {
	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionPing,
		RequestID: frame.RequestID,
		Payload: map[string]string{
			"message": "pong",
		},
	}
}

// handleBindStreamFrame 处理 gateway.bindStream 请求并登记连接到会话路由表。
func handleBindStreamFrame(ctx context.Context, frame MessageFrame) MessageFrame {
	params, err := decodeBindStreamParams(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	relay, relayExists := StreamRelayFromContext(ctx)
	connectionID, connectionExists := ConnectionIDFromContext(ctx)
	if !relayExists || !connectionExists {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, "stream relay context is unavailable"))
	}

	if bindErr := relay.BindConnection(connectionID, StreamBinding{
		SessionID: params.SessionID,
		RunID:     params.RunID,
		Channel:   params.Channel,
		Explicit:  true,
	}); bindErr != nil {
		return errorFrame(frame, bindErr)
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionBindStream,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		RunID:     params.RunID,
		Payload: map[string]any{
			"message": "stream binding updated",
			"channel": params.Channel,
		},
	}
}

// handleWakeOpenURLFrame 解析并处理 wake.openUrl 请求。
func handleWakeOpenURLFrame(_ context.Context, frame MessageFrame) MessageFrame {
	intent, err := decodeWakeIntent(frame.Payload)
	if err != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidFrame, "invalid wake payload"))
	}

	result, wakeErr := wakeOpenURLHandler.Handle(intent)
	if wakeErr != nil {
		return errorFrame(frame, toFrameError(wakeErr))
	}
	sessionID := intent.SessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = strings.TrimSpace(frame.SessionID)
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionWakeOpenURL,
		RequestID: frame.RequestID,
		SessionID: sessionID,
		Payload:   result,
	}
}

type bindStreamParams struct {
	SessionID string
	RunID     string
	Channel   StreamChannel
}

// decodeBindStreamParams 将 payload 解析为 bind_stream 所需参数。
func decodeBindStreamParams(payload any) (bindStreamParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.BindStreamParams:
		return normalizeBindStreamParams(typed)
	case *protocol.BindStreamParams:
		if typed == nil {
			return bindStreamParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid bind_stream payload")
		}
		return normalizeBindStreamParams(*typed)
	case map[string]any:
		return normalizeBindStreamParams(protocol.BindStreamParams{
			SessionID: readStringValue(typed, "session_id"),
			RunID:     readStringValue(typed, "run_id"),
			Channel:   readStringValue(typed, "channel"),
		})
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return bindStreamParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid bind_stream payload")
		}
		var decoded protocol.BindStreamParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return bindStreamParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid bind_stream payload")
		}
		return normalizeBindStreamParams(decoded)
	}
}

// normalizeBindStreamParams 对 bind_stream 参数执行归一化与有效性校验。
func normalizeBindStreamParams(params protocol.BindStreamParams) (bindStreamParams, *FrameError) {
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		return bindStreamParams{}, NewMissingRequiredFieldError("payload.session_id")
	}

	runID := strings.TrimSpace(params.RunID)
	channel := strings.ToLower(strings.TrimSpace(params.Channel))
	if channel == "" {
		channel = string(StreamChannelAll)
	}
	parsedChannel, validChannel := ParseStreamChannel(channel)
	if !validChannel {
		return bindStreamParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid bind_stream channel")
	}

	return bindStreamParams{
		SessionID: sessionID,
		RunID:     runID,
		Channel:   parsedChannel,
	}, nil
}

// readStringValue 从 map 负载中读取并归一化字符串字段。
func readStringValue(payload map[string]any, key string) string {
	rawValue, exists := payload[key]
	if !exists {
		return ""
	}
	stringValue, ok := rawValue.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue)
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
