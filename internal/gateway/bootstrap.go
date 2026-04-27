package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
	toolkits "neo-code/internal/tools"
)

const (
	// defaultRuntimeOperationTimeout 定义网关调用 runtime 的硬超时，避免资源长期占用。
	defaultRuntimeOperationTimeout = 30 * time.Minute
	defaultLocalSubjectID          = "local_admin"
)

type requestFrameHandler func(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame

var wakeOpenURLHandler = handlers.NewWakeOpenURLHandler()

var allowedSystemToolNames = map[string]struct{}{
	toolkits.ToolNameMemoList:     {},
	toolkits.ToolNameMemoRemember: {},
	toolkits.ToolNameMemoRecall:   {},
	toolkits.ToolNameMemoRemove:   {},
}

// dispatchRequestFrame 统一分发 request 帧到对应处理器。
func dispatchRequestFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	handler, ok := defaultRegistry.Lookup(frame.Action)
	if !ok {
		return errorFrame(frame, NewFrameError(ErrorCodeUnsupportedAction, "action is not implemented in gateway step 2"))
	}
	return handler(ctx, frame, runtimePort)
}

// handlePingFrame 处理 gateway.ping 探活请求。
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

// handleAuthenticateFrame 处理 gateway.authenticate，并写入连接级认证状态。
func handleAuthenticateFrame(ctx context.Context, frame MessageFrame) MessageFrame {
	params, err := decodeAuthenticatePayload(frame.Payload)
	if err != nil {
		return errorFrame(frame, err)
	}

	authenticator, hasAuthenticator := TokenAuthenticatorFromContext(ctx)
	if !hasAuthenticator {
		return errorFrame(frame, NewFrameError(ErrorCodeInternalError, "token authenticator is unavailable"))
	}
	subjectID, valid := authenticator.ResolveSubjectID(params.Token)
	if !valid || strings.TrimSpace(subjectID) == "" {
		return errorFrame(frame, NewFrameError(ErrorCodeUnauthorized, "invalid auth token"))
	}

	if authState, ok := ConnectionAuthStateFromContext(ctx); ok {
		authState.MarkAuthenticated(subjectID)
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionAuthenticate,
		RequestID: frame.RequestID,
		Payload: map[string]string{
			"message":    "authenticated",
			"subject_id": subjectID,
		},
	}
}

// handleBindStreamFrame 处理 gateway.bindStream 并注册连接订阅关系。
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

// handleWakeOpenURLFrame 处理 wake.openUrl 请求。
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

// handleRunFrame 处理 gateway.run，采用“受理即返回”的异步执行模型。
func handleRunFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	effectiveRunID := normalizeRunID(strings.TrimSpace(frame.RunID), strings.TrimSpace(frame.RequestID))
	input := RunInput{
		SubjectID:  subjectID,
		RequestID:  strings.TrimSpace(frame.RequestID),
		SessionID:  strings.TrimSpace(frame.SessionID),
		RunID:      effectiveRunID,
		InputText:  strings.TrimSpace(frame.InputText),
		InputParts: append([]InputPart(nil), frame.InputParts...),
		Workdir:    strings.TrimSpace(frame.Workdir),
	}
	frame.RunID = input.RunID

	runExecutionContext := deriveRuntimeExecutionContext(ctx)
	callCtx, cancel := withRuntimeOperationTimeout(runExecutionContext)
	frameSnapshot := frame
	inputSnapshot := input
	go func() {
		defer cancel()
		if err := runtimePort.Run(callCtx, inputSnapshot); err != nil {
			failedFrame := runtimeCallFailedFrame(callCtx, frameSnapshot, err, "run")
			if logger, ok := GatewayLoggerFromContext(callCtx); ok && logger != nil && failedFrame.Error != nil {
				logger.Printf(
					"gateway run async failed: request_id=%s session_id=%s run_id=%s code=%s message=%s",
					strings.TrimSpace(frameSnapshot.RequestID),
					strings.TrimSpace(frameSnapshot.SessionID),
					strings.TrimSpace(frameSnapshot.RunID),
					strings.TrimSpace(failedFrame.Error.Code),
					strings.TrimSpace(failedFrame.Error.Message),
				)
			}
		}
	}()

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

