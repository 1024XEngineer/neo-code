package runtime

import (
	"context"
	"fmt"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/decider"
	runtimefacts "neo-code/internal/runtime/facts"
	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

// acceptanceServiceInput 收敛一次最终验收裁决所需的最小输入。
type acceptanceServiceInput struct {
	RunID                   string
	SessionID               string
	TaskKind                decider.TaskKind
	UserGoal                string
	CompletionPassed        bool
	CompletionBlockedReason string
	Facts                   runtimefacts.RuntimeFacts
	Todos                   decider.TodoSnapshot
	Progress                decider.ProgressSnapshot
	LastAssistantText       string
	HookAnnotations         []string
	HookGuards              []decider.HookGuardSignal
	NoProgressStreak        int
	MaxNoProgress           int
	VerificationProfile     agentsession.VerificationProfile
	VerificationInput       verify.FinalVerifyInput
}

// acceptanceService 负责生成 runtime 唯一终态裁决输出。
type acceptanceService struct{}

// Decide 统一执行 completion/verification/decider 聚合，并输出 AcceptanceDecision。
func (s *acceptanceService) Decide(ctx context.Context, input acceptanceServiceInput) (acceptance.AcceptanceDecision, error) {
	output := acceptance.AcceptanceDecision{
		Status:                  acceptance.AcceptanceContinue,
		StopReason:              controlplane.StopReasonTodoNotConverged,
		CompletionPassed:        input.CompletionPassed,
		VerificationPassed:      false,
		CompletionBlockedReason: strings.TrimSpace(input.CompletionBlockedReason),
	}
	verificationGate, err := runVerificationGate(ctx, input)
	if err != nil {
		return acceptance.AcceptanceDecision{}, err
	}
	output.VerificationPassed = verificationGate.Passed
	output.VerifierResults = append([]verify.VerificationResult(nil), verificationGate.Results...)

	noProgressExceeded := input.MaxNoProgress > 0 && input.NoProgressStreak >= input.MaxNoProgress
	decision := decider.Decide(decider.DecisionInput{
		RunID:              strings.TrimSpace(input.RunID),
		SessionID:          strings.TrimSpace(input.SessionID),
		TaskKind:           input.TaskKind,
		UserGoal:           strings.TrimSpace(input.UserGoal),
		Facts:              input.Facts,
		Todos:              input.Todos,
		Progress:           input.Progress,
		LastAssistantText:  strings.TrimSpace(input.LastAssistantText),
		CompletionPassed:   input.CompletionPassed,
		CompletionReason:   strings.TrimSpace(input.CompletionBlockedReason),
		NoProgressExceeded: noProgressExceeded,
		HookAnnotations:    append([]string(nil), input.HookAnnotations...),
		HookGuards:         append([]decider.HookGuardSignal(nil), input.HookGuards...),
	})

	output.MissingFacts = append([]decider.MissingFact(nil), decision.MissingFacts...)
	output.RequiredNextActions = append([]decider.RequiredAction(nil), decision.RequiredNextActions...)
	if decision.RequiredInput != nil {
		cloned := *decision.RequiredInput
		if len(cloned.Details) > 0 {
			details := make(map[string]any, len(cloned.Details))
			for k, v := range cloned.Details {
				details[k] = v
			}
			cloned.Details = details
		}
		output.RequiredInput = &cloned
	}
	output.IntentHint = decision.IntentHint
	output.EffectiveTaskKind = decision.EffectiveTaskKind
	output.UserVisibleSummary = strings.TrimSpace(decision.UserVisibleSummary)
	output.InternalSummary = strings.TrimSpace(decision.InternalSummary)
	output.ContinueHint = strings.TrimSpace(buildDeciderContinueHint(decision))
	output.StopReason = toControlplaneStopReason(decision.StopReason)
	output.ErrorClass = ""

	if output.StopReason == "" {
		output.StopReason = controlplane.StopReasonTodoNotConverged
	}
	if noProgressExceeded && decision.Status == decider.DecisionIncomplete {
		output.StopReason = controlplane.StopReasonNoProgressAfterFinalIntercept
	}

	// accepted 必须同时通过 completion 与 verification gate。
	if input.CompletionPassed && verificationGate.Passed && decision.Status == decider.DecisionAccepted {
		output.Status = acceptance.AcceptanceAccepted
		output.StopReason = controlplane.StopReasonAccepted
		output.ContinueHint = ""
		output.CompletionPassed = true
		output.VerificationPassed = true
		return output, nil
	}

	// verification gate 全部通过时信任其结果：即使 decider 基于启发式返回 continue，
	// verification gate 已实际运行所有 profile 指定的 verifier 且全部 pass，应直接 accepted。
	// 避免 decider 与 verification gate 数据源不一致导致死循环。
	if input.CompletionPassed && verificationGate.Passed {
		output.Status = acceptance.AcceptanceAccepted
		output.StopReason = controlplane.StopReasonAccepted
		output.ContinueHint = ""
		output.CompletionPassed = true
		output.VerificationPassed = true
		return output, nil
	}

	if input.CompletionPassed && !verificationGate.Passed {
		return mergeVerificationFailure(output, verificationGate), nil
	}

	switch decision.Status {
	case decider.DecisionAccepted:
		// completion 不通过时即便 decider accepted，也必须继续。
		output.Status = acceptance.AcceptanceContinue
	case decider.DecisionFailed, decider.DecisionBlocked:
		output.Status = acceptance.AcceptanceFailed
		if output.StopReason == "" {
			output.StopReason = controlplane.StopReasonVerificationFailed
		}
	case decider.DecisionIncomplete:
		output.Status = acceptance.AcceptanceIncomplete
		if output.StopReason == "" {
			output.StopReason = controlplane.StopReasonNoProgressAfterFinalIntercept
		}
	default:
		output.Status = acceptance.AcceptanceContinue
	}
	if output.Status == acceptance.AcceptanceContinue && output.ContinueHint == "" {
		output.ContinueHint = finalContinueReminder
	}
	// 死循环兜底：多轮 final 被拦截且无进展 + 存在 open required todo → 追加强制清理指令
	if output.Status == acceptance.AcceptanceContinue && input.NoProgressStreak >= 2 && input.Todos.Summary.RequiredOpen > 0 {
		staleHint := buildStaleTodoResetHint(input.Todos.Summary.RequiredOpen, input.NoProgressStreak)
		if output.ContinueHint == "" {
			output.ContinueHint = staleHint
		} else {
			output.ContinueHint = output.ContinueHint + "\n\n" + staleHint
		}
	}
	if input.VerificationInput.RuntimeState.MaxTurnsReached && output.Status == acceptance.AcceptanceContinue {
		output.Status = acceptance.AcceptanceIncomplete
		if output.StopReason == controlplane.StopReasonVerificationFailed {
			output.StopReason = controlplane.StopReasonMaxTurnExceededWithFailedVerification
		} else {
			output.StopReason = controlplane.StopReasonMaxTurnExceededWithUnconvergedTodos
		}
	}
	output.ErrorClass = normalizeAcceptanceErrorClass(output.ErrorClass, input, output)
	return output, nil
}

// runVerificationGate 执行 verifier gate；completion 未通过时仅回填必要证据，不执行重 verifier。
func runVerificationGate(ctx context.Context, input acceptanceServiceInput) (verify.VerificationGateDecision, error) {
	if !input.CompletionPassed {
		results := make([]verify.VerificationResult, 0, 1)
		if strings.EqualFold(strings.TrimSpace(input.CompletionBlockedReason), string(controlplane.CompletionBlockedReasonPendingTodo)) {
			if synthetic := synthesizeTodoConvergenceEvidence(toSessionTodos(input.Todos)); synthetic != nil {
				results = append(results, *synthetic)
			}
		}
		return verify.VerificationGateDecision{
			Passed:  false,
			Reason:  controlplane.StopReasonTodoNotConverged,
			Results: results,
		}, nil
	}
	if !input.VerificationProfile.Valid() {
		return verify.VerificationGateDecision{
			Passed: false,
			Reason: controlplane.StopReasonVerificationConfigMissing,
			Results: []verify.VerificationResult{{
				Name:       "verification_profile",
				Status:     verify.VerificationFail,
				Summary:    "verification profile invalid",
				Reason:     fmt.Sprintf("invalid verification profile %q", input.VerificationProfile),
				ErrorClass: verify.ErrorClassEnvMissing,
			}},
		}, nil
	}

	policy := acceptance.DefaultPolicy{Executor: verify.PolicyCommandExecutor{}}
	verifiers, err := policy.ResolveVerifiers(input.VerificationInput)
	if err != nil {
		return verify.VerificationGateDecision{
			Passed: false,
			Reason: controlplane.StopReasonVerificationConfigMissing,
			Results: []verify.VerificationResult{{
				Name:       "verification_profile",
				Status:     verify.VerificationFail,
				Summary:    "verification profile resolution failed",
				Reason:     err.Error(),
				ErrorClass: verify.ErrorClassEnvMissing,
			}},
		}, nil
	}
	orch := verify.Orchestrator{Verifiers: verifiers}
	return orch.RunFinalVerification(ctx, input.VerificationInput)
}

// mergeVerificationFailure 统一把 verification gate 非通过映射到终态决策。
func mergeVerificationFailure(
	base acceptance.AcceptanceDecision,
	gate verify.VerificationGateDecision,
) acceptance.AcceptanceDecision {
	out := base
	out.VerificationPassed = gate.Passed
	out.VerifierResults = append([]verify.VerificationResult(nil), gate.Results...)
	out.StopReason = gate.Reason

	first := firstNonPassVerifierResult(gate.Results)
	if gate.Passed || first == nil {
		return out
	}
	out.ErrorClass = first.ErrorClass
	switch first.Status {
	case verify.VerificationSoftBlock:
		out.Status = acceptance.AcceptanceContinue
		if out.StopReason == "" {
			out.StopReason = controlplane.StopReasonTodoNotConverged
		}
		if out.ContinueHint == "" {
			out.ContinueHint = finalContinueReminder
		}
	case verify.VerificationHardBlock:
		out.Status = acceptance.AcceptanceIncomplete
		if first.WaitingExternal {
			out.StopReason = controlplane.StopReasonTodoWaitingExternal
		}
	default:
		out.Status = acceptance.AcceptanceFailed
		if out.StopReason == "" || out.StopReason == controlplane.StopReasonAccepted {
			out.StopReason = controlplane.StopReasonVerificationFailed
		}
		if out.ErrorClass == "" {
			out.ErrorClass = verify.ErrorClassUnknown
		}
	}
	out.ErrorClass = normalizeAcceptanceErrorClass(out.ErrorClass, acceptanceServiceInput{}, out)
	return out
}

// normalizeAcceptanceErrorClass 统一补齐终态 error_class，避免 TUI/Gateway 出现 unknown/empty 推断歧义。
func normalizeAcceptanceErrorClass(
	current verify.ErrorClass,
	input acceptanceServiceInput,
	decision acceptance.AcceptanceDecision,
) verify.ErrorClass {
	if current != "" {
		return current
	}
	switch decision.StopReason {
	case controlplane.StopReasonVerificationConfigMissing:
		return verify.ErrorClassEnvMissing
	case controlplane.StopReasonVerificationExecutionDenied:
		return verify.ErrorClassPermissionDenied
	case controlplane.StopReasonVerificationExecutionError:
		return verify.ErrorClassUnknown
	case controlplane.StopReasonRequiredTodoFailed:
		return verify.ErrorClassUnknown
	case controlplane.StopReasonNoProgressAfterFinalIntercept:
		return verify.ErrorClassUnknown
	case controlplane.StopReasonVerificationFailed:
		if input.TaskKind == decider.TaskKindSubAgent && len(input.Facts.SubAgents.Failed) > 0 {
			return verify.ErrorClass("subagent_failed")
		}
		if input.TaskKind == decider.TaskKindWorkspaceWrite {
			if errClass := latestToolErrorClass(input.Facts.Errors.ToolErrors, "filesystem_write_file"); errClass != "" {
				return verify.ErrorClass(errClass)
			}
		}
		if errClass := latestToolErrorClass(input.Facts.Errors.ToolErrors, "spawn_subagent"); errClass != "" {
			return verify.ErrorClass(errClass)
		}
		if errClass := latestToolErrorClass(input.Facts.Errors.ToolErrors, "filesystem_write_file"); errClass != "" {
			return verify.ErrorClass(errClass)
		}
	}
	if decision.Status == acceptance.AcceptanceFailed || decision.Status == acceptance.AcceptanceIncomplete {
		return verify.ErrorClassUnknown
	}
	return ""
}

// latestToolErrorClass 返回目标工具最近一次非空错误分类。
func latestToolErrorClass(errors []runtimefacts.ToolErrorFact, tool string) string {
	target := strings.TrimSpace(tool)
	for i := len(errors) - 1; i >= 0; i-- {
		entry := errors[i]
		if target != "" && !strings.EqualFold(strings.TrimSpace(entry.Tool), target) {
			continue
		}
		errClass := strings.TrimSpace(entry.ErrorClass)
		if errClass != "" {
			return errClass
		}
	}
	return ""
}

// buildStaleTodoResetHint 构造死循环兜底指令：当多轮 final 被拦截且无进展时，强制要求模型清理 stale todo。
func buildStaleTodoResetHint(requiredOpen, noProgressStreak int) string {
	var b strings.Builder
	b.WriteString("<stale_todo_reset>\n")
	b.WriteString(fmt.Sprintf("CRITICAL: You have been blocked for %d consecutive final attempts with %d unfinished required todo(s).\n", noProgressStreak, requiredOpen))
	b.WriteString("If these todos are NO LONGER RELEVANT to the user's CURRENT request,\n")
	b.WriteString("you MUST mark them canceled using todo_write set_status=canceled RIGHT NOW.\n")
	b.WriteString("Do NOT attempt to complete stale todos that belong to a PREVIOUS task.\n")
	b.WriteString("After canceling irrelevant todos, proceed with the user's current request.\n")
	b.WriteString("</stale_todo_reset>")
	return b.String()
}

func firstNonPassVerifierResult(results []verify.VerificationResult) *verify.VerificationResult {
	for _, result := range results {
		if result.Status == verify.VerificationPass {
			continue
		}
		cloned := result
		return &cloned
	}
	return nil
}

func toSessionTodos(snapshot decider.TodoSnapshot) []agentsession.TodoItem {
	if len(snapshot.Items) == 0 {
		return nil
	}
	out := make([]agentsession.TodoItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		required := item.Required
		status := agentsession.TodoStatus(strings.TrimSpace(item.Status))
		out = append(out, agentsession.TodoItem{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        status,
			Required:      &required,
			Artifacts:     append([]string(nil), item.Artifacts...),
			FailureReason: strings.TrimSpace(item.FailureReason),
		})
	}
	return out
}

func resolveAcceptanceMaxNoProgress(cfg config.VerificationConfig) int {
	limit := cfg.MaxNoProgress
	if limit <= 0 {
		return 3
	}
	return limit
}
