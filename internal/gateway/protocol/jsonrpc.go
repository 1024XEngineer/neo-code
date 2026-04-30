package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	// JSONRPCVersion 表示当前网关控制面固定使用的 JSON-RPC 协议版本。
	JSONRPCVersion = "2.0"
)

const (
	// MethodGatewayAuthenticate 表示连接握手认证方法。
	MethodGatewayAuthenticate = "gateway.authenticate"
	// MethodGatewayPing 表示网关探活方法。
	MethodGatewayPing = "gateway.ping"
	// MethodGatewayBindStream 表示客户端向网关声明流式订阅绑定的方法。
	MethodGatewayBindStream = "gateway.bindStream"
	// MethodGatewayRun 表示通过网关触发一次运行时执行。
	MethodGatewayRun = "gateway.run"
	// MethodGatewayCompact 表示通过网关触发一次会话压缩。
	MethodGatewayCompact = "gateway.compact"
	// MethodGatewayExecuteSystemTool 表示通过网关触发一次系统工具执行。
	MethodGatewayExecuteSystemTool = "gateway.executeSystemTool"
	// MethodGatewayActivateSessionSkill 表示通过网关在会话内激活技能。
	MethodGatewayActivateSessionSkill = "gateway.activateSessionSkill"
	// MethodGatewayDeactivateSessionSkill 表示通过网关在会话内停用技能。
	MethodGatewayDeactivateSessionSkill = "gateway.deactivateSessionSkill"
	// MethodGatewayListSessionSkills 表示通过网关查询会话激活技能列表。
	MethodGatewayListSessionSkills = "gateway.listSessionSkills"
	// MethodGatewayListAvailableSkills 表示通过网关查询可用技能列表。
	MethodGatewayListAvailableSkills = "gateway.listAvailableSkills"
	// MethodGatewayCancel 表示取消当前活跃运行。
	MethodGatewayCancel = "gateway.cancel"
	// MethodGatewayListSessions 表示查询会话摘要列表。
	MethodGatewayListSessions = "gateway.listSessions"
	// MethodGatewayLoadSession 表示加载单个会话详情。
	MethodGatewayLoadSession = "gateway.loadSession"
	// MethodGatewayListSessionTodos 表示查询会话 Todo 快照。
	MethodGatewayListSessionTodos = "session.todos.list"
	// MethodGatewayResolvePermission 表示提交权限审批决策。
	MethodGatewayResolvePermission = "gateway.resolvePermission"
	// MethodGatewayEvent 表示网关向客户端推送运行时事件的通知方法。
	MethodGatewayEvent = "gateway.event"
	// MethodWakeOpenURL 表示 URL Scheme 唤醒方法。
	MethodWakeOpenURL = "wake.openUrl"
)

const (
	// JSONRPCCodeParseError 表示请求体不是合法 JSON。
	JSONRPCCodeParseError = -32700
	// JSONRPCCodeInvalidRequest 表示请求结构不符合 JSON-RPC 规范。
	JSONRPCCodeInvalidRequest = -32600
	// JSONRPCCodeMethodNotFound 表示方法未注册。
	JSONRPCCodeMethodNotFound = -32601
	// JSONRPCCodeInvalidParams 表示参数不合法。
	JSONRPCCodeInvalidParams = -32602
	// JSONRPCCodeInternalError 表示服务端内部错误。
	JSONRPCCodeInternalError = -32603
)

