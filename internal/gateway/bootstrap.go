package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
)

const (
	// defaultRuntimeOperationTimeout 定义网关调用 runtime 的硬超时时间，防止资源被无限占用。
	defaultRuntimeOperationTimeout = 30 * time.Minute
)

type requestFrameHandler func(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame

var wakeOpenURLHandler = handlers.NewWakeOpenURLHandler()

var requestFrameHandlers = map[FrameAction]requestFrameHandler{
	FrameActionAuthenticate: func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handleAuthenticateFrame(ctx, frame)
	},
	FrameActionPing: func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handlePingFrame(ctx, frame)
	},
	FrameActionBindStream: func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handleBindStreamFrame(ctx, frame)
	},
	FrameActionWakeOpenURL: func(ctx context.Context, frame MessageFrame, _ RuntimePort) MessageFrame {
		return handleWakeOpenURLFrame(ctx, frame)
	},
	FrameActionRun:               handleRunFrame,
	FrameActionCompact:           handleCompactFrame,
	FrameActionCancel:            handleCancelFrame,
	FrameActionListSessions:      handleListSessionsFrame,
	FrameActionLoadSession:       handleLoadSessionFrame,
	FrameActionResolvePermission: handleResolvePermissionFrame,
}

// dispatchRequestFrame 统一分发 request 帧到对应动作处理器。
func dispatchRequestFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	handler, ok := requestFrameHandlers[frame.Action]
	if !ok {
		return errorFrame(frame, NewFrameError(ErrorCodeUnsupportedAction, "action is not implemented in gateway step 2"))
	}
	return handler(ctx, frame, runtimePort)
}

// handlePingFrame 处理 ping 探活请求并返回 pong 响应。
func handlePingFrame(_ context.Context, frame MessageFrame) MessageFrame {
	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionPing,
		RequestID: frame.RequestID,
		Payload: map[string]string{
			"message": "pong",
			"version": GatewayVersion,
		},
	}
}

// handleAuthenticateFrame 处理 gateway.authenticate 请求并更新连接级认证状态。
func handleAuthenticateFrame(ctx context.Context, frame MessageFrame) MessageFrame {
	params, err := decodeAuthenticatePayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	authenticator, hasAuthenticator := TokenAuthenticatorFromContext(ctx)
	if !hasAuthenticator {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, "token authenticator is unavailable"))
	}
	if !authenticator.ValidateToken(params.Token) {
		return errorFrame(frame, NewFrameError(ErrorCodeUnauthorized, "invalid auth token"))
	}

	if authState, ok := ConnectionAuthStateFromContext(ctx); ok {
		authState.MarkAuthenticated()
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionAuthenticate,
		RequestID: frame.RequestID,
		Payload: map[string]string{
			"message": "authenticated",
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

// handleRunFrame 执行 gateway.run 动作，将请求转发到 RuntimePort。
func handleRunFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	effectiveRunID := normalizeRunID(strings.TrimSpace(frame.RunID), strings.TrimSpace(frame.RequestID))
	input := RunInput{
		RequestID:  frame.RequestID,
		SessionID:  strings.TrimSpace(frame.SessionID),
		RunID:      effectiveRunID,
		InputText:  strings.TrimSpace(frame.InputText),
		InputParts: append([]InputPart(nil), frame.InputParts...),
		Workdir:    strings.TrimSpace(frame.Workdir),
	}
	frame.RunID = input.RunID
	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	if err := runtimePort.Run(callCtx, input); err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "run")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionRun,
		RequestID: frame.RequestID,
		SessionID: input.SessionID,
		RunID:     input.RunID,
		Payload: map[string]string{
			"message": "run accepted",
		},
	}
}

// handleCompactFrame 执行 gateway.compact 动作，并返回 runtime 压缩结果。
func handleCompactFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	result, err := runtimePort.Compact(callCtx, CompactInput{
		RequestID: frame.RequestID,
		SessionID: strings.TrimSpace(frame.SessionID),
		RunID:     strings.TrimSpace(frame.RunID),
	})
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "compact")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionCompact,
		RequestID: frame.RequestID,
		SessionID: strings.TrimSpace(frame.SessionID),
		RunID:     strings.TrimSpace(frame.RunID),
		Payload:   result,
	}
}

// handleCancelFrame 执行 gateway.cancel 动作，返回当前运行取消状态。
func handleCancelFrame(_ context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	canceled := runtimePort.CancelActiveRun()
	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionCancel,
		RequestID: frame.RequestID,
		Payload: map[string]any{
			"canceled": canceled,
		},
	}
}

// handleListSessionsFrame 执行 gateway.listSessions 动作，返回会话摘要列表。
func handleListSessionsFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	summaries, err := runtimePort.ListSessions(callCtx)
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "list_sessions")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionListSessions,
		RequestID: frame.RequestID,
		Payload: map[string]any{
			"sessions": summaries,
		},
	}
}

