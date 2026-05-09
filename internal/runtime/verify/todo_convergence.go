package verify

import (
	"context"
	"fmt"
	"strings"
)

const todoConvergenceVerifierName = "todo_convergence"

var waitingExternalBlockedReasons = map[string]struct{}{
	"permission_wait":        {},
	"user_input_wait":        {},
	"external_resource_wait": {},
}

// TodoConvergenceVerifier 检查 required todo 是否已经满足唯一收尾语义。
type TodoConvergenceVerifier struct{}

// Name 返回 verifier 名称。
func (TodoConvergenceVerifier) Name() string {
	return todoConvergenceVerifierName
}

// VerifyFinal 对 required todo 进行终态收敛校验。
func (TodoConvergenceVerifier) VerifyFinal(_ context.Context, input FinalVerifyInput) (VerificationResult, error) {
	totalRequired := 0
	completedIDs := make([]string, 0)
	failedIDs := make([]string, 0)
	canceledWithoutReplacementIDs := make([]string, 0)
	pendingIDs := make([]string, 0)
	inProgressIDs := make([]string, 0)
	blockedIDs := make([]string, 0)
	waitingExternalIDs := make([]string, 0)
	replacements := collectSupersededTodoIDs(input.Todos)

	for _, todo := range input.Todos {
		if !todo.Required {
			continue
		}
		totalRequired++
		switch strings.ToLower(strings.TrimSpace(todo.Status)) {
		case "completed":
			completedIDs = append(completedIDs, strings.TrimSpace(todo.ID))
		case "failed":
			failedIDs = append(failedIDs, strings.TrimSpace(todo.ID))
		case "canceled":
			if _, ok := replacements[strings.TrimSpace(todo.ID)]; ok {
				completedIDs = append(completedIDs, strings.TrimSpace(todo.ID))
			} else {
				canceledWithoutReplacementIDs = append(canceledWithoutReplacementIDs, strings.TrimSpace(todo.ID))
			}
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
		"total_required_todos":                  totalRequired,
		"completed_ids":                         completedIDs,
		"failed_ids":                            failedIDs,
		"canceled_without_replacement_ids":      canceledWithoutReplacementIDs,
		"pending_ids":                           pendingIDs,
		"in_progress_ids":                       inProgressIDs,
		"blocked_ids":                           blockedIDs,
		"waiting_external_ids":                  waitingExternalIDs,
		"replacement_supersedes_required_todos": mapKeys(replacements),
	}

	if len(failedIDs) > 0 {
		return VerificationResult{
			Name:       todoConvergenceVerifierName,
			Status:     VerificationFail,
			Summary:    fmt.Sprintf("%d required todo(s) failed", len(failedIDs)),
			Reason:     "required todos failed",
			ErrorClass: ErrorClassUnknown,
			Evidence:   evidence,
		}, nil
	}
	if len(canceledWithoutReplacementIDs) > 0 {
		return VerificationResult{
			Name:       todoConvergenceVerifierName,
			Status:     VerificationFail,
			Summary:    fmt.Sprintf("%d required todo(s) were canceled without replacement", len(canceledWithoutReplacementIDs)),
			Reason:     "required todos canceled without replacement",
			ErrorClass: ErrorClassUnknown,
			Evidence:   evidence,
		}, nil
	}
	if len(waitingExternalIDs) > 0 {
		return VerificationResult{
			Name:            todoConvergenceVerifierName,
			Status:          VerificationFail,
			Summary:         fmt.Sprintf("%d required todo(s) wait for external input", len(waitingExternalIDs)),
			Reason:          "required todos are blocked by external dependency",
			WaitingExternal: true,
			Evidence:        evidence,
		}, nil
	}
	if len(pendingIDs) > 0 || len(inProgressIDs) > 0 || len(blockedIDs) > 0 {
		return VerificationResult{
			Name:     todoConvergenceVerifierName,
			Status:   VerificationFail,
			Summary:  "required todos are not converged",
			Reason:   "required todos are still pending, in progress, or internally blocked",
			Evidence: evidence,
		}, nil
	}

	return VerificationResult{
		Name:     todoConvergenceVerifierName,
		Status:   VerificationPass,
		Summary:  "required todos are converged",
		Reason:   "all required todos completed or were explicitly superseded",
		Evidence: evidence,
	}, nil
}

// collectSupersededTodoIDs 收集显式 replacement todo 声明替代的原 todo ID。
func collectSupersededTodoIDs(todos []TodoSnapshot) map[string]struct{} {
	if len(todos) == 0 {
		return nil
	}
	replacements := make(map[string]struct{})
	for _, todo := range todos {
		if !todo.Required || strings.EqualFold(strings.TrimSpace(todo.Status), "canceled") {
			continue
		}
		for _, superseded := range todo.Supersedes {
			superseded = strings.TrimSpace(superseded)
			if superseded != "" {
				replacements[superseded] = struct{}{}
			}
		}
	}
	return replacements
}

// mapKeys 返回 map key 列表，便于把 supersedes 事实放入 evidence。
func mapKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
