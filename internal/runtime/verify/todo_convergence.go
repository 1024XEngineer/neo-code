package verify

import (
	"context"
	"fmt"
	"strings"
)

const (
	todoConvergenceVerifierName = "todo_convergence"
)

var waitingExternalBlockedReasons = map[string]struct{}{
	"permission_wait":        {},
	"user_input_wait":        {},
	"external_resource_wait": {},
}

// TodoConvergenceVerifier 检查 required todo 是否已经全部收敛到终态。
type TodoConvergenceVerifier struct{}

// Name 返回 verifier 名称。
func (TodoConvergenceVerifier) Name() string {
	return todoConvergenceVerifierName
}

// VerifyFinal 对 required todo 进行终态收敛验证。
func (TodoConvergenceVerifier) VerifyFinal(_ context.Context, input FinalVerifyInput) (VerificationResult, error) {
	totalRequired := 0
	terminalCount := 0
	pendingIDs := make([]string, 0)
	inProgressIDs := make([]string, 0)
	blockedIDs := make([]string, 0)
	waitingExternalIDs := make([]string, 0)

	for _, todo := range input.Todos {
		if !todo.Required {
			continue
		}
		totalRequired++
		switch strings.ToLower(strings.TrimSpace(todo.Status)) {
		case "completed", "failed", "canceled":
			terminalCount++
		case "pending":
			pendingIDs = append(pendingIDs, strings.TrimSpace(todo.ID))
		case "in_progress":
			inProgressIDs = append(inProgressIDs, strings.TrimSpace(todo.ID))
		case "blocked":
			blockedIDs = append(blockedIDs, strings.TrimSpace(todo.ID))
			reason := strings.ToLower(strings.TrimSpace(todo.BlockedReason))
			if _, ok := waitingExternalBlockedReasons[reason]; ok {
				waitingExternalIDs = append(waitingExternalIDs, strings.TrimSpace(todo.ID))
			}
		default:
			pendingIDs = append(pendingIDs, strings.TrimSpace(todo.ID))
		}
	}

	evidence := map[string]any{
		"total_required_todos": totalRequired,
		"terminal_count":       terminalCount,
		"pending_ids":          pendingIDs,
		"in_progress_ids":      inProgressIDs,
		"blocked_ids":          blockedIDs,
		"waiting_external_ids": waitingExternalIDs,
	}

	if totalRequired == 0 || terminalCount == totalRequired {
		return VerificationResult{
			Name:     todoConvergenceVerifierName,
			Status:   VerificationPass,
			Summary:  "required todos are converged",
			Reason:   "all required todos are terminal",
			Evidence: evidence,
		}, nil
	}

	if len(waitingExternalIDs) > 0 {
		return VerificationResult{
			Name:            todoConvergenceVerifierName,
			Status:          VerificationHardBlock,
			Summary:         fmt.Sprintf("%d required todo(s) wait for external input", len(waitingExternalIDs)),
			Reason:          "required todos are blocked by external dependency",
			WaitingExternal: true,
			Evidence:        evidence,
		}, nil
	}

	return VerificationResult{
		Name:     todoConvergenceVerifierName,
		Status:   VerificationSoftBlock,
		Summary:  fmt.Sprintf("required todos not converged: terminal=%d/%d", terminalCount, totalRequired),
		Reason:   "required todos are still pending, in progress, or internally blocked",
		Evidence: evidence,
	}, nil
}
