package chatcompletions

import (
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestComputePromptCacheKey(t *testing.T) {
	t.Parallel()

	base := providertypes.GenerateRequest{
		SystemPrompt:       "stable\n\ndynamic",
		SystemStablePrefix: "stable",
		Tools: []providertypes.ToolSpec{
			{Name: "read_file"},
			{Name: "grep"},
		},
	}

	key := computePromptCacheKey("gpt-5", base)
	if len(key) != promptCacheKeyHexLength {
		t.Fatalf("len(key) = %d, want %d", len(key), promptCacheKeyHexLength)
	}
	if got := computePromptCacheKey("gpt-5", base); got != key {
		t.Fatal("same input must produce a stable key")
	}

	if got := computePromptCacheKey("gpt-5-mini", base); got == key {
		t.Fatal("model change must change the key")
	}

	withExtraTool := base
	withExtraTool.Tools = append(append([]providertypes.ToolSpec{}, base.Tools...), providertypes.ToolSpec{Name: "bash"})
	if got := computePromptCacheKey("gpt-5", withExtraTool); got == key {
		t.Fatal("tool set change must change the key")
	}

	noPrefix := base
	noPrefix.SystemStablePrefix = ""
	if got := computePromptCacheKey("gpt-5", noPrefix); got != "" {
		t.Fatalf("key without stable prefix = %q, want empty", got)
	}
}

func TestBuildRequestGatesPromptCacheKeyByDriver(t *testing.T) {
	t.Parallel()

	req := providertypes.GenerateRequest{
		Model:              "gpt-5",
		SystemPrompt:       "stable\n\ndynamic",
		SystemStablePrefix: "stable",
	}

	cases := []struct {
		name   string
		driver string
		mode   string
		want   bool
	}{
		// 通用驱动服务任意网关，未显式声明支持前默认不发送，避免端点 400 回归。
		{name: "通用兼容驱动默认不发送", driver: provider.DriverOpenAICompat, mode: "", want: false},
		{name: "显式 auto 后发送", driver: provider.DriverOpenAICompat, mode: provider.PromptCacheModeAuto, want: true},
		{name: "显式 off 不发送", driver: provider.DriverOpenAICompat, mode: provider.PromptCacheModeOff, want: false},
		// 自研实现是硬约束，显式 auto 也不发送。
		{name: "DeepSeek 显式 auto 也不发送", driver: provider.DriverDeepSeek, mode: provider.PromptCacheModeAuto, want: false},
		{name: "Qwen 显式 auto 也不发送", driver: provider.DriverQwen, mode: provider.PromptCacheModeAuto, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{
				Driver:          tc.driver,
				PromptCacheMode: tc.mode,
			}, req)
			if err != nil {
				t.Fatalf("BuildRequest() error = %v", err)
			}
			if tc.want && payload.PromptCacheKey == "" {
				t.Fatal("expected prompt_cache_key to be set")
			}
			if !tc.want && payload.PromptCacheKey != "" {
				t.Fatalf("expected prompt_cache_key to stay empty, got %q", payload.PromptCacheKey)
			}
		})
	}
}