// handleCompactFrame 处理 gateway.compact 请求。
func handleCompactFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	result, err := runtimePort.Compact(callCtx, CompactInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
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

// handleExecuteSystemToolFrame 处理 gateway.executeSystemTool 请求。
func handleExecuteSystemToolFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	params, parseErr := decodeExecuteSystemToolPayload(frame.Payload)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}
	if params.SessionID == "" {
		params.SessionID = strings.TrimSpace(frame.SessionID)
	}
	if params.RunID == "" {
		params.RunID = strings.TrimSpace(frame.RunID)
	}
	if params.Workdir == "" {
		params.Workdir = strings.TrimSpace(frame.Workdir)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	result, err := runtimePort.ExecuteSystemTool(callCtx, ExecuteSystemToolInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: params.SessionID,
		RunID:     params.RunID,
		Workdir:   params.Workdir,
		ToolName:  params.ToolName,
		Arguments: append([]byte(nil), params.Arguments...),
	})
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "execute_system_tool")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionExecuteSystemTool,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		RunID:     params.RunID,
		Payload:   result,
	}
}

// handleActivateSessionSkillFrame 处理 gateway.activateSessionSkill 请求。
func handleActivateSessionSkillFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	params, parseErr := decodeActivateSessionSkillPayload(frame.Payload)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}
	if params.SessionID == "" {
		params.SessionID = strings.TrimSpace(frame.SessionID)
	}
	if params.SessionID == "" {
		return errorFrame(frame, NewMissingRequiredFieldError("payload.session_id"))
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	if err := runtimePort.ActivateSessionSkill(callCtx, SessionSkillMutationInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: params.SessionID,
		SkillID:   params.SkillID,
	}); err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "activate_session_skill")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionActivateSessionSkill,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		Payload: map[string]any{
			"session_id": params.SessionID,
			"skill_id":   params.SkillID,
			"message":    "skill activated",
		},
	}
}

// handleDeactivateSessionSkillFrame 处理 gateway.deactivateSessionSkill 请求。
func handleDeactivateSessionSkillFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	params, parseErr := decodeDeactivateSessionSkillPayload(frame.Payload)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}
	if params.SessionID == "" {
		params.SessionID = strings.TrimSpace(frame.SessionID)
	}
	if params.SessionID == "" {
		return errorFrame(frame, NewMissingRequiredFieldError("payload.session_id"))
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	if err := runtimePort.DeactivateSessionSkill(callCtx, SessionSkillMutationInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: params.SessionID,
		SkillID:   params.SkillID,
	}); err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "deactivate_session_skill")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionDeactivateSessionSkill,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		Payload: map[string]any{
			"session_id": params.SessionID,
			"skill_id":   params.SkillID,
			"message":    "skill deactivated",
		},
	}
}

// handleListSessionSkillsFrame 处理 gateway.listSessionSkills 请求。
func handleListSessionSkillsFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	params, parseErr := decodeListSessionSkillsPayload(frame.Payload)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}
	if params.SessionID == "" {
		params.SessionID = strings.TrimSpace(frame.SessionID)
	}
	if params.SessionID == "" {
		return errorFrame(frame, NewMissingRequiredFieldError("payload.session_id"))
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	states, err := runtimePort.ListSessionSkills(callCtx, ListSessionSkillsInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: params.SessionID,
	})
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "list_session_skills")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionListSessionSkills,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		Payload: map[string]any{
			"skills": states,
		},
	}
}

// handleListAvailableSkillsFrame 处理 gateway.listAvailableSkills 请求。
func handleListAvailableSkillsFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	params, parseErr := decodeListAvailableSkillsPayload(frame.Payload)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}
	if params.SessionID == "" {
		params.SessionID = strings.TrimSpace(frame.SessionID)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	states, err := runtimePort.ListAvailableSkills(callCtx, ListAvailableSkillsInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: params.SessionID,
	})
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "list_available_skills")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionListAvailableSkills,
		RequestID: frame.RequestID,
		SessionID: params.SessionID,
		Payload: map[string]any{
			"skills": states,
		},
	}
}