const (
	// GatewayCodeInvalidFrame 表示请求帧结构非法。
	GatewayCodeInvalidFrame = "invalid_frame"
	// GatewayCodeInvalidAction 表示动作参数非法。
	GatewayCodeInvalidAction = "invalid_action"
	// GatewayCodeInvalidMultimodalPayload 表示多模态载荷非法。
	GatewayCodeInvalidMultimodalPayload = "invalid_multimodal_payload"
	// GatewayCodeMissingRequiredField 表示缺少必填字段。
	GatewayCodeMissingRequiredField = "missing_required_field"
	// GatewayCodeUnsupportedAction 表示动作尚未实现。
	GatewayCodeUnsupportedAction = "unsupported_action"
	// GatewayCodeInternalError 表示网关内部错误。
	GatewayCodeInternalError = "internal_error"
	// GatewayCodeTimeout 表示网关处理请求时发生超时。
	GatewayCodeTimeout = "timeout"
	// GatewayCodeUnsafePath 表示路径存在安全风险。
	GatewayCodeUnsafePath = "unsafe_path"
	// GatewayCodeUnauthorized 表示请求未通过认证校验。
	GatewayCodeUnauthorized = "unauthorized"
	// GatewayCodeAccessDenied 表示请求已认证但未通过 ACL 校验。
	GatewayCodeAccessDenied     = "access_denied"
	GatewayCodeResourceNotFound = "resource_not_found"
)

// JSONRPCRequest 表示控制面接收到的 JSON-RPC 请求。
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse 表示控制面输出的 JSON-RPC 响应。
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCNotification 表示控制面向客户端主动推送的 JSON-RPC 通知。
type JSONRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCError 表示 JSON-RPC 错误载荷。
type JSONRPCError struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    *JSONRPCErrorData `json:"data,omitempty"`
}

// JSONRPCErrorData 表示网关扩展错误字段。
type JSONRPCErrorData struct {
	GatewayCode string `json:"gateway_code,omitempty"`
}

// NormalizedRequest 表示从 JSON-RPC 归一化后的内部请求模型。
type NormalizedRequest struct {
	ID        json.RawMessage
	RequestID string
	Action    string
	SessionID string
	RunID     string
	Workdir   string
	Payload   any
}

// AuthenticateParams 表示 gateway.authenticate 的标准化参数。
type AuthenticateParams struct {
	Token string `json:"token"`
}

// BindStreamParams 表示 gateway.bindStream 的标准化参数载荷。
type BindStreamParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

// RunInputMedia 用于承载 gateway.run 中图片分片的媒体元数据。
type RunInputMedia struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type"`
	FileName string `json:"file_name,omitempty"`
}

// RunInputPart 表示 gateway.run 中的单个输入分片。
type RunInputPart struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	Media *RunInputMedia `json:"media,omitempty"`
}

// RunParams 表示 gateway.run 的参数载荷。
type RunParams struct {
	SessionID  string         `json:"session_id,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	InputText  string         `json:"input_text,omitempty"`
	InputParts []RunInputPart `json:"input_parts,omitempty"`
	Workdir    string         `json:"workdir,omitempty"`
}

// CancelParams 表示 gateway.cancel 可选参数。
type CancelParams struct {
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
}

// CompactParams 表示 gateway.compact 参数。
type CompactParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
}

// ExecuteSystemToolParams 表示 gateway.executeSystemTool 参数。
type ExecuteSystemToolParams struct {
	SessionID string          `json:"session_id,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ActivateSessionSkillParams 表示 gateway.activateSessionSkill 参数。
type ActivateSessionSkillParams struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
}

// DeactivateSessionSkillParams 表示 gateway.deactivateSessionSkill 参数。
type DeactivateSessionSkillParams struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
}

// ListSessionSkillsParams 表示 gateway.listSessionSkills 参数。
type ListSessionSkillsParams struct {
	SessionID string `json:"session_id"`
}

// ListAvailableSkillsParams 表示 gateway.listAvailableSkills 参数。
type ListAvailableSkillsParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// LoadSessionParams 表示 gateway.loadSession 参数。
type LoadSessionParams struct {
	SessionID string `json:"session_id"`
}

// ListSessionTodosParams 表示 session.todos.list 参数。
type ListSessionTodosParams struct {
	SessionID string `json:"session_id"`
}

// ResolvePermissionParams 表示 gateway.resolvePermission 参数。
type ResolvePermissionParams struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

