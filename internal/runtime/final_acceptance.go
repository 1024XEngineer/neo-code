package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/decider"
	runtimefacts "neo-code/internal/runtime/facts"
	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

const finalContinueReminder = "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet."

// beforeAcceptFinal 在 runtime 接受模型 final 前执行唯一的 completion/verifier/acceptance 闭环。
func (s *Service) beforeAcceptFinal(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
	completionPassed bool,
	signals beforeCompletionHookSignals,
) (acceptance.AcceptanceDecision, error) {
	if state == nil {
		return acceptance.AcceptanceDecision{}, nil
	}

	maxNoProgress := resolveAcceptanceMaxNoProgress(snapshot.Config.Runtime.Verification)
	noProgressStreak := state.finalInterceptStreak
	if noProgressStreak < 0 {
		noProgressStreak = 0
	}
	if state.mustUseToolAfterFinalContinue && state.noToolAfterFinalContinueStreak > noProgressStreak {
		noProgressStreak = state.noToolAfterFinalContinueStreak
	}

	input := s.buildAcceptanceServiceInput(
		state,
		snapshot,
		assistant,
		completionPassed,
		signals,
		noProgressStreak,
		maxNoProgress,
	)
	service := &acceptanceService{}
	acceptanceDecision, err := service.Decide(ctx, input)
	if err != nil {
		return acceptance.AcceptanceDecision{}, err
	}
	deciderDecision := toDeciderDecisionFromAcceptance(acceptanceDecision)
	state.mu.Lock()
	state.lastDeciderDecision = deciderDecision
	pendingFinalProgress := state.pendingFinalProgress
	state.mu.Unlock()
	s.emitRunScopedOptional(EventDecisionMade, state, deciderDecision)
	s.emitRuntimeSnapshotUpdated(ctx, state, "decision_made")
	if acceptanceDecision.Status == acceptance.AcceptanceContinue && pendingFinalProgress {
		acceptanceDecision.HasProgress = true
	}
	return acceptanceDecision, nil
}

// buildAcceptanceServiceInput 从当前运行态抽取 AcceptanceService 所需输入。
func (s *Service) buildAcceptanceServiceInput(
	state *runState,
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
	completionPassed bool,
	signals beforeCompletionHookSignals,
	noProgressStreak int,
	maxNoProgress int,
) acceptanceServiceInput {
	state.mu.Lock()
	taskKind := state.taskKind
	userGoal := state.userGoal
	completionReason := strings.TrimSpace(string(state.completion.CompletionBlockedReason))
	verificationProfile := state.session.TaskState.VerificationProfile
	sessionMessages := append([]providertypes.Message(nil), state.session.Messages...)
	sessionTodos := cloneTodosForPersistence(state.session.Todos)
	sessionTaskState := state.session.TaskState
	todoSnapshot := buildTodoSnapshotFromItems(sessionTodos)
	factsSnapshot := runtimefacts.RuntimeFacts{}
	if state.factsCollector != nil {
		factsSnapshot = state.factsCollector.Snapshot()
	}
	taskID := strings.TrimSpace(state.taskID)
	runID := strings.TrimSpace(state.runID)
	sessionID := strings.TrimSpace(state.session.ID)
	turn := state.turn
	maxTurnsReached := state.maxTurnsReached
	maxTurns := resolveRuntimeMaxTurns(snapshot.Config.Runtime)
	state.mu.Unlock()

	if strings.TrimSpace(userGoal) == "" {
		userGoal = renderPartsForVerification(assistant.Parts)
	}
	if strings.TrimSpace(string(taskKind)) == "" {
		taskKind = decider.InferTaskKind(userGoal)
	}
	if todoSnapshot.Summary.RequiredOpen > 0 {
		completionPassed = false
		if completionReason == "" {
			completionReason = string(controlplane.CompletionBlockedReasonPendingTodo)
		}
	}
	if !maxTurnsReached && maxTurns > 0 && turn+1 >= maxTurns {
		maxTurnsReached = true
	}
	verifyInput := verify.FinalVerifyInput{
		SessionID:          sessionID,
		RunID:              runID,
		TaskID:             taskID,
		Workdir:            snapshot.Workdir,
		Messages:           buildVerifyMessages(sessionMessages),
		Todos:              buildVerifyTodos(sessionTodos),
		LastAssistantFinal: renderPartsForVerification(assistant.Parts),
		TaskState:          buildVerifyTaskState(sessionTaskState),
		RuntimeState: verify.RuntimeStateSnapshot{
			Turn:                 turn,
			MaxTurns:             maxTurns,
			MaxTurnsReached:      maxTurnsReached,
			FinalInterceptStreak: noProgressStreak,
		},
		VerificationConfig: snapshot.Config.Runtime.Verification.Clone(),
	}
	return acceptanceServiceInput{
		RunID:                   runID,
		SessionID:               sessionID,
		TaskKind:                taskKind,
		UserGoal:                userGoal,
		CompletionPassed:        completionPassed,
		CompletionBlockedReason: completionReason,
		Facts:                   factsSnapshot,
		Todos:                   toDeciderTodoSnapshot(todoSnapshot),
		Progress:                toDeciderProgress(factsSnapshot),
		LastAssistantText:       renderPartsForVerification(assistant.Parts),
		HookAnnotations:         append([]string(nil), signals.Annotations...),
		HookGuards:              append([]decider.HookGuardSignal(nil), signals.Guards...),
		NoProgressStreak:        noProgressStreak,
		MaxNoProgress:           maxNoProgress,
		VerificationProfile:     verificationProfile,
		VerificationInput:       verifyInput,
	}
}

