package provider

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestResolveAPIKeyValueWithResolver(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: "TEST_KEY",
		APIKeyResolver: func(envName string) (string, error) {
			if envName != "TEST_KEY" {
				return "", errors.New("unexpected env name")
			}
			return "resolved-key-value", nil
		},
	}
	key, err := cfg.ResolveAPIKeyValue()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "resolved-key-value" {
		t.Fatalf("key = %q, want %q", key, "resolved-key-value")
	}
}

func TestResolveAPIKeyValueResolverReturnsError(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: "TEST_KEY",
		APIKeyResolver: func(envName string) (string, error) {
			return "", errors.New("key not found")
		},
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "key not found") {
		t.Fatalf("expected resolver error, got %v", err)
	}
}

func TestResolveAPIKeyValueResolverReturnsEmptyString(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: "TEST_KEY",
		APIKeyResolver: func(envName string) (string, error) {
			return "  ", nil
		},
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty value error, got %v", err)
	}
}

func TestResolveAPIKeyValueFromEnvVar(t *testing.T) {
	envName := "NEOTEST_RESOLVE_KEY_VALUE"
	os.Setenv(envName, "my-secret-key")
	defer os.Unsetenv(envName)

	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: envName,
	}
	key, err := cfg.ResolveAPIKeyValue()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "my-secret-key" {
		t.Fatalf("key = %q, want %q", key, "my-secret-key")
	}
}

func TestResolveAPIKeyValueEnvVarEmpty(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: "NEOTEST_NONEXISTENT_VAR",
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty env var error, got %v", err)
	}
}

func TestResolveAPIKeyValueEmptyEnvName(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test-provider",
		APIKeyEnv: "",
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error, got %v", err)
	}
}

func TestResolveAPIKeyValueEmptyEnvNameNoProviderName(t *testing.T) {
	cfg := RuntimeConfig{
		APIKeyEnv: "",
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil {
		t.Fatal("expected error for empty api_key_env without provider name")
	}
}

func TestResolveAPIKeyValueWhitespaceOnlyEnvName(t *testing.T) {
	cfg := RuntimeConfig{
		Name:      "test",
		APIKeyEnv: "   ",
	}
	_, err := cfg.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error for whitespace env name, got %v", err)
	}
}

func TestStaticAPIKeyResolver(t *testing.T) {
	resolver := StaticAPIKeyResolver("test-key-123")
	key, err := resolver("ANYTHING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "test-key-123" {
		t.Fatalf("key = %q, want %q", key, "test-key-123")
	}
}

func TestStaticAPIKeyResolverEmptyKey(t *testing.T) {
	resolver := StaticAPIKeyResolver("")
	_, err := resolver("ANYTHING")
	if err == nil || !strings.Contains(err.Error(), "static api key is empty") {
		t.Fatalf("expected empty static key error, got %v", err)
	}
}

func TestStaticAPIKeyResolverWhitespaceKey(t *testing.T) {
	resolver := StaticAPIKeyResolver("   ")
	_, err := resolver("ANYTHING")
	if err == nil || !strings.Contains(err.Error(), "static api key is empty") {
		t.Fatalf("expected empty static key error for whitespace, got %v", err)
	}
}

func TestResolvedGenerateMaxRetries(t *testing.T) {
	// 0 is valid (means no retries)
	cfg := RuntimeConfig{GenerateMaxRetries: 0}
	got := cfg.ResolvedGenerateMaxRetries()
	if got != 0 {
		t.Fatalf("expected 0 retries, got %d", got)
	}

	// positive value is preserved
	cfg2 := RuntimeConfig{GenerateMaxRetries: 5}
	got2 := cfg2.ResolvedGenerateMaxRetries()
	if got2 != 5 {
		t.Fatalf("expected 5, got %d", got2)
	}

	// negative value maps to default
	cfg3 := RuntimeConfig{GenerateMaxRetries: -1}
	got3 := cfg3.ResolvedGenerateMaxRetries()
	if got3 != DefaultGenerateMaxRetries {
		t.Fatalf("expected default retries %d, got %d", DefaultGenerateMaxRetries, got3)
	}
}

func TestResolvedGenerateStartTimeout(t *testing.T) {
	cfg := RuntimeConfig{GenerateStartTimeout: 0}
	got := cfg.ResolvedGenerateStartTimeout()
	if got <= 0 {
		t.Fatalf("expected positive timeout, got %v", got)
	}
}

func TestResolvedGenerateIdleTimeout(t *testing.T) {
	cfg := RuntimeConfig{GenerateIdleTimeout: 0}
	got := cfg.ResolvedGenerateIdleTimeout()
	if got <= 0 {
		t.Fatalf("expected positive timeout, got %v", got)
	}
}