// NormalizeJSONRPCRequest 将 JSON-RPC 请求归一化为内部请求模型，并做方法级参数解析。
func NormalizeJSONRPCRequest(request JSONRPCRequest) (NormalizedRequest, *JSONRPCError) {
	normalized := NormalizedRequest{}

	requestID, idErr := normalizeJSONRPCID(request.ID)
	normalized.RequestID = requestID
	if idErr != nil {
		return normalized, idErr
	}
	normalized.ID = cloneJSONRawMessage(request.ID)

	if strings.TrimSpace(request.JSONRPC) != JSONRPCVersion {
		return normalized, NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid jsonrpc version",
			GatewayCodeInvalidFrame,
		)
	}

	method := strings.TrimSpace(request.Method)
	if method == "" {
		return normalized, NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"missing required field: method",
			GatewayCodeMissingRequiredField,
		)
	}

	switch method {
	case MethodGatewayAuthenticate:
		params, parseErr := decodeAuthenticateParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "authenticate"
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayPing:
		normalized.Action = "ping"
		return normalized, nil
	case MethodGatewayBindStream:
		params, parseErr := decodeBindStreamParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "bind_stream"
		normalized.SessionID = params.SessionID
		normalized.RunID = params.RunID
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayRun:
		params, parseErr := decodeRunParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "run"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Workdir = strings.TrimSpace(params.Workdir)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayCompact:
		params, parseErr := decodeCompactParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "compact"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayExecuteSystemTool:
		params, parseErr := decodeExecuteSystemToolParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "execute_system_tool"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Workdir = strings.TrimSpace(params.Workdir)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayActivateSessionSkill:
		params, parseErr := decodeActivateSessionSkillParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "activate_session_skill"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayDeactivateSessionSkill:
		params, parseErr := decodeDeactivateSessionSkillParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "deactivate_session_skill"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessionSkills:
		params, parseErr := decodeListSessionSkillsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_session_skills"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListAvailableSkills:
		params, parseErr := decodeListAvailableSkillsParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "list_available_skills"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayCancel:
		params, parseErr := decodeCancelParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "cancel"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.RunID = strings.TrimSpace(params.RunID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessions:
		normalized.Action = "list_sessions"
		return normalized, nil
	case MethodGatewayLoadSession:
		params, parseErr := decodeLoadSessionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "load_session"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayListSessionTodos:
		params, parseErr := decodeListSessionTodosParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "session_todos_list"
		normalized.SessionID = strings.TrimSpace(params.SessionID)
		normalized.Payload = params
		return normalized, nil
	case MethodGatewayResolvePermission:
		params, parseErr := decodeResolvePermissionParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = "resolve_permission"
		normalized.Payload = params
		return normalized, nil
	case MethodWakeOpenURL:
		intent, parseErr := decodeWakeIntentParams(request.Params)
		if parseErr != nil {
			return normalized, parseErr
		}
		normalized.Action = MethodWakeOpenURL
		normalized.SessionID = strings.TrimSpace(intent.SessionID)
		normalized.Workdir = strings.TrimSpace(intent.Workdir)
		normalized.Payload = intent
		return normalized, nil
	default:
		return normalized, NewJSONRPCError(
			JSONRPCCodeMethodNotFound,
			"method not found",
			GatewayCodeUnsupportedAction,
		)
	}
}

// decodeListSessionTodosParams 对 session.todos.list 的 params 执行反序列化与字段校验。
func decodeListSessionTodosParams(raw json.RawMessage) (ListSessionTodosParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}
	var params ListSessionTodosParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for session.todos.list",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	if params.SessionID == "" {
		return ListSessionTodosParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// NewJSONRPCResultResponse 创建 JSON-RPC 成功响应，并将 result 编码为 RawMessage。
func NewJSONRPCResultResponse(id json.RawMessage, result any) (JSONRPCResponse, *JSONRPCError) {
	rawResult, err := json.Marshal(result)
	if err != nil {
		return JSONRPCResponse{}, NewJSONRPCError(
			JSONRPCCodeInternalError,
			"failed to encode jsonrpc result",
			GatewayCodeInternalError,
		)
	}

	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      cloneJSONRawMessage(id),
		Result:  json.RawMessage(rawResult),
	}, nil
}

