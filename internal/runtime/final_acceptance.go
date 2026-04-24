package runtime

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

const finalContinueReminder = "There are unfinished required todos or unmet acceptance checks. Continue execution. Do not finalize yet."

// beforeAcceptFinal 在 runtime 接受模型 final 前执行双门控验收。
func (s *Service) beforeAcceptFinal(
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
	if !verificationCfg.FinalInterceptValue() {
		// final_intercept 关闭时仅绕过 verifier，不得绕过 completion gate。
		verificationCfg.Enabled = boolPtrRuntime(false)
	}

	policy := acceptance.DefaultPolicy{
		Executor: verify.PolicyCommandExecutor{},
	}
	engine := acceptance.NewEngine(policy)

	maxNoProgress := verificationCfg.MaxNoProgress
	if maxNoProgress <= 0 {
		maxNoProgress = 3
	}
	noProgressStreak := state.progress.LastScore.NoProgressStreak
	if noProgressStreak < 0 {
		noProgressStreak = 0
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
			RuntimeState: verify.RuntimeStateSnapshot{
				Turn:                 state.turn,
				MaxTurns:             resolveRuntimeMaxTurns(snapshot.Config.Runtime),
				MaxTurnsReached:      maxTurnsReached,
				FinalInterceptStreak: noProgressStreak,
			},
			Metadata: map[string]any{
				"task_type": inferTaskType(state),
			},
			VerificationConfig: verificationCfg,
		},
		NoProgressExceeded: maxNoProgress > 0 && noProgressStreak >= maxNoProgress,
		MaxTurnsReached:    maxTurnsReached,
		MaxTurnsLimit:      maxTurnsLimit,
	}

	decision, err := engine.EvaluateFinal(ctx, input)
	if err != nil {
		return acceptance.AcceptanceDecision{}, err
	}
	// 继续分支复用 runtime progress 结果，避免把“final 被拦截”误判为“无进展”。
	if decision.Status == acceptance.AcceptanceContinue && hasRuntimeProgress(state) {
		decision.HasProgress = true
	}
	return decision, nil
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
			RetryCount:    item.RetryCount,
			RetryLimit:    item.RetryLimit,
			FailureReason: strings.TrimSpace(item.FailureReason),
		})
	}
	return todos
}

// buildVerifyMessages 将会话消息压缩为 verifier 所需最小快照。
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

// inferTaskType 基于 task_id 与 task_state 文本推断当前任务类型。
func inferTaskType(state *runState) string {
	if state == nil {
		return "unknown"
	}
	corpus := strings.ToLower(strings.TrimSpace(
		state.taskID + " " + state.session.TaskState.Goal + " " + state.session.TaskState.NextStep,
	))
	switch {
	case containsAny(corpus,
		"fix bug", "bugfix", "修 bug", "修bug", "修复", "排查", "故障", "报错",
	):
		return "fix_bug"
	case containsAny(corpus, "refactor", "重构", "代码整理", "结构优化"):
		return "refactor"
	case containsAny(corpus, "edit code", "modify code", "patch", "修改代码", "改代码", "代码调整", "打补丁"):
		return "edit_code"
	case containsAny(corpus, "create file", "scaffold", "创建文件", "新建文件", "新增文件", "脚手架"):
		return "create_file"
	case containsAny(corpus, "docs", "documentation", "文档", "readme", "说明文档"):
		return "docs"
	case containsAny(corpus, "config", "yaml", "json", "toml", "配置", "yml", "环境变量"):
		return "config"
	default:
		return "unknown"
	}
}

// containsAny 判断语料中是否包含任一关键字。
func containsAny(corpus string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(corpus, strings.TrimSpace(keyword)) {
			return true
		}
	}
	return false
}

// boolPtrRuntime 返回 bool 指针，便于在运行期快速构造配置快照字段。
func boolPtrRuntime(value bool) *bool {
	v := value
	return &v
}

// applyAcceptanceResultProgress 根据 acceptance 输出更新 final 拦截熔断计数器。
func applyAcceptanceResultProgress(state *runState, decision acceptance.AcceptanceDecision) {
	if state == nil {
		return
	}
	switch decision.Status {
	case acceptance.AcceptanceContinue:
		if decision.HasProgress {
			state.finalInterceptStreak = 0
			return
		}
		state.finalInterceptStreak++
	default:
		state.finalInterceptStreak = 0
	}
}

// hasRuntimeProgress 判断 runtime 当前快照是否存在业务或探索进展。
func hasRuntimeProgress(state *runState) bool {
	if state == nil {
		return false
	}
	score := state.progress.LastScore
	return score.HasBusinessProgress || score.HasExplorationProgress
}
