package config

import (
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestValidateOptionalPromptCache(t *testing.T) {
	t.Parallel()

	valid := []string{"", "auto", "off", " AUTO ", "Off"}
	for _, value := range valid {
		if err := validateOptionalPromptCache(value); err != nil {
			t.Fatalf("validateOptionalPromptCache(%q) unexpected error: %v", value, err)
		}
	}

	if err := validateOptionalPromptCache("always"); err == nil {
		t.Fatal("expected error for unsupported prompt_cache value")
	} else if !strings.Contains(err.Error(), provider.PromptCacheModeAuto) {
		t.Fatalf("error should mention accepted values, got %v", err)
	}
}

func TestProviderConfigPromptCacheReachesRuntimeConfig(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:        "cached-provider",
			Driver:      "openaicompat",
			BaseURL:     "https://llm.example.com/v1",
			Model:       "server-default",
			APIKeyEnv:   "CACHED_PROVIDER_KEY",
			PromptCache: "off",
		},
	}

	runtimeCfg, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if runtimeCfg.PromptCacheMode != provider.PromptCacheModeOff {
		t.Fatalf("PromptCacheMode = %q, want %q", runtimeCfg.PromptCacheMode, provider.PromptCacheModeOff)
	}
}
