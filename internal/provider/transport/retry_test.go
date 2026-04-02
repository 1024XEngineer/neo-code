package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"neo-code/internal/provider"
)

// --- isNetworkError ---

func TestIsNetworkError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection refused", errors.New("dial tcp 127.0.0.1:8080: connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"connection timed out", errors.New("connection timed out"), true},
		{"i/o timeout", errors.New("dial tcp: i/o timeout"), true},
		{"no such host", errors.New("dial tcp: lookup api.openai.com: no such host"), true},
		{"dial tcp", errors.New("dial tcp 127.0.0.1:443: operation was refused"), true},
		{"timeout in message", errors.New("request timeout"), true},
		{"tls cert error", errors.New("x509: certificate signed by unknown authority"), false},
		{"generic error", errors.New("something went wrong"), false},
		{"empty message", errors.New(""), false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isNetworkError(tt.err); got != tt.want {
				t.Fatalf("isNetworkError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DefaultRetryableFunc ---

func TestDefaultRetryableFunc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"retryable ProviderError", &provider.ProviderError{StatusCode: 429, Code: provider.ErrorCodeRateLimit, Retryable: true}, true},
		{"non-retryable ProviderError", &provider.ProviderError{StatusCode: 401, Code: provider.ErrorCodeAuthFailed, Retryable: false}, false},
		{"wrapped retryable ProviderError", fmt.Errorf("wrapped: %w", &provider.ProviderError{StatusCode: 500, Retryable: true}), true},
		{"wrapped non-retryable ProviderError", fmt.Errorf("wrapped: %w", &provider.ProviderError{StatusCode: 400, Retryable: false}), false},
		{"network error", errors.New("dial tcp 127.0.0.1:8080: connection refused"), true},
		{"non-network non-provider error", errors.New("bad request"), false},
		{"empty error message", errors.New(""), false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultRetryableFunc(tt.err); got != tt.want {
				t.Fatalf("DefaultRetryableFunc() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DefaultRetryConfig ---

func TestDefaultRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 2 {
		t.Fatalf("MaxRetries = %d, want 2", cfg.MaxRetries)
	}
	if cfg.WaitBase != 500*time.Millisecond {
		t.Fatalf("WaitBase = %v, want 500ms", cfg.WaitBase)
	}
	if cfg.MaxWait != 5*time.Second {
		t.Fatalf("MaxWait = %v, want 5s", cfg.MaxWait)
	}
	if cfg.RetryableFunc == nil {
		t.Fatal("RetryableFunc should not be nil")
	}
}

// --- NewRetryTransport ---

func TestNewRetryTransport_NilBase(t *testing.T) {
	t.Parallel()

	rt := NewRetryTransport(nil, RetryConfig{})
	if rt.base != http.DefaultTransport {
		t.Fatal("nil base should fall back to http.DefaultTransport")
	}
}

func TestNewRetryTransport_NilRetryableFunc(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{MaxRetries: 1}
	rt := NewRetryTransport(http.DefaultTransport, cfg)
	if rt.config.RetryableFunc == nil {
		t.Fatal("nil RetryableFunc should fall back to DefaultRetryableFunc")
	}
}

// --- mockRoundTripper ---

type mockRoundTripper struct {
	//fn returns (response, error). Called sequentially.
	fn []func() (*http.Response, error)
	// callCount tracks how many times RoundTrip was invoked.
	callCount int
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.fn) {
		return nil, errors.New("mock: unexpected call")
	}
	return m.fn[idx]()
}

func newSuccessResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// --- RoundTrip ---

func TestRoundTrip_FirstSuccess(t *testing.T) {
	t.Parallel()

	wantResp := newSuccessResponse(http.StatusOK, "ok")
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return wantResp, nil },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{MaxRetries: 2})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if m.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", m.callCount)
	}
}

