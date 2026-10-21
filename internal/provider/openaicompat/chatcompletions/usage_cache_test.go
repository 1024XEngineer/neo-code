package chatcompletions

import (
	"testing"

	"github.com/openai/openai-go/v3"

	providertypes "neo-code/internal/provider/types"
)

func TestExtractStreamUsageNormalizesCachedTokens(t *testing.T) {
	t.Parallel()

	raw := openai.CompletionUsage{
		PromptTokens:     1000,
		CompletionTokens: 120,
		TotalTokens:      1120,
	}
	raw.PromptTokensDetails.CachedTokens = 600

	var usage providertypes.Usage
	extractStreamUsage(&usage, raw)

	if usage.InputTokens != 400 {
		t.Fatalf("InputTokens = %d, want 400 (prompt tokens minus cached)", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 600 {
		t.Fatalf("CacheReadInputTokens = %d, want 600", usage.CacheReadInputTokens)
	}
	if usage.TotalInputTokens() != 1000 {
		t.Fatalf("TotalInputTokens() = %d, want 1000 without double counting", usage.TotalInputTokens())
	}
	if !usage.InputObserved || !usage.OutputObserved {
		t.Fatal("observed flags must stay true")
	}
}

func TestExtractLegacyStreamUsageNormalizesCachedTokens(t *testing.T) {
	t.Parallel()

	raw := &streamUsage{PromptTokens: 500, CompletionTokens: 40, TotalTokens: 540}
	raw.PromptTokensDetails.CachedTokens = 500

	var usage providertypes.Usage
	extractLegacyStreamUsage(&usage, raw)

	if usage.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0 when everything is cached", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 500 {
		t.Fatalf("CacheReadInputTokens = %d, want 500", usage.CacheReadInputTokens)
	}
	if usage.TotalInputTokens() != 500 {
		t.Fatalf("TotalInputTokens() = %d, want 500", usage.TotalInputTokens())
	}
}

func TestExtractStreamUsageBoundsExcessiveCachedTokens(t *testing.T) {
	t.Parallel()

	raw := openai.CompletionUsage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}
	// 厂商返回异常数据时 cached 可能大于 prompt。此时 cached 需被收敛到 prompt，
	// 否则 TotalInputTokens() 会高于厂商上报的总量。
	raw.PromptTokensDetails.CachedTokens = 150

	var usage providertypes.Usage
	extractStreamUsage(&usage, raw)

	if usage.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0 after clamping", usage.InputTokens)
	}
	if usage.CacheReadInputTokens != 100 {
		t.Fatalf("CacheReadInputTokens = %d, want 100 (bounded by prompt tokens)", usage.CacheReadInputTokens)
	}
	if usage.TotalInputTokens() != 100 {
		t.Fatalf("TotalInputTokens() = %d, want 100 (must not exceed reported prompt tokens)", usage.TotalInputTokens())
	}
}

func TestExtractLegacyStreamUsageBoundsExcessiveCachedTokens(t *testing.T) {
	t.Parallel()

	raw := &streamUsage{PromptTokens: 40, CompletionTokens: 5, TotalTokens: 45}
	raw.PromptTokensDetails.CachedTokens = 90

	var usage providertypes.Usage
	extractLegacyStreamUsage(&usage, raw)

	if usage.CacheReadInputTokens != 40 {
		t.Fatalf("CacheReadInputTokens = %d, want 40", usage.CacheReadInputTokens)
	}
	if usage.TotalInputTokens() != 40 {
		t.Fatalf("TotalInputTokens() = %d, want 40", usage.TotalInputTokens())
	}
}

func TestNormalizeCachedPromptTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		prompt       int
		cached       int
		wantUncached int
		wantCached   int
	}{
		{prompt: 1000, cached: 600, wantUncached: 400, wantCached: 600},
		{prompt: 1000, cached: 0, wantUncached: 1000, wantCached: 0},
		{prompt: 1000, cached: 1000, wantUncached: 0, wantCached: 1000},
		{prompt: 100, cached: 150, wantUncached: 0, wantCached: 100},
		{prompt: -5, cached: 10, wantUncached: 0, wantCached: 0},
		{prompt: 10, cached: -5, wantUncached: 10, wantCached: 0},
	}
	for _, tc := range cases {
		uncached, cached := normalizeCachedPromptTokens(tc.prompt, tc.cached)
		if uncached != tc.wantUncached || cached != tc.wantCached {
			t.Fatalf("normalizeCachedPromptTokens(%d, %d) = (%d, %d), want (%d, %d)",
				tc.prompt, tc.cached, uncached, cached, tc.wantUncached, tc.wantCached)
		}
		if uncached+cached > max(tc.prompt, 0) {
			t.Fatalf("sum %d exceeds clamped prompt %d", uncached+cached, tc.prompt)
		}
	}
}