// toDeciderDecisionFromAcceptance 将统一 acceptance 决策投影为 runtime snapshot 兼容的 decider 视图。
func toDeciderDecisionFromAcceptance(decision acceptance.AcceptanceDecision) decider.Decision {
	status := decider.DecisionContinue
	switch decision.Status {
	case acceptance.AcceptanceAccepted:
		status = decider.DecisionAccepted
	case acceptance.AcceptanceFailed:
		status = decider.DecisionFailed
	case acceptance.AcceptanceIncomplete:
		status = decider.DecisionIncomplete
	}
	return decider.Decision{
		Status:              status,
		StopReason:          strings.TrimSpace(string(decision.StopReason)),
		MissingFacts:        append([]decider.MissingFact(nil), decision.MissingFacts...),
		RequiredNextActions: append([]decider.RequiredAction(nil), decision.RequiredNextActions...),
		RequiredInput:       cloneRequiredInput(decision.RequiredInput),
		IntentHint:          decision.IntentHint,
		EffectiveTaskKind:   decision.EffectiveTaskKind,
		UserVisibleSummary:  strings.TrimSpace(decision.UserVisibleSummary),
		InternalSummary:     strings.TrimSpace(decision.InternalSummary),
	}
}

func cloneRequiredInput(in *decider.RequiredInput) *decider.RequiredInput {
	if in == nil {
		return nil
	}
	cloned := *in
	if len(in.Details) == 0 {
		return &cloned
	}
	cloned.Details = make(map[string]any, len(in.Details))
	for k, v := range in.Details {
		cloned.Details[k] = v
	}
	return &cloned
}

