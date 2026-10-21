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
		want   bool
	}{
		{name: "通用兼容驱动发送路由键", driver: provider.DriverOpenAICompat, want: true},
		{name: "DeepSeek 自研路径不发送", driver: provider.DriverDeepSeek, want: false},
		{name: "Qwen 自研路径不发送", driver: provider.DriverQwen, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{Driver: tc.driver}, req)
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