// NewJSONRPCErrorResponse 创建 JSON-RPC 错误响应。
func NewJSONRPCErrorResponse(id json.RawMessage, rpcError *JSONRPCError) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      cloneJSONRawMessage(id),
		Error:   rpcError,
	}
}

// NewJSONRPCNotification 创建 JSON-RPC 通知载荷，供网关向客户端推送事件使用。
func NewJSONRPCNotification(method string, params any) JSONRPCNotification {
	return JSONRPCNotification{
		JSONRPC: JSONRPCVersion,
		Method:  strings.TrimSpace(method),
		Params:  params,
	}
}

// NewJSONRPCError 创建带 gateway_code 的 JSON-RPC 错误对象。
func NewJSONRPCError(code int, message, gatewayCode string) *JSONRPCError {
	errorPayload := &JSONRPCError{
		Code:    code,
		Message: message,
	}
	if strings.TrimSpace(gatewayCode) != "" {
		errorPayload.Data = &JSONRPCErrorData{GatewayCode: gatewayCode}
	}
	return errorPayload
}

// GatewayCodeFromJSONRPCError 从 JSON-RPC 错误载荷中提取稳定 gateway_code。
func GatewayCodeFromJSONRPCError(rpcError *JSONRPCError) string {
	if rpcError == nil || rpcError.Data == nil {
		return ""
	}
	return strings.TrimSpace(rpcError.Data.GatewayCode)
}

// MapGatewayCodeToJSONRPCCode 将稳定网关错误码映射到 JSON-RPC 错误码。
func MapGatewayCodeToJSONRPCCode(gatewayCode string) int {
	switch strings.TrimSpace(gatewayCode) {
	case GatewayCodeUnsupportedAction:
		return JSONRPCCodeMethodNotFound
	case GatewayCodeInvalidAction,
		GatewayCodeInvalidFrame,
		GatewayCodeInvalidMultimodalPayload,
		GatewayCodeMissingRequiredField,
		GatewayCodeUnsafePath,
		GatewayCodeUnauthorized,
		GatewayCodeAccessDenied,
		GatewayCodeResourceNotFound:
		return JSONRPCCodeInvalidParams
	case GatewayCodeInternalError:
		return JSONRPCCodeInternalError
	case GatewayCodeTimeout:
		return JSONRPCCodeInternalError
	default:
		return JSONRPCCodeInternalError
	}
}

// normalizeJSONRPCID 校验并提取请求 ID，确保控制面请求具备可关联标识。
func normalizeJSONRPCID(id json.RawMessage) (string, *JSONRPCError) {
	trimmed := bytes.TrimSpace(id)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"missing required field: id",
			GatewayCodeMissingRequiredField,
		)
	}

	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid field: id",
			GatewayCodeInvalidFrame,
		)
	}

	switch value := decoded.(type) {
	case string:
		identifier := strings.TrimSpace(value)
		if identifier == "" {
			return "", NewJSONRPCError(
				JSONRPCCodeInvalidRequest,
				"invalid field: id",
				GatewayCodeInvalidFrame,
			)
		}
		return identifier, nil
	case float64:
		identifier := strings.TrimSpace(string(trimmed))
		if identifier == "" {
			return "", NewJSONRPCError(
				JSONRPCCodeInvalidRequest,
				"invalid field: id",
				GatewayCodeInvalidFrame,
			)
		}
		return identifier, nil
	default:
		return "", NewJSONRPCError(
			JSONRPCCodeInvalidRequest,
			"invalid field: id",
			GatewayCodeInvalidFrame,
		)
	}
}

// decodeStrictJSON 使用 DisallowUnknownFields 对 params 做严格反序列化。
func decodeStrictJSON(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing json values")
	}
	return nil
}

