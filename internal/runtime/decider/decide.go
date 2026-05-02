package decider

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"neo-code/internal/runtime/facts"
)

// TODO(runtime-control-plane): FinalDecider 当前同时承担了意图推断、有效任务类型推导、事实解释、
// 终态裁决与下一步动作合成。待验收循环稳定后拆分为 IntentInferer、EffectiveTaskKindDeriver、
// AcceptanceProfile 与 NextActionPlanner，降低单模块职责耦合。
//
// Decide 执行最终终态裁决，作为 runtime 的唯一决策入口。
func Decide(input DecisionInput) Decision {
	intent := InferTaskIntent(input.UserGoal)
	hint := input.TaskKind
	if strings.TrimSpace(string(hint)) == "" {
		hint = intent.Hint
	}
	effectiveTaskKind := DeriveEffectiveTaskKind(hint, input.Facts, input.Todos)

	var decision Decision
	if input.Todos.Summary.RequiredFailed > 0 {
		decision = Decision{
			Status:             DecisionFailed,
			StopReason:         "required_todo_failed",
			UserVisibleSummary: "存在 required todo 失败，任务已终止。",
			InternalSummary:    "required todo entered failed terminal state",
		}
		return finalizeDecision(decision, hint, effectiveTaskKind)
	}
	if input.NoProgressExceeded {
		decision = Decision{
			Status:             DecisionIncomplete,
			StopReason:         "no_progress_after_final_intercept",
			UserVisibleSummary: "连续多轮缺少新事实，任务以未完成结束。",
			InternalSummary:    "no progress exceeded while final intercepted",
		}
		return finalizeDecision(decision, hint, effectiveTaskKind)
	}
	if !input.CompletionPassed {
		decision = continueWithCompletionReason(input)
		return finalizeDecision(decision, hint, effectiveTaskKind)
	}

	switch effectiveTaskKind {
	case TaskKindTodoState:
		decision = decideTodoState(input)
	case TaskKindWorkspaceWrite:
		decision = decideWorkspaceWrite(input)
	case TaskKindSubAgent:
		decision = decideSubAgent(input)
	case TaskKindReadOnly:
		decision = decideReadOnly(input)
	case TaskKindMixed:
		decision = decideMixed(input)
	case TaskKindChatAnswer:
		fallthrough
	default:
		decision = Decision{
			Status:             DecisionAccepted,
			StopReason:         "accepted",
			UserVisibleSummary: "任务完成。",
			InternalSummary:    "chat answer accepted by completion gate",
		}
	}
	return finalizeDecision(decision, hint, effectiveTaskKind)
}

