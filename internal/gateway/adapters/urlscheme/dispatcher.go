package urlscheme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
)

const (
	// ErrorCodeGatewayUnavailable 表示无法连接本地网关进程。
	ErrorCodeGatewayUnavailable = "gateway_unreachable"
	// ErrorCodeUnexpectedResponse 表示网关返回了不符合预期的帧结构。
	ErrorCodeUnexpectedResponse = "unexpected_response"
	// ErrorCodeNotSupported 表示当前平台暂未实现目标能力。
	ErrorCodeNotSupported = "not_supported"
	// ErrorCodeInternal 表示调度器内部错误。
	ErrorCodeInternal = "internal_error"
	// defaultDispatchIOTimeout 表示单次调度读写超时时间。
	defaultDispatchIOTimeout = 10 * time.Second
)

var dispatchRequestCounter uint64

// DispatchRequest 表示 URL Scheme 调度输入参数。
type DispatchRequest struct {
	RawURL        string
	ListenAddress string
}

// DispatchResult 表示 URL Scheme 调度输出。
type DispatchResult struct {
	ListenAddress string               `json:"listen_address"`
	Request       gateway.MessageFrame `json:"request"`
	Response      gateway.MessageFrame `json:"response"`
}

// DispatchError 表示调度过程中的结构化错误。
type DispatchError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error 返回调度错误文本。
func (e *DispatchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Dispatcher 负责将 neocode:// URL 转发到网关本地控制面。
type Dispatcher struct {
	resolveListenAddressFn func(string) (string, error)
	dialFn                 func(address string) (net.Conn, error)
	requestIDFn            func() string
}

// NewDispatcher 创建默认 URL Scheme 调度器。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		resolveListenAddressFn: transport.ResolveListenAddress,
		dialFn:                 transport.Dial,
		requestIDFn:            nextDispatchRequestID,
	}
}

// Dispatch 将 URL 映射为 wake.openUrl 请求，并通过 IPC 转发到网关。
func (d *Dispatcher) Dispatch(ctx context.Context, request DispatchRequest) (DispatchResult, error) {
	intent, err := protocol.ParseNeoCodeURL(request.RawURL)
	if err != nil {
		return DispatchResult{}, toDispatchError(err)
	}

	listenAddress, err := d.resolveListenAddressFn(request.ListenAddress)
	if err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("resolve listen address: %v", err))
	}

	conn, err := d.dialFn(listenAddress)
	if err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeGatewayUnavailable, fmt.Sprintf("dial gateway failed: %v", err))
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := applyDispatchDeadline(conn, ctx); err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("set connection deadline: %v", err))
	}

	requestFrame := gateway.MessageFrame{
		Type:      gateway.FrameTypeRequest,
		Action:    gateway.FrameActionWakeOpenURL,
		RequestID: d.requestIDFn(),
		SessionID: intent.SessionID,
		Workdir:   intent.Workdir,
		Payload:   intent,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(requestFrame); err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeInternal, fmt.Sprintf("write request frame: %v", err))
	}

	var responseFrame gateway.MessageFrame
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&responseFrame); err != nil {
		return DispatchResult{}, newDispatchError(ErrorCodeUnexpectedResponse, fmt.Sprintf("decode response frame: %v", err))
	}

	switch responseFrame.Type {
	case gateway.FrameTypeAck:
		return DispatchResult{
			ListenAddress: listenAddress,
			Request:       requestFrame,
			Response:      responseFrame,
		}, nil
	case gateway.FrameTypeError:
		if responseFrame.Error == nil {
			return DispatchResult{}, newDispatchError(ErrorCodeUnexpectedResponse, "gateway error frame missing error payload")
		}
		return DispatchResult{}, newDispatchError(responseFrame.Error.Code, responseFrame.Error.Message)
	default:
		return DispatchResult{}, newDispatchError(ErrorCodeUnexpectedResponse, fmt.Sprintf("unsupported response frame type: %s", responseFrame.Type))
	}
}

// Dispatch 使用默认调度器执行 URL 转发。
func Dispatch(ctx context.Context, request DispatchRequest) (DispatchResult, error) {
	return NewDispatcher().Dispatch(ctx, request)
}

// applyDispatchDeadline 为调度连接设置统一超时控制。
func applyDispatchDeadline(conn net.Conn, ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return conn.SetDeadline(time.Now().Add(defaultDispatchIOTimeout))
}

// toDispatchError 将不同来源错误转换为统一结构化错误。
func toDispatchError(err error) error {
	if err == nil {
		return nil
	}

	var parseErr *protocol.ParseError
	if errors.As(err, &parseErr) {
		return newDispatchError(parseErr.Code, parseErr.Message)
	}

	var dispatchErr *DispatchError
	if errors.As(err, &dispatchErr) {
		return dispatchErr
	}

	return newDispatchError(ErrorCodeInternal, err.Error())
}

// nextDispatchRequestID 生成 url-dispatch 请求唯一标识。
func nextDispatchRequestID() string {
	sequence := atomic.AddUint64(&dispatchRequestCounter, 1)
	return fmt.Sprintf("wake-%d", sequence)
}

// newDispatchError 创建结构化调度错误。
func newDispatchError(code, message string) *DispatchError {
	return &DispatchError{
		Code:    code,
		Message: message,
	}
}