// decodeAuthenticateParams 对 gateway.authenticate 的 params 执行反序列化与最小校验。
func decodeAuthenticateParams(raw json.RawMessage) (AuthenticateParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return AuthenticateParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params AuthenticateParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return AuthenticateParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.authenticate",
			GatewayCodeInvalidFrame,
		)
	}
	params.Token = strings.TrimSpace(params.Token)
	if params.Token == "" {
		return AuthenticateParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.token",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeWakeIntentParams 对 wake.openUrl 的 params 执行延迟反序列化与最小校验。
func decodeWakeIntentParams(raw json.RawMessage) (WakeIntent, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return WakeIntent{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var intent WakeIntent
	if err := decodeStrictJSON(trimmed, &intent); err != nil {
		return WakeIntent{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for wake.openUrl",
			GatewayCodeInvalidFrame,
		)
	}
	intent.Action = strings.ToLower(strings.TrimSpace(intent.Action))
	intent.SessionID = strings.TrimSpace(intent.SessionID)
	intent.Workdir = strings.TrimSpace(intent.Workdir)
	if len(intent.Params) == 0 {
		intent.Params = nil
	}
	return intent, nil
}

// decodeBindStreamParams 对 gateway.bindStream 的 params 执行反序列化与最小参数校验。
func decodeBindStreamParams(raw json.RawMessage) (BindStreamParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return BindStreamParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params BindStreamParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return BindStreamParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.bindStream",
			GatewayCodeInvalidFrame,
		)
	}

	params.SessionID = strings.TrimSpace(params.SessionID)
	params.RunID = strings.TrimSpace(params.RunID)
	params.Channel = strings.ToLower(strings.TrimSpace(params.Channel))
	if params.Channel == "" {
		params.Channel = "all"
	}

	if params.SessionID == "" {
		return BindStreamParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}

	switch params.Channel {
	case "all", "ipc", "ws", "sse":
	default:
		return BindStreamParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid field: params.channel",
			GatewayCodeInvalidAction,
		)
	}

	return params, nil
}

// decodeRunParams 对 gateway.run 的 params 执行反序列化与字段清理。
func decodeRunParams(raw json.RawMessage) (RunParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return RunParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params RunParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return RunParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.run",
			GatewayCodeInvalidFrame,
		)
	}

	params.SessionID = strings.TrimSpace(params.SessionID)
	params.RunID = strings.TrimSpace(params.RunID)
	params.InputText = strings.TrimSpace(params.InputText)
	params.Workdir = strings.TrimSpace(params.Workdir)
	if len(params.InputParts) == 0 {
		params.InputParts = nil
	} else {
		for index := range params.InputParts {
			params.InputParts[index].Type = strings.ToLower(strings.TrimSpace(params.InputParts[index].Type))
			params.InputParts[index].Text = strings.TrimSpace(params.InputParts[index].Text)
			if params.InputParts[index].Media != nil {
				params.InputParts[index].Media.URI = strings.TrimSpace(params.InputParts[index].Media.URI)
				params.InputParts[index].Media.MimeType = strings.TrimSpace(params.InputParts[index].Media.MimeType)
				params.InputParts[index].Media.FileName = strings.TrimSpace(params.InputParts[index].Media.FileName)
			}
		}
	}
	return params, nil
}

