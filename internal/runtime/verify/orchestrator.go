package verify

import (
	"context"
	"fmt"
	"strings"

	"neo-code/internal/runtime/controlplane"
)

// Orchestrator 按固定顺序执行 verifier 并在首个非 pass 结果处短路。
type Orchestrator struct {
	Verifiers []FinalVerifier
}

// RunFinalVerification 执行 verifier 列表并生成统一 gate 决议。
func (o Orchestrator) RunFinalVerification(ctx context.Context, input FinalVerifyInput) (VerificationGateDecision, error) {
	results := make([]VerificationResult, 0, len(o.Verifiers))
	decision := VerificationGateDecision{
		Passed: true,
		Reason: controlplane.StopReasonAccepted,
	}
	for _, verifier := range o.Verifiers {
		if verifier == nil {
			continue
		}
		verifierName := strings.TrimSpace(verifier.Name())
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
		results = append(results, result)
		if result.Status == VerificationPass {
			continue
		}

		decision.Passed = false
		switch result.Status {
		case VerificationSoftBlock:
			decision.Reason = controlplane.StopReasonTodoNotConverged
		case VerificationHardBlock:
			if result.WaitingExternal {
				decision.Reason = controlplane.StopReasonTodoWaitingExternal
			} else {
				decision.Reason = controlplane.StopReasonTodoNotConverged
			}
		default:
			decision.Reason = stopReasonForVerificationFailure(result)
		}
		decision.Results = results
		return decision, nil
	}
	decision.Results = results
	return decision, nil
}

// stopReasonForVerificationFailure 将 verifier 失败映射到稳定 stop reason。
func stopReasonForVerificationFailure(result VerificationResult) controlplane.StopReason {
	if strings.EqualFold(strings.TrimSpace(result.Name), todoConvergenceVerifierName) {
		if len(evidenceStringIDs(result.Evidence["failed_ids"])) > 0 ||
			strings.Contains(strings.ToLower(strings.TrimSpace(result.Reason)), "required todos failed") {
			return controlplane.StopReasonRequiredTodoFailed
		}
	}
	switch result.ErrorClass {
	case ErrorClassEnvMissing:
		return controlplane.StopReasonVerificationConfigMissing
	case ErrorClassPermissionDenied:
		return controlplane.StopReasonVerificationExecutionDenied
	case ErrorClassTimeout, ErrorClassCommandNotFound:
		return controlplane.StopReasonVerificationExecutionError
	default:
		return controlplane.StopReasonVerificationFailed
	}
}

func evidenceStringIDs(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, entry := range typed {
			values = append(values, strings.TrimSpace(fmt.Sprintf("%v", entry)))
		}
		return values
	default:
		return nil
	}
}
