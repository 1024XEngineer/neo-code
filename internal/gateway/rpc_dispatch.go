package gateway

import (
	"context"
	"log"
	"strings"

	"neo-code/internal/gateway/protocol"
)

// dispatchRPCRequest 统一将 JSON-RPC 请求归一化并分发到网关内部 MessageFrame 处理链路。
func dispatchRPCRequest(ctx context.Context, request protocol.JSONRPCRequest, runtimePort RuntimePort) protocol.JSONRPCResponse {
	startedAt := requestStartTime()
	method := strings.TrimSpace(request.Method)
	metricMethod := normalizeMethodMetricLabel(method)
	source := string(RequestSourceFromContext(ctx))
	metrics, _ := GatewayMetricsFromContext(ctx)

	normalized, rpcErr := protocol.NormalizeJSONRPCRequest(request)
	if rpcErr != nil {
		if metrics != nil {
			metrics.IncRequests(source, metricMethod, "error")
		}
		emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
			RequestID:   "",
			SessionID:   "",
			Method:      method,
			Source:      source,
			Status:      "error",
			GatewayCode: protocol.GatewayCodeFromJSONRPCError(rpcErr),
			LatencyMS:   requestLatencyMS(startedAt),
		})
		return protocol.NewJSONRPCErrorResponse(normalized.ID, rpcErr)
	}

	if authErr := authorizeRPCRequest(ctx, request.Method, normalized.Action); authErr != nil {
		if metrics != nil {
			metrics.IncRequests(source, metricMethod, "error")
			if gatewayCode := protocol.GatewayCodeFromJSONRPCError(authErr); gatewayCode == ErrorCodeUnauthorized.String() {
				metrics.IncAuthFailures(source, gatewayCode)
			}
			if gatewayCode := protocol.GatewayCodeFromJSONRPCError(authErr); gatewayCode == ErrorCodeAccessDenied.String() {
				metrics.IncACLDenied(source, metricMethod)
			}
		}
		emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
			RequestID:   normalized.RequestID,
			SessionID:   normalized.SessionID,
			Method:      method,
			Source:      source,
			Status:      "error",
			GatewayCode: protocol.GatewayCodeFromJSONRPCError(authErr),
			LatencyMS:   requestLatencyMS(startedAt),
		})
		return protocol.NewJSONRPCErrorResponse(normalized.ID, authErr)
	}

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameAction(normalized.Action),
		RequestID: normalized.RequestID,
		SessionID: normalized.SessionID,
		RunID:     normalized.RunID,
		Workdir:   normalized.Workdir,
		Mode:      normalized.Mode,
		Payload:   normalized.Payload,
	}

	frame = hydrateFrameRunPayload(frame)
	frame = hydrateFrameSessionFromConnection(ctx, frame)
	if requiresSession(frame.Action) && strings.TrimSpace(frame.SessionID) == "" {
		if metrics != nil {
			metrics.IncRequests(source, metricMethod, "error")
		}
		emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
			RequestID:   normalized.RequestID,
			SessionID:   normalized.SessionID,
			Method:      method,
			Source:      source,
			Status:      "error",
			GatewayCode: protocol.GatewayCodeMissingRequiredField,
			LatencyMS:   requestLatencyMS(startedAt),
		})
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
			if metrics != nil {
				metrics.IncRequests(source, metricMethod, "error")
			}
			emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
				RequestID:   normalized.RequestID,
				SessionID:   normalized.SessionID,
				Method:      method,
				Source:      source,
				Status:      "error",
				GatewayCode: protocol.GatewayCodeInternalError,
				LatencyMS:   requestLatencyMS(startedAt),
			})
			return protocol.NewJSONRPCErrorResponse(normalized.ID, encodeErr)
		}
		if metrics != nil {
			metrics.IncRequests(source, metricMethod, "ok")
		}
		emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
			RequestID: normalized.RequestID,
			SessionID: responseFrame.SessionID,
			Method:    method,
			Source:    source,
			Status:    "ok",
			LatencyMS: requestLatencyMS(startedAt),
		})
		return rpcResponse
	}

	frameErr := responseFrame.Error
	if frameErr == nil {
		frameErr = NewFrameError(ErrorCodeInternalError, "gateway response missing error payload")
	}
	rpcResponse := protocol.NewJSONRPCErrorResponse(
		normalized.ID,
		protocol.NewJSONRPCError(
			protocol.MapGatewayCodeToJSONRPCCode(frameErr.Code),
			frameErr.Message,
			frameErr.Code,
		),
	)
	if metrics != nil {
		metrics.IncRequests(source, metricMethod, "error")
		if frameErr.Code == ErrorCodeUnauthorized.String() {
			metrics.IncAuthFailures(source, frameErr.Code)
		}
		if frameErr.Code == ErrorCodeAccessDenied.String() {
			metrics.IncACLDenied(source, metricMethod)
		}
	}
	emitRequestLog(ctx, nilSafeLoggerFromContext(ctx), RequestLogEntry{
		RequestID:   normalized.RequestID,
		SessionID:   normalized.SessionID,
		Method:      method,
		Source:      source,
		Status:      "error",
		GatewayCode: frameErr.Code,
		LatencyMS:   requestLatencyMS(startedAt),
	})
	return rpcResponse
}

