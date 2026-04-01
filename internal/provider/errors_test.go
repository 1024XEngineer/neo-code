package provider

import (
	"errors"
	"fmt"
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
