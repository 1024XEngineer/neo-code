package decider

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"neo-code/internal/runtime/facts"
)

// Decide 执行最终终态裁决，作为 runtime 的唯一决策入口。
func Decide(input DecisionInput) Decision {
	intent := input.TaskIntent
	if strings.TrimSpace(string(intent.Hint)) == "" {
		if strings.TrimSpace(string(input.TaskKind)) != "" {
			intent = TaskIntent{
				Hint:       input.TaskKind,
				Confidence: 0.6,
				Reasons:    []string{"provided_task_kind"},
			}
		} else {
			intent = InferTaskIntent(input.UserGoal)
		}
	}
	hint := intent.Hint
	if strings.TrimSpace(string(hint)) == "" {
		hint = input.TaskKind
	}
	if strings.TrimSpace(string(hint)) == "" {
		hint = TaskKindChatAnswer
	}
	effectiveKind := DeriveEffectiveTaskKind(hint, input.Facts, input.Todos)
	intent.Effective = effectiveKind
	input.TaskKind = effectiveKind

	if input.Todos.Summary.RequiredFailed > 0 {
		return Decision{
			Status:             DecisionFailed,
			StopReason:         "required_todo_failed",
			IntentHint:         intent,
			EffectiveTaskKind:  effectiveKind,
			UserVisibleSummary: "存在 required todo 失败，任务已终止。",
			InternalSummary:    "required todo entered failed terminal state",
		}
	}
	if input.NoProgressExceeded {
		return Decision{
			Status:             DecisionIncomplete,
			StopReason:         "no_progress_after_final_intercept",
			IntentHint:         intent,
			EffectiveTaskKind:  effectiveKind,
			UserVisibleSummary: "连续多轮缺少新事实，任务以未完成结束。",
			InternalSummary:    "no progress exceeded while final intercepted",
		}
	}
	if !input.CompletionPassed {
		return withDecisionContext(continueWithCompletionReason(input), intent, effectiveKind)
	}

	var out Decision
	switch input.TaskKind {
	case TaskKindTodoState:
		out = decideTodoState(input)
	case TaskKindWorkspaceWrite:
		out = decideWorkspaceWrite(input)
	case TaskKindSubAgent:
		out = decideSubAgent(input)
	case TaskKindReadOnly:
		out = decideReadOnly(input)
	case TaskKindMixed:
		out = decideMixed(input)
	case TaskKindChatAnswer:
		fallthrough
	default:
		out = Decision{
			Status:             DecisionAccepted,
			StopReason:         "accepted",
			UserVisibleSummary: "任务完成。",
			InternalSummary:    "chat answer accepted by completion gate",
		}
	}
	return withDecisionContext(out, intent, effectiveKind)
}

