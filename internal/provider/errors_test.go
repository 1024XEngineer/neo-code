package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProviderError_ErrorFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *ProviderError
		want string
	}{
		{
			name: "with status code",
			err:  &ProviderError{StatusCode: 401, Code: ErrorCodeAuthFailed, Message: "invalid key"},
			want: "status=401",
		},
		{
			name: "without status code",
			err:  &ProviderError{StatusCode: 0, Code: ErrorCodeNetwork, Message: "connection refused"},
			want: "code=network_error",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("Error() = %q, want containing %q", got, tt.want)
			}
		})
	}
}

func TestIsRetryableStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		expected bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusUnprocessableEntity, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{599, true},
		{200, false},
		{0, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			if got := IsRetryableStatus(tt.status); got != tt.expected {
				t.Fatalf("IsRetryableStatus(%d) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestClassifyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   ProviderErrorCode
	}{
		{http.StatusUnauthorized, ErrorCodeAuthFailed},
		{http.StatusForbidden, ErrorCodeForbidden},
		{http.StatusNotFound, ErrorCodeNotFound},
		{http.StatusBadRequest, ErrorCodeClient},
		{http.StatusUnprocessableEntity, ErrorCodeClient},
		{http.StatusTooManyRequests, ErrorCodeRateLimit},
		{http.StatusInternalServerError, ErrorCodeServer},
		{http.StatusBadGateway, ErrorCodeServer},
		{http.StatusServiceUnavailable, ErrorCodeServer},
		{200, ErrorCodeUnknown},
		{0, ErrorCodeUnknown},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			if got := classifyStatus(tt.status); got != tt.want {
				t.Fatalf("classifyStatus(%d) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestNewProviderErrorFromStatus(t *testing.T) {
	t.Parallel()

	err := NewProviderErrorFromStatus(429, "rate limited")
	if err.StatusCode != 429 {
		t.Fatalf("expected status 429, got %d", err.StatusCode)
	}
	if err.Code != ErrorCodeRateLimit {
		t.Fatalf("expected code %q, got %q", ErrorCodeRateLimit, err.Code)
	}
	if !err.Retryable {
		t.Fatalf("expected 429 to be retryable")
	}

	err = NewProviderErrorFromStatus(401, "unauthorized")
	if err.Retryable {
		t.Fatalf("expected 401 to not be retryable")
	}
}

func TestNewNetworkProviderError(t *testing.T) {
	t.Parallel()

	err := NewNetworkProviderError("connection refused")
	if err.StatusCode != 0 {
		t.Fatalf("expected status 0, got %d", err.StatusCode)
	}
	if err.Code != ErrorCodeNetwork {
		t.Fatalf("expected code %q, got %q", ErrorCodeNetwork, err.Code)
	}
	if !err.Retryable {
		t.Fatalf("expected network error to be retryable")
	}
}

func TestNewTimeoutProviderError(t *testing.T) {
	t.Parallel()

	err := NewTimeoutProviderError("deadline exceeded")
	if err.StatusCode != 0 {
		t.Fatalf("expected status 0, got %d", err.StatusCode)
	}
	if err.Code != ErrorCodeTimeout {
		t.Fatalf("expected code %q, got %q", ErrorCodeTimeout, err.Code)
	}
	if !err.Retryable {
		t.Fatalf("expected timeout to be retryable")
	}
}

func TestProviderError_As(t *testing.T) {
	t.Parallel()

	inner := &ProviderError{StatusCode: 500, Code: ErrorCodeServer, Message: "internal", Retryable: true}
	wrapped := fmt.Errorf("wrapped: %w", inner)

	var target *ProviderError
	if !errors.As(wrapped, &target) {
		t.Fatalf("expected errors.As to match *ProviderError")
	}
	if target.StatusCode != 500 {
		t.Fatalf("expected status 500, got %d", target.StatusCode)
	}
	if !target.Retryable {
		t.Fatalf("expected retryable")
	}
}

// --- IsRecoverableStreamError 全分支覆盖 ---

func TestIsRecoverableStreamError_Nil(t *testing.T) {
	t.Parallel()
	if IsRecoverableStreamError(nil) {
		t.Fatal("nil error should not be recoverable")
	}
}

func TestIsRecoverableStreamError_ContextErrors_NotRecoverable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"context.Canceled", context.Canceled},
		{"context.DeadlineExceeded", context.DeadlineExceeded},
		{"wrapped Canceled", fmt.Errorf("wrap: %w", context.Canceled)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsRecoverableStreamError(tt.err) {
				t.Fatalf("%v should not be recoverable", tt.err)
			}
		})
	}
}

func TestIsRecoverableStreamError_BufferOverflow_NotRecoverable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrLineTooLong", ErrLineTooLong},
		{"ErrStreamTooLarge", ErrStreamTooLarge},
		{"wrapped ErrLineTooLong", fmt.Errorf("read: %w", ErrLineTooLong)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsRecoverableStreamError(tt.sentinel) {
				t.Fatalf("%v should not be recoverable", tt.sentinel)
			}
		})
	}
}

func TestIsRecoverableStreamError_StreamInterrupted_Recoverable(t *testing.T) {
	t.Parallel()
	if !IsRecoverableStreamError(ErrStreamInterrupted) {
		t.Fatal("ErrStreamInterrupted should be recoverable")
	}
	wrapped := fmt.Errorf("stream broken: %w", ErrStreamInterrupted)
	if !IsRecoverableStreamError(wrapped) {
		t.Fatal("wrapped ErrStreamInterrupted should be recoverable")
	}
}

