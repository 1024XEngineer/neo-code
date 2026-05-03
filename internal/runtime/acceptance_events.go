package runtime

import (
	"strings"

	"neo-code/internal/runtime/acceptance"
)

// emitAcceptanceDecisionEvents 将验收决策及其 verifier 轨迹统一转换为运行时事件，保证观测链路一致。
func (s *Service) emitAcceptanceDecisionEvents(state *runState, decision acceptance.AcceptanceDecision) {
	for _, result := range decision.VerifierResults {
		s.emitRunScopedOptional(EventVerificationStageFinished, state, VerificationStageFinishedPayload{
			Name:       result.Name,
			Status:     result.Status,
			Summary:    result.Summary,
			Reason:     result.Reason,
			ErrorClass: result.ErrorClass,
		})
	}
	s.emitRunScopedOptional(EventVerificationFinished, state, VerificationFinishedPayload{
		AcceptanceStatus: decision.Status,
		StopReason:       decision.StopReason,
		ErrorClass:       decision.ErrorClass,
	})
	s.emitRunScopedOptional(EventAcceptanceDecided, state, AcceptanceDecidedPayload{
		Status:                  decision.Status,
		StopReason:              decision.StopReason,
		ErrorClass:              decision.ErrorClass,
		CompletionBlockedReason: strings.TrimSpace(decision.CompletionBlockedReason),
		UserVisibleSummary:      decision.UserVisibleSummary,
		InternalSummary:         decision.InternalSummary,
		ContinueHint:            decision.ContinueHint,
	})
}
