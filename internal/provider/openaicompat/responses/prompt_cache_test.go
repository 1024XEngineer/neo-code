package responses

import (
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestBuildRequestPromptCacheKey(t *testing.T) {
	t.Parallel()

	req := providertypes.GenerateRequest{
		Model:              "gpt-5",
		SystemPrompt:       "stable\n\ndynamic",
		SystemStablePrefix: "stable",
		Tools:              []providertypes.ToolSpec{{Name: "read_file"}, {Name: "grep"}},
	}

	t.Run("默认不发送", func(t *testing.T) {
		t.Parallel()
		payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{Driver: provider.DriverOpenAICompat}, req)
		if err != nil {
			t.Fatalf("BuildRequest() error = %v", err)
		}
		if payload.PromptCacheKey != "" {
			t.Fatalf("PromptCacheKey = %q, want empty by default", payload.PromptCacheKey)
		}
	})

	t.Run("显式 auto 后发送且与 chat completions 一致", func(t *testing.T) {
		t.Parallel()
		payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{
			Driver:          provider.DriverOpenAICompat,
			PromptCacheMode: provider.PromptCacheModeAuto,
		}, req)
		if err != nil {
			t.Fatalf("BuildRequest() error = %v", err)
		}
		want := provider.ComputePromptCacheKey("gpt-5", "stable", []string{"read_file", "grep"})
		if payload.PromptCacheKey != want {
			t.Fatalf("PromptCacheKey = %q, want %q", payload.PromptCacheKey, want)
		}
	})

	t.Run("自研厂商恒不发送", func(t *testing.T) {
		t.Parallel()
		payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{
			Driver:          provider.DriverDeepSeek,
			PromptCacheMode: provider.PromptCacheModeAuto,
		}, req)
		if err != nil {
			t.Fatalf("BuildRequest() error = %v", err)
		}
		if payload.PromptCacheKey != "" {
			t.Fatalf("PromptCacheKey = %q, want empty for self-implemented driver", payload.PromptCacheKey)
		}
	})

	t.Run("无稳定前缀不发送", func(t *testing.T) {
		t.Parallel()
		noPrefix := req
		noPrefix.SystemStablePrefix = ""
		payload, err := BuildRequest(t.Context(), provider.RuntimeConfig{
			Driver:          provider.DriverOpenAICompat,
			PromptCacheMode: provider.PromptCacheModeAuto,
		}, noPrefix)
		if err != nil {
			t.Fatalf("BuildRequest() error = %v", err)
		}
		if payload.PromptCacheKey != "" {
			t.Fatalf("PromptCacheKey = %q, want empty without stable prefix", payload.PromptCacheKey)
		}
	})
}