// continueWithCompletionReason 把 completion gate 阻塞转成可执行缺失事实提示。
func continueWithCompletionReason(input DecisionInput) Decision {
	reason := strings.TrimSpace(input.CompletionReason)
	switch reason {
	case "pending_todo":
		openTodos := collectOpenRequiredTodos(input.Todos.Items)
		if len(openTodos) == 0 {
			return Decision{
				Status:       DecisionContinue,
				StopReason:   "todo_not_converged",
				MissingFacts: []MissingFact{{Kind: "required_todo_terminal"}},
				RequiredInput: &RequiredInput{
					Kind:    "missing_required_todo_id",
					Message: "缺少 required todo 标识，无法推进状态收敛。",
				},
				UserVisibleSummary: "仍有 required todo 未收敛，但当前无法确定待推进项。",
				InternalSummary:    "completion blocked by pending_todo without resolvable open todo id",
			}
		}
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:    "required_todo_terminal",
				Target:  strings.Join(openTodos, ","),
				Details: map[string]any{"open_required_ids": openTodos},
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "set_status",
					"id":     firstOrEmpty(openTodos),
					"status": "completed",
				},
			}},
			UserVisibleSummary: "仍有 required todo 未收敛，需要继续推进 todo 状态。",
			InternalSummary:    "completion blocked by pending_todo",
		}
	case "unverified_write":
		target, expectedContent, ok := selectVerificationTarget(input)
		if !ok {
			return Decision{
				Status:     DecisionContinue,
				StopReason: "todo_not_converged",
				MissingFacts: []MissingFact{{
					Kind: "file_written",
					Details: map[string]any{
						"reason": "cannot infer target path/content from user goal",
					},
				}},
				RequiredInput: &RequiredInput{
					Kind:    "missing_file_target_or_content",
					Message: "无法从当前任务中确定要验证的文件路径或内容，需要用户补充。",
				},
				UserVisibleSummary: "写入事实尚未完成验证，但当前缺少可执行验证目标。",
				InternalSummary:    "completion blocked by unverified_write without resolvable verification target",
			}
		}
		return buildWriteVerificationDecision(
			target,
			expectedContent,
			"写入事实尚未完成验证，需要补充 verification facts。",
			"completion blocked by unverified_write",
		)
	case "post_execute_closure_required":
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "post_execute_closure",
				Target: "latest_tool_results",
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_post_execute_closure",
				Message: "需要基于最新工具结果补充闭环信息后再尝试完成。",
			},
			UserVisibleSummary: "请先基于最新工具结果完成闭环，再尝试最终收尾。",
			InternalSummary:    "completion blocked by post_execute_closure_required",
		}
	default:
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "仍缺少可验证事实，请继续调用工具推进任务。",
			InternalSummary:    "completion gate blocked without classified reason",
		}
	}
}

// decideTodoState 依据 todo 快照判定状态类任务。
func decideTodoState(input DecisionInput) Decision {
	if input.Todos.Summary.Total == 0 && len(input.Facts.Todos.CreatedIDs) == 0 {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "todo_created",
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_todo_content",
				Message: "尚未提供 Todo 内容，需要用户补充待办事项。",
			},
			UserVisibleSummary: "尚未创建目标 Todo，请先调用 todo_write。",
			InternalSummary:    "todo_state task missing created todo facts",
		}
	}
	if input.Todos.Summary.RequiredOpen > 0 {
		openIDs := collectOpenRequiredTodos(input.Todos.Items)
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:    "required_todo_terminal",
				Target:  strings.Join(openIDs, ","),
				Details: map[string]any{"open_required_ids": openIDs},
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "todo_write",
				ArgsHint: map[string]any{
					"action": "set_status",
					"id":     firstOrEmpty(openIDs),
					"status": "completed",
				},
			}},
			UserVisibleSummary: "Todo 已创建但 required 项仍未完成。",
			InternalSummary:    "todo_state task still has open required todos",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "Todo 状态已满足任务目标。",
		InternalSummary:    "todo_state facts satisfied",
	}
}