// continueWithCompletionReason 把 completion gate 阻塞转成可执行缺失事实提示。
func continueWithCompletionReason(input DecisionInput) Decision {
	reason := strings.TrimSpace(input.CompletionReason)
	switch reason {
	case "pending_todo":
		openTodos := collectOpenRequiredTodos(input.Todos.Items)
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
					Kind: "verification_target_missing",
					Details: map[string]any{
						"reason": "cannot infer target path/content from user goal",
					},
				}},
				RequiredInput: &RequiredInput{
					Kind:    "missing_file_target_or_content",
					Message: "无法从当前任务中确定要验证的文件路径或内容，需要用户补充。",
				},
				UserVisibleSummary: "写入事实尚未完成验证，但当前缺少可执行的验证目标。",
				InternalSummary:    "completion blocked by unverified_write without executable verification target",
			}
		}
		scope := "artifact:" + target
		if expectedContent != "" {
			return Decision{
				Status:     DecisionContinue,
				StopReason: "todo_not_converged",
				MissingFacts: []MissingFact{{
					Kind:     "verification_passed",
					Target:   target,
					Expected: expectedContent,
				}},
				RequiredNextActions: []RequiredAction{
					{
						Tool: "filesystem_read_file",
						ArgsHint: map[string]any{
							"path":               target,
							"expect_contains":    []string{expectedContent},
							"verification_scope": scope,
						},
					},
				},
				UserVisibleSummary: "写入事实尚未完成验证，需要补充 verification facts。",
				InternalSummary:    "completion blocked by unverified_write with content-aware verification action",
			}
		}
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "file_exists",
				Target: target,
			}},
			RequiredNextActions: []RequiredAction{
				{
					Tool: "filesystem_glob",
					ArgsHint: map[string]any{
						"pattern":            target,
						"expect_min_matches": 1,
						"verification_scope": scope,
					},
				},
			},
			UserVisibleSummary: "写入事实尚未完成验证，需要补充 verification facts。",
			InternalSummary:    "completion blocked by unverified_write with existence fallback action",
		}
	case "post_execute_closure_required":
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "post_execute_closure",
				Target: "latest_tool_results",
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_post_execute_closure_context",
				Message: "需要基于最新工具结果补充对应的收尾动作（例如 todo 更新或验证动作）。",
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
				Message: "尚未创建 Todo，且无法从当前任务确定 Todo 内容，请补充。",
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
				Message: "还没有写入事实，且缺少可直接执行的文件路径或内容，请补充明确目标。",
			},
			UserVisibleSummary: "还没有写入事实，请先执行文件写入。",
			InternalSummary:    "workspace_write task missing file_written fact",
		}
	}
	target, _, ok := selectVerificationTarget(input)
	if !ok {
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind: "verification_target_missing",
				Details: map[string]any{
					"reason": "no writable artifact found for verification target selection",
				},
			}},
			RequiredInput: &RequiredInput{
				Kind:    "missing_file_target",
				Message: "无法确定待验证文件，请补充路径或重新执行写入。",
			},
			UserVisibleSummary: "已存在写入事实，但无法确定当前轮次的验证目标。",
			InternalSummary:    "workspace_write has writes but no selectable verification target",
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
		verificationTarget, expectedContent, _ := selectVerificationTarget(input)
		scope := fmt.Sprintf("artifact:%s", verificationTarget)
		if expectedContent != "" {
			return Decision{
				Status:     DecisionContinue,
				StopReason: "todo_not_converged",
				MissingFacts: []MissingFact{{
					Kind:     "verification_passed",
					Target:   verificationTarget,
					Expected: expectedContent,
				}},
				RequiredNextActions: []RequiredAction{{
					Tool: "filesystem_read_file",
					ArgsHint: map[string]any{
						"path":               verificationTarget,
						"expect_contains":    []string{expectedContent},
						"verification_scope": scope,
					},
				}},
				UserVisibleSummary: "已写入文件但尚未形成通过的验证事实。",
				InternalSummary:    "workspace_write task missing content verification facts bound to target artifact",
			}
		}
		return Decision{
			Status:     DecisionContinue,
			StopReason: "todo_not_converged",
			MissingFacts: []MissingFact{{
				Kind:   "file_exists",
				Target: verificationTarget,
			}},
			RequiredNextActions: []RequiredAction{{
				Tool: "filesystem_glob",
				ArgsHint: map[string]any{
					"pattern":            verificationTarget,
					"expect_min_matches": 1,
					"verification_scope": scope,
				},
			}},
			UserVisibleSummary: "已写入文件但尚未形成通过的验证事实。",
			InternalSummary:    "workspace_write task missing existence verification facts bound to target artifact",
		}
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
				Message: "尚未产生子代理启动事实，且缺少可直接执行的子代理任务指令。",
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
				Kind:    "missing_subagent_artifact_pattern",
				Message: "子代理已完成，但缺少可验证的产物路径或模式，需要补充。",
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
	if len(input.Facts.Files.Exists) == 0 &&
		len(input.Facts.Commands.Executed) == 0 &&
		len(input.Facts.Verification.Passed) == 0 &&
		len(input.LastAssistantText) == 0 {
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
	targetBase := strings.TrimSpace(filepath.Base(target))
	targetArtifactScope := "artifact:" + target

	for _, fact := range allFacts.Verification.Passed {
		scope := strings.TrimSpace(fact.Scope)
		if scope == "" {
			continue
		}
		if strings.EqualFold(scope, target) || strings.EqualFold(scope, targetBase) || strings.EqualFold(scope, targetArtifactScope) {
			return true
		}
		if strings.HasPrefix(strings.ToLower(scope), "artifact:") {
			normalized := strings.TrimPrefix(scope, "artifact:")
			if strings.EqualFold(strings.TrimSpace(normalized), target) || strings.EqualFold(strings.TrimSpace(normalized), targetBase) {
				return true
			}
		}
	}
	for _, fact := range allFacts.Files.ContentMatch {
		if !fact.VerificationPassed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fact.Path), target) || strings.EqualFold(strings.TrimSpace(filepath.Base(fact.Path)), targetBase) {
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
	errorCount := 0
	for _, fact := range errors {
		if !strings.EqualFold(strings.TrimSpace(fact.Tool), "filesystem_write_file") {
			continue
		}
		content := strings.ToLower(strings.TrimSpace(fact.Content))
		if content == "" {
			content = strings.ToLower(strings.TrimSpace(fact.ErrorClass))
		}
		if strings.Contains(content, strings.ToLower(target)) || strings.Contains(content, "permission denied") ||
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

// DeriveEffectiveTaskKind 基于运行事实与 todo 快照修正任务类型，文本仅作为弱提示。
func DeriveEffectiveTaskKind(hint TaskKind, allFacts facts.RuntimeFacts, todos TodoSnapshot) TaskKind {
	hasWrite := len(allFacts.Files.Written) > 0 || len(allFacts.Files.ContentMatch) > 0
	hasVerification := len(allFacts.Verification.Passed) > 0
	hasSubAgent := len(allFacts.SubAgents.Started) > 0 || len(allFacts.SubAgents.Completed) > 0 || len(allFacts.SubAgents.Failed) > 0
	hasTodo := todos.Summary.Total > 0 || todos.Summary.RequiredTotal > 0 || todos.Summary.RequiredOpen > 0 ||
		len(allFacts.Todos.CreatedIDs) > 0 || len(allFacts.Todos.CompletedIDs) > 0 || len(allFacts.Todos.FailedIDs) > 0
	hasRead := len(allFacts.Files.Exists) > 0 || len(allFacts.Commands.Executed) > 0

	switch {
	case hasSubAgent && (hasWrite || hasTodo || hasVerification):
		return TaskKindMixed
	case hasSubAgent:
		return TaskKindSubAgent
	case hasWrite && (hasTodo || hasVerification || hasRead):
		return TaskKindWorkspaceWrite
	case hasWrite:
		return TaskKindWorkspaceWrite
	case hasTodo && !hasWrite:
		return TaskKindTodoState
	case hasRead || hasVerification:
		return TaskKindReadOnly
	default:
		if strings.TrimSpace(string(hint)) == "" {
			return TaskKindChatAnswer
		}
		return hint
	}
}

// selectVerificationTarget 选择当前任务应验证的目标文件与期望内容。
func selectVerificationTarget(input DecisionInput) (path string, expectedContent string, ok bool) {
	writes := input.Facts.Files.Written
	if len(writes) == 0 {
		return "", "", false
	}

	if goalPath := detectGoalFilePath(input.UserGoal); goalPath != "" {
		for i := len(writes) - 1; i >= 0; i-- {
			writePath := strings.TrimSpace(writes[i].Path)
			if strings.EqualFold(writePath, goalPath) {
				return writePath, strings.TrimSpace(writes[i].ExpectedContent), true
			}
		}
	}

	for i := len(writes) - 1; i >= 0; i-- {
		writePath := strings.TrimSpace(writes[i].Path)
		if writePath == "" {
			continue
		}
		if !hasVerificationForTarget(input.Facts, writePath) {
			return writePath, strings.TrimSpace(writes[i].ExpectedContent), true
		}
	}

	for i := len(writes) - 1; i >= 0; i-- {
		writePath := strings.TrimSpace(writes[i].Path)
		if writePath != "" {
			return writePath, strings.TrimSpace(writes[i].ExpectedContent), true
		}
	}
	return "", "", false
}

// detectGoalFilePath 尝试从用户目标中抽取显式文件路径。
func detectGoalFilePath(goal string) string {
	text := strings.TrimSpace(goal)
	if text == "" {
		return ""
	}
	separators := strings.NewReplacer("，", " ", ",", " ", "。", " ", "\n", " ", "\t", " ", "\"", " ", "'", " ", "(", " ", ")", " ")
	tokens := strings.Fields(separators.Replace(text))
	for _, token := range tokens {
		candidate := strings.TrimSpace(token)
		if candidate == "" {
			continue
		}
		candidate = strings.Trim(candidate, "[]{}:;")
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, "/") || strings.Contains(lower, ".") {
			if hasFileSuffix(lower) {
				return candidate
			}
		}
	}
	return ""
}

func hasFileSuffix(path string) bool {
	return containsAny(path, ".txt", ".go", ".md", ".json", ".yaml", ".yml", ".ts", ".tsx", ".py")
}

func withDecisionContext(decision Decision, intent TaskIntent, effective TaskKind) Decision {
	decision.IntentHint = intent
	decision.EffectiveTaskKind = effective
	decision.RequiredNextActions = filterExecutableActions(decision.RequiredNextActions)
	return decision
}

func filterExecutableActions(actions []RequiredAction) []RequiredAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]RequiredAction, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Tool) == "" {
			continue
		}
		if hasPlaceholderAction(action) {
			continue
		}
		out = append(out, action)
	}
	return out
}

func hasPlaceholderAction(action RequiredAction) bool {
	raw, err := json.Marshal(action)
	if err != nil {
		return false
	}
	text := string(raw)
	return strings.Contains(text, "<") || strings.Contains(text, ">")
}