// mapDeciderDecisionToAcceptance 把 FinalDecider 裁决映射到 acceptance 协议。
// Deprecated: 仅保留给 legacy 回滚对照与测试使用；P7 主链直接消费 AcceptanceService.Decide 产物。
func mapDeciderDecisionToAcceptance(decision decider.Decision) acceptance.AcceptanceDecision {
	out := acceptance.AcceptanceDecision{
		StopReason:         toControlplaneStopReason(decision.StopReason),
		RequiredInput:      cloneRequiredInput(decision.RequiredInput),
		IntentHint:         decision.IntentHint,
		EffectiveTaskKind:  decision.EffectiveTaskKind,
		UserVisibleSummary: strings.TrimSpace(decision.UserVisibleSummary),
		InternalSummary:    strings.TrimSpace(decision.InternalSummary),
		ContinueHint:       buildDeciderContinueHint(decision),
	}
	switch decision.Status {
	case decider.DecisionAccepted:
		out.Status = acceptance.AcceptanceAccepted
		if out.StopReason == "" {
			out.StopReason = controlplane.StopReasonAccepted
		}
	case decider.DecisionFailed, decider.DecisionBlocked:
		out.Status = acceptance.AcceptanceFailed
		if out.StopReason == "" {
			out.StopReason = controlplane.StopReasonVerificationFailed
		}
	case decider.DecisionIncomplete:
		out.Status = acceptance.AcceptanceIncomplete
		if out.StopReason == "" {
			out.StopReason = controlplane.StopReasonNoProgressAfterFinalIntercept
		}
	default:
		out.Status = acceptance.AcceptanceContinue
		if out.StopReason == "" {
			out.StopReason = controlplane.StopReasonTodoNotConverged
		}
	}
	return out
}

// toDeciderTodoSnapshot 转换 runtime todo 快照到 decider 输入结构。
func toDeciderTodoSnapshot(snapshot TodoSnapshot) decider.TodoSnapshot {
	out := decider.TodoSnapshot{
		Summary: decider.TodoSummary{
			Total:             snapshot.Summary.Total,
			RequiredTotal:     snapshot.Summary.RequiredTotal,
			RequiredCompleted: snapshot.Summary.RequiredCompleted,
			RequiredFailed:    snapshot.Summary.RequiredFailed,
			RequiredOpen:      snapshot.Summary.RequiredOpen,
		},
	}
	if len(snapshot.Items) == 0 {
		return out
	}
	out.Items = make([]decider.TodoViewItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, decider.TodoViewItem{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        strings.TrimSpace(item.Status),
			Required:      item.Required,
			Artifacts:     append([]string(nil), item.Artifacts...),
			FailureReason: strings.TrimSpace(item.FailureReason),
			BlockedReason: strings.TrimSpace(item.BlockedReason),
			Revision:      item.Revision,
		})
	}
	return out
}

// toDeciderProgress 构建 decider 所需的最小进度快照。
func toDeciderProgress(factsSnapshot runtimefacts.RuntimeFacts) decider.ProgressSnapshot {
	return decider.ProgressSnapshot{
		FactCount: max(0, factsSnapshot.Progress.ObservedFactCount),
	}
}

// toControlplaneStopReason 把 decider stop reason 映射为 controlplane 枚举。
func toControlplaneStopReason(reason string) controlplane.StopReason {
	normalized := strings.TrimSpace(reason)
	switch normalized {
	case string(controlplane.StopReasonAccepted):
		return controlplane.StopReasonAccepted
	case string(controlplane.StopReasonTodoNotConverged):
		return controlplane.StopReasonTodoNotConverged
	case string(controlplane.StopReasonNoProgressAfterFinalIntercept):
		return controlplane.StopReasonNoProgressAfterFinalIntercept
	case string(controlplane.StopReasonRequiredTodoFailed):
		return controlplane.StopReasonRequiredTodoFailed
	case string(controlplane.StopReasonVerificationFailed):
		return controlplane.StopReasonVerificationFailed
	case string(controlplane.StopReasonTodoWaitingExternal):
		return controlplane.StopReasonTodoWaitingExternal
	case string(controlplane.StopReasonVerificationConfigMissing):
		return controlplane.StopReasonVerificationConfigMissing
	case string(controlplane.StopReasonVerificationExecutionDenied):
		return controlplane.StopReasonVerificationExecutionDenied
	case string(controlplane.StopReasonVerificationExecutionError):
		return controlplane.StopReasonVerificationExecutionError
	default:
		return ""
	}
}