// decideWorkspaceWrite 依据写入与验证事实判定文件任务。
func decideWorkspaceWrite(input DecisionInput) Decision {
	if len(input.Facts.Files.Written) == 0 {
		if hasSatisfiedWorkspaceWriteWithoutNewWrite(input) {
			return Decision{
				Status:             DecisionAccepted,
				StopReason:         "accepted",
				UserVisibleSummary: "目标文件状态已满足，无需重复写入。",
				InternalSummary:    "workspace_write satisfied by noop_write verification facts",
			}
		}
		if !hasExplicitFileTarget(input.UserGoal) {
			return Decision{
				Status:             DecisionAccepted,
				StopReason:         "accepted",
				UserVisibleSummary: "任务未声明明确文件目标，已按通用编辑任务收尾。",
				InternalSummary:    "workspace_write downgraded to generic edit due missing explicit file target",
			}
		}
		errorDetail := latestToolErrorDetail(input.Facts.Errors.ToolErrors, "filesystem_write_file")
		details := map[string]any{}
		if errorDetail != "" {
			details["last_write_error"] = errorDetail
		}
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:    "file_written",
				Details: details,
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_file_target_or_content",
				Message: "无法从当前任务中确定要写入的文件路径或内容，需要用户补充。",
			},
			UserVisibleSummary: "还没有写入事实，请先执行文件写入。",
			InternalSummary:    "workspace_write task missing file_written fact",
		}
	}
	target, expectedContent, ok := selectVerificationTarget(input)
	if !ok {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "verification_passed",
				Details: map[string]any{
					"reason": "missing resolvable verification target",
				},
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_file_target_or_content",
				Message: "无法确定当前需要验收的写入目标，请补充文件路径或重试写入。",
			},
			UserVisibleSummary: "已检测到写入事实，但无法确定验收目标。",
			InternalSummary:    "workspace_write has writes but cannot resolve verification target",
		}
	}
	if hasWorkspaceWriteHardFailure(input.Facts.Errors.ToolErrors, target) {
		return Decision{
			Status:             DecisionFailed,
			StopReason:         "verification_failed",
			UserVisibleSummary: "文件写入出现持续失败，任务终止。请检查路径权限或写入策略。",
			InternalSummary:    "workspace_write hard failure detected from tool error facts",
		}
	}
	if !hasVerificationForTarget(input.Facts, target) {
		return buildWriteVerificationDecision(
			target,
			expectedContent,
			"已写入文件但尚未形成通过的验证事实。",
			"workspace_write task missing",
		)
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "文件写入与验证事实已满足。",
		InternalSummary:    "workspace_write facts satisfied",
	}
}

// decideSubAgent 依据子代理启动/完成事实判定子代理任务。
func decideSubAgent(input DecisionInput) Decision {
	if len(input.Facts.SubAgents.Started) == 0 {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "subagent_started",
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_subagent_instruction",
				Message: "需要明确的子代理任务指令后才能执行 spawn_subagent。",
			},
			UserVisibleSummary: "尚未产生子代理启动事实，请显式调用 spawn_subagent。",
			InternalSummary:    "subagent task missing start fact",
		}
	}
	if len(input.Facts.SubAgents.Failed) > 0 && len(input.Facts.SubAgents.Completed) == 0 {
		return Decision{
			Status:             DecisionFailed,
			StopReason:         "verification_failed",
			UserVisibleSummary: "子代理执行失败，任务终止。",
			InternalSummary:    "subagent task failed without completion fact",
		}
	}
	if len(input.Facts.SubAgents.Completed) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "子代理已启动但尚未完成。",
			InternalSummary:    "subagent task started but no completed fact",
		}
	}
	if isWriteIntentGoal(input.UserGoal) && !hasSubAgentArtifactEvidence(input.Facts) {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "subagent_artifact_or_file_fact",
				Target: "workspace_artifact",
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_subagent_artifact_path",
				Message: "需要提供子代理产物路径或可验证的文件目标。",
			},
			UserVisibleSummary: "子代理已完成，但缺少可验证的产物事实。",
			InternalSummary:    "subagent completed without artifact/file evidence for write-intent goal",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "子代理完成事实已满足。",
		InternalSummary:    "subagent task completed facts satisfied",
	}
}

// decideReadOnly 判定只读任务是否可结束。
func decideReadOnly(input DecisionInput) Decision {
	if len(input.Facts.Files.Exists) == 0 && len(input.Facts.Commands.Executed) == 0 && len(input.LastAssistantText) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "尚无可验证读取事实，请先执行只读工具。",
			InternalSummary:    "read_only task has no read/search facts",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "只读分析任务已完成。",
		InternalSummary:    "read_only facts satisfied",
	}
}

