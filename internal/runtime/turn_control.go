package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type toolExecutionSummary struct {
	Calls                       []providertypes.ToolCall
	Results                     []tools.ToolResult
	HasSuccessfulWorkspaceWrite bool
	HasSuccessfulVerification   bool
}

// collectCompletionState 基于当前运行态与本轮 assistant 行为生成 completion 输入。
func collectCompletionState(
	state *runState,
	_ providertypes.Message,
	_ bool,
) controlplane.CompletionState {
	current := state.completion
	current.HasPendingAgentTodos = hasPendingAgentTodos(state.session.Todos)
	if current.TodoOnlyTaskCandidate && current.TodoStateSatisfied {
		current.HasPendingAgentTodos = false
	}
	// 当存在 required TODO 且全部已终态时，清除 unverified writes 标记。
	// required TODO 全部收敛本身就证明工作已完成，无需额外 verification tool 调用。
	if !current.HasPendingAgentTodos && hasCompletedRequiredTodos(state.session.Todos) {
		current.HasUnverifiedWrites = false
	}
	return current
}

// applyToolExecutionCompletion 更新一轮工具执行后的 completion 事实。
func applyToolExecutionCompletion(current controlplane.CompletionState, summary toolExecutionSummary) controlplane.CompletionState {
	if !current.TodoOnlyTaskCandidate {
		// keep false
	} else {
		for _, call := range summary.Calls {
			if !strings.EqualFold(strings.TrimSpace(call.Name), tools.ToolNameTodoWrite) {
				current.TodoOnlyTaskCandidate = false
				break
			}
		}
	}
	if len(summary.Results) == 0 {
		if summary.HasSuccessfulWorkspaceWrite {
			current.HasUnverifiedWrites = true
		}
		if summary.HasSuccessfulVerification {
			current.HasUnverifiedWrites = false
		}
		return current
	}
	for _, result := range summary.Results {
		if result.IsError {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(result.Name), tools.ToolNameTodoWrite) {
			if stateFact, ok := result.Metadata["state_fact"].(string); ok {
				normalized := strings.TrimSpace(stateFact)
				if normalized != "" {
					current.TodoStateChanged = true
					switch normalized {
					case "todo_created", "todo_updated", "todo_completed", "todo_failed":
						current.TodoStateSatisfied = true
					}
				}
			}
		}
		if result.Facts.WorkspaceWrite && !toolResultNoopWrite(result.Metadata) {
			current.HasUnverifiedWrites = true
		}
		if result.Facts.VerificationPerformed && result.Facts.VerificationPassed {
			current.HasUnverifiedWrites = false
		}
	}
	return current
}

// collectProgressInput 基于执行前后事实组装 progress 评估输入。
func collectProgressInput(
	runState controlplane.RunState,
	beforeTask agentsession.TaskState,
	afterTask agentsession.TaskState,
	beforeTodos []agentsession.TodoItem,
	afterTodos []agentsession.TodoItem,
	summary toolExecutionSummary,
	noProgressLimit int,
	repeatLimit int,
) controlplane.ProgressInput {
	evidence := deriveProgressEvidence(beforeTask, afterTask, beforeTodos, afterTodos, summary)
	return controlplane.ProgressInput{
		RunState:             runState,
		Evidence:             evidence,
		CurrentToolSignature: computeToolSignature(summary.Calls),
		ResultFingerprint:    computeToolResultFingerprint(summary.Results),
		SubgoalFingerprint:   computeSubgoalFingerprint(afterTask, afterTodos, summary.Calls),
		NoProgressLimit:      noProgressLimit,
		RepeatCycleLimit:     repeatLimit,
	}
}

// deriveProgressEvidence 从本轮前后快照和工具摘要中提取结构化 evidence。
func deriveProgressEvidence(
	beforeTask agentsession.TaskState,
	afterTask agentsession.TaskState,
	beforeTodos []agentsession.TodoItem,
	afterTodos []agentsession.TodoItem,
	summary toolExecutionSummary,
) []controlplane.ProgressEvidenceRecord {
	var evidence []controlplane.ProgressEvidenceRecord

	if computeTaskStateSignature(beforeTask) != computeTaskStateSignature(afterTask) {
		evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceTaskStateChanged})
	}
	if computeTodoStateSignature(beforeTodos) != computeTodoStateSignature(afterTodos) {
		evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceTodoStateChanged})
	}
	if summary.HasSuccessfulWorkspaceWrite {
		evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceWriteApplied})
	}
	if summary.HasSuccessfulVerification {
		evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceVerifyPassed})
	}
	if hasSuccessfulInformationalResult(summary.Results) {
		evidence = append(evidence, controlplane.ProgressEvidenceRecord{Kind: controlplane.EvidenceNewInfoNonDup})
	}
	return evidence
}