func TestRoundTrip_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	wantResp := newSuccessResponse(http.StatusOK, "ok")
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return nil, errors.New("dial tcp 127.0.0.1:8080: connection refused") },
			func() (*http.Response, error) { return wantResp, nil },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{
		MaxRetries: 2,
		WaitBase:   1 * time.Millisecond, // 快速完成测试
		MaxWait:    10 * time.Millisecond,
	})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if m.callCount != 2 {
		t.Fatalf("callCount = %d, want 2", m.callCount)
	}
}

func TestRoundTrip_AllRetriesFail(t *testing.T) {
	t.Parallel()

	networkErr := errors.New("connection refused")
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return nil, networkErr },
			func() (*http.Response, error) { return nil, networkErr },
			func() (*http.Response, error) { return nil, networkErr },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{
		MaxRetries: 2,
		WaitBase:   1 * time.Millisecond,
		MaxWait:    10 * time.Millisecond,
	})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
	if !errors.Is(err, networkErr) {
		t.Fatalf("expected networkErr, got %v", err)
	}
	if m.callCount != 3 {
		t.Fatalf("callCount = %d, want 3 (1 initial + 2 retries)", m.callCount)
	}
}

func TestRoundTrip_NonRetryableError(t *testing.T) {
	t.Parallel()

	authErr := provider.NewProviderErrorFromStatus(http.StatusUnauthorized, "invalid api key")
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return nil, authErr },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{MaxRetries: 2})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	if err == nil {
		t.Fatal("expected error for non-retryable")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatal("expected *ProviderError")
	}
	if pErr.Code != provider.ErrorCodeAuthFailed {
		t.Fatalf("code = %s, want auth_failed", pErr.Code)
	}
	if m.callCount != 1 {
		t.Fatalf("callCount = %d, want 1 (no retry)", m.callCount)
	}
}

func TestRoundTrip_ContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return nil, errors.New("connection refused") },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{
		MaxRetries: 2,
		WaitBase:   1 * time.Millisecond,
		MaxWait:    10 * time.Millisecond,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	// 首次请求会发出，然后 context.Err() 检查应在重试前终止
	if m.callCount < 1 {
		t.Fatalf("expected at least 1 call, got %d", m.callCount)
	}
}

func TestRoundTrip_ContextCanceledDuringBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) {
				callCount++
				return nil, errors.New("connection refused")
			},
			func() (*http.Response, error) {
				// 在第二次调用之前 context 已取消，这里不应被调用
				callCount++
				return nil, errors.New("should not reach")
			},
		},
	}

	rt := NewRetryTransport(m, RetryConfig{
		MaxRetries: 3,
		WaitBase:   500 * time.Millisecond, // 足够长，便于在 backoff 期间取消
		MaxWait:    1 * time.Second,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)

	// 首次请求失败后进入 backoff 期间取消
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error")
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1 (first call, then canceled during backoff)", callCount)
	}
}

func TestRoundTrip_ZeroMaxRetries(t *testing.T) {
	t.Parallel()

	networkErr := errors.New("connection refused")
	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) { return nil, networkErr },
		},
	}

	rt := NewRetryTransport(m, RetryConfig{MaxRetries: 0})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	if err == nil {
		t.Fatal("expected error")
	}
	if m.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", m.callCount)
	}
}

func TestRoundTrip_SeekerBodyRewound(t *testing.T) {
	t.Parallel()

	wantResp := newSuccessResponse(http.StatusOK, "ok")
	bodyContent := []byte("request body")
	seekedPositions := make([]int64, 0)

	m := &mockRoundTripper{
		fn: []func() (*http.Response, error){
			func() (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
			func() (*http.Response, error) {
				return wantResp, nil
			},
		},
	}

	rt := NewRetryTransport(m, RetryConfig{
		MaxRetries: 1,
		WaitBase:   1 * time.Millisecond,
		MaxWait:    10 * time.Millisecond,
	})

	// seekReadCloser 同时实现 io.ReadCloser + io.Seeker，
	// 因为 io.NopCloser 会丢失 Seek 接口。
	body := &seekReadCloser{
		Reader: *bytes.NewReader(bodyContent),
		onSeek: func(offset int64, whence int) {
			if whence == io.SeekStart && offset == 0 {
				seekedPositions = append(seekedPositions, offset)
			}
		},
	}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", body)
	req.Header.Set("Content-Type", "application/json")

	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seekedPositions) != 1 {
		t.Fatalf("expected 1 seek(0) call, got %d: %v", len(seekedPositions), seekedPositions)
	}
}

