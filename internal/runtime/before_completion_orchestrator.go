package runtime

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/decider"
	runtimehooks "neo-code/internal/runtime/hooks"
)

// beforeCompletionHookSignals 收敛 before_completion_decision 阶段 user/repo hook 的可消费信号。
type beforeCompletionHookSignals struct {
	Annotations []string
	Guards      []decider.HookGuardSignal
}

// runBeforeCompletionDecisionAcceptance 执行 before_completion_decision 专用编排：
// 1) 先执行 user/repo hooks 收集 annotation/guard signal；
// 2) 再执行普通 internal hooks 用于观测；
// 3) 最后由 runtime 内部 AcceptanceService 作为 before_completion_decision 的收口裁决阶段，生成唯一 AcceptanceDecision。
// AcceptanceDecision 走强类型 runtime 内部路径，不通过通用 HookResult metadata 承载。
func (s *Service) runBeforeCompletionDecisionAcceptance(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
	workdir string,
	completionPassed bool,
	hasToolCalls bool,
	assistantRole string,
) (acceptance.AcceptanceDecision, error) {
	if s == nil {
		return acceptance.AcceptanceDecision{}, nil
	}

	point := runtimehooks.HookPointBeforeCompletionDecision
	hookInput := s.buildRunHookContext(
		state,
		runtimehooks.HookContext{
			Metadata: map[string]any{
				"completion_passed": completionPassed,
				"has_tool_calls":    hasToolCalls,
				"assistant_role":    strings.TrimSpace(assistantRole),
				"workdir":           strings.TrimSpace(workdir),
			},
		},
	)
	scopedCtx := withRuntimeHookEnvelope(ctx, hookRuntimeEnvelope{
		RunID:     firstNonBlank(hookRunIDFromState(state), hookInput.RunID),
		SessionID: firstNonBlank(hookSessionIDFromState(state), hookInput.SessionID),
		Turn:      hookTurnFromState(state),
		Phase:     hookPhaseFromState(state),
	})

	signals := beforeCompletionHookSignals{}
	if s.hookExecutor != nil {
		baseExecutor, userExecutor, repoExecutor := splitHookExecutors(s.hookExecutor)

		for _, item := range []struct {
			executor HookExecutor
			source   runtimehooks.HookSource
		}{
			{executor: userExecutor, source: runtimehooks.HookSourceUser},
			{executor: repoExecutor, source: runtimehooks.HookSourceRepo},
		} {
			if item.executor == nil {
				continue
			}
			output := item.executor.Run(scopedCtx, point, hookInput.Clone())
			annotations, guards := collectBeforeCompletionSignals(output, item.source)
			signals.Annotations = append(signals.Annotations, annotations...)
			signals.Guards = append(signals.Guards, guards...)
			s.recordUserHookAnnotations(state, output)
		}

		// internal hooks 在该点位最后执行；其结果仅用于观测，不参与 user/repo signal 收集。
		if baseExecutor != nil {
			output := baseExecutor.Run(scopedCtx, point, hookInput.Clone())
			s.recordUserHookAnnotations(state, output)
		}
	}
	s.emitRunScopedOptional(EventVerificationStarted, state, VerificationStartedPayload{
		CompletionPassed:        completionPassed,
		CompletionBlockedReason: strings.TrimSpace(string(state.completion.CompletionBlockedReason)),
	})
	// 收口裁决阶段：消费 completion/facts/todo/verification/user-repo signals，生成唯一终态裁决。
	return s.beforeAcceptFinal(ctx, state, snapshot, assistant, completionPassed, signals)
}

// buildRunHookContext 构造带 run/session 元数据的 hook 输入。
func (s *Service) buildRunHookContext(state *runState, input runtimehooks.HookContext) runtimehooks.HookContext {
	runID := firstNonBlank(hookRunIDFromState(state), input.RunID)
	sessionID := firstNonBlank(hookSessionIDFromState(state), input.SessionID)
	input.RunID = firstNonBlank(input.RunID, runID)
	input.SessionID = firstNonBlank(input.SessionID, sessionID)
	if input.Metadata == nil {
		input.Metadata = make(map[string]any, 8)
	}
	input.Metadata["run_id"] = input.RunID
	input.Metadata["session_id"] = input.SessionID
	if state != nil {
		input.Metadata["runtime_run_token"] = state.runToken
		if _, exists := input.Metadata["phase"]; !exists {
			input.Metadata["phase"] = hookPhaseFromState(state)
		}
		input.Metadata["turn"] = hookTurnFromState(state)
	}
	return input
}

// splitHookExecutors 拆解 composeRuntimeHookExecutors 形成的链，恢复 internal/user/repo 三段执行器。
func splitHookExecutors(executor HookExecutor) (base HookExecutor, user HookExecutor, repo HookExecutor) {
	switch typed := executor.(type) {
	case *repoComposedHookExecutor:
		subBase, subUser, _ := splitHookExecutors(typed.base)
		return subBase, subUser, typed.repo
	case *userComposedHookExecutor:
		subBase, _, subRepo := splitHookExecutors(typed.base)
		return subBase, typed.user, subRepo
	default:
		return executor, nil, nil
	}
}

// collectBeforeCompletionSignals 从 user/repo hook 结果提取 annotation 与 guard 信号。
func collectBeforeCompletionSignals(
	output runtimehooks.RunOutput,
	defaultSource runtimehooks.HookSource,
) ([]string, []decider.HookGuardSignal) {
	if len(output.Results) == 0 {
		return nil, nil
	}
	annotations := make([]string, 0, len(output.Results))
	guards := make([]decider.HookGuardSignal, 0, len(output.Results))
	for _, result := range output.Results {
		source := strings.TrimSpace(string(result.Source))
		if source == "" {
			source = strings.TrimSpace(string(defaultSource))
		}
		message := strings.TrimSpace(result.Message)
		errText := strings.TrimSpace(result.Error)
		if message != "" {
			annotations = append(annotations, message)
		}

		isGuard := result.Status == runtimehooks.HookResultFailed || result.Metadata.GuardSignal
		if !isGuard {
			continue
		}
		guard := decider.HookGuardSignal{
			HookID:  strings.TrimSpace(result.HookID),
			Source:  source,
			Message: firstNonBlank(message, errText),
		}
		guards = append(guards, guard)
	}
	return annotations, guards
}
