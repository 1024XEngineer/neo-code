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

func TestDefaultPromptCacheMode(t *testing.T) {
	t.Parallel()

	// 仅 anthropic 默认开启：cache_control 是官方协议字段。
	if got := DefaultPromptCacheMode(DriverAnthropic); got != PromptCacheModeAuto {
		t.Fatalf("anthropic default = %q, want auto", got)
	}
	// openaicompat 服务任意兼容网关，未确认支持前不发送，避免 400 回归。
	if got := DefaultPromptCacheMode(DriverOpenAICompat); got != PromptCacheModeOff {
		t.Fatalf("openaicompat default = %q, want off", got)
	}
	if got := DefaultPromptCacheMode(""); got != PromptCacheModeOff {
		t.Fatalf("empty driver default = %q, want off", got)
	}
	if got := DefaultPromptCacheMode("  ANTHROPIC "); got != PromptCacheModeAuto {
		t.Fatalf("driver matching must ignore case and spaces, got %q", got)
	}
}

func TestResolvePromptCacheMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		configured string
		driver     string
		want       string
	}{
		{configured: "auto", driver: DriverOpenAICompat, want: PromptCacheModeAuto},
		{configured: "off", driver: DriverAnthropic, want: PromptCacheModeOff},
		{configured: "", driver: DriverAnthropic, want: PromptCacheModeAuto},
		{configured: "", driver: DriverOpenAICompat, want: PromptCacheModeOff},
		// 拼写错误不应被静默解释成开启，回落 driver 默认值。
		{configured: "always", driver: DriverAnthropic, want: PromptCacheModeAuto},
		{configured: "always", driver: DriverOpenAICompat, want: PromptCacheModeOff},
	}
	for _, tc := range cases {
		if got := ResolvePromptCacheMode(tc.configured, tc.driver); got != tc.want {
			t.Fatalf("ResolvePromptCacheMode(%q, %q) = %q, want %q", tc.configured, tc.driver, got, tc.want)
		}
	}
}

func TestPromptCacheEnabled(t *testing.T) {
	t.Parallel()

	if !PromptCacheEnabled("", DriverAnthropic) {
		t.Fatal("anthropic should be enabled by default")
	}
	if PromptCacheEnabled("", DriverOpenAICompat) {
		t.Fatal("openaicompat should stay disabled until explicitly enabled")
	}
	if !PromptCacheEnabled(PromptCacheModeAuto, DriverOpenAICompat) {
		t.Fatal("explicit auto should enable openaicompat")
	}
	if PromptCacheEnabled(PromptCacheModeOff, DriverAnthropic) {
		t.Fatal("off mode should disable anthropic")
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

	eligible := []string{DriverOpenAICompat, "", "custom-gateway"}
	for _, driver := range eligible {
		if !SupportsPromptCacheKey(driver) {
			t.Fatalf("driver %q should be eligible for prompt_cache_key", driver)
		}
	}
}

func TestShouldSendPromptCacheKey(t *testing.T) {
	t.Parallel()

	// 硬约束优先：即使显式开启，自研厂商也不发送。
	if ShouldSendPromptCacheKey(PromptCacheModeAuto, DriverDeepSeek) {
		t.Fatal("deepseek must never send prompt_cache_key even when explicitly enabled")
	}
	// 默认关闭：未显式声明支持的兼容网关不发送。
	if ShouldSendPromptCacheKey("", DriverOpenAICompat) {
		t.Fatal("openaicompat must stay disabled by default")
	}
	if !ShouldSendPromptCacheKey(PromptCacheModeAuto, DriverOpenAICompat) {
		t.Fatal("explicit auto should enable openaicompat")
	}
	if ShouldSendPromptCacheKey(PromptCacheModeOff, DriverOpenAICompat) {
		t.Fatal("off mode should disable")
	}
}
