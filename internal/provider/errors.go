package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// 通用领域错误。
var (
	ErrDriverNotFound          = errors.New("provider driver not found")
	ErrDriverAlreadyRegistered = errors.New("provider: driver already registered")
	ErrDiscoveryConfig         = errors.New("provider: discovery config invalid")

	// 流级哨兵错误，用于区分可恢复/不可恢复的流中断原因。
	ErrStreamInterrupted    = errors.New("provider: stream interrupted")
	ErrLineTooLong          = errors.New("provider: SSE line exceeds max length")
	ErrStreamTooLarge       = errors.New("provider: stream total size exceeds limit")
	ErrThinkingNotSupported = errors.New("provider: thinking not supported for this model")
)

type ProviderErrorCode string

const (
	ErrorCodeAuthFailed     ProviderErrorCode = "auth_failed"      // 认证失败（401）
	ErrorCodeForbidden      ProviderErrorCode = "forbidden"        // 权限不足（403）
	ErrorCodeNotFound       ProviderErrorCode = "not_found"        // 资源不存在（404）
	ErrorCodeClient         ProviderErrorCode = "client_error"     // 客户端请求错误（4xx，排除上述分类）
	ErrorCodeRateLimit      ProviderErrorCode = "rate_limited"     // 限流（429）
	ErrorCodeServer         ProviderErrorCode = "server_error"     // 服务端错误（5xx）
	ErrorCodeTimeout        ProviderErrorCode = "timeout"          // 超时
	ErrorCodeNetwork        ProviderErrorCode = "network_error"    // 网络错误（连接拒绝、DNS 失败等）
	ErrorCodeContextTooLong ProviderErrorCode = "context_too_long" // 上下文超出模型窗口
	ErrorCodeUnknown        ProviderErrorCode = "unknown"          // 未知错误
)

var contextTooLongFragments = []string{
	"context length",
	"context_length_exceeded",
	"context window",
	"maximum context length",
	"maximum prompt length",
	"prompt is too long",
}

var contextTokenFragments = []string{
	"requested too many tokens",
	"too many tokens",
}

var contextTokenHints = []string{
	"context",
	"prompt",
	"message",
	"input",
	"history",
	"window",
}

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

func NewDiscoveryConfigError(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return ErrDiscoveryConfig
	}
	return fmt.Errorf("%w: %s", ErrDiscoveryConfig, message)
}

func IsDiscoveryConfigError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrDiscoveryConfig)
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
	// Only elevate to context_too_long for generic client errors (e.g. 400/413).
	// Do not override specific classifications such as rate_limited (429) even if
	// the message contains token-count fragments, which would mis-route throttling
	// errors into the reactive-compact path.
	if code == ErrorCodeClient && matchesContextTooLong(message) {
		code = ErrorCodeContextTooLong
	}
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

// IsThinkingNotSupportedError 判断错误是否为 thinking 不支持错误。
func IsThinkingNotSupportedError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrThinkingNotSupported)
}

var thinkingNotSupportedFragments = []string{
	"thinking",
	"reasoning",
}

// WrapIfThinkingNotSupported 检查错误消息是否包含 thinking/reasoning 相关关键字，
// 若是则用 ErrThinkingNotSupported 包装，供 runtime 降级重试。
func WrapIfThinkingNotSupported(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	for _, f := range thinkingNotSupportedFragments {
		if strings.Contains(msg, f) {
			return fmt.Errorf("%w: %w", ErrThinkingNotSupported, err)
		}
	}
	return err
}

// IsContextTooLong 判断 provider 错误是否表示请求上下文超出模型窗口。
// 优先识别 typed error，必要时再回退到消息文本匹配，兼容不同厂商或额外包装层。
// 已被归类为 rate_limited (429) 的错误不会因文本片段而被误判为 context_too_long。
func IsContextTooLong(err error) bool {
	if err == nil {
		return false
	}

	var pErr *ProviderError
	if errors.As(err, &pErr) {
		if pErr.Code == ErrorCodeContextTooLong {
			return true
		}
		// Skip text fallback for errors that are already classified as a specific
		// non-context error (e.g. rate_limited).  Token-count fragments in 429
		// messages must not route the runtime into the reactive-compact path.
		if pErr.Code == ErrorCodeRateLimit {
			return false
		}
		if matchesContextTooLong(pErr.Message) {
			return true
		}
	}

	return matchesContextTooLong(err.Error())
}

// matchesContextTooLong 统一收敛常见“上下文过长”报错片段，减少 runtime 对厂商文案的感知。
func matchesContextTooLong(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}
	for _, fragment := range contextTooLongFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	for _, fragment := range contextTokenFragments {
		if !strings.Contains(normalized, fragment) {
			continue
		}
		for _, hint := range contextTokenHints {
			if strings.Contains(normalized, hint) {
				return true
			}
		}
	}
	return false
}