// handleCancelFrame 处理 gateway.cancel 请求，按 run_id 精确取消任务。
func handleCancelFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	cancelInput, parseErr := decodeCancelInput(frame)
	if parseErr != nil {
		return errorFrame(frame, parseErr)
	}

	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	canceled, err := runtimePort.CancelRun(callCtx, CancelInput{
		SubjectID: subjectID,
		RequestID: strings.TrimSpace(frame.RequestID),
		SessionID: cancelInput.SessionID,
		RunID:     cancelInput.RunID,
	})
	if err != nil {
		return runtimeCallFailedFrame(callCtx, frame, err, "cancel")
	}

	return MessageFrame{
		Type:      FrameTypeAck,
		Action:    FrameActionCancel,
		RequestID: frame.RequestID,
		Payload: map[string]any{
			"canceled": canceled,
			"run_id":   cancelInput.RunID,
		},
	}
}

// handleListSessionsFrame 处理 gateway.listSessions 请求。
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

// handleLoadSessionFrame 处理 gateway.loadSession 请求。
func handleLoadSessionFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	// TODO(Security): 当前为本地单用户场景，后续若演进为多租户，需校验 Subject 对 session_id 的所有权，防止 IDOR 越权访问。
	callCtx, cancel := withRuntimeOperationTimeout(ctx)
	defer cancel()
	session, err := runtimePort.LoadSession(callCtx, LoadSessionInput{
		SubjectID: subjectID,
		SessionID: strings.TrimSpace(frame.SessionID),
	})
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

// handleResolvePermissionFrame 处理 gateway.resolvePermission 请求。
func handleResolvePermissionFrame(ctx context.Context, frame MessageFrame, runtimePort RuntimePort) MessageFrame {
	if runtimePort == nil {
		return runtimePortUnavailableFrame(frame)
	}
	subjectID, subjectErr := requireAuthenticatedSubjectID(ctx)
	if subjectErr != nil {
		return errorFrame(frame, subjectErr)
	}

	input, err := decodePermissionResolutionInput(frame.Payload)
	if err != nil {
		return errorFrame(frame, NewFrameError(ErrorCodeInvalidAction, "invalid resolve_permission payload"))
	}
	input.SubjectID = subjectID
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

// runtimePortUnavailableFrame 在 runtime 未注入时返回统一错误。
func runtimePortUnavailableFrame(frame MessageFrame) MessageFrame {
	return errorFrame(frame, NewFrameError(ErrorCodeInternalError, "runtime port is unavailable"))
}

// withRuntimeOperationTimeout 为 runtime 调用附加硬超时。
func withRuntimeOperationTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultRuntimeOperationTimeout)
}

// deriveRuntimeExecutionContext 为异步 run 选择合适的执行上下文。
func deriveRuntimeExecutionContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if RequestSourceFromContext(ctx) == RequestSourceHTTP {
		return context.WithoutCancel(ctx)
	}
	return ctx
}

// requireAuthenticatedSubjectID 从上下文中提取已认证主体。
func requireAuthenticatedSubjectID(ctx context.Context) (string, *FrameError) {
	if subjectID := strings.TrimSpace(AuthenticatedSubjectIDFromContext(ctx)); subjectID != "" {
		if authState, ok := ConnectionAuthStateFromContext(ctx); ok && !authState.IsAuthenticated() {
			authState.MarkAuthenticated(subjectID)
		}
		return subjectID, nil
	}

	authenticator, hasAuthenticator := TokenAuthenticatorFromContext(ctx)
	if !hasAuthenticator {
		return defaultLocalSubjectID, nil
	}

	requestToken := RequestTokenFromContext(ctx)
	if requestToken == "" {
		return "", NewFrameError(ErrorCodeUnauthorized, "missing authenticated subject")
	}
	subjectID, valid := authenticator.ResolveSubjectID(requestToken)
	if !valid || strings.TrimSpace(subjectID) == "" {
		return "", NewFrameError(ErrorCodeUnauthorized, "missing authenticated subject")
	}
	if authState, ok := ConnectionAuthStateFromContext(ctx); ok {
		authState.MarkAuthenticated(subjectID)
	}
	return strings.TrimSpace(subjectID), nil
}

