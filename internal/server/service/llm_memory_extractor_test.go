package service

import (
	"context"
	"testing"
	"time"

	"go-llm-demo/internal/server/domain"
)

type stubLLMChatProvider struct {
	response string
	err      error
}

func (s stubLLMChatProvider) GetModelName() string {
	return "stub"
}

func (s stubLLMChatProvider) Chat(context.Context, []domain.Message) (<-chan string, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(chan string, 1)
	go func() {
		defer close(out)
		out <- s.response
	}()
	return out, nil
}

func TestLLMMemoryExtractorParsesStructuredResponse(t *testing.T) {
	extractor := NewLLMMemoryExtractor(stubLLMChatProvider{
		response: `{"items":[{"type":"user_preference","scope":"user","summary":"Use Chinese for future replies","details":"The user asked for Chinese responses going forward.","confidence":0.93}]}`,
	}, LLMMemoryExtractorOptions{Timeout: time.Second})

	items, err := extractor.Extract(context.Background(), "以后请用中文回复。", "好的，后续我会用中文。")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one extracted item, got %+v", items)
	}
	if items[0].Type != domain.TypeUserPreference {
		t.Fatalf("expected user_preference, got %+v", items[0])
	}
}

func TestLLMMemoryExtractorFallsBackToRuleExtractorOnInvalidJSON(t *testing.T) {
	extractor := NewLLMMemoryExtractor(stubLLMChatProvider{
		response: `not-json`,
	}, LLMMemoryExtractorOptions{
		Timeout:  time.Second,
		Fallback: NewRuleBasedMemoryExtractor(),
	})

	items, err := extractor.Extract(context.Background(), "以后回答中文，命令和说明都用中文。", "好的，后续我会统一使用中文回复。")
	if err != nil {
		t.Fatalf("extract with fallback: %v", err)
	}

	var hasPreference bool
	for _, item := range items {
		if item.Type == domain.TypeUserPreference {
			hasPreference = true
		}
	}
	if !hasPreference {
		t.Fatalf("expected fallback rule extractor to produce user_preference, got %+v", items)
	}
}

func TestBuildMemoryExtractorAutoFallsBackToRuleWhenProviderMissing(t *testing.T) {
	extractor, err := BuildMemoryExtractor(MemoryExtractorModeAuto, nil, LLMMemoryExtractorOptions{})
	if err != nil {
		t.Fatalf("build extractor: %v", err)
	}

	items, err := extractor.Extract(context.Background(), "What does memory_repository.go do?", "internal/server/infra/repository/memory_repository.go is responsible for persistent memory storage.")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var hasCodeFact bool
	for _, item := range items {
		if item.Type == domain.TypeCodeFact {
			hasCodeFact = true
		}
	}
	if !hasCodeFact {
		t.Fatalf("expected auto mode without provider to use rule extractor, got %+v", items)
	}
}