// buildDeciderContinueHint 生成 FinalDecider continue 场景下的结构化执行提示。
func buildDeciderContinueHint(decision decider.Decision) string {
	if decision.Status != decider.DecisionContinue {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<acceptance_continue>\n")
	if summary := strings.TrimSpace(decision.UserVisibleSummary); summary != "" {
		builder.WriteString("<summary>")
		builder.WriteString(xmlEscape(summary))
		builder.WriteString("</summary>\n")
	}
	if len(decision.MissingFacts) > 0 {
		builder.WriteString("<missing_facts>\n")
		for _, fact := range decision.MissingFacts {
			builder.WriteString(fmt.Sprintf(
				"<fact kind=\"%s\" target=\"%s\">%s</fact>\n",
				xmlEscape(strings.TrimSpace(fact.Kind)),
				xmlEscape(strings.TrimSpace(fact.Target)),
				xmlEscape(evidenceJSONPreview(fact.Details)),
			))
		}
		builder.WriteString("</missing_facts>\n")
	}
	if len(decision.RequiredNextActions) > 0 {
		builder.WriteString("<required_next_actions>\n")
		for _, action := range decision.RequiredNextActions {
			builder.WriteString(fmt.Sprintf(
				"<action tool=\"%s\">%s</action>\n",
				xmlEscape(strings.TrimSpace(action.Tool)),
				xmlEscape(evidenceJSONPreview(action.ArgsHint)),
			))
		}
		builder.WriteString("</required_next_actions>\n")
	}
	builder.WriteString("<rule>Do not claim completion with plain text. Call tools to produce objective facts before final response.</rule>\n")
	builder.WriteString("</acceptance_continue>")
	return strings.TrimSpace(builder.String())
}

// beforeAcceptFinalLegacy 是历史 acceptance/verify 实现，仅用于回滚对照与测试覆盖。
// Deprecated: P7 主链不再调用该路径，最终裁决统一走 beforeAcceptFinal -> AcceptanceService。
func (s *Service) beforeAcceptFinalLegacy(
	ctx context.Context,
	state *runState,
	snapshot TurnBudgetSnapshot,
	assistant providertypes.Message,
	completionPassed bool,
) (acceptance.AcceptanceDecision, error) {
	if state == nil {
		return acceptance.AcceptanceDecision{}, nil
	}

	verificationCfg := snapshot.Config.Runtime.Verification.Clone()
	policy := acceptance.DefaultPolicy{
		Executor: verify.PolicyCommandExecutor{},
	}
	engine := acceptance.NewEngine(policy)

	maxNoProgress := verificationCfg.MaxNoProgress
	if maxNoProgress <= 0 {
		maxNoProgress = 3
	}
	noProgressStreak := state.finalInterceptStreak
	if noProgressStreak < 0 {
		noProgressStreak = 0
	}
	if state.mustUseToolAfterFinalContinue && state.noToolAfterFinalContinueStreak > noProgressStreak {
		noProgressStreak = state.noToolAfterFinalContinueStreak
	}
	maxTurnsLimit := state.maxTurnsLimit
	maxTurnsReached := state.maxTurnsReached
	if !maxTurnsReached {
		resolvedMaxTurns := resolveRuntimeMaxTurns(snapshot.Config.Runtime)
		if resolvedMaxTurns > 0 && state.turn+1 >= resolvedMaxTurns {
			maxTurnsReached = true
			maxTurnsLimit = resolvedMaxTurns
		}
	}

	input := acceptance.FinalAcceptanceInput{
		CompletionGate: acceptance.CompletionGateDecision{
			Passed: completionPassed,
			Reason: string(state.completion.CompletionBlockedReason),
		},
		VerificationInput: verify.FinalVerifyInput{
			SessionID:          state.session.ID,
			RunID:              state.runID,
			TaskID:             state.taskID,
			Workdir:            snapshot.Workdir,
			Messages:           buildVerifyMessages(state.session.Messages),
			Todos:              buildVerifyTodos(state.session.Todos),
			LastAssistantFinal: renderPartsForVerification(assistant.Parts),
			ToolResults:        nil,
			TaskState:          buildVerifyTaskState(state.session.TaskState),
			RuntimeState: verify.RuntimeStateSnapshot{
				Turn:                 state.turn,
				MaxTurns:             resolveRuntimeMaxTurns(snapshot.Config.Runtime),
				MaxTurnsReached:      maxTurnsReached,
				FinalInterceptStreak: noProgressStreak,
			},
			VerificationConfig: verificationCfg,
		},
		NoProgressExceeded: noProgressStreak >= maxNoProgress,
		MaxTurnsReached:    maxTurnsReached,
		MaxTurnsLimit:      maxTurnsLimit,
	}

	decision, err := engine.EvaluateFinal(ctx, input)
	if err != nil {
		return acceptance.AcceptanceDecision{}, err
	}
	if decision.Status == acceptance.AcceptanceContinue && len(decision.VerifierResults) == 0 {
		if synthetic := synthesizeTodoConvergenceEvidence(state.session.Todos); synthetic != nil {
			decision.VerifierResults = append(decision.VerifierResults, *synthetic)
		}
	}
	if decision.Status == acceptance.AcceptanceContinue && state.pendingFinalProgress {
		decision.HasProgress = true
	}
	if strings.TrimSpace(decision.CompletionBlockedReason) == "" {
		decision.CompletionBlockedReason = strings.TrimSpace(string(state.completion.CompletionBlockedReason))
	}
	if decision.Status == acceptance.AcceptanceContinue {
		decision.ContinueHint = buildAcceptanceContinueHint(decision)
	}
	return decision, nil
}

// synthesizeTodoConvergenceEvidence 在 completion gate 拦截且 verifier 未运行时，回填 todo 证据供 continue hint 使用。
func synthesizeTodoConvergenceEvidence(todos []agentsession.TodoItem) *verify.VerificationResult {
	if len(todos) == 0 {
		return nil
	}
	pendingIDs := make([]string, 0)
	inProgressIDs := make([]string, 0)
	blockedIDs := make([]string, 0)
	statusByID := make(map[string]string)
	artifactsByID := make(map[string][]string)
	checksByID := make(map[string][]verify.TodoContentCheckSnapshot)

	for _, todo := range todos {
		if !todo.RequiredValue() {
			continue
		}
		id := strings.TrimSpace(todo.ID)
		if id == "" {
			continue
		}
		status := strings.TrimSpace(string(todo.Status))
		statusByID[id] = status
		switch status {
		case string(agentsession.TodoStatusPending):
			pendingIDs = append(pendingIDs, id)
		case string(agentsession.TodoStatusInProgress):
			inProgressIDs = append(inProgressIDs, id)
		case string(agentsession.TodoStatusBlocked):
			blockedIDs = append(blockedIDs, id)
		}
		if len(todo.Artifacts) > 0 {
			artifactsByID[id] = append([]string(nil), todo.Artifacts...)
		}
		if len(todo.ContentChecks) > 0 {
			checksByID[id] = buildVerifyTodoContentChecks(todo.ContentChecks)
		}
	}

	if len(pendingIDs) == 0 && len(inProgressIDs) == 0 && len(blockedIDs) == 0 {
		return nil
	}
	slices.Sort(pendingIDs)
	slices.Sort(inProgressIDs)
	slices.Sort(blockedIDs)

	return &verify.VerificationResult{
		Name:    "todo_convergence",
		Status:  verify.VerificationSoftBlock,
		Summary: "required todos are not converged",
		Reason:  "required todos are still pending, in progress, or blocked",
		Evidence: map[string]any{
			"pending_ids":     pendingIDs,
			"in_progress_ids": inProgressIDs,
			"blocked_ids":     blockedIDs,
			"todo_statuses":   statusByID,
			"todo_artifacts":  artifactsByID,
			"todo_checks":     checksByID,
		},
	}
}

// buildAcceptanceContinueHint 构造带 verifier 证据的 continue 提示，强制下一轮先补工具事实再尝试 final。
func buildAcceptanceContinueHint(decision acceptance.AcceptanceDecision) string {
	const actionDirective = "Do not claim completion with plain text. Next turn MUST call todo_write and/or verification tools to add objective facts before any final response."
	blockedReason := strings.TrimSpace(decision.CompletionBlockedReason)
	if len(decision.VerifierResults) == 0 && blockedReason == "" {
		if base := strings.TrimSpace(decision.ContinueHint); base != "" {
			return strings.TrimSpace(base + "\n" + actionDirective)
		}
		return strings.TrimSpace(finalContinueReminder + "\n" + actionDirective)
	}

	var builder strings.Builder
	builder.WriteString("<acceptance_continue>\n")
	if blockedReason != "" {
		builder.WriteString(fmt.Sprintf("<completion_blocked_reason>%s</completion_blocked_reason>\n", xmlEscape(blockedReason)))
	}
	builder.WriteString("<rule>")
	builder.WriteString(actionDirective)
	builder.WriteString("</rule>\n")

	if section := renderCompletionBlockedReasonHintSection(blockedReason, decision.VerifierResults); section != "" {
		builder.WriteString(section)
	}
	if section := renderTodoConvergenceHintSection(decision.VerifierResults); section != "" {
		builder.WriteString(section)
	}
	if section := renderVerifierFailureHintSection(decision.VerifierResults); section != "" {
		builder.WriteString(section)
	}
	builder.WriteString("</acceptance_continue>")
	return strings.TrimSpace(builder.String())
}

// renderCompletionBlockedReasonHintSection 根据 completion gate 阻塞原因输出结构化执行指令。
func renderCompletionBlockedReasonHintSection(
	blockedReason string,
	results []verify.VerificationResult,
) string {
	switch strings.TrimSpace(blockedReason) {
	case string(controlplane.CompletionBlockedReasonPendingTodo):
		pending := extractPendingTodoIDs(results)
		if len(pending) == 0 {
			return "<pending_todo><required_action>Use todo_write to move required todos to terminal states, then retry acceptance.</required_action></pending_todo>\n"
		}
		return fmt.Sprintf(
			"<pending_todo><open_required_ids>%s</open_required_ids><required_action>Use todo_write to close these required todos before final response.</required_action></pending_todo>\n",
			strings.Join(pending, ","),
		)
	case string(controlplane.CompletionBlockedReasonUnverifiedWrite):
		return "<unverified_write><required_action>Produce VerificationPerformed and VerificationPassed facts via verification tools before final response.</required_action></unverified_write>\n"
	case string(controlplane.CompletionBlockedReasonPostExecuteClosureRequired):
		return "<post_execute_closure_required><required_action>First close loop from latest tool results (todo updates/artifact checks), then retry final acceptance.</required_action></post_execute_closure_required>\n"
	default:
		return ""
	}
}

// extractPendingTodoIDs 从 verifier 证据提取 required 未收敛 todo 列表。
func extractPendingTodoIDs(results []verify.VerificationResult) []string {
	for _, result := range results {
		if strings.TrimSpace(result.Name) != "todo_convergence" {
			continue
		}
		evidence := result.Evidence
		if len(evidence) == 0 {
			return nil
		}
		ids := append([]string{}, evidenceStringList(evidence["pending_ids"])...)
		ids = append(ids, evidenceStringList(evidence["in_progress_ids"])...)
		ids = append(ids, evidenceStringList(evidence["blocked_ids"])...)
		return normalizeEvidenceList(ids)
	}
	return nil
}

// renderTodoConvergenceHintSection 渲染 todo_convergence 证据，明确 pending/in_progress/blocked 清单。
func renderTodoConvergenceHintSection(results []verify.VerificationResult) string {
	for _, result := range results {
		if strings.TrimSpace(result.Name) != "todo_convergence" {
			continue
		}
		evidence := result.Evidence
		if len(evidence) == 0 {
			return ""
		}
		pending := evidenceStringList(evidence["pending_ids"])
		inProgress := evidenceStringList(evidence["in_progress_ids"])
		blocked := evidenceStringList(evidence["blocked_ids"])
		waitingExternal := evidenceStringList(evidence["waiting_external_ids"])
		statuses := evidenceJSONPreview(evidence["todo_statuses"])
		artifacts := evidenceJSONPreview(evidence["todo_artifacts"])
		checks := evidenceJSONPreview(evidence["todo_checks"])

		var builder strings.Builder
		builder.WriteString("<todo_convergence>\n")
		builder.WriteString(fmt.Sprintf("<pending_ids>%s</pending_ids>\n", strings.Join(pending, ",")))
		builder.WriteString(fmt.Sprintf("<in_progress_ids>%s</in_progress_ids>\n", strings.Join(inProgress, ",")))
		builder.WriteString(fmt.Sprintf("<blocked_ids>%s</blocked_ids>\n", strings.Join(blocked, ",")))
		if len(waitingExternal) > 0 {
			builder.WriteString(fmt.Sprintf("<waiting_external_ids>%s</waiting_external_ids>\n", strings.Join(waitingExternal, ",")))
		}
		if statuses != "" {
			builder.WriteString(fmt.Sprintf("<todo_statuses>%s</todo_statuses>\n", xmlEscape(statuses)))
		}
		if artifacts != "" {
			builder.WriteString(fmt.Sprintf("<todo_artifacts>%s</todo_artifacts>\n", xmlEscape(artifacts)))
		}
		if checks != "" {
			builder.WriteString(fmt.Sprintf("<todo_checks>%s</todo_checks>\n", xmlEscape(checks)))
		}
		builder.WriteString("<required_action>For each listed todo, use todo_write status transitions and attach artifacts/check facts via tools. Do not finalize yet.</required_action>\n")
		builder.WriteString("</todo_convergence>\n")
		return builder.String()
	}
	return ""
}

// renderVerifierFailureHintSection 渲染非通过 verifier 的摘要，避免 continue 只有泛化提醒。
func renderVerifierFailureHintSection(results []verify.VerificationResult) string {
	nonPass := make([]verify.VerificationResult, 0, len(results))
	for _, result := range results {
		if result.Status == verify.VerificationPass {
			continue
		}
		nonPass = append(nonPass, result)
	}
	if len(nonPass) == 0 {
		return ""
	}
	sortVerificationResults(nonPass)

	var builder strings.Builder
	builder.WriteString("<verifier_evidence>\n")
	for _, result := range nonPass {
		builder.WriteString(fmt.Sprintf(
			"<verifier name=\"%s\" status=\"%s\"><summary>%s</summary><reason>%s</reason></verifier>\n",
			xmlEscape(strings.TrimSpace(result.Name)),
			xmlEscape(string(result.Status)),
			xmlEscape(strings.TrimSpace(result.Summary)),
			xmlEscape(strings.TrimSpace(result.Reason)),
		))
	}
	builder.WriteString("</verifier_evidence>\n")
	return builder.String()
}

// evidenceStringList 将 verifier evidence 中的字符串列表统一提取为去重、去空白后的有序值。
func evidenceStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return normalizeEvidenceList(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			switch raw := item.(type) {
			case string:
				values = append(values, raw)
			default:
				if encoded, err := json.Marshal(raw); err == nil {
					values = append(values, string(encoded))
				}
			}
		}
		return normalizeEvidenceList(values)
	default:
		return nil
	}
}