// normalizeRunID 归一化 run_id，优先保留显式值，其次回退 request_id。
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

// runtimeCallFailedFrame 将 runtime 错误映射为对外稳定错误码，并避免泄露底层细节。
func runtimeCallFailedFrame(ctx context.Context, frame MessageFrame, err error, operation string) MessageFrame {
	normalizedOperation := strings.TrimSpace(operation)
	if normalizedOperation == "" {
		normalizedOperation = "runtime operation"
	}

	errorCode := ErrorCodeInternalError
	message := fmt.Sprintf("%s failed", normalizedOperation)
	switch {
	case errors.Is(err, ErrRuntimeAccessDenied):
		errorCode = ErrorCodeAccessDenied
		message = fmt.Sprintf("%s access denied", normalizedOperation)
	case errors.Is(err, ErrRuntimeResourceNotFound):
		errorCode = ErrorCodeResourceNotFound
		message = fmt.Sprintf("%s target not found", normalizedOperation)
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

type cancelParams struct {
	SessionID string
	RunID     string
}

type executeSystemToolParams struct {
	SessionID string
	RunID     string
	Workdir   string
	ToolName  string
	Arguments []byte
}

type sessionSkillMutationParams struct {
	SessionID string
	SkillID   string
}

type listSessionSkillsParams struct {
	SessionID string
}

type listAvailableSkillsParams struct {
	SessionID string
}

// decodeBindStreamParams 解析 bind_stream 的负载参数。
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

// decodeAuthenticatePayload 解析 authenticate 的负载参数。
func decodeAuthenticatePayload(payload any) (authenticateParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.AuthenticateParams:
		token := strings.TrimSpace(typed.Token)
		if token == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: token}, nil
	case *protocol.AuthenticateParams:
		if typed == nil {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		token := strings.TrimSpace(typed.Token)
		if token == "" {
			return authenticateParams{}, NewMissingRequiredFieldError("payload.token")
		}
		return authenticateParams{Token: token}, nil
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

// decodeCancelInput 解析 cancel 参数并强制要求 run_id。
func decodeCancelInput(frame MessageFrame) (cancelParams, *FrameError) {
	params := cancelParams{
		SessionID: strings.TrimSpace(frame.SessionID),
		RunID:     strings.TrimSpace(frame.RunID),
	}

	switch typed := frame.Payload.(type) {
	case protocol.CancelParams:
		if params.SessionID == "" {
			params.SessionID = strings.TrimSpace(typed.SessionID)
		}
		if params.RunID == "" {
			params.RunID = strings.TrimSpace(typed.RunID)
		}
	case *protocol.CancelParams:
		if typed != nil {
			if params.SessionID == "" {
				params.SessionID = strings.TrimSpace(typed.SessionID)
			}
			if params.RunID == "" {
				params.RunID = strings.TrimSpace(typed.RunID)
			}
		}
	case map[string]any:
		if params.SessionID == "" {
			params.SessionID = readStringValue(typed, "session_id")
		}
		if params.RunID == "" {
			params.RunID = readStringValue(typed, "run_id")
		}
	case nil:
		// no-op
	default:
		raw, marshalErr := json.Marshal(typed)
		if marshalErr != nil {
			return cancelParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid cancel payload")
		}
		var decoded protocol.CancelParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return cancelParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid cancel payload")
		}
		if params.SessionID == "" {
			params.SessionID = strings.TrimSpace(decoded.SessionID)
		}
		if params.RunID == "" {
			params.RunID = strings.TrimSpace(decoded.RunID)
		}
	}

	if params.RunID == "" {
		return cancelParams{}, NewMissingRequiredFieldError("payload.run_id")
	}
	return params, nil
}

// decodeExecuteSystemToolPayload 解析 execute_system_tool 负载并收敛为统一输入结构。
func decodeExecuteSystemToolPayload(payload any) (executeSystemToolParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.ExecuteSystemToolParams:
		return normalizeExecuteSystemToolParams(typed)
	case *protocol.ExecuteSystemToolParams:
		if typed == nil {
			return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool payload")
		}
		return normalizeExecuteSystemToolParams(*typed)
	case map[string]any:
		params := protocol.ExecuteSystemToolParams{
			SessionID: readStringValue(typed, "session_id"),
			RunID:     readStringValue(typed, "run_id"),
			Workdir:   readStringValue(typed, "workdir"),
			ToolName:  readStringValue(typed, "tool_name"),
		}
		if rawArgs, exists := typed["arguments"]; exists {
			encodedArgs, err := json.Marshal(rawArgs)
			if err != nil {
				return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool arguments")
			}
			params.Arguments = encodedArgs
		}
		return normalizeExecuteSystemToolParams(params)
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool payload")
		}
		var decoded protocol.ExecuteSystemToolParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool payload")
		}
		return normalizeExecuteSystemToolParams(decoded)
	}
}

