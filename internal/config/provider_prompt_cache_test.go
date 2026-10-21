package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestCustomProviderPromptCacheRoundTrip(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "cached-gateway"
	if err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "CACHED_GATEWAY_KEY",
		ModelSource:           ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		PromptCache:           "OFF",
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
	if err != nil {
		t.Fatalf("loadCustomProvider() error = %v", err)
	}
	if cfg.PromptCache != provider.PromptCacheModeOff {
		t.Fatalf("PromptCache = %q, want %q after round trip", cfg.PromptCache, provider.PromptCacheModeOff)
	}

	runtimeCfg, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	providerCfg, err := runtimeCfg.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if providerCfg.PromptCacheMode != provider.PromptCacheModeOff {
		t.Fatalf("PromptCacheMode = %q, want %q", providerCfg.PromptCacheMode, provider.PromptCacheModeOff)
	}
}

func TestCustomProviderFileAcceptsPromptCacheKey(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	// 该键曾被 KnownFields(true) 拒绝：结构体缺少 prompt_cache 字段时，按文档写配置会直接加载失败。
	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
prompt_cache: off
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	found := false
	for _, p := range cfg.Providers {
		if p.Name == "company-gateway" {
			found = true
			if p.PromptCache != provider.PromptCacheModeOff {
				t.Fatalf("PromptCache = %q, want off", p.PromptCache)
			}
		}
	}
	if !found {
		t.Fatal("custom provider company-gateway not loaded")
	}
}

func TestCustomProviderFileRejectsInvalidPromptCache(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "bad-cache-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: bad-cache-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: BAD_CACHE_GATEWAY_KEY
discovery_endpoint_path: /models
prompt_cache: always
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil {
		t.Fatal("expected load to reject unsupported prompt_cache value")
	}
	if !strings.Contains(err.Error(), "prompt_cache") {
		t.Fatalf("error should mention prompt_cache, got %v", err)
	}
}
