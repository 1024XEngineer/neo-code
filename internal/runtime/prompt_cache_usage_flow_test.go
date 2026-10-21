package runtime

import (
	"context"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/streaming"
)

// cacheUsageProvider 返回一次携带缓存读写的 message_done，用于验证 usage 穿过生成链路。
type cacheUsageProvider struct {
	usage *providertypes.Usage
}

func (p *cacheUsageProvider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	return providertypes.BudgetEstimate{EstimatedInputTokens: 1}, nil
}

func (p *cacheUsageProvider) Generate(
	ctx context.Context,
	req providertypes.GenerateRequest,
	events chan<- providertypes.StreamEvent,
) error {
	events <- providertypes.NewTextDeltaStreamEvent("done")
	events <- providertypes.NewMessageDoneStreamEvent("end_turn", p.usage)
	return nil
}

func TestGenerateStreamingMessageSumsCachedInputTokens(t *testing.T) {
	t.Parallel()

	// Anthropic 口径：input_tokens 不含缓存部分，缓存读写单独上报。
	usage := &providertypes.Usage{
		InputTokens:           500,
		OutputTokens:          80,
		InputObserved:         true,
		OutputObserved:        true,
		CacheReadInputTokens:  1800,
		CacheWriteInputTokens: 1200,
	}

	result := generateStreamingMessage(
		context.Background(),
		&cacheUsageProvider{usage: usage},
		providertypes.GenerateRequest{Model: "claude-sonnet-4"},
		streaming.Hooks{},
	)
	if result.err != nil {
		t.Fatalf("generateStreamingMessage() err = %v", result.err)
	}

	// 关键断言：必须累计含缓存的总输入，否则账本低估会让 compact 永不触发。
	if result.inputTokens != 3500 {
		t.Fatalf("inputTokens = %d, want 3500 (500 + 1800 + 1200)", result.inputTokens)
	}
	if result.cacheReadTokens != 1800 {
		t.Fatalf("cacheReadTokens = %d, want 1800", result.cacheReadTokens)
	}
	if result.cacheWriteTokens != 1200 {
		t.Fatalf("cacheWriteTokens = %d, want 1200", result.cacheWriteTokens)
	}
	if !result.inputObserved || !result.outputObserved {
		t.Fatal("observed flags must be preserved")
	}
}

func TestReconcileLedgerPreservesCacheUsage(t *testing.T) {
	t.Parallel()

	service := &Service{}
	id := controlplane.TurnBudgetID{AttemptSeq: 1, RequestHash: "hash"}
	observation := TurnBudgetUsageObservation{
		ID:                    id,
		InputTokens:           3500,
		OutputTokens:          80,
		InputObserved:         true,
		OutputObserved:        true,
		CacheReadInputTokens:  1800,
		CacheWriteInputTokens: 1200,
	}
	decision := controlplane.TurnBudgetDecision{
		ID:                   id,
		EstimatedInputTokens: 9999,
	}

	result, err := service.reconcileLedger(nil, decision, observation)
	if err != nil {
		t.Fatalf("reconcileLedger() error = %v", err)
	}
	// 已观测时必须使用观测值而不是估算值，否则缓存写读会被估算覆盖。
	if result.inputTokens != 3500 {
		t.Fatalf("inputTokens = %d, want 3500 from observation", result.inputTokens)
	}
	if result.cacheReadTokens != 1800 || result.cacheWriteTokens != 1200 {
		t.Fatalf("cache detail = (%d, %d), want (1800, 1200)", result.cacheReadTokens, result.cacheWriteTokens)
	}

	payload := newLedgerReconciledPayload(observation, result)
	if payload.CacheReadInputTokens != 1800 || payload.CacheWriteInputTokens != 1200 {
		t.Fatalf("ledger payload cache detail = (%d, %d), want (1800, 1200)",
			payload.CacheReadInputTokens, payload.CacheWriteInputTokens)
	}

	tokenPayload := TokenUsagePayload{
		InputTokens:           result.inputTokens,
		OutputTokens:          result.outputTokens,
		CacheReadInputTokens:  result.cacheReadTokens,
		CacheWriteInputTokens: result.cacheWriteTokens,
	}

	// token_usage 事件同样必须携带缓存明细，否则客户端无法计算命中率。
	if tokenPayload.CacheReadInputTokens != 1800 || tokenPayload.CacheWriteInputTokens != 1200 {
		t.Fatalf("token usage payload cache detail = (%d, %d), want (1800, 1200)",
			tokenPayload.CacheReadInputTokens, tokenPayload.CacheWriteInputTokens)
	}
	if tokenPayload.InputTokens != 3500 {
		t.Fatalf("token usage payload InputTokens = %d, want 3500", tokenPayload.InputTokens)
	}
}