// normalizeExecuteSystemToolParams 校验并归一化 execute_system_tool 请求参数。
func normalizeExecuteSystemToolParams(params protocol.ExecuteSystemToolParams) (executeSystemToolParams, *FrameError) {
	normalized := executeSystemToolParams{
		SessionID: strings.TrimSpace(params.SessionID),
		RunID:     strings.TrimSpace(params.RunID),
		Workdir:   strings.TrimSpace(params.Workdir),
		ToolName:  strings.TrimSpace(params.ToolName),
	}
	if normalized.ToolName == "" {
		return executeSystemToolParams{}, NewMissingRequiredFieldError("payload.tool_name")
	}
	if _, allowed := allowedSystemToolNames[normalized.ToolName]; !allowed {
		return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool tool_name")
	}

	arguments := bytes.TrimSpace(params.Arguments)
	switch {
	case len(arguments) == 0, bytes.Equal(arguments, []byte("null")):
		normalized.Arguments = []byte("{}")
	case !json.Valid(arguments):
		return executeSystemToolParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid execute_system_tool arguments")
	default:
		normalized.Arguments = append([]byte(nil), arguments...)
	}
	return normalized, nil
}

// decodeActivateSessionSkillPayload 解析 activate_session_skill 负载并收敛为统一输入结构。
func decodeActivateSessionSkillPayload(payload any) (sessionSkillMutationParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.ActivateSessionSkillParams:
		return normalizeSessionSkillMutationParams(typed.SessionID, typed.SkillID, "activate_session_skill")
	case *protocol.ActivateSessionSkillParams:
		if typed == nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid activate_session_skill payload")
		}
		return normalizeSessionSkillMutationParams(typed.SessionID, typed.SkillID, "activate_session_skill")
	case map[string]any:
		return normalizeSessionSkillMutationParams(
			readStringValue(typed, "session_id"),
			readStringValue(typed, "skill_id"),
			"activate_session_skill",
		)
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid activate_session_skill payload")
		}
		var decoded protocol.ActivateSessionSkillParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid activate_session_skill payload")
		}
		return normalizeSessionSkillMutationParams(decoded.SessionID, decoded.SkillID, "activate_session_skill")
	}
}

// decodeDeactivateSessionSkillPayload 解析 deactivate_session_skill 负载并收敛为统一输入结构。
func decodeDeactivateSessionSkillPayload(payload any) (sessionSkillMutationParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.DeactivateSessionSkillParams:
		return normalizeSessionSkillMutationParams(typed.SessionID, typed.SkillID, "deactivate_session_skill")
	case *protocol.DeactivateSessionSkillParams:
		if typed == nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid deactivate_session_skill payload")
		}
		return normalizeSessionSkillMutationParams(typed.SessionID, typed.SkillID, "deactivate_session_skill")
	case map[string]any:
		return normalizeSessionSkillMutationParams(
			readStringValue(typed, "session_id"),
			readStringValue(typed, "skill_id"),
			"deactivate_session_skill",
		)
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid deactivate_session_skill payload")
		}
		var decoded protocol.DeactivateSessionSkillParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid deactivate_session_skill payload")
		}
		return normalizeSessionSkillMutationParams(decoded.SessionID, decoded.SkillID, "deactivate_session_skill")
	}
}