// seekReadCloser 组合 bytes.Reader（已有 Seek）+ Close，供 http.Request.Body 使用。
type seekReadCloser struct {
	bytes.Reader
	onSeek func(offset int64, whence int)
}

func (s *seekReadCloser) Close() error { return nil }

func (s *seekReadCloser) Seek(offset int64, whence int) (int64, error) {
	if s.onSeek != nil {
		s.onSeek(offset, whence)
	}
	return s.Reader.Seek(offset, whence)
}

// --- drainAndClose ---

func TestDrainAndClose_NilResp(t *testing.T) {
	t.Parallel()

	rt := &RetryTransport{}
	if err := rt.drainAndClose(nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDrainAndClose_NilBody(t *testing.T) {
	t.Parallel()

	rt := &RetryTransport{}
	if err := rt.drainAndClose(&http.Response{Body: nil}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDrainAndClose_ReadsBody(t *testing.T) {
	t.Parallel()

	content := "some response body to drain"
	rt := &RetryTransport{}
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(content)),
	}
	if err := rt.drainAndClose(resp); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// --- backoff ---

func TestBackoff_ExponentialGrowth(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		WaitBase: 100 * time.Millisecond,
		MaxWait:  10 * time.Second, // 足够大，不触发上限截断
	}
	rt := NewRetryTransport(http.DefaultTransport, cfg)

	b1 := rt.backoff(1)
	_ = rt.backoff(2)
	b3 := rt.backoff(3)

	// 指数退避：b1 ≈ 100ms, b2 ≈ 200ms, b3 ≈ 400ms（含抖动）
	// 验证大致递增关系（抖动可能让相邻值接近，但 b3 应明显大于 b1）
	if b3 <= b1 {
		t.Fatalf("backoff(3)=%v should be > backoff(1)=%v", b3, b1)
	}
}

func TestBackoff_RespectsMaxWait(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		WaitBase: 500 * time.Millisecond,
		MaxWait:  800 * time.Millisecond, // 很小的上限
	}
	rt := NewRetryTransport(http.DefaultTransport, cfg)

	// attempt=10: 500ms << 9 = 256000ms，远超 800ms 上限
	b := rt.backoff(10)
	if b > 800*time.Millisecond {
		t.Fatalf("backoff(10)=%v should be <= 800ms MaxWait", b)
	}
}

func TestBackoff_ZeroWaitBase(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		WaitBase: 0, // 应回退到 500ms
		MaxWait:  5 * time.Second,
	}
	rt := NewRetryTransport(http.DefaultTransport, cfg)

	b := rt.backoff(1)
	// 500ms * [0.5, 1.5) => [250ms, 750ms)
	if b < 200*time.Millisecond || b > 800*time.Millisecond {
		t.Fatalf("backoff(1) with zero base = %v, expected ~[250ms, 750ms]", b)
	}
}

func TestBackoff_JitterRange(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		WaitBase: 200 * time.Millisecond,
		MaxWait:  10 * time.Second,
	}
	rt := NewRetryTransport(http.DefaultTransport, cfg)

	// 多次采样验证抖动范围
	for i := 0; i < 20; i++ {
		b := rt.backoff(1)
		// 200ms * [0.5, 1.5) => [100ms, 300ms)
		if b < 100*time.Millisecond || b >= 300*time.Millisecond {
			t.Fatalf("backoff(1)=%v outside expected jitter range [100ms, 300ms)", b)
		}
	}
}
