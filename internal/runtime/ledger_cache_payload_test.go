package runtime

import (
	"testing"

	"neo-code/internal/runtime/controlplane"
)

func TestNewLedgerReconciledPayloadCarriesCacheUsage(t *testing.T) {
	t.Parallel()

	observation := TurnBudgetUsageObservation{
		ID:                    controlplane.TurnBudgetID{AttemptSeq: 2, RequestHash: "hash-1"},
		InputTokens:           1200,
		OutputTokens:          80,
		InputObserved:         true,
		OutputObserved:        true,
		CacheReadInputTokens:  900,
		CacheWriteInputTokens: 0,
	}
	result := ledgerReconcileResult{
		inputTokens:     1200,
		inputSource:     usageSourceObserved,
		outputTokens:    80,
		outputSource:    usageSourceObserved,
		cacheReadTokens: 900,
	}

	payload := newLedgerReconciledPayload(observation, result)
	if payload.CacheReadInputTokens != 900 {
		t.Fatalf("CacheReadInputTokens = %d, want 900", payload.CacheReadInputTokens)
	}
	if payload.CacheWriteInputTokens != 0 {
		t.Fatalf("CacheWriteInputTokens = %d, want 0", payload.CacheWriteInputTokens)
	}
	if payload.InputTokens != 1200 {
		t.Fatalf("InputTokens = %d, want 1200", payload.InputTokens)
	}
}