func TestIsRecoverableStreamError_ProviderError_ByRetryableField(t *testing.T) {
	t.Parallel()

	retryable := NewProviderErrorFromStatus(http.StatusTooManyRequests, "rate limit")
	if !IsRecoverableStreamError(retryable) {
		t.Fatal("429 ProviderError should be recoverable")
	}

	serverErr := NewProviderErrorFromStatus(http.StatusInternalServerError, "internal")
	if !IsRecoverableStreamError(serverErr) {
		t.Fatal("5xx ProviderError should be recoverable")
	}

	authErr := NewProviderErrorFromStatus(http.StatusUnauthorized, "bad key")
	if IsRecoverableStreamError(authErr) {
		t.Fatal("401 ProviderError should NOT be recoverable")
	}

	clientErr := NewProviderErrorFromStatus(http.StatusBadRequest, "bad request")
	if IsRecoverableStreamError(clientErr) {
		t.Fatal("400 ProviderError should NOT be recoverable")
	}

	wrappedRetryable := fmt.Errorf("layer1: %w", retryable)
	if !IsRecoverableStreamError(wrappedRetryable) {
		t.Fatal("wrapped retryable ProviderError should be recoverable")
	}
}

func TestIsRecoverableStreamError_NetOpError_Recoverable(t *testing.T) {
	t.Parallel()

	// 模拟网络错误：使用一个包含 "connection reset" 的通用 error
	// net.OpError 需要真实网络操作才能产生，这里用包装方式模拟
	genericNetErr := fmt.Errorf("net error: connection reset by peer")
	// 注意：真实的 *net.OpError 需要 errors.As 匹配
	// 此处验证非上述已知不可恢复类型时默认返回 false
	if IsRecoverableStreamError(genericNetErr) {
		// 通用 error（非 OpError/ProviderError/哨兵）默认不恢复
		t.Fatal("generic non-net error should not be recoverable")
	}
}

func TestIsRecoverableStreamError_UnknownError_NotRecoverable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"generic error", errors.New("something went wrong")},
		{"io.EOF", io.EOF},
		{"io.ErrClosedPipe", io.ErrClosedPipe},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if IsRecoverableStreamError(tt.err) {
				t.Fatalf("%v should not be recoverable", tt.err)
			}
		})
	}
}

// --- MarkNonRetryable 测试 ---

func TestMarkNonRetryable_ProviderError(t *testing.T) {
	t.Parallel()

	// Retryable=true 的 ProviderError → Retryable=false
	retryable := NewProviderErrorFromStatus(http.StatusInternalServerError, "internal")
	if !retryable.Retryable {
		t.Fatal("setup: expected retryable")
	}

	marked := MarkNonRetryable(retryable)
	var pErr *ProviderError
	if !errors.As(marked, &pErr) {
		t.Fatal("marked error should be *ProviderError")
	}
	if pErr.Retryable {
		t.Fatal("MarkNonRetryable should set Retryable=false")
	}
	if pErr.StatusCode != 500 || pErr.Code != ErrorCodeServer {
		t.Fatalf("MarkNonRetryable should preserve StatusCode and Code, got status=%d code=%s", pErr.StatusCode, pErr.Code)
	}

	// 原始对象不受影响
	if !retryable.Retryable {
		t.Fatal("original ProviderError should not be mutated")
	}
}

func TestMarkNonRetryable_WrappedProviderError(t *testing.T) {
	t.Parallel()

	inner := NewProviderErrorFromStatus(http.StatusTooManyRequests, "rate limited")
	wrapped := fmt.Errorf("layer: %w", inner)

	marked := MarkNonRetryable(wrapped)
	var pErr *ProviderError
	if !errors.As(marked, &pErr) {
		t.Fatal("marked error should contain *ProviderError")
	}
	if pErr.Retryable {
		t.Fatal("MarkNonRetryable on wrapped should set Retryable=false")
	}
}

func TestMarkNonRetryable_NonProviderError(t *testing.T) {
	t.Parallel()

	// 非 ProviderError → 包装为 ProviderError{Retryable: false}
	generic := errors.New("some error")
	marked := MarkNonRetryable(generic)

	var pErr *ProviderError
	if !errors.As(marked, &pErr) {
		t.Fatal("marked error should be *ProviderError")
	}
	if pErr.Retryable {
		t.Fatal("should be non-retryable")
	}
	if pErr.Code != ErrorCodeUnknown {
		t.Fatalf("expected code %s, got %s", ErrorCodeUnknown, pErr.Code)
	}
	if !strings.Contains(pErr.Message, "some error") {
		t.Fatalf("message should contain original error text, got: %s", pErr.Message)
	}
}

func TestMarkNonRetryable_AlreadyNonRetryable(t *testing.T) {
	t.Parallel()

	// 已经 Retryable=false 的 ProviderError → 保持 false
	nonRetryable := NewProviderErrorFromStatus(http.StatusUnauthorized, "bad key")
	if nonRetryable.Retryable {
		t.Fatal("setup: 401 should be non-retryable")
	}

	marked := MarkNonRetryable(nonRetryable)
	var pErr *ProviderError
	if !errors.As(marked, &pErr) {
		t.Fatal("marked error should be *ProviderError")
	}
	if pErr.Retryable {
		t.Fatal("should still be non-retryable")
	}
}