// evidenceJSONPreview 将 evidence 任意结构转成紧凑 JSON 文本，便于作为提示中的可执行事实。
func evidenceJSONPreview(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

// normalizeEvidenceList 对 evidence 文本列表做去重与排序，保证提示稳定可测。
func normalizeEvidenceList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	slices.Sort(normalized)
	return normalized
}

// sortVerificationResults 保证 verifier 输出顺序稳定，减少提示抖动。
func sortVerificationResults(results []verify.VerificationResult) {
	slices.SortFunc(results, func(a verify.VerificationResult, b verify.VerificationResult) int {
		return strings.Compare(strings.TrimSpace(a.Name), strings.TrimSpace(b.Name))
	})
}

// xmlEscape 对可见提示中的 verifier 文本做最小转义，避免破坏 XML 结构。
func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

// recordAcceptanceTerminal 将 acceptance 输出映射为 runtime 唯一终态记录。
func recordAcceptanceTerminal(state *runState, decision acceptance.AcceptanceDecision) {
	if state == nil {
		return
	}
	status := acceptance.TerminalStatusFromAcceptance(decision.Status)
	state.markTerminalDecision(status, decision.StopReason, strings.TrimSpace(decision.InternalSummary))
}

// buildVerifyTodos 将 session todo 转换为 verifier 快照。
func buildVerifyTodos(items []agentsession.TodoItem) []verify.TodoSnapshot {
	if len(items) == 0 {
		return nil
	}
	todos := make([]verify.TodoSnapshot, 0, len(items))
	for _, item := range items {
		todos = append(todos, verify.TodoSnapshot{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        strings.TrimSpace(string(item.Status)),
			Required:      item.RequiredValue(),
			BlockedReason: string(item.BlockedReasonValue()),
			Acceptance:    append([]string(nil), item.Acceptance...),
			Artifacts:     append([]string(nil), item.Artifacts...),
			Supersedes:    append([]string(nil), item.Supersedes...),
			ContentChecks: buildVerifyTodoContentChecks(item.ContentChecks),
			RetryCount:    item.RetryCount,
			RetryLimit:    item.RetryLimit,
			FailureReason: strings.TrimSpace(item.FailureReason),
		})
	}
	return todos
}

