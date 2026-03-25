package service

import (
	"fmt"
	"strings"
	"time"

	"go-llm-demo/internal/server/domain"
)

const (
	MemoryExtractorModeRule = "rule"
	MemoryExtractorModeLLM  = "llm"
	MemoryExtractorModeAuto = "auto"
)

type LLMMemoryExtractorOptions struct {
	Timeout  time.Duration
	Fallback domain.MemoryExtractor
}

// BuildMemoryExtractor constructs the configured extractor strategy.
func BuildMemoryExtractor(mode string, llmProvider domain.ChatProvider, opts LLMMemoryExtractorOptions) (domain.MemoryExtractor, error) {
	switch normalizeExtractorMode(mode) {
	case MemoryExtractorModeRule:
		return NewRuleBasedMemoryExtractor(), nil
	case MemoryExtractorModeLLM:
		if llmProvider == nil {
			return nil, fmt.Errorf("llm memory extractor requires a chat provider")
		}
		return NewLLMMemoryExtractor(llmProvider, opts), nil
	case MemoryExtractorModeAuto:
		if llmProvider == nil {
			return NewRuleBasedMemoryExtractor(), nil
		}
		opts.Fallback = NewRuleBasedMemoryExtractor()
		return NewLLMMemoryExtractor(llmProvider, opts), nil
	default:
		return nil, fmt.Errorf("unsupported memory extractor mode %q", strings.TrimSpace(mode))
	}
}

func normalizeExtractorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", MemoryExtractorModeRule:
		return MemoryExtractorModeRule
	case MemoryExtractorModeLLM:
		return MemoryExtractorModeLLM
	case MemoryExtractorModeAuto:
		return MemoryExtractorModeAuto
	default:
		return ""
	}
}
