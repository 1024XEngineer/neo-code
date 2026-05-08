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