// authorizeRPCRequest 统一执行控制面认证与 ACL 授权。
func authorizeRPCRequest(ctx context.Context, method, action string) *protocol.JSONRPCError {
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	normalizedMethod := strings.ToLower(strings.TrimSpace(method))
	if normalizedAction == string(FrameActionAuthenticate) {
		if !isMethodAllowedByACL(ctx, method) {
			return protocol.NewJSONRPCError(
				protocol.MapGatewayCodeToJSONRPCCode(ErrorCodeAccessDenied.String()),
				"access denied",
				ErrorCodeAccessDenied.String(),
			)
		}
		return nil
	}
	if RequestSourceFromContext(ctx) == RequestSourceIPC &&
		normalizedMethod == strings.ToLower(strings.TrimSpace(protocol.MethodWakeOpenURL)) {
		if !isMethodAllowedByACL(ctx, method) {
			return protocol.NewJSONRPCError(
				protocol.MapGatewayCodeToJSONRPCCode(ErrorCodeAccessDenied.String()),
				"access denied",
				ErrorCodeAccessDenied.String(),
			)
		}
		return nil
	}

	if !isRequestAuthenticated(ctx) {
		return protocol.NewJSONRPCError(
			protocol.MapGatewayCodeToJSONRPCCode(ErrorCodeUnauthorized.String()),
			"unauthorized",
			ErrorCodeUnauthorized.String(),
		)
	}
	if !isMethodAllowedByACL(ctx, method) {
		return protocol.NewJSONRPCError(
			protocol.MapGatewayCodeToJSONRPCCode(ErrorCodeAccessDenied.String()),
			"access denied",
			ErrorCodeAccessDenied.String(),
		)
	}
	return nil
}

// isRequestAuthenticated 判断请求是否处于已认证状态。
func isRequestAuthenticated(ctx context.Context) bool {
	authState, stateExists := ConnectionAuthStateFromContext(ctx)
	if stateExists && authState.IsAuthenticated() && strings.TrimSpace(authState.SubjectID()) != "" {
		return true
	}

	authenticator, hasAuthenticator := TokenAuthenticatorFromContext(ctx)
	if !hasAuthenticator {
		return true
	}
	requestToken := RequestTokenFromContext(ctx)
	if requestToken == "" {
		return false
	}

	subjectID, valid := authenticator.ResolveSubjectID(requestToken)
	if !valid || strings.TrimSpace(subjectID) == "" {
		return false
	}
	if stateExists {
		authState.MarkAuthenticated(subjectID)
	}
	return true
}

