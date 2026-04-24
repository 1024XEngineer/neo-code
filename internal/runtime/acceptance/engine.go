package acceptance

import (
	"context"
	"fmt"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

// Engine 聚合 completion gate 与 verifier 结果，输出统一验收决策。
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

// EvaluateFinal 执行 final 验收并输出结构化决策。
func (e *Engine) EvaluateFinal(ctx context.Context, input FinalAcceptanceInput) (AcceptanceDecision, error) {
	if !input.CompletionGate.Passed {
		return AcceptanceDecision{
			Status:             AcceptanceContinue,
			StopReason:         controlplane.StopReasonTodoNotConverged,
			UserVisibleSummary: "当前回合尚未达到可收尾条件，继续执行。",
			InternalSummary:    "completion gate did not pass",
			ContinueHint:       "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet.",
			HasProgress:        false,
		}, nil
	}

	verificationCfg := input.VerificationInput.VerificationConfig
	hookCfg := verificationCfg.Hooks
	verifierEnabled := verificationCfg.EnabledValue()

	var decision AcceptanceDecision
	if verifierEnabled {
		verifiers := e.policy.ResolveVerifiers(input.VerificationInput)
		if hookDecision, failed := runHookStage(
			ctx,
			hookCfg.BeforeVerification,
			hookStageBeforeVerification,
			beforeVerificationHooks(len(verifiers)),
			input,
		); failed {
			return hookDecision, nil
		}

		orch := verify.Orchestrator{Verifiers: verifiers}
		gateDecision, err := orch.RunFinalVerification(ctx, input.VerificationInput)
		if err != nil {
			return AcceptanceDecision{}, err
		}
		if hookDecision, failed := runHookStage(
			ctx,
			hookCfg.AfterVerification,
			hookStageAfterVerification,
			afterVerificationHooks(len(gateDecision.Results)),
			input,
		); failed {
			return hookDecision, nil
		}
		decision = aggregateVerificationDecision(gateDecision)
	} else {
		decision = AcceptanceDecision{
			Status:             AcceptanceAccepted,
			StopReason:         controlplane.StopReasonCompatibilityFallback,
			UserVisibleSummary: "已通过兼容路径接受 final（验证引擎关闭）。",
			InternalSummary:    "verification disabled, compatibility fallback accepted",
			HasProgress:        true,
		}
	}
	if hookDecision, failed := runHookStage(
		ctx,
		hookCfg.BeforeCompletionDecision,
		hookStageBeforeCompletionDecision,
		beforeCompletionDecisionHooks(),
		input,
	); failed {
		return hookDecision, nil
	}
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

	if hasRetryExhausted(input.VerificationInput.Todos) {
		decision.Status = AcceptanceFailed
		decision.StopReason = controlplane.StopReasonRetryExhausted
		decision.UserVisibleSummary = "存在重试耗尽的待办项，任务失败。"
		decision.InternalSummary = "todo retry exhausted"
	}

	return decision, nil
}

const (
	hookStageBeforeVerification       = "before_verification"
	hookStageAfterVerification        = "after_verification"
	hookStageBeforeCompletionDecision = "before_completion_decision"
	hookFailurePolicyFailOpen         = "fail_open"
)

// runHookStage 在指定阶段执行内置 hook，并根据 failure policy 决定是否终止验收。
func runHookStage(
	ctx context.Context,
	spec config.HookSpec,
	stage string,
	hooks []prioritizedHook,
	input FinalAcceptanceInput,
) (AcceptanceDecision, bool) {
	err := runConfiguredHooks(ctx, spec, stage, hooks, input)
	if err == nil {
		return AcceptanceDecision{}, false
	}
	if isFailOpenPolicy(spec.FailurePolicy) {
		return AcceptanceDecision{}, false
	}
	return hookFailureDecision(stage, err), true
}

// aggregateVerificationDecision 按 fail -> hard_block -> soft_block -> pass 规则聚合结果。
func aggregateVerificationDecision(gate verify.VerificationGateDecision) AcceptanceDecision {
	firstFail := firstResultByStatus(gate.Results, verify.VerificationFail)
	if firstFail != nil {
		stopReason := gate.Reason
		if stopReason == "" || stopReason == controlplane.StopReasonAccepted {
			stopReason = controlplane.StopReasonVerificationFailed
		}
		return AcceptanceDecision{
			Status:             AcceptanceFailed,
			StopReason:         stopReason,
			ErrorClass:         firstFail.ErrorClass,
			UserVisibleSummary: "验证未通过，任务失败。",
			InternalSummary:    "at least one verifier returned fail",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			Retryable:          firstFail.Retryable,
			HasProgress:        false,
		}
	}

	firstHard := firstResultByStatus(gate.Results, verify.VerificationHardBlock)
	if firstHard != nil {
		reason := controlplane.StopReasonTodoNotConverged
		if firstHard.WaitingExternal {
			reason = controlplane.StopReasonTodoWaitingExternal
		}
		return AcceptanceDecision{
			Status:             AcceptanceIncomplete,
			StopReason:         reason,
			UserVisibleSummary: "任务仍依赖外部条件，当前以未完成状态结束。",
			InternalSummary:    "at least one verifier returned hard_block",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			HasProgress:        false,
			WaitingExternal:    firstHard.WaitingExternal,
		}
	}

	firstSoft := firstResultByStatus(gate.Results, verify.VerificationSoftBlock)
	if firstSoft != nil {
		return AcceptanceDecision{
			Status:             AcceptanceContinue,
			StopReason:         controlplane.StopReasonTodoNotConverged,
			UserVisibleSummary: "仍有未满足的验收条件，继续执行。",
			InternalSummary:    "at least one verifier returned soft_block",
			ContinueHint:       "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet.",
			VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
			HasProgress:        false,
		}
	}

	return AcceptanceDecision{
		Status:             AcceptanceAccepted,
		StopReason:         controlplane.StopReasonAccepted,
		UserVisibleSummary: "任务通过验收，已完成。",
		InternalSummary:    "completion gate and verification gate both passed",
		VerifierResults:    append([]verify.VerificationResult(nil), gate.Results...),
		HasProgress:        true,
	}
}

// firstResultByStatus 返回首个匹配状态的 verifier 结果。
func firstResultByStatus(results []verify.VerificationResult, status verify.VerificationStatus) *verify.VerificationResult {
	for _, result := range results {
		if result.Status == status {
			cloned := result
			return &cloned
		}
	}
	return nil
}

// hasRetryExhausted 判断 todo 快照中是否存在重试耗尽项。
func hasRetryExhausted(todos []verify.TodoSnapshot) bool {
	for _, todo := range todos {
		if !todo.Required {
			continue
		}
		if todo.RetryLimit <= 0 {
			continue
		}
		if todo.RetryCount >= todo.RetryLimit {
			return true
		}
	}
	return false
}