// decideMixed 对混合任务采用保守策略：必须同时具备状态推进与至少一个验证事实。
func decideMixed(input DecisionInput) Decision {
	if len(input.Facts.Verification.Passed) == 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "混合任务尚未形成验证通过事实。",
			InternalSummary:    "mixed task missing verification passed facts",
		}
	}
	if input.Todos.Summary.RequiredOpen > 0 {
		return Decision{
			Status:             DecisionContinue,
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "混合任务 required todo 尚未收敛。",
			InternalSummary:    "mixed task has open required todos",
		}
	}
	return Decision{
		Status:             DecisionAccepted,
		StopReason:         "accepted",
		UserVisibleSummary: "混合任务事实已满足。",
		InternalSummary:    "mixed task satisfied by verification + todo closure",
	}
}

// buildWriteVerificationDecision 统一构造写入后缺少验证事实时的继续决策。
func buildWriteVerificationDecision(target string, expectedContent string, userSummary string, internalPrefix string) Decision {
	scope := fmt.Sprintf("artifact:%s", target)
	if expectedContent != "" {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:     "verification_passed",
				Target:   target,
				Expected: expectedContent,
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "filesystem_read_file",
				ArgsHint: map[string]any{
					"path":               target,
					"expect_contains":    []string{expectedContent},
					"verification_scope": scope,
				},
			}},
			UserVisibleSummary: userSummary,
			InternalSummary:    internalPrefix + " content verification facts bound to target artifact",
		}
	}
	return Decision{
		Status:     DecisionContinue,
		StopReason: "todo_not_converged",
		MissingFacts: []MissingFact{{
			Kind:   "file_exists",
			Target: target,
		}},
		RequiredNextActions: []RequiredAction{{
			Tool: "filesystem_glob",
			ArgsHint: map[string]any{
				"pattern":            target,
				"expect_min_matches": 1,
				"verification_scope": scope,
			},
		}},
		UserVisibleSummary: userSummary,
		InternalSummary:    internalPrefix + " existence verification facts bound to target artifact",
	}
}