// decodeListSessionSkillsPayload 解析 list_session_skills 负载并收敛为统一输入结构。
func decodeListSessionSkillsPayload(payload any) (listSessionSkillsParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.ListSessionSkillsParams:
		return normalizeListSessionSkillsParams(typed.SessionID), nil
	case *protocol.ListSessionSkillsParams:
		if typed == nil {
			return listSessionSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_session_skills payload")
		}
		return normalizeListSessionSkillsParams(typed.SessionID), nil
	case map[string]any:
		return normalizeListSessionSkillsParams(readStringValue(typed, "session_id")), nil
	case nil:
		return listSessionSkillsParams{}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return listSessionSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_session_skills payload")
		}
		var decoded protocol.ListSessionSkillsParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return listSessionSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_session_skills payload")
		}
		return normalizeListSessionSkillsParams(decoded.SessionID), nil
	}
}

// decodeListAvailableSkillsPayload 解析 list_available_skills 负载并收敛为统一输入结构。
func decodeListAvailableSkillsPayload(payload any) (listAvailableSkillsParams, *FrameError) {
	switch typed := payload.(type) {
	case protocol.ListAvailableSkillsParams:
		return normalizeListAvailableSkillsParams(typed.SessionID), nil
	case *protocol.ListAvailableSkillsParams:
		if typed == nil {
			return listAvailableSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_available_skills payload")
		}
		return normalizeListAvailableSkillsParams(typed.SessionID), nil
	case map[string]any:
		return normalizeListAvailableSkillsParams(readStringValue(typed, "session_id")), nil
	case nil:
		return listAvailableSkillsParams{}, nil
	default:
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return listAvailableSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_available_skills payload")
		}
		var decoded protocol.ListAvailableSkillsParams
		if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr != nil {
			return listAvailableSkillsParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid list_available_skills payload")
		}
		return normalizeListAvailableSkillsParams(decoded.SessionID), nil
	}
}

// normalizeSessionSkillMutationParams 校验并归一化会话技能启停请求参数。
func normalizeSessionSkillMutationParams(
	sessionID string,
	skillID string,
	operation string,
) (sessionSkillMutationParams, *FrameError) {
	normalized := sessionSkillMutationParams{
		SessionID: strings.TrimSpace(sessionID),
		SkillID:   strings.TrimSpace(skillID),
	}
	if normalized.SessionID == "" {
		return sessionSkillMutationParams{}, NewMissingRequiredFieldError("payload.session_id")
	}
	if normalized.SkillID == "" {
		return sessionSkillMutationParams{}, NewMissingRequiredFieldError("payload.skill_id")
	}
	if strings.TrimSpace(operation) == "" {
		return sessionSkillMutationParams{}, NewFrameError(ErrorCodeInvalidAction, "invalid session_skill payload")
	}
	return normalized, nil
}

// normalizeListSessionSkillsParams 归一化 list_session_skills 请求参数。
func normalizeListSessionSkillsParams(sessionID string) listSessionSkillsParams {
	return listSessionSkillsParams{
		SessionID: strings.TrimSpace(sessionID),
	}
}

// normalizeListAvailableSkillsParams 归一化 list_available_skills 请求参数。
func normalizeListAvailableSkillsParams(sessionID string) listAvailableSkillsParams {
	return listAvailableSkillsParams{
		SessionID: strings.TrimSpace(sessionID),
	}
}

// normalizeBindStreamParams 校验并归一化 bind_stream 请求参数。
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

// readStringValue 读取 map 负载中的字符串字段并去空白。
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

// decodeWakeIntent 将任意 payload 解码为 WakeIntent。
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

// normalizeWakeIntent 归一化 WakeIntent 的关键字段。
func normalizeWakeIntent(intent protocol.WakeIntent) protocol.WakeIntent {
	intent.Action = strings.ToLower(strings.TrimSpace(intent.Action))
	intent.SessionID = strings.TrimSpace(intent.SessionID)
	intent.Workdir = strings.TrimSpace(intent.Workdir)
	if len(intent.Params) == 0 {
		intent.Params = nil
	}
	return intent
}

// toFrameError 将 wake handler 错误映射为网关稳定错误码。
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
