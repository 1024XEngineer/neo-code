package provider

import (
	"fmt"
	"net/http"
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
