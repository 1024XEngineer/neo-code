package transport

import (
	"errors"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"neo-code/internal/provider"
)

// RetryConfig 控制 RetryTransport 的重试行为。
type RetryConfig struct {
	// MaxRetries 最大重试次数（不含首次请求）。0 表示不重试。
	MaxRetries int
	// WaitBase 初始等待时间，默认 500ms。
	WaitBase time.Duration
	// MaxWait 最大等待时间，默认 5s。
	MaxWait time.Duration
	// RetryableFunc 自定义判断函数。为 nil 时使用 DefaultRetryableFunc。
	RetryableFunc func(error) bool
}

// DefaultRetryConfig 返回默认重试配置。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    2,
		WaitBase:      500 * time.Millisecond,
		MaxWait:       5 * time.Second,
		RetryableFunc: DefaultRetryableFunc,
	}
}

// DefaultRetryableFunc 判断 error 是否可重试。
// - *ProviderError 且 Retryable == true
// - 网络错误（超时、连接拒绝等）
func DefaultRetryableFunc(err error) bool {
	if err == nil {
		return false
	}
	var pErr *provider.ProviderError
	if ok := errors.As(err, &pErr); ok {
		return pErr.Retryable
	}
	// 网络层错误（超时、连接拒绝、DNS 失败等）视为可重试。
	if isNetworkError(err) {
		return true
	}
	return false
}

// isNetworkError 判断是否为网络层错误（不包含 TLS 错误，避免重试证书问题）。
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 常见网络错误关键字
	networkIndicators := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"timeout",
		"no such host",
		"i/o timeout",
		"dial tcp",
	}
	for _, indicator := range networkIndicators {
		if strings.Contains(msg, indicator) {
			return true
		}
	}
	return false
}

// RetryTransport 是一个 http.RoundTripper 中间件，在可重试错误上自动重试。
// 只在首次请求（尚未读取 resp.Body）阶段生效，SSE 流式传输中途断连不在其重试范围内。
type RetryTransport struct {
	base   http.RoundTripper
	config RetryConfig
}

// NewRetryTransport 创建 RetryTransport。
// base 为底层 transport，通常为 http.DefaultTransport。
func NewRetryTransport(base http.RoundTripper, cfg RetryConfig) *RetryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.RetryableFunc == nil {
		cfg.RetryableFunc = DefaultRetryableFunc
	}
	return &RetryTransport{
		base:   base,
		config: cfg,
	}
}

// RoundTrip 执行 HTTP 请求，遇到可重试错误时自动重试。
func (rt *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response

	for attempt := 0; attempt <= rt.config.MaxRetries; attempt++ {
		// 每次重试前重新读取 request body（如果有 seeker）。
		if attempt > 0 {
			if err := rt.drainAndClose(lastResp); err != nil {
				log.Printf("retry transport: drain previous response: %v", err)
			}
			if req.Body != nil {
				if seeker, ok := req.Body.(io.Seeker); ok {
					if _, err := seeker.Seek(0, io.SeekStart); err != nil {
						return nil, err
					}
				}
			}
			wait := rt.backoff(attempt)
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(wait):
			}
		}

		resp, err := rt.base.RoundTrip(req)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		lastResp = nil

		// 检查是否可重试。
		if !rt.config.RetryableFunc(err) {
			return nil, err
		}

		// 检查 context 是否已取消。
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
	}

	return nil, lastErr
}

// drainAndClose 读空并关闭前一次失败的 resp.Body，防止连接泄漏。
func (rt *RetryTransport) drainAndClose(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// backoff 计算指数退避 + 随机抖动的等待时间。
// attempt 从 1 开始（首次重试）。
func (rt *RetryTransport) backoff(attempt int) time.Duration {
	base := rt.config.WaitBase
	if base <= 0 {
		base = 500 * time.Millisecond
	}

	// 指数退避
	wait := base << (attempt - 1)

	// 随机抖动：[0.5, 1.5) * wait
	jitter := float64(wait) * (0.5 + rand.Float64())
	wait = time.Duration(jitter)

	// 上限
	maxWait := rt.config.MaxWait
	if maxWait <= 0 {
		maxWait = 5 * time.Second
	}
	if wait > maxWait {
		wait = maxWait
	}

	return wait
}
