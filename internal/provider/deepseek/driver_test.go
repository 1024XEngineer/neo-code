package deepseek

import (
	"testing"

	"neo-code/internal/provider"
)

func TestDriverName(t *testing.T) {
	d := Driver()
	if d.Name != provider.DriverDeepSeek {
		t.Fatalf("expected driver name %q, got %q", provider.DriverDeepSeek, d.Name)
	}
	if d.Build == nil {
		t.Fatal("build func is nil")
	}
}

func TestNewValidatesBaseURL(t *testing.T) {
	t.Parallel()
	_, err := New(provider.RuntimeConfig{BaseURL: "", APIKeyEnv: "KEY"})
	if err == nil {
		t.Fatal("expected error for empty baseURL")
	}
}

func TestNewValidatesAPIKeyEnv(t *testing.T) {
	t.Parallel()
	_, err := New(provider.RuntimeConfig{BaseURL: "https://api.example.com", APIKeyEnv: ""})
	if err == nil {
		t.Fatal("expected error for empty api_key_env")
	}
}

func TestNewSucceedsWithValidConfig(t *testing.T) {
	t.Parallel()
	p, err := New(provider.RuntimeConfig{
		BaseURL:   "https://api.example.com",
		APIKeyEnv: "TEST_KEY",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}
