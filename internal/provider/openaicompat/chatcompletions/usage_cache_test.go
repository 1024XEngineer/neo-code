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

func TestExtractStreamUsageClampsNegativeInput(t *testing.T) {
	t.Parallel()

	raw := openai.CompletionUsage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}
	// 厂商返回异常数据时 cached 可能大于 prompt，必须收敛为 0 而不是产生负数。
	raw.PromptTokensDetails.CachedTokens = 150

	var usage providertypes.Usage
	extractStreamUsage(&usage, raw)
	if usage.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0 after clamping", usage.InputTokens)
	}
}
