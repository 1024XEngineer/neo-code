//go:build windows

package config

import (
	"os"
	"strings"
	"testing"
)

func TestProviderConfigResolveAPIKeyFallsBackToUserEnv(t *testing.T) {
	const envName = "NEOCODE_TEST_USER_ENV_FALLBACK_KEY"
	const envValue = "user-env-secret"

	originalProcessValue, hadOriginalProcessValue := os.LookupEnv(envName)
	originalUserValue, hadOriginalUserValue, lookupErr := LookupUserEnvVar(envName)
	if lookupErr != nil {
		t.Fatalf("LookupUserEnvVar() error = %v", lookupErr)
	}

	t.Cleanup(func() {
		if hadOriginalProcessValue {
			_ = os.Setenv(envName, originalProcessValue)
		} else {
			_ = os.Unsetenv(envName)
		}

		if hadOriginalUserValue {
			_ = PersistUserEnvVar(envName, originalUserValue)
		} else {
			_ = DeleteUserEnvVar(envName)
		}
	})

	_ = os.Unsetenv(envName)
	if err := PersistUserEnvVar(envName, envValue); err != nil {
		if containsPermissionDenied(err) {
			t.Skipf("skip windows user env persistence test without registry permission: %v", err)
		}
		t.Fatalf("PersistUserEnvVar() error = %v", err)
	}

	cfg := ProviderConfig{
		Name:      "windows-fallback-provider",
		Driver:    "openaicompat",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: envName,
	}
	got, err := cfg.ResolveAPIKey()
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if got != envValue {
		t.Fatalf("ResolveAPIKey() = %q, want %q", got, envValue)
	}
}

func containsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "access is denied")
}
