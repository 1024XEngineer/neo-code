package provider

import "testing"

func TestNormalizePromptCacheMode(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":         PromptCacheModeAuto,
		"auto":     PromptCacheModeAuto,
		" AUTO ":   PromptCacheModeAuto,
		"off":      PromptCacheModeOff,
		"Off":      PromptCacheModeOff,
		"false":    PromptCacheModeOff,
		"disabled": PromptCacheModeOff,
		"none":     PromptCacheModeOff,
		"unknown":  PromptCacheModeAuto,
	}
	for input, want := range cases {
		if got := NormalizePromptCacheMode(input); got != want {
			t.Fatalf("NormalizePromptCacheMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPromptCacheEnabled(t *testing.T) {
	t.Parallel()

	if !PromptCacheEnabled("") {
		t.Fatal("empty mode should default to enabled")
	}
	if !PromptCacheEnabled(PromptCacheModeAuto) {
		t.Fatal("auto mode should be enabled")
	}
	if PromptCacheEnabled(PromptCacheModeOff) {
		t.Fatal("off mode should be disabled")
	}
}

func TestSupportsPromptCacheKey(t *testing.T) {
	t.Parallel()

	// 自研 HTTP 实现的厂商不发送 OpenAI 的缓存路由键。
	unsupported := []string{DriverDeepSeek, DriverQwen, DriverGLM, DriverMiMo, DriverMiniMax}
	for _, driver := range unsupported {
		if SupportsPromptCacheKey(driver) {
			t.Fatalf("driver %q should not send prompt_cache_key", driver)
		}
	}
	if SupportsPromptCacheKey("  DEEPSEEK  ") {
		t.Fatal("driver matching must ignore case and surrounding spaces")
	}

	supported := []string{DriverOpenAICompat, "", "custom-gateway"}
	for _, driver := range supported {
		if !SupportsPromptCacheKey(driver) {
			t.Fatalf("driver %q should send prompt_cache_key", driver)
		}
	}
}