// buildVerifyTodoContentChecks 将 session 内容校验规则转换为 verifier 快照。
func buildVerifyTodoContentChecks(items []agentsession.TodoContentCheck) []verify.TodoContentCheckSnapshot {
	if len(items) == 0 {
		return nil
	}
	checks := make([]verify.TodoContentCheckSnapshot, 0, len(items))
	for _, item := range items {
		checks = append(checks, verify.TodoContentCheckSnapshot{
			Artifact: strings.TrimSpace(item.Artifact),
			Contains: append([]string(nil), item.Contains...),
		})
	}
	return checks
}

// buildVerifyTaskState 将 task_state 中与验收相关的结构化字段投影给 verifier。
func buildVerifyTaskState(state agentsession.TaskState) verify.TaskStateSnapshot {
	return verify.TaskStateSnapshot{
		VerificationProfile: string(state.VerificationProfile),
		KeyArtifacts:        append([]string(nil), state.KeyArtifacts...),
	}
}

// buildVerifyMessages 将会话消息压缩为 verifier 所需的最小快照。
func buildVerifyMessages(messages []providertypes.Message) []verify.MessageLike {
	if len(messages) == 0 {
		return nil
	}
	out := make([]verify.MessageLike, 0, len(messages))
	for _, message := range messages {
		out = append(out, verify.MessageLike{
			Role:    strings.TrimSpace(message.Role),
			Content: renderPartsForVerification(message.Parts),
		})
	}
	return out
}

// renderPartsForVerification 将消息分片合并为 verifier 侧可读文本。
func renderPartsForVerification(parts []providertypes.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartText {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		segments = append(segments, text)
	}
	return strings.Join(segments, "\n")
}

// applyAcceptanceResultProgress 根据 acceptance 输出更新 final 拦截计数唯一真相源。
func applyAcceptanceResultProgress(state *runState, decision acceptance.AcceptanceDecision) {
	if state == nil {
		return
	}
	switch decision.Status {
	case acceptance.AcceptanceContinue:
		if state.pendingFinalProgress {
			state.finalInterceptStreak = 0
		} else {
			state.finalInterceptStreak++
		}
	default:
		state.finalInterceptStreak = 0
	}
	state.pendingFinalProgress = false
}