// decodeCancelParams 对 gateway.cancel 的 params 执行反序列化，缺省或 null 视为空参数。
func decodeCancelParams(raw json.RawMessage) (CancelParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return CancelParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params CancelParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return CancelParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.cancel",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	params.RunID = strings.TrimSpace(params.RunID)
	if params.RunID == "" {
		return CancelParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.run_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeCompactParams 对 gateway.compact 的 params 执行反序列化与必填字段校验。
func decodeCompactParams(raw json.RawMessage) (CompactParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return CompactParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params CompactParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return CompactParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.compact",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	params.RunID = strings.TrimSpace(params.RunID)
	if params.SessionID == "" {
		return CompactParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeExecuteSystemToolParams 对 gateway.executeSystemTool 的 params 执行反序列化与字段校验。
func decodeExecuteSystemToolParams(raw json.RawMessage) (ExecuteSystemToolParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ExecuteSystemToolParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params ExecuteSystemToolParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ExecuteSystemToolParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.executeSystemTool",
			GatewayCodeInvalidFrame,
		)
	}

	params.SessionID = strings.TrimSpace(params.SessionID)
	params.RunID = strings.TrimSpace(params.RunID)
	params.Workdir = strings.TrimSpace(params.Workdir)
	params.ToolName = strings.TrimSpace(params.ToolName)
	params.Arguments = cloneJSONRawMessage(bytes.TrimSpace(params.Arguments))

	if params.ToolName == "" {
		return ExecuteSystemToolParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.tool_name",
			GatewayCodeMissingRequiredField,
		)
	}

	return params, nil
}

// decodeActivateSessionSkillParams 对 gateway.activateSessionSkill 的 params 执行反序列化与字段校验。
func decodeActivateSessionSkillParams(raw json.RawMessage) (ActivateSessionSkillParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ActivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params ActivateSessionSkillParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ActivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.activateSessionSkill",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	params.SkillID = strings.TrimSpace(params.SkillID)
	if params.SessionID == "" {
		return ActivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	if params.SkillID == "" {
		return ActivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.skill_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeDeactivateSessionSkillParams 对 gateway.deactivateSessionSkill 的 params 执行反序列化与字段校验。
func decodeDeactivateSessionSkillParams(raw json.RawMessage) (DeactivateSessionSkillParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return DeactivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params DeactivateSessionSkillParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return DeactivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.deactivateSessionSkill",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	params.SkillID = strings.TrimSpace(params.SkillID)
	if params.SessionID == "" {
		return DeactivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	if params.SkillID == "" {
		return DeactivateSessionSkillParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.skill_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeListSessionSkillsParams 对 gateway.listSessionSkills 的 params 执行反序列化与字段校验。
func decodeListSessionSkillsParams(raw json.RawMessage) (ListSessionSkillsParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ListSessionSkillsParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params ListSessionSkillsParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ListSessionSkillsParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.listSessionSkills",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	if params.SessionID == "" {
		return ListSessionSkillsParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeListAvailableSkillsParams 对 gateway.listAvailableSkills 的 params 执行反序列化与字段清理。
func decodeListAvailableSkillsParams(raw json.RawMessage) (ListAvailableSkillsParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ListAvailableSkillsParams{}, nil
	}

	var params ListAvailableSkillsParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ListAvailableSkillsParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.listAvailableSkills",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	return params, nil
}

// decodeLoadSessionParams 对 gateway.loadSession 的 params 执行反序列化与必填字段校验。
func decodeLoadSessionParams(raw json.RawMessage) (LoadSessionParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return LoadSessionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params LoadSessionParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return LoadSessionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.loadSession",
			GatewayCodeInvalidFrame,
		)
	}
	params.SessionID = strings.TrimSpace(params.SessionID)
	if params.SessionID == "" {
		return LoadSessionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.session_id",
			GatewayCodeMissingRequiredField,
		)
	}
	return params, nil
}

// decodeResolvePermissionParams 对 gateway.resolvePermission 的 params 执行反序列化与决策校验。
func decodeResolvePermissionParams(raw json.RawMessage) (ResolvePermissionParams, *JSONRPCError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ResolvePermissionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params",
			GatewayCodeMissingRequiredField,
		)
	}

	var params ResolvePermissionParams
	if err := decodeStrictJSON(trimmed, &params); err != nil {
		return ResolvePermissionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid params for gateway.resolvePermission",
			GatewayCodeInvalidFrame,
		)
	}
	params.RequestID = strings.TrimSpace(params.RequestID)
	params.Decision = strings.ToLower(strings.TrimSpace(params.Decision))
	if params.RequestID == "" {
		return ResolvePermissionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"missing required field: params.request_id",
			GatewayCodeMissingRequiredField,
		)
	}
	switch params.Decision {
	case "allow_once", "allow_session", "reject":
	default:
		return ResolvePermissionParams{}, NewJSONRPCError(
			JSONRPCCodeInvalidParams,
			"invalid field: params.decision",
			GatewayCodeInvalidAction,
		)
	}
	return params, nil
}

// cloneJSONRawMessage 复制 RawMessage，避免共享底层切片导致的并发风险。
func cloneJSONRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}
