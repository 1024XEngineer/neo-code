package context

import (
	"strings"
	"testing"

	"neo-code/internal/config"
)

func TestBuildAskPromptWithoutHistoryReturnsQuery(t *testing.T) {
	got := BuildAskPrompt(nil, "  current question  ", AskPromptConfig{})
	if got.Prompt != "current question" {
		t.Fatalf("BuildAskPrompt().Prompt = %q, want %q", got.Prompt, "current question")
	}
	if got.Compacted {
		t.Fatal("BuildAskPrompt().Compacted = true, want false")
	}
	if got.Summary != "" {
		t.Fatalf("BuildAskPrompt().Summary = %q, want empty", got.Summary)
	}
	if len(got.RetainedTurns) != 0 {
		t.Fatalf("BuildAskPrompt().RetainedTurns = %#v, want empty", got.RetainedTurns)
	}
}

func TestBuildAskPromptKeepsContextAndCurrentQuestion(t *testing.T) {
	history := []AskTurn{
		{UserQuery: "first question", Assistant: "first answer"},
		{UserQuery: "second question", Assistant: "second answer"},
	}
	got := BuildAskPrompt(history, "current question", AskPromptConfig{
		MaxInputTokens:  2048,
		RetainTurns:     1,
		SummaryMaxChars: 400,
	})
	if !strings.Contains(got.Prompt, "current question") {
		t.Fatalf("prompt should contain current question, got %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "first question") {
		t.Fatalf("prompt should contain summarized history, got %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "second question") {
		t.Fatalf("prompt should contain retained turn, got %q", got.Prompt)
	}
	if got.Compacted {
		t.Fatal("prompt should not compact under high max_input_tokens")
	}
}

func TestBuildAskPromptHonorsTokenTrim(t *testing.T) {
	history := []AskTurn{
		{UserQuery: "this is a very long historical question", Assistant: "this is a very long historical answer"},
		{UserQuery: "another very long historical question", Assistant: "another very long historical answer"},
	}
	cfg := AskPromptConfig{
		MaxInputTokens:  6,
		RetainTurns:     1,
		SummaryMaxChars: 24,
	}
	got := BuildAskPrompt(history, "this is a very long current question", cfg)
	if got.Prompt == "" {
		t.Fatal("BuildAskPrompt() returned empty prompt")
	}
	if len([]rune(got.Prompt)) > cfg.MaxInputTokens*4 {
		t.Fatalf("prompt runes = %d, want <= %d", len([]rune(got.Prompt)), cfg.MaxInputTokens*4)
	}
	if !strings.Contains(got.Prompt, "Current question") && !strings.Contains(got.Prompt, "current question") {
		t.Fatalf("prompt should keep current question section, got %q", got.Prompt)
	}
	if !got.Compacted {
		t.Fatal("prompt should compact when over token limit")
	}
	if len(got.RetainedTurns) == 0 {
		t.Fatal("retained turns should not be empty after compact")
	}
}

func TestBuildAskPromptEmptyQuery(t *testing.T) {
	got := BuildAskPrompt([]AskTurn{{UserQuery: "q1", Assistant: "a1"}}, "  ", AskPromptConfig{})
	if got.Prompt != "" {
		t.Fatalf("expected empty prompt for empty query, got %q", got.Prompt)
	}
}

func TestBuildAskPromptSummaryEdgeCases(t *testing.T) {
	// 空轮次
	if got := buildAskPromptSummary(nil, 100); got != "" {
		t.Fatalf("expected empty summary for nil turns, got %q", got)
	}
	// maxChars <= 0
	if got := buildAskPromptSummary([]AskTurn{{UserQuery: "q", Assistant: "a"}}, 0); got != "" {
		t.Fatalf("expected empty summary for zero maxChars, got %q", got)
	}
	// 正常摘要
	got := buildAskPromptSummary([]AskTurn{
		{UserQuery: "q1", Assistant: "a1"},
		{UserQuery: "q2", Assistant: "a2"},
	}, 1000)
	if !strings.Contains(got, "Q: q1") {
		t.Fatalf("expected Q: q1 in summary, got %q", got)
	}
	if !strings.Contains(got, "A: a1") {
		t.Fatalf("expected A: a1 in summary, got %q", got)
	}
}

func TestCompactAskTurnsEdgeCases(t *testing.T) {
	// 空历史
	sum, ret := compactAskTurns(nil, 5)
	if len(sum) != 0 || len(ret) != 0 {
		t.Fatalf("expected empty for nil history, got %d/%d", len(sum), len(ret))
	}

	// retainTurns <= 0 使用默认值 1，2条历史保留1条
	history := []AskTurn{
		{UserQuery: "q1", Assistant: "a1"},
		{UserQuery: "q2", Assistant: "a2"},
	}
	sum, ret = compactAskTurns(history, 0)
	if len(ret) != 1 {
		t.Fatalf("expected 1 retained (default retainTurns=1), got %d", len(ret))
	}
	if len(sum) != 1 {
		t.Fatalf("expected 1 summary turn, got %d", len(sum))
	}

	// retainTurns >= len(history) 全部保留
	sum, ret = compactAskTurns(history, 10)
	if len(sum) != 0 {
		t.Fatalf("expected no summary turns, got %d", len(sum))
	}
	if len(ret) != 2 {
		t.Fatalf("expected all 2 retained, got %d", len(ret))
	}

	// 正常分拆
	history = []AskTurn{
		{UserQuery: "q1", Assistant: "a1"},
		{UserQuery: "q2", Assistant: "a2"},
		{UserQuery: "q3", Assistant: "a3"},
		{UserQuery: "q4", Assistant: "a4"},
	}
	sum, ret = compactAskTurns(history, 2)
	if len(sum) != 2 {
		t.Fatalf("expected 2 summary turns, got %d", len(sum))
	}
	if len(ret) != 2 {
		t.Fatalf("expected 2 retained turns, got %d", len(ret))
	}
}

func TestComposeAskPromptEdgeCases(t *testing.T) {
	// 空当前问题
	if got := composeAskPrompt("summary", []AskTurn{{UserQuery: "q1"}}, ""); got != "" {
		t.Fatalf("expected empty for empty query, got %q", got)
	}

	// 仅当前问题（无摘要无历史）
	got := composeAskPrompt("", nil, "just a question")
	if got != "just a question" {
		t.Fatalf("expected plain question, got %q", got)
	}

	// 有摘要有历史有当前问题
	got = composeAskPrompt("summary text", []AskTurn{{UserQuery: "q1", Assistant: "a1"}}, "current?")
	if !strings.Contains(got, "Current question") {
		t.Fatalf("expected Current question section, got %q", got)
	}
	if !strings.Contains(got, "Summary") {
		t.Fatalf("expected Summary section, got %q", got)
	}
	if !strings.Contains(got, "Recent turns") {
		t.Fatalf("expected Recent turns section, got %q", got)
	}
}

func TestTrimAskPromptEdgeCases(t *testing.T) {
	query := "current question?"

	// prompt 在限制内
	shortPrompt := "short prompt\n\nCurrent question:\n" + query
	got := trimAskPrompt(shortPrompt, query, 10000, 500)
	if got != shortPrompt {
		t.Fatalf("expected unchanged prompt, got %q", got)
	}

	// prompt 超过限制，有 summaryMaxChars
	longPrompt := strings.Repeat("very long prompt text ", 200)
	got = trimAskPrompt(longPrompt, query, 100, 50)
	if got == longPrompt {
		t.Fatal("expected trimmed prompt")
	}
	if !strings.Contains(got, "Current question") {
		t.Fatalf("expected Current question in trimmed prompt, got %q", got)
	}

	// 仅保留当前问题部分
	veryLongPrompt := strings.Repeat("x", 5000)
	got = trimAskPrompt(veryLongPrompt, query, len([]rune("Current question:\n"))+len([]rune(query)), 0)
	if !strings.Contains(got, query) {
		t.Fatalf("expected current question in fallback, got %q", got)
	}

	// 极端情况：maxChars 非常小
	got = trimAskPrompt("long prompt here", "long question", 5, 0)
	if len([]rune(got)) > 5 {
		t.Fatalf("expected very short prompt, got %q (%d runes)", got, len([]rune(got)))
	}
}

func TestTrimTextByRunesEdgeCases(t *testing.T) {
	// 空字符串
	if got := trimTextByRunes("", 10); got != "" {
		t.Fatalf("expected empty for empty string, got %q", got)
	}
	// maxRunes <= 0
	if got := trimTextByRunes("hello", 0); got != "" {
		t.Fatalf("expected empty for zero maxRunes, got %q", got)
	}
	// 文本在限制内
	if got := trimTextByRunes("hello", 10); got != "hello" {
		t.Fatalf("expected unchanged, got %q", got)
	}
	// 文本需要裁剪
	got := trimTextByRunes("hello world this is long", 10)
	if len([]rune(got)) > 10 {
		t.Fatalf("expected <= 10 runes, got %d: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ... suffix, got %q", got)
	}
	// maxRunes <= 3: no ellipsis
	got = trimTextByRunes("hello world", 3)
	if strings.HasSuffix(got, "...") {
		t.Fatalf("expected no ellipsis for maxRunes <= 3, got %q", got)
	}
}

func TestEstimateTokenCountByRunes(t *testing.T) {
	// 空字符串
	if got := estimateTokenCountByRunes(""); got != 0 {
		t.Fatalf("expected 0 for empty, got %d", got)
	}
	// 空白字符串
	if got := estimateTokenCountByRunes("   "); got != 0 {
		t.Fatalf("expected 0 for whitespace, got %d", got)
	}
	// 短文本
	got := estimateTokenCountByRunes("hello")
	if got < 1 {
		t.Fatalf("expected positive token count, got %d", got)
	}
	// 长文本
	got = estimateTokenCountByRunes(strings.Repeat("a", 4000))
	if got != 1000 { // (4000+3)/4 = 1000
		t.Fatalf("expected 1000, got %d", got)
	}
}

func TestNormalizeAskPromptConfigUsesDefaults(t *testing.T) {
	got := normalizeAskPromptConfig(AskPromptConfig{})
	if got.MaxInputTokens != config.DefaultAskMaxInputTokens {
		t.Fatalf("MaxInputTokens = %d, want %d", got.MaxInputTokens, config.DefaultAskMaxInputTokens)
	}
	if got.RetainTurns != config.DefaultAskRetainTurns {
		t.Fatalf("RetainTurns = %d, want %d", got.RetainTurns, config.DefaultAskRetainTurns)
	}
	if got.SummaryMaxChars != config.DefaultAskSummaryMaxChars {
		t.Fatalf("SummaryMaxChars = %d, want %d", got.SummaryMaxChars, config.DefaultAskSummaryMaxChars)
	}
}