// isMethodAllowedByACL 按 source + method 判定 ACL 放行结果。
func isMethodAllowedByACL(ctx context.Context, method string) bool {
	acl, hasACL := RequestACLFromContext(ctx)
	if !hasACL {
		return true
	}
	source := RequestSourceFromContext(ctx)
	return acl.IsAllowed(source, method)
}

// nilSafeLoggerFromContext 返回上下文中注入的 logger，未注入时返回 nil。
func nilSafeLoggerFromContext(ctx context.Context) *log.Logger {
	logger, _ := GatewayLoggerFromContext(ctx)
	return logger
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
	// new_session 请求跳过绑定回填，由下游创建全新会话
	if frame.SkipSessionHydration {
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

// hydrateFrameRunPayload 将 gateway.run 的参数映射到 MessageFrame 统一字段，供后续校验与处理复用。
func hydrateFrameRunPayload(frame MessageFrame) MessageFrame {
	if frame.Action != FrameActionRun {
		return frame
	}

	var params protocol.RunParams
	switch typed := frame.Payload.(type) {
	case protocol.RunParams:
		params = typed
	case *protocol.RunParams:
		if typed == nil {
			return frame
		}
		params = *typed
	default:
		return frame
	}

	// new_session=true 时跳过连接绑定的 session_id 回填，让后端创建全新会话
	if params.NewSession {
		frame.SkipSessionHydration = true
	}

	if strings.TrimSpace(frame.SessionID) == "" {
		frame.SessionID = strings.TrimSpace(params.SessionID)
	}
	if strings.TrimSpace(frame.RunID) == "" {
		frame.RunID = strings.TrimSpace(params.RunID)
	}
	if strings.TrimSpace(frame.Workdir) == "" {
		frame.Workdir = strings.TrimSpace(params.Workdir)
	}
	if strings.TrimSpace(frame.Mode) == "" {
		frame.Mode = strings.TrimSpace(params.Mode)
	}
	if strings.TrimSpace(frame.InputText) == "" {
		frame.InputText = strings.TrimSpace(params.InputText)
	}
	if len(frame.InputParts) == 0 {
		frame.InputParts = convertProtocolRunInputParts(params.InputParts)
	}
	return frame
}

// convertProtocolRunInputParts 将 protocol 层 run input parts 转换为 gateway 协议分片结构。
func convertProtocolRunInputParts(parts []protocol.RunInputPart) []InputPart {
	if len(parts) == 0 {
		return nil
	}

	converted := make([]InputPart, 0, len(parts))
	for _, part := range parts {
		convertedPart := InputPart{
			Type: InputPartType(strings.ToLower(strings.TrimSpace(part.Type))),
			Text: strings.TrimSpace(part.Text),
		}
		if part.Media != nil {
			convertedPart.Media = &Media{
				URI:      strings.TrimSpace(part.Media.URI),
				MimeType: strings.TrimSpace(part.Media.MimeType),
				FileName: strings.TrimSpace(part.Media.FileName),
			}
		}
		converted = append(converted, convertedPart)
	}
	return converted
}

// requiresSession 判断指定动作在分发阶段是否必须携带 session_id。
func requiresSession(action FrameAction) bool {
	switch action {
	case FrameActionBindStream,
		FrameActionCompact,
		FrameActionLoadSession,
		FrameActionListSessionTodos,
		FrameActionGetRuntimeSnapshot,
		FrameActionActivateSessionSkill,
		FrameActionDeactivateSessionSkill,
		FrameActionListSessionSkills,
		FrameActionDeleteSession,
		FrameActionRenameSession,
		FrameActionSetSessionModel,
		FrameActionGetSessionModel:
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
	if frame.Action == FrameActionAuthenticate {
		return
	}

	relay.AutoBindFromFrame(connectionID, frame)
}
