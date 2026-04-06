package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// 通用领域错误。
var (
	ErrDriverNotFound          = errors.New("provider driver not found")
	ErrDriverAlreadyRegistered = errors.New("provider: driver already registered")

	// 流级哨兵错误，用于区分可恢复/不可恢复的流中断原因。
	ErrStreamInterrupted = errors.New("provider: stream interrupted")
	ErrLineTooLong       = errors.New("provider: SSE line exceeds max length")
	ErrStreamTooLarge    = errors.New("provider: stream total size exceeds limit")
)

type ProviderErrorCode string

const (
	ErrorCodeAuthFailed ProviderErrorCode = "auth_failed"   // 认证失败（401）
	ErrorCodeForbidden  ProviderErrorCode = "forbidden"     // 权限不足（403）
	ErrorCodeNotFound   ProviderErrorCode = "not_found"     // 资源不存在（404）
	ErrorCodeClient     ProviderErrorCode = "client_error"  // 客户端请求错误（4xx，排除上述分类）
	ErrorCodeRateLimit  ProviderErrorCode = "rate_limited"  // 限流（429）
	ErrorCodeServer     ProviderErrorCode = "server_error"  // 服务端错误（5xx）
	ErrorCodeTimeout    ProviderErrorCode = "timeout"       // 超时
	ErrorCodeNetwork    ProviderErrorCode = "network_error" // 网络错误（连接拒绝、DNS 失败等）
	ErrorCodeUnknown    ProviderErrorCode = "unknown"       // 未知错误
)

// ProviderError 是 provider 层的领域错误类型。
type ProviderError struct {
	StatusCode int               // HTTP 状态码，0 表示非 HTTP 错误（如网络超时）
	Code       ProviderErrorCode // 语义化的错误分类
	Message    string            // 可读错误信息
	Retryable  bool              // 是否建议重试
}

func (e *ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("provider error (status=%d, code=%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("provider error (code=%s): %s", e.Code, e.Message)
}

// IsRetryableStatus 根据通用 HTTP 语义判断状态码是否可重试。
// 429（限流）和 5xx（服务端错误）视为可重试。
func IsRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		(statusCode >= http.StatusInternalServerError && statusCode < 600)
}

// classifyStatus 将 HTTP 状态码映射为 ProviderErrorCode。
func classifyStatus(statusCode int) ProviderErrorCode {
	switch statusCode {
	case http.StatusUnauthorized:
		return ErrorCodeAuthFailed
	case http.StatusForbidden:
		return ErrorCodeForbidden
	case http.StatusNotFound:
		return ErrorCodeNotFound
	case http.StatusTooManyRequests:
		return ErrorCodeRateLimit
	default:
		if statusCode >= 400 && statusCode < 500 {
			return ErrorCodeClient
		}
		if statusCode >= 500 && statusCode < 600 {
			return ErrorCodeServer
		}
	}
	return ErrorCodeUnknown
}

// NewProviderErrorFromStatus 根据 HTTP 状态码和消息构造 ProviderError。
func NewProviderErrorFromStatus(statusCode int, message string) *ProviderError {
	code := classifyStatus(statusCode)
	return &ProviderError{
		StatusCode: statusCode,
		Code:       code,
		Message:    message,
		Retryable:  IsRetryableStatus(statusCode),
	}
}

// NewNetworkProviderError 构造网络错误类型的 ProviderError（如连接超时、拒绝等）。
func NewNetworkProviderError(message string) *ProviderError {
	return &ProviderError{
		StatusCode: 0,
		Code:       ErrorCodeNetwork,
		Message:    message,
		Retryable:  true, // 网络抖动默认可重试
	}
}

// NewTimeoutProviderError 构造超时类型的 ProviderError。
func NewTimeoutProviderError(message string) *ProviderError {
	return &ProviderError{
		StatusCode: 0,
		Code:       ErrorCodeTimeout,
		Message:    message,
		Retryable:  true, // 超时默认可重试
	}
}

// MarkNonRetryable 将错误标记为不可重试，用于防止上层重试叠加放大。
//
// 若错误链中包含 *ProviderError，返回其 Retryable=false 的副本；
// 否则将原始错误包装为 *ProviderError{Code: ErrorCodeUnknown, Retryable: false}。
// 原始错误通过 Unwrap 保留，不影响 errors.Is/As 对原始哨兵的匹配。
func MarkNonRetryable(err error) error {
	var pErr *ProviderError
	if errors.As(err, &pErr) {
		clone := *pErr
		clone.Retryable = false
		return &clone
	}
	return &ProviderError{
		StatusCode: 0,
		Code:       ErrorCodeUnknown,
		Message:    err.Error(),
		Retryable:  false,
	}
}

// IsRecoverableStreamError 判断流读取错误是否可通过透明重连恢复。
//
// 不可恢复的情况：
//   - context 取消/超时（调用方主动终止）
//   - 缓冲区溢出（重连只会再次溢出）
//   - 认证失败等业务错误（重连无意义）
//
// 可恢复的情况：
//   - ProviderError 且 Retryable=true（5xx、429 等）
//   - 网络层临时错误（*net.OpError）
//   - ErrStreamInterrupted（通用流中断标记）
func IsRecoverableStreamError(err error) bool {
	if err == nil {
		return false
	}
	// context 取消 → 不可恢复
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// 缓冲区溢出 → 不可恢复（重连同样会溢出）
	if errors.Is(err, ErrLineTooLong) || errors.Is(err, ErrStreamTooLarge) {
		return false
	}
	// 流中断标记 → 可恢复
	if errors.Is(err, ErrStreamInterrupted) {
		return true
	}
	// ProviderError → 依据 Retryable 字段
	var pErr *ProviderError
	if errors.As(err, &pErr) {
		return pErr.Retryable
	}
	// 网络层临时故障（连接重置、超时等）→ 可恢复
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	return false
}