// handleLoadSessionFrame 执行 gateway.loadSession 动作，返回单个会话详情。
func handleLoadSessionFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	// TODO(Security): 当前为本地单用户场景，后续若演进为多租户，需在此处校验 Subject 对 session_id 的所有权，防止 IDOR 越权访问。
	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	session, err := runtimePort.LoadSession(callCtx, strings.TrimSpace(frame.SessionID))
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "load_session")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionLoadSession,
		RequestID: frame.RequestID,
		SessionID: strings.TrimSpace(frame.SessionID),
		Payload:   session,
	}
}

// handleResolvePermissionFrame 执行 gateway.resolvePermission 动作，将审批决策写入 runtime。
func handleResolvePermissionFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}

	input, err := decodePermissionResolutionInput(frame.Payload)
	if err != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidAction, "invalid resolve_permission payload"))
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" {
		return errorFrame(frame, NewMissingRequiredFieldError("payload.request_id"))
	}
	if !isValidPermissionResolutionDecision(input.Decision) {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidAction, "invalid resolve_permission decision"))
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	if err := runtimePort.ResolvePermission(callCtx, input); err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "resolve_permission")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionResolvePermission,
		RequestID: frame.RequestID,
		Payload: map[string]any{
			"request_id": input.RequestID,
			"decision":   input.Decision,
			"message":    "permission resolved",
		},
	}
}

// runtimePortUnavailableFrame 构造一个 runtime 未注入时的统一错误响应。
func runtimePortUnavailableFrame(frame MessageFrame) MessageFrame {
	return errorFrame(frame, NewFrameError(ErrorCodeInternalError, "runtime port is unavailable"))
}

// withRuntimeOperationTimeout 为 runtime 调用附加硬超时，避免客户端异常导致资源被长期占用。
func withRuntimeOperationTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultRuntimeOperationTimeout)
}

// normalizeRunID 返回最终生效的 run_id，优先保留显式 run_id，其次回退 request_id，最后生成网关侧默认值。
func normalizeRunID(runID, requestID string) string {
	normalizedRunID := strings.TrimSpace(runID)
	if normalizedRunID != "" {
		return normalizedRunID
	}
	normalizedRequestID := strings.TrimSpace(requestID)
	if normalizedRequestID != "" {
		return normalizedRequestID
	}
	return fmt.Sprintf("run_%d", time.Now().UnixNano())
}

// runtimeCallFailedFrame 构造 runtime 调用失败时的统一错误响应，并将底层错误仅写入服务端日志。
func runtimeCallFailedFrame(ctx context.Context, frame MessageFrame, err error, operation string) MessageFrame {
	normalizedOperation := strings.TrimSpace(operation)
	if normalizedOperation == "" {
		normalizedOperation = "runtime operation"
	}

	errorCode := ErrorCodeInternalError
	message := fmt.Sprintf("%s failed", normalizedOperation)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		errorCode = ErrorCodeTimeout
		message = fmt.Sprintf("%s timed out", normalizedOperation)
	case errors.Is(err, context.Canceled):
		errorCode = ErrorCodeInvalidAction
		message = fmt.Sprintf("%s canceled", normalizedOperation)
	}

	if logger, ok := GatewayLoggerFromContext(ctx); ok && logger != nil && err != nil {
		logger.Printf(
			"gateway runtime call failed: operation=%s request_id=%s session_id=%s run_id=%s error=%v",
			normalizedOperation,
			strings.TrimSpace(frame.RequestID),
			strings.TrimSpace(frame.SessionID),
			strings.TrimSpace(frame.RunID),
			err,
		)
	}

	return errorFrame(frame, NewFrameError(errorCode, message))
}

type bindStreamParams struct {
	SessionID string
	RunID     string
	Channel   StreamChannel
}

type authenticateParams struct {
	Token string
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

// decodeAuthenticatePayload 将 payload 解析为 authenticate 所需参数。
func decodeAuthenticatePayload(payload any) (authenticateParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.AuthenticateParams:
		if strings.TrimSpace(typed.Token) == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: strings.TrimSpace(typed.Token)}, nil
	case *protocol.AuthenticateParams:
		if typed == nil || strings.TrimSpace(typed.Token) == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: strings.TrimSpace(typed.Token)}, nil
	case map[string]any:
		token := readStringValue(typed, "token")
		if token == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: token}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return authenticateParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid authenticate payload")
		}
		var decoded protocol.AuthenticateParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return authenticateParams{}, NewFrameError(ErrorCodeInvalidFrame, "invalid authenticate payload")
		}
		token := strings.TrimSpace(decoded.Token)
		if token == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: token}, nil
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
