package acceptance

import (
	"context"
	"fmt"

	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

// Engine 负责聚合 completion gate 与 verifier gate，并输出唯一的收尾决策。
type Engine struct {
	policy AcceptancePolicy
}

// NewEngine 创建 acceptance engine。
func NewEngine(policy AcceptancePolicy) *Engine {
	if policy == nil {
		policy = DefaultPolicy{}
	}
	return &Engine{policy: policy}
}

// EvaluateFinal 执行 final acceptance 主链，输出结构化终态决策。
func (e *Engine) EvaluateFinal(ctx context.Context, input FinalAcceptanceInput) (AcceptanceDecision, error) {
	decision := AcceptanceDecision{
		Status:                  AcceptanceContinue,
		StopReason:              controlplane.StopReasonTodoNotConverged,
		CompletionBlockedReason: input.CompletionGate.Reason,
		UserVisibleSummary:      "当前回合尚未达到可收尾条件，继续执行。",
		InternalSummary:         "completion gate did not pass",
		ContinueHint:            "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet.",
	}
	if input.CompletionGate.Passed {
		verifiers, err := e.policy.ResolveVerifiers(input.VerificationInput)
		if err != nil {
			return AcceptanceDecision{
				Status:                  AcceptanceFailed,
				StopReason:              controlplane.StopReasonVerificationConfigMissing,
				ErrorClass:              verify.ErrorClassEnvMissing,
				CompletionBlockedReason: input.CompletionGate.Reason,
				UserVisibleSummary:      "验收配置无效，任务失败。",
				InternalSummary:         fmt.Sprintf("verification profile resolution failed: %v", err),
			}, nil
		}
		orch := verify.Orchestrator{Verifiers: verifiers}
		gateDecision, err := orch.RunFinalVerification(ctx, input.VerificationInput)
		if err != nil {
			return AcceptanceDecision{}, err
		}
		decision = aggregateVerificationDecision(gateDecision)
	}
	decision.CompletionBlockedReason = input.CompletionGate.Reason

	if input.NoProgressExceeded && decision.Status == AcceptanceContinue {
		decision.Status = AcceptanceIncomplete
		decision.StopReason = controlplane.StopReasonNoProgressAfterFinalIntercept
		decision.UserVisibleSummary = "多次拦截 final 且无进展，已停止并标记为未完成。"
		decision.InternalSummary = "no-progress breaker triggered after repeated final interception"
	}

	if input.MaxTurnsReached && decision.Status == AcceptanceContinue {
		decision.Status = AcceptanceIncomplete
		if decision.StopReason == controlplane.StopReasonVerificationFailed {
			decision.StopReason = controlplane.StopReasonMaxTurnExceededWithFailedVerification
		} else {
			decision.StopReason = controlplane.StopReasonMaxTurnExceededWithUnconvergedTodos
		}
		decision.UserVisibleSummary = fmt.Sprintf("达到最大轮次限制（%d），任务未完成。", input.MaxTurnsLimit)
		decision.InternalSummary = "max turn reached while final was still intercepted"
	}

	return decision, nil
}

// aggregateVerificationDecision 将 verifier gate 的首个非 pass 结果映射为 acceptance 决策。
func aggregateVerificationDecision(gate verify.VerificationGateDecision) AcceptanceDecision {
	first := firstNonPassResult(gate.Results)
	if first == nil {
		return AcceptanceDecision{
			Status:             AcceptanceAccepted,
			StopReason:         controlplane.StopReasonAccepted,
			UserVisibleSummary: "任务通过验收，已完成。",
			InternalSummary:    "completion gate and verification gate both passed",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			HasProgress:        true,
		}
	}

	switch first.Status {
	case verify.VerificationSoftBlock:
		return AcceptanceDecision{
			Status:             AcceptanceContinue,
			StopReason:         controlplane.StopReasonTodoNotConverged,
			UserVisibleSummary: "仍有未满足的验收条件，继续执行。",
			InternalSummary:    "first verifier returned soft_block",
			ContinueHint:       "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet.",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
		}
	case verify.VerificationHardBlock:
		reason := controlplane.StopReasonTodoNotConverged
		if first.WaitingExternal {
			reason = controlplane.StopReasonTodoWaitingExternal
		}
		return AcceptanceDecision{
			Status:             AcceptanceIncomplete,
			StopReason:         reason,
			UserVisibleSummary: "任务仍依赖外部条件，当前以未完成状态结束。",
			InternalSummary:    "first verifier returned hard_block",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			WaitingExternal:    first.WaitingExternal,
		}
	default:
		stopReason := gate.Reason
		if stopReason == "" || stopReason == controlplane.StopReasonAccepted {
			stopReason = controlplane.StopReasonVerificationFailed
		}
		return AcceptanceDecision{
			Status:             AcceptanceFailed,
			StopReason:         stopReason,
			ErrorClass:         first.ErrorClass,
			UserVisibleSummary: "验证未通过，任务失败。",
			InternalSummary:    "first verifier returned fail",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			Retryable:          first.Retryable,
		}
	}
}

// firstNonPassResult 返回首个非 pass 的 verifier 结果。
func firstNonPassResult(results []verify.VerificationResult) *verify.VerificationResult {
	for _, result := range results {
		if result.Status == verify.VerificationPass {
			continue
		}
		cloned := result
		return &cloned
	}
	return nil
}
