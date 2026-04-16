package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	// JSONRPCVersion 表示当前网关控制面固定使用的 JSON-RPC 协议版本。
	JSONRPCVersion = "2.0"
)

const (
	// MethodGatewayPing 表示网关探活方法。
	MethodGatewayPing = "gateway.ping"
	// MethodGatewayBindStream 表示客户端向网关声明流式订阅绑定的方法。
	MethodGatewayBindStream = "gateway.bindStream"
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
	// GatewayCodeInvalidMultimodalPayload 表示多模态负载非法。
	GatewayCodeInvalidMultimodalPayload = "invalid_multimodal_payload"
	// GatewayCodeMissingRequiredField 表示缺少必填字段。
	GatewayCodeMissingRequiredField = "missing_required_field"
	// GatewayCodeUnsupportedAction 表示动作尚未实现。
	GatewayCodeUnsupportedAction = "unsupported_action"
	// GatewayCodeInternalError 表示网关内部错误。
	GatewayCodeInternalError = "internal_error"
	// GatewayCodeUnsafePath 表示路径存在安全风险。
	GatewayCodeUnsafePath = "unsafe_path"
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

// JSONRPCError 表示 JSON-RPC 错误负载。
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

// BindStreamParams 表示 gateway.bindStream 的标准化参数载荷。
type BindStreamParams struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
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

// NewJSONRPCNotification 创建 JSON-RPC 通知负载，供网关向客户端推送事件使用。
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

// GatewayCodeFromJSONRPCError 从 JSON-RPC 错误负载中提取稳定 gateway_code。
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
		GatewayCodeUnsafePath:
		return JSONRPCCodeInvalidParams
	case GatewayCodeInternalError:
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
	if err := json.Unmarshal(trimmed, &intent); err != nil {
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
	if err := json.Unmarshal(trimmed, &params); err != nil {
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

// cloneJSONRawMessage 复制 RawMessage，避免共享底层切片导致的并发风险。
func cloneJSONRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}