// collectOpenRequiredTodos 收集 required 且未终态的 todo id。
func collectOpenRequiredTodos(items []TodoViewItem) []string {
	ids := make([]string, 0)
	for _, item := range items {
		if !item.Required {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "completed", "failed", "canceled":
			continue
		default:
			if id := strings.TrimSpace(item.ID); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// firstOrEmpty 返回首个元素，不存在时返回空串。
func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// hasVerificationForTarget 判断目标文件是否已经有通过的验证事实，避免跨文件误判 accepted。
func hasVerificationForTarget(allFacts facts.RuntimeFacts, targetPath string) bool {
	target := strings.TrimSpace(targetPath)
	if target == "" {
		return false
	}
	targetArtifactScope := "artifact:" + target
	normalizedTarget := strings.ToLower(filepath.Clean(target))

	for _, fact := range allFacts.Verification.Passed {
		scope := strings.TrimSpace(fact.Scope)
		if scope == "" {
			continue
		}
		normalizedScope := strings.ToLower(filepath.Clean(scope))
		if strings.EqualFold(scope, target) || strings.EqualFold(scope, targetArtifactScope) || normalizedScope == normalizedTarget {
			return true
		}
		if strings.HasPrefix(strings.ToLower(scope), "artifact:") {
			normalized := strings.TrimPrefix(scope, "artifact:")
			cleaned := strings.TrimSpace(normalized)
			if strings.EqualFold(cleaned, target) || strings.ToLower(filepath.Clean(cleaned)) == normalizedTarget {
				return true
			}
		}
	}
	for _, fact := range allFacts.Files.ContentMatch {
		if !fact.VerificationPassed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fact.Path), target) ||
			strings.ToLower(filepath.Clean(strings.TrimSpace(fact.Path))) == normalizedTarget {
			return true
		}
	}
	return false
}

// latestToolErrorDetail 返回指定工具的最新错误摘要，便于构造可执行 continue 提示。
func latestToolErrorDetail(errors []facts.ToolErrorFact, toolName string) string {
	targetTool := strings.TrimSpace(toolName)
	for i := len(errors) - 1; i >= 0; i-- {
		fact := errors[i]
		if !strings.EqualFold(strings.TrimSpace(fact.Tool), targetTool) {
			continue
		}
		content := strings.TrimSpace(fact.Content)
		if content == "" {
			content = strings.TrimSpace(fact.ErrorClass)
		}
		if content != "" {
			return content
		}
	}
	return ""
}

// hasWorkspaceWriteHardFailure 判断写入目标是否出现高置信不可恢复错误，防止无意义循环重试。
func hasWorkspaceWriteHardFailure(errors []facts.ToolErrorFact, targetPath string) bool {
	target := strings.TrimSpace(targetPath)
	if target == "" {
		return false
	}
	targetLower := strings.ToLower(target)
	targetBaseLower := strings.ToLower(strings.TrimSpace(filepath.Base(target)))
	errorCount := 0
	for _, fact := range errors {
		if !strings.EqualFold(strings.TrimSpace(fact.Tool), "filesystem_write_file") {
			continue
		}
		content := strings.ToLower(strings.TrimSpace(fact.Content))
		if content == "" {
			content = strings.ToLower(strings.TrimSpace(fact.ErrorClass))
		}
		mentionsTarget := strings.Contains(content, targetLower) ||
			(targetBaseLower != "" && strings.Contains(content, targetBaseLower))
		if !mentionsTarget {
			continue
		}
		if strings.Contains(content, "permission denied") ||
			strings.Contains(content, "path not allowed") || strings.Contains(content, "no such file") {
			errorCount++
		}
	}
	return errorCount >= 2
}

// isWriteIntentGoal 判断用户目标是否显式要求产物写入。
func isWriteIntentGoal(goal string) bool {
	return containsAny(strings.ToLower(strings.TrimSpace(goal)),
		"创建文件", "写入", "修改文件", "新增文件", "create file", "write file", "edit file", "update file", ".txt", ".go", ".md", ".json")
}

// hasExplicitFileTarget 判断用户目标是否包含可定位文件目标，避免对泛化“编辑一下”任务过度拦截。
func hasExplicitFileTarget(goal string) bool {
	normalized := strings.ToLower(strings.TrimSpace(goal))
	return containsAny(
		normalized,
		".txt", ".go", ".md", ".json", ".yaml", ".yml", ".ts", ".tsx", ".py", "/",
		"readme", "package.json",
	)
}

// hasSatisfiedWorkspaceWriteWithoutNewWrite 判断未产生新写入时是否已有可验收写入结果。
func hasSatisfiedWorkspaceWriteWithoutNewWrite(input DecisionInput) bool {
	goalPaths := extractGoalPaths(input.UserGoal)
	for _, goalPath := range goalPaths {
		if hasVerificationForTarget(input.Facts, goalPath) {
			return true
		}
	}
	if len(goalPaths) > 0 {
		return false
	}
	target, _, ok := selectVerificationTarget(input)
	return ok && hasVerificationForTarget(input.Facts, target)
}

// hasSubAgentArtifactEvidence 判断子代理任务是否已有可验证产物事实。
func hasSubAgentArtifactEvidence(allFacts facts.RuntimeFacts) bool {
	for _, fact := range allFacts.SubAgents.Completed {
		if len(fact.Artifacts) > 0 {
			return true
		}
	}
	if len(allFacts.Files.Written) > 0 || len(allFacts.Files.Exists) > 0 || len(allFacts.Files.ContentMatch) > 0 {
		return true
	}
	return false
}

// latestWriteVerificationHint 返回最适合下一步验证动作的写入目标与期望内容（若可用）。
func latestWriteVerificationHint(allFacts facts.RuntimeFacts, preferredPath string) (string, string) {
	normalizedPreferred := strings.TrimSpace(preferredPath)
	writes := allFacts.Files.Written
	for i := len(writes) - 1; i >= 0; i-- {
		fact := writes[i]
		path := strings.TrimSpace(fact.Path)
		if path == "" {
			continue
		}
		if normalizedPreferred != "" && !strings.EqualFold(path, normalizedPreferred) {
			continue
		}
		return path, strings.TrimSpace(fact.ExpectedContent)
	}
	if normalizedPreferred != "" {
		return normalizedPreferred, ""
	}
	return "", ""
}

// finalizeDecision 统一补全决策元信息，确保快照可观测 hint 与 effective kind。
func finalizeDecision(decision Decision, intentHint TaskKind, effective TaskKind) Decision {
	decision.IntentHint = intentHint
	decision.EffectiveTaskKind = effective
	return decision
}

var filePathPattern = regexp.MustCompile(`(?i)(?:^|[\s"'` + "`" + `])([a-z0-9_\-./]+\.[a-z0-9]{1,8})(?:$|[\s"'` + "`" + `,;:])`)

// selectVerificationTarget 选择当前回合应验证的写入目标，避免回退到历史首条写入。
func selectVerificationTarget(input DecisionInput) (path string, expectedContent string, ok bool) {
	goalPaths := extractGoalPaths(input.UserGoal)
	for i := len(goalPaths) - 1; i >= 0; i-- {
		goalPath := goalPaths[i]
		for j := len(input.Facts.Files.Written) - 1; j >= 0; j-- {
			writeFact := input.Facts.Files.Written[j]
			if strings.EqualFold(strings.TrimSpace(writeFact.Path), goalPath) {
				return strings.TrimSpace(writeFact.Path), strings.TrimSpace(writeFact.ExpectedContent), true
			}
		}
	}
	for i := len(input.Facts.Files.Written) - 1; i >= 0; i-- {
		writeFact := input.Facts.Files.Written[i]
		target := strings.TrimSpace(writeFact.Path)
		if target == "" {
			continue
		}
		if !hasVerificationForTarget(input.Facts, target) {
			return target, strings.TrimSpace(writeFact.ExpectedContent), true
		}
	}
	for i := len(input.Facts.Files.Written) - 1; i >= 0; i-- {
		writeFact := input.Facts.Files.Written[i]
		target := strings.TrimSpace(writeFact.Path)
		if target != "" {
			return target, strings.TrimSpace(writeFact.ExpectedContent), true
		}
	}
	for i := len(goalPaths) - 1; i >= 0; i-- {
		goalPath := strings.TrimSpace(goalPaths[i])
		if goalPath != "" && hasVerificationForTarget(input.Facts, goalPath) {
			return goalPath, "", true
		}
	}
	for i := len(input.Facts.Files.ContentMatch) - 1; i >= 0; i-- {
		matchFact := input.Facts.Files.ContentMatch[i]
		if !matchFact.VerificationPassed {
			continue
		}
		target := strings.TrimSpace(matchFact.Path)
		if target == "" {
			continue
		}
		return target, firstOrEmpty(matchFact.ExpectedContains), true
	}
	for i := len(input.Facts.Verification.Passed) - 1; i >= 0; i-- {
		verifyFact := input.Facts.Verification.Passed[i]
		scope := strings.TrimSpace(verifyFact.Scope)
		if scope == "" {
			continue
		}
		target := scope
		if strings.HasPrefix(strings.ToLower(target), "artifact:") {
			target = strings.TrimSpace(target[len("artifact:"):])
		}
		if target == "" {
			continue
		}
		return target, "", true
	}
	return "", "", false
}

// extractGoalPaths 从用户目标文本提取可能的文件路径。
func extractGoalPaths(goal string) []string {
	matches := filePathPattern.FindAllStringSubmatch(strings.TrimSpace(goal), -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if _, exists := seen[strings.ToLower(value)]; exists {
			continue
		}
		seen[strings.ToLower(value)] = struct{}{}
		out = append(out, value)
	}
	return out
}
