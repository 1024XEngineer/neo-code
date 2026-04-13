package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// Version 表示网关当前支持的 JSON-RPC 版本。
	Version = "2.0"
	// MethodCorePing 表示 core ping 请求方法名。
	MethodCorePing = "core.ping"
)

const (
	// ErrorCodeParseError 表示请求 JSON 解码失败。
	ErrorCodeParseError = -32700
	// ErrorCodeInvalidRequest 表示请求结构不合法。
	ErrorCodeInvalidRequest = -32600
	// ErrorCodeMethodNotFound 表示方法不存在。
	ErrorCodeMethodNotFound = -32601
	// ErrorCodeInternalError 表示处理流程内部错误。
	ErrorCodeInternalError = -32603
)

// Request 表示网关 JSON-RPC 请求结构。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response 表示网关 JSON-RPC 响应结构。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError 表示 JSON-RPC 错误负载。
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MethodHandler 表示单个 JSON-RPC 方法处理器。
type MethodHandler func(ctx context.Context, req Request) Response

// Router 负责 JSON-RPC 的解码、路由与编码。
type Router struct {
	handlers map[string]MethodHandler
}

// NewRouter 创建 JSON-RPC 路由器。
func NewRouter() *Router {
	return &Router{handlers: make(map[string]MethodHandler)}
}

// Register 注册一个方法处理器。
func (r *Router) Register(method string, handler MethodHandler) {
	if r == nil || strings.TrimSpace(method) == "" || handler == nil {
		return
	}
	r.handlers[method] = handler
}

// HandleRaw 处理原始 JSON 请求并返回编码后的响应。
func (r *Router) HandleRaw(ctx context.Context, payload []byte) []byte {
	request, err := DecodeRequest(payload)
	if err != nil {
		return mustEncodeResponse(NewErrorResponse(nil, ErrorCodeParseError, "parse error"))
	}
	response := r.handle(ctx, request)
	return mustEncodeResponse(response)
}

// DecodeRequest 负责把原始 JSON 解码为请求对象并做最小校验。
func DecodeRequest(payload []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	if strings.TrimSpace(req.JSONRPC) == "" {
		req.JSONRPC = Version
	}
	return req, nil
}

// EncodeResponse 把响应对象编码为 JSON 字节。
func EncodeResponse(resp Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode response: %w", err)
	}
	return data, nil
}

// NewSuccessResponse 构建标准成功响应。
func NewSuccessResponse(id json.RawMessage, result any) Response {
	return Response{
		JSONRPC: Version,
		ID:      id,
		Result:  result,
	}
}

// NewErrorResponse 构建标准错误响应。
func NewErrorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: Version,
		ID:      id,
		Error: &ResponseError{
			Code:    code,
			Message: message,
		},
	}
}

// handle 执行请求校验、方法路由与错误映射。
func (r *Router) handle(ctx context.Context, request Request) Response {
	if errResponse, ok := validateRequest(request); ok {
		return errResponse
	}
	handler, ok := r.handlers[request.Method]
	if !ok {
		return NewErrorResponse(request.ID, ErrorCodeMethodNotFound, "method not found")
	}
	return handler(ctx, request)
}

// validateRequest 校验请求中的协议版本与方法名字段。
func validateRequest(request Request) (Response, bool) {
	if strings.TrimSpace(request.JSONRPC) != Version {
		return NewErrorResponse(request.ID, ErrorCodeInvalidRequest, "invalid jsonrpc version"), true
	}
	if strings.TrimSpace(request.Method) == "" {
		return NewErrorResponse(request.ID, ErrorCodeInvalidRequest, "missing method"), true
	}
	return Response{}, false
}

// mustEncodeResponse 在编码失败时回退为内部错误响应。
func mustEncodeResponse(response Response) []byte {
	data, err := EncodeResponse(response)
	if err == nil {
		return data
	}
	fallback, _ := EncodeResponse(NewErrorResponse(nil, ErrorCodeInternalError, "internal error"))
	return fallback
}
