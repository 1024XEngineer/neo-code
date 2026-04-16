package memo

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
)

func TestRuleExtractorExtractWithSignal(t *testing.T) {
	extractor := NewRuleExtractor()
	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("请帮我写个函数")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("好的")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住以后都用中文注释")}},
	}

	entries, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Type != TypeUser {
		t.Errorf("Type = %q, want %q", entries[0].Type, TypeUser)
	}
	if entries[0].Source != SourceAutoExtract {
		t.Errorf("Source = %q, want %q", entries[0].Source, SourceAutoExtract)
	}
	if !strings.Contains(entries[0].Title, "记住以后都用中文注释") {
		t.Errorf("Title = %q, should contain original text", entries[0].Title)
	}
}

func TestRuleExtractorExtractNoSignal(t *testing.T) {
	extractor := NewRuleExtractor()
	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("帮我写个排序函数")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("好的，这是一个快速排序")}},
	}

	entries, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 (no signal)", len(entries))
	}
}

func TestRuleExtractorExtractNoUserMessage(t *testing.T) {
	extractor := NewRuleExtractor()
	messages := []providertypes.Message{
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("好的")}},
	}

	entries, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 (no user message)", len(entries))
	}
}

func TestRuleExtractorExtractEmptyMessages(t *testing.T) {
	extractor := NewRuleExtractor()
	entries, err := extractor.Extract(context.Background(), nil)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func TestRuleExtractorExtractCancelledContext(t *testing.T) {
	extractor := NewRuleExtractor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := extractor.Extract(ctx, []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个")}},
	})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRuleExtractorExtractEnglishSignals(t *testing.T) {
	extractor := NewRuleExtractor()

	tests := []struct {
		content string
		want    bool
	}{
		{"always use tabs for indentation", true},
		{"never use console.log", true},
		{"I prefer dark mode", true},
		{"remember to check nil", true},
		{"avoid global variables", true},
		{"from now on use TypeScript", true},
		{"make sure to validate input", true},
		{"write a function", false},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			messages := []providertypes.Message{
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(tt.content)}},
			}
			entries, _ := extractor.Extract(context.Background(), messages)
			got := len(entries) > 0
			if got != tt.want {
				t.Errorf("signal(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestRuleExtractorExtractLongContent(t *testing.T) {
	extractor := NewRuleExtractor()
	// 超过 150 rune，验证按 rune 截断且保持 UTF-8 合法。
	longContent := "记住" + strings.Repeat("中", 200)
	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart(longContent)}},
	}

	entries, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if got := len([]rune(entries[0].Title)); got > 150 {
		t.Errorf("Title rune length = %d, should be <= 150", got)
	}
	if !strings.HasSuffix(entries[0].Title, "...") {
		t.Error("Title should be truncated with ...")
	}
	if !utf8.ValidString(entries[0].Title) {
		t.Errorf("Title should remain valid UTF-8: %q", entries[0].Title)
	}
}

func TestRuleExtractorExtractOnlyLastUserMessage(t *testing.T) {
	extractor := NewRuleExtractor()
	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住偏好A")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("好的")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("写个函数")}},
	}

	entries, _ := extractor.Extract(context.Background(), messages)
	// 最后一条用户消息"写个函数"没有信号词，不应提取
	if len(entries) != 0 {
		t.Errorf("should only check last user message, got %d entries", len(entries))
	}
}

func TestContainsSignal(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"记住这个偏好", true},
		{"我喜欢 tab 缩进", true},
		{"别再用空格了", true},
		{"请帮我写代码", false},
		{"REMEMBER to test", true},
		{"NEVER do this again", true},
	}

	for _, tt := range tests {
		got := containsSignal(tt.text)
		if got != tt.want {
			t.Errorf("containsSignal(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestTruncateWithEllipsisBoundaries(t *testing.T) {
	if got := truncateWithEllipsis("abcdef", 0); got != "" {
		t.Fatalf("truncateWithEllipsis(0) = %q, want empty", got)
	}
	if got := truncateWithEllipsis("abcdef", 3); got != "abc" {
		t.Fatalf("truncateWithEllipsis(3) = %q, want %q", got, "abc")
	}
	if got := truncateWithEllipsis("abc", 10); got != "abc" {
		t.Fatalf("truncateWithEllipsis(no truncate) = %q", got)
	}
}

func TestExtractAndStore(t *testing.T) {
	t.Run("nil extractor returns silently", func(t *testing.T) {
		ExtractAndStore(context.Background(), nil, nil, nil)
	})

	t.Run("nil service returns silently", func(t *testing.T) {
		ExtractAndStore(context.Background(), NewRuleExtractor(), nil, nil)
	})

	t.Run("no signal does not add entries", func(t *testing.T) {
		store := NewFileStore(t.TempDir(), t.TempDir())
		svc := NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, nil)
		messages := []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("写个函数")}},
		}
		ExtractAndStore(context.Background(), NewRuleExtractor(), svc, messages)
		entries, _ := svc.List(context.Background())
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("with signal adds entry", func(t *testing.T) {
		store := NewFileStore(t.TempDir(), t.TempDir())
		svc := NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, nil)
		messages := []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住以后都用中文注释")}},
		}
		ExtractAndStore(context.Background(), NewRuleExtractor(), svc, messages)
		entries, _ := svc.List(context.Background())
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Type != TypeUser {
			t.Errorf("Type = %q, want %q", entries[0].Type, TypeUser)
		}
	})
}
