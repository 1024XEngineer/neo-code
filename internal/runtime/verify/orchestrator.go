package verify

import (
	"context"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
)

// Orchestrator 按固定顺序执行 verifier 并收敛 verification gate。
type Orchestrator struct {
	Verifiers []FinalVerifier
}

// RunFinalVerification 执行 verifier 列表并生成统一 gate 决议。
func (o Orchestrator) RunFinalVerification(ctx context.Context, input FinalVerifyInput) (VerificationGateDecision, error) {
	results := make([]VerificationResult, 0, len(o.Verifiers))
	waitingExternal := false
	for _, verifier := range o.Verifiers {
		if verifier == nil {
			continue
		}
		verifierName := strings.TrimSpace(verifier.Name())
		verifierCfg, hasCfg := input.VerificationConfig.Verifiers[verifierName]
		result, err := verifier.VerifyFinal(ctx, input)
		if err != nil {
			result = VerificationResult{
				Name:       verifierName,
				Status:     VerificationFail,
				Summary:    err.Error(),
				Reason:     "verifier execution error",
				ErrorClass: ErrorClassUnknown,
			}
		}
		result = NormalizeResult(result)
		if result.Name == "" {
			result.Name = verifierName
		}
		if hasCfg {
			result = applyVerifierFailurePolicy(result, verifierCfg)
		}
		if result.WaitingExternal {
			waitingExternal = true
		}
		results = append(results, result)
	}

	decision := VerificationGateDecision{
		Passed:  true,
		Reason:  controlplane.StopReasonAccepted,
		Results: results,
	}
	for _, result := range results {
		switch result.Status {
		case VerificationFail:
			decision.Passed = false
			decision.Reason = stopReasonForVerificationFailure(result)
			return decision, nil
		case VerificationHardBlock:
			decision.Passed = false
			if waitingExternal {
				decision.Reason = controlplane.StopReasonTodoWaitingExternal
			} else {
				decision.Reason = controlplane.StopReasonTodoNotConverged
			}
		case VerificationSoftBlock:
			decision.Passed = false
			if decision.Reason == controlplane.StopReasonAccepted {
				decision.Reason = controlplane.StopReasonTodoNotConverged
			}
		}
	}
	return decision, nil
}

// applyVerifierFailurePolicy 根据 fail_open/fail_closed 配置归一化 verifier 失败语义。
func applyVerifierFailurePolicy(result VerificationResult, cfg config.VerifierConfig) VerificationResult {
	if cfg.FailClosed && result.Status == VerificationSoftBlock && result.ErrorClass == ErrorClassEnvMissing {
		evidence := result.Evidence
		if evidence == nil {
			evidence = make(map[string]any)
		}
		evidence["fail_closed_applied"] = true
		evidence["original_status"] = string(VerificationSoftBlock)
		result.Status = VerificationFail
		if result.ErrorClass == "" {
			result.ErrorClass = ErrorClassUnknown
		}
		result.Evidence = evidence
	}
	if !cfg.FailOpen {
		return result
	}
	if result.Status != VerificationFail {
		return result
	}
	evidence := result.Evidence
	if evidence == nil {
		evidence = make(map[string]any)
	}
	evidence["fail_open_applied"] = true
	evidence["original_status"] = string(VerificationFail)
	result.Status = VerificationPass
	result.ErrorClass = ""
	result.Retryable = false
	result.WaitingExternal = false
	result.Summary = "verifier failure ignored by fail_open policy"
	result.Reason = "fail_open policy downgraded verifier failure"
	result.Evidence = evidence
	return result
}

// stopReasonForVerificationFailure 将 verifier 失败映射到细粒度 stop reason。
func stopReasonForVerificationFailure(result VerificationResult) controlplane.StopReason {
	normalizedSummary := strings.ToLower(strings.TrimSpace(result.Summary))
	normalizedReason := strings.ToLower(strings.TrimSpace(result.Reason))
	switch {
	case strings.Contains(normalizedReason, "missing verifier command configuration"),
		strings.Contains(normalizedSummary, "required but missing"),
		result.ErrorClass == ErrorClassEnvMissing:
		return controlplane.StopReasonVerificationConfigMissing
	case strings.Contains(normalizedReason, "denied"),
		strings.Contains(normalizedSummary, "denied"),
		result.ErrorClass == ErrorClassPermissionDenied:
		return controlplane.StopReasonVerificationExecutionDenied
	case strings.Contains(normalizedReason, "execution"),
		strings.Contains(normalizedSummary, "execution"),
		result.ErrorClass == ErrorClassTimeout,
		result.ErrorClass == ErrorClassCommandNotFound:
		return controlplane.StopReasonVerificationExecutionError
	default:
		return controlplane.StopReasonVerificationFailed
	}
}