// computeTaskStateSignature 计算 task_state 的结构化签名。
func computeTaskStateSignature(task agentsession.TaskState) string {
	encoded, err := json.Marshal(task.Clone())
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// computeToolResultFingerprint 计算本轮工具结果的聚合指纹。
func computeToolResultFingerprint(results []tools.ToolResult) string {
	if len(results) == 0 {
		return ""
	}
	type normalizedResult struct {
		Name       string `json:"name"`
		IsError    bool   `json:"is_error"`
		Content    string `json:"content"`
		ErrorClass string `json:"error_class,omitempty"`
	}

	normalized := make([]normalizedResult, 0, len(results))
	for _, result := range results {
		if strings.TrimSpace(result.Name) == "" {
			return ""
		}
		entry := normalizedResult{
			Name:    strings.TrimSpace(result.Name),
			IsError: result.IsError,
			Content: normalizeToolResultContent(result.Content),
		}
		if result.IsError {
			entry.ErrorClass = classifyToolError(result)
		}
		normalized = append(normalized, entry)
	}

	encoded, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// computeSubgoalFingerprint 生成当前轮子目标的轻量指纹。
func computeSubgoalFingerprint(
	task agentsession.TaskState,
	todos []agentsession.TodoItem,
	calls []providertypes.ToolCall,
) string {
	type subgoalSnapshot struct {
		NextStep  string   `json:"next_step,omitempty"`
		OpenItems []string `json:"open_items,omitempty"`
		Todos     []string `json:"todos,omitempty"`
	}

	snapshot := subgoalSnapshot{
		NextStep:  strings.TrimSpace(task.NextStep),
		OpenItems: append([]string(nil), task.OpenItems...),
	}
	for _, item := range todos {
		if item.Status.IsTerminal() {
			continue
		}
		snapshot.Todos = append(snapshot.Todos, strings.TrimSpace(item.Content))
	}
	if snapshot.NextStep == "" && len(snapshot.OpenItems) == 0 && len(snapshot.Todos) == 0 {
		return computeToolSignature(calls)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// hasPendingAgentTodos 判断当前 session 中是否仍存在未闭合 todo。
func hasPendingAgentTodos(items []agentsession.TodoItem) bool {
	for _, item := range items {
		if !item.RequiredValue() {
			continue
		}
		if item.Status.IsTerminal() {
			continue
		}
		return true
	}
	return false
}

// hasCompletedRequiredTodos 判断是否存在至少一个 required TODO 且全部已终态。
func hasCompletedRequiredTodos(items []agentsession.TodoItem) bool {
	hasRequired := false
	for _, item := range items {
		if !item.RequiredValue() {
			continue
		}
		hasRequired = true
		if !item.Status.IsTerminal() {
			return false
		}
	}
	return hasRequired
}

// hasSuccessfulInformationalResult 判断本轮是否至少获得一个成功的非写入工具结果。
func hasSuccessfulInformationalResult(results []tools.ToolResult) bool {
	for _, result := range results {
		if result.IsError {
			continue
		}
		switch strings.TrimSpace(result.Name) {
		case tools.ToolNameFilesystemWriteFile, tools.ToolNameFilesystemEdit:
			continue
		default:
			return true
		}
	}
	return false
}

// shouldPromotePendingFinalProgress 判断本轮执行结果是否可以作为 final 拦截后的有效推进信号。
func shouldPromotePendingFinalProgress(
	score controlplane.ProgressScore,
	summary toolExecutionSummary,
	completion controlplane.CompletionState,
	lastBlockedReason string,
) bool {
	if score.HasBusinessProgress {
		return true
	}
	if !score.HasExplorationProgress {
		return false
	}

	// 只读 read/glob 首次探索可算推进；同签名/同结果/同子目标且阻塞原因未变化时，不再重置 final 拦截计数。
	if hasSuccessfulReadOrGlobResult(summary.Results) &&
		score.SameToolSignature &&
		score.SameResultFingerprint &&
		score.SameSubgoal == controlplane.SubgoalRelationSame &&
		strings.EqualFold(
			strings.TrimSpace(lastBlockedReason),
			strings.TrimSpace(string(completion.CompletionBlockedReason)),
		) {
		return false
	}
	return true
}

// hasSuccessfulReadOrGlobResult 判断本轮是否存在成功的 filesystem_read_file / filesystem_glob 结果。
func hasSuccessfulReadOrGlobResult(results []tools.ToolResult) bool {
	for _, result := range results {
		if result.IsError {
			continue
		}
		switch strings.TrimSpace(result.Name) {
		case tools.ToolNameFilesystemReadFile, tools.ToolNameFilesystemGlob:
			return true
		}
	}
	return false
}

// hasSuccessfulVerificationResult 判断本轮是否存在显式验证成功的结构化事实。
func hasSuccessfulVerificationResult(results []tools.ToolResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if result.IsError || !result.Facts.VerificationPerformed || !result.Facts.VerificationPassed {
			continue
		}
		return true
	}
	return false
}

// normalizeToolResultContent 对工具结果文本做稳定化裁剪，避免无关差异放大指纹抖动。
func normalizeToolResultContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= 256 {
		return trimmed
	}
	return trimmed[:256]
}

// classifyToolError 为错误结果生成轻量分类，避免直接依赖完整错误文案。
func classifyToolError(result tools.ToolResult) string {
	if explicit := strings.TrimSpace(result.ErrorClass); explicit != "" {
		return explicit
	}
	trimmed := strings.ToLower(strings.TrimSpace(result.Content))
	switch {
	case strings.Contains(trimmed, "timeout"):
		return "timeout"
	case strings.Contains(trimmed, "denied"):
		return "permission_denied"
	case strings.Contains(trimmed, "not found"):
		return "not_found"
	default:
		return "generic_error"
	}
}
