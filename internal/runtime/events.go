package runtime

import (
	"time"

	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

// EventType 标识 runtime 事件类型。
type EventType string

// RuntimeEvent 是 runtime 对外发送的统一事件结构。
type RuntimeEvent struct {
	Type           EventType
	RunID          string
	SessionID      string
	Turn           int
	Phase          string
	Timestamp      time.Time
	PayloadVersion int
	Payload        any
}

// PhaseChangedPayload 描述 phase 迁移。
type PhaseChangedPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BudgetCheckedPayload 为预算检查预留负载。
type BudgetCheckedPayload struct {
	AttemptSeq           int    `json:"attempt_seq"`
	RequestHash          string `json:"request_hash"`
	Action               string `json:"action"`
	Reason               string `json:"reason,omitempty"`
	EstimatedInputTokens int    `json:"estimated_input_tokens"`
	PromptBudget         int    `json:"prompt_budget"`
	EstimateSource       string `json:"estimate_source,omitempty"`
	EstimateGatePolicy   string `json:"estimate_gate_policy,omitempty"`
	ContextWindow        int    `json:"context_window,omitempty"`
}

// BudgetEstimateFailedPayload 描述预算估算失败时的降级诊断信息。
type BudgetEstimateFailedPayload struct {
	AttemptSeq  int    `json:"attempt_seq"`
	RequestHash string `json:"request_hash"`
	Message     string `json:"message"`
}

// ProgressEvaluatedPayload 汇总 progress 控制面的评估结果。
type ProgressEvaluatedPayload struct {
	Score controlplane.ProgressScore `json:"score"`
}

// StopReasonDecidedPayload 承载唯一停止原因决议结果。
type StopReasonDecidedPayload struct {
	Reason controlplane.StopReason `json:"reason"`
	Detail string                  `json:"detail,omitempty"`
}

// VerificationStartedPayload 描述 final 验收验证开始事件。
type VerificationStartedPayload struct {
	CompletionPassed        bool   `json:"completion_passed"`
	CompletionBlockedReason string `json:"completion_blocked_reason,omitempty"`
}

// VerificationStageFinishedPayload 描述单个 verifier 阶段完成事件。
type VerificationStageFinishedPayload struct {
	Name       string                    `json:"name"`
	Status     verify.VerificationStatus `json:"status"`
	Summary    string                    `json:"summary,omitempty"`
	Reason     string                    `json:"reason,omitempty"`
	ErrorClass verify.ErrorClass         `json:"error_class,omitempty"`
}

// VerificationFinishedPayload 描述整体验证流程结束事件。
type VerificationFinishedPayload struct {
	AcceptanceStatus acceptance.AcceptanceStatus `json:"acceptance_status"`
	StopReason       controlplane.StopReason     `json:"stop_reason,omitempty"`
	ErrorClass       verify.ErrorClass           `json:"error_class,omitempty"`
}

// VerificationCompletedPayload 描述验证通过并可完成的事件。
type VerificationCompletedPayload struct {
	StopReason controlplane.StopReason `json:"stop_reason,omitempty"`
}

// VerificationFailedPayload 描述验证失败事件。
type VerificationFailedPayload struct {
	StopReason controlplane.StopReason `json:"stop_reason,omitempty"`
	ErrorClass verify.ErrorClass       `json:"error_class,omitempty"`
}

// AcceptanceDecidedPayload 描述 acceptance engine 决议结果。
type AcceptanceDecidedPayload struct {
	Status                  acceptance.AcceptanceStatus `json:"status"`
	StopReason              controlplane.StopReason     `json:"stop_reason,omitempty"`
	ErrorClass              verify.ErrorClass           `json:"error_class,omitempty"`
	CompletionBlockedReason string                      `json:"completion_blocked_reason,omitempty"`
	UserVisibleSummary      string                      `json:"user_visible_summary,omitempty"`
	InternalSummary         string                      `json:"internal_summary,omitempty"`
	ContinueHint            string                      `json:"continue_hint,omitempty"`
}

// LedgerReconciledPayload 为账本对账预留负载。
type LedgerReconciledPayload struct {
	AttemptSeq      int    `json:"attempt_seq"`
	RequestHash     string `json:"request_hash"`
	InputTokens     int    `json:"input_tokens"`
	InputSource     string `json:"input_source"`
	OutputTokens    int    `json:"output_tokens"`
	OutputSource    string `json:"output_source"`
	HasUnknownUsage bool   `json:"has_unknown_usage"`
}

// newBudgetCheckedPayload 将预算决策对象展开为对外事件 payload，保持可观测字段稳定。
func newBudgetCheckedPayload(decision controlplane.TurnBudgetDecision) BudgetCheckedPayload {
	return BudgetCheckedPayload{
		AttemptSeq:           decision.ID.AttemptSeq,
		RequestHash:          decision.ID.RequestHash,
		Action:               string(decision.Action),
		Reason:               decision.Reason,
		EstimatedInputTokens: decision.EstimatedInputTokens,
		PromptBudget:         decision.PromptBudget,
		EstimateSource:       decision.EstimateSource,
		EstimateGatePolicy:   decision.EstimateGatePolicy,
		ContextWindow:        decision.ContextWindow,
	}
}

// newBudgetEstimateFailedPayload 将估算失败错误转换为 runtime 诊断事件 payload。
func newBudgetEstimateFailedPayload(id controlplane.TurnBudgetID, err error) BudgetEstimateFailedPayload {
	payload := BudgetEstimateFailedPayload{
		AttemptSeq:  id.AttemptSeq,
		RequestHash: id.RequestHash,
	}
	if err != nil {
		payload.Message = err.Error()
	}
	return payload
}

// newLedgerReconciledPayload 将 usage observation 与调和结果拼装为对外事件 payload。
func newLedgerReconciledPayload(
	observation TurnBudgetUsageObservation,
	result ledgerReconcileResult,
) LedgerReconciledPayload {
	return LedgerReconciledPayload{
		AttemptSeq:      observation.ID.AttemptSeq,
		RequestHash:     observation.ID.RequestHash,
		InputTokens:     result.inputTokens,
		InputSource:     result.inputSource,
		OutputTokens:    result.outputTokens,
		OutputSource:    result.outputSource,
		HasUnknownUsage: result.hasUnknownUsage,
	}
}

// PermissionRequestPayload 描述一次权限请求。
type PermissionRequestPayload struct {
	RequestID     string `json:"request_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolName      string `json:"tool_name"`
	ToolCategory  string `json:"tool_category"`
	ActionType    string `json:"action_type"`
	Operation     string `json:"operation"`
	TargetType    string `json:"target_type"`
	Target        string `json:"target"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	RuleID        string `json:"rule_id"`
	RememberScope string `json:"remember_scope,omitempty"`
}

// PermissionResolvedPayload 描述权限请求被处理后的状态。
type PermissionResolvedPayload struct {
	RequestID     string `json:"request_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolName      string `json:"tool_name"`
	ToolCategory  string `json:"tool_category"`
	ActionType    string `json:"action_type"`
	Operation     string `json:"operation"`
	TargetType    string `json:"target_type"`
	Target        string `json:"target"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	RuleID        string `json:"rule_id"`
	RememberScope string `json:"remember_scope,omitempty"`
	ResolvedAs    string `json:"resolved_as,omitempty"`
}

// SessionSkillEventPayload 描述会话级 skill 变更事件。
type SessionSkillEventPayload struct {
	SkillID string `json:"skill_id"`
}

// TodoViewItem 描述 Todo 列表单项快照，供 TUI/网关/桌面端统一渲染。
type TodoViewItem struct {
	ID            string   `json:"id"`
	Content       string   `json:"content"`
	Status        string   `json:"status"`
	Required      bool     `json:"required"`
	Artifacts     []string `json:"artifacts,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"`
	BlockedReason string   `json:"blocked_reason,omitempty"`
	Revision      int64    `json:"revision"`
}

// TodoSummary 描述 Todo 收敛摘要，避免客户端重复统计。
type TodoSummary struct {
	Total             int `json:"total"`
	RequiredTotal     int `json:"required_total"`
	RequiredCompleted int `json:"required_completed"`
	RequiredFailed    int `json:"required_failed"`
	RequiredOpen      int `json:"required_open"`
}

// TodoSnapshot 描述一次 Todo 视图快照。
type TodoSnapshot struct {
	Items   []TodoViewItem `json:"items,omitempty"`
	Summary TodoSummary    `json:"summary,omitempty"`
}

// TodoEventPayload 描述 todo_write 相关事件。
type TodoEventPayload struct {
	Action  string         `json:"action"`
	Reason  string         `json:"reason,omitempty"`
	Items   []TodoViewItem `json:"items,omitempty"`
	Summary TodoSummary    `json:"summary,omitempty"`
}

// InputNormalizedPayload 描述输入归一化完成后的摘要信息。
type InputNormalizedPayload struct {
	TextLength int `json:"text_length"`
	ImageCount int `json:"image_count"`
}

// AssetSavedPayload 描述单个附件成功保存后的结果。
type AssetSavedPayload struct {
	Index    int    `json:"index"`
	Path     string `json:"path,omitempty"`
	AssetID  string `json:"asset_id"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// AssetSaveFailedPayload 描述单个附件保存失败的结构化信息。
type AssetSaveFailedPayload struct {
	Index   int    `json:"index"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// RepositoryContextUnavailablePayload 描述 repository 事实注入失败但主链继续时的诊断信息。
type RepositoryContextUnavailablePayload struct {
	Stage  string `json:"stage"`
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason"`
}

// HookEventPayload 描述 hook 生命周期事件负载。
type HookEventPayload struct {
	HookID     string    `json:"hook_id"`
	Point      string    `json:"point"`
	Scope      string    `json:"scope"`
	Source     string    `json:"source"`
	Kind       string    `json:"kind"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// HookBlockedPayload 描述 hook 阻断事件负载。
type HookBlockedPayload struct {
	HookID     string `json:"hook_id"`
	Source     string `json:"source,omitempty"`
	Point      string `json:"point"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Enforced   bool   `json:"enforced"`
}

// HookNotificationPayload 描述异步回灌通知事件，仅用于可观测和下一轮临时提示。
type HookNotificationPayload struct {
	HookID       string `json:"hook_id"`
	Source       string `json:"source,omitempty"`
	Point        string `json:"point"`
	Status       string `json:"status,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Message      string `json:"message,omitempty"`
	DedupeKey    string `json:"dedupe_key,omitempty"`
	Notification string `json:"notification,omitempty"`
}

// RepoHooksTrustStoreInvalidPayload 描述 trust store 不可用时的降级信息。
type RepoHooksTrustStoreInvalidPayload struct {
	TrustStorePath string `json:"trust_store_path"`
	Reason         string `json:"reason"`
}

// RepoHooksLifecyclePayload 描述 repo hooks 发现/加载/跳过等生命周期信息。
type RepoHooksLifecyclePayload struct {
	Workspace      string `json:"workspace"`
	HooksPath      string `json:"hooks_path,omitempty"`
	TrustStorePath string `json:"trust_store_path,omitempty"`
	HookCount      int    `json:"hook_count,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

const (
	// EventUserMessage 表示用户消息已写入会话。
	EventUserMessage EventType = "user_message"
	// EventAgentChunk 表示 assistant 流式文本分片。
	EventAgentChunk EventType = "agent_chunk"
	// EventThinkingDelta 表示模型思考/推理内容的流式分片。
	EventThinkingDelta EventType = "thinking_delta"
	// EventAgentDone 表示 assistant 正常结束。
	EventAgentDone EventType = "agent_done"
	// EventToolStart 表示工具开始执行。
	EventToolStart EventType = "tool_start"
	// EventToolResult 表示工具执行完成并写回会话。
	EventToolResult EventType = "tool_result"
	// EventToolChunk 表示工具流式输出分片。
	EventToolChunk EventType = "tool_chunk"
	// EventToolDiff 表示写工具修改了某个文件。
	EventToolDiff EventType = "tool_diff"
	// EventRunCanceled 表示运行被取消。
	EventRunCanceled EventType = "run_canceled"
	// EventError 表示运行出现终止错误。
	EventError EventType = "error"
	// EventToolCallThinking 表示模型发起工具调用思考阶段。
	EventToolCallThinking EventType = "tool_call_thinking"
	// EventPermissionRequested 表示发起权限请求。
	EventPermissionRequested EventType = "permission_requested"
	// EventPermissionResolved 表示权限请求已决议。
	EventPermissionResolved EventType = "permission_resolved"
	// EventCompactStart 表示 compact 开始。
	EventCompactStart EventType = "compact_start"
	// EventCompactApplied 表示 compact 成功应用。
	EventCompactApplied EventType = "compact_applied"
	// EventCompactError 表示 compact 失败。
	EventCompactError EventType = "compact_error"
	// EventTokenUsage 表示 token 用量上报。
	EventTokenUsage EventType = "token_usage"
	// EventSkillActivated 表示 skill 激活。
	EventSkillActivated EventType = "skill_activated"
	// EventSkillDeactivated 表示 skill 停用。
	EventSkillDeactivated EventType = "skill_deactivated"
	// EventSkillMissing 表示会话记录的 skill 丢失。
	EventSkillMissing EventType = "skill_missing"
	// EventPhaseChanged 表示运行 phase 迁移。
	EventPhaseChanged EventType = "phase_changed"
	// EventBudgetChecked 表示预算控制面对冻结请求完成一次预算决策。
	EventBudgetChecked EventType = "budget_checked"
	// EventBudgetEstimateFailed 表示预算估算失败并进入降级放行。
	EventBudgetEstimateFailed EventType = "budget_estimate_failed"
	// EventProgressEvaluated 表示 progress 评估完成。
	EventProgressEvaluated EventType = "progress_evaluated"
	// EventStopReasonDecided 表示 stop reason 已决议。
	EventStopReasonDecided EventType = "stop_reason_decided"
	// EventVerificationStarted 表示 final 验证流程开始。
	EventVerificationStarted EventType = "verification_started"
	// EventVerificationStageFinished 表示单个 verifier 阶段完成。
	EventVerificationStageFinished EventType = "verification_stage_finished"
	// EventVerificationFinished 表示 final 验证流程结束。
	EventVerificationFinished EventType = "verification_finished"
	// EventVerificationCompleted 表示验证通过并可完成。
	EventVerificationCompleted EventType = "verification_completed"
	// EventVerificationFailed 表示验证失败。
	EventVerificationFailed EventType = "verification_failed"
	// EventAcceptanceDecided 表示 acceptance 决议已生成。
	EventAcceptanceDecided EventType = "acceptance_decided"
	// EventLedgerReconciled 表示本轮 usage 已按新账本语义完成调和。
	EventLedgerReconciled EventType = "ledger_reconciled"
	// EventTodoUpdated 表示 todo_write 成功更新。
	EventTodoUpdated EventType = "todo_updated"
	// EventTodoConflict 表示 todo_write 触发冲突类错误。
	EventTodoConflict EventType = "todo_conflict"
	// EventTodoSummaryInjected 表示本轮上下文注入了 Todo 摘要。
	EventTodoSummaryInjected EventType = "todo_summary_injected"
	// EventInputNormalized 表示用户输入已完成归一化。
	EventInputNormalized EventType = "input_normalized"
	// EventAssetSaved 表示本轮用户输入附件已完成持久化。
	EventAssetSaved EventType = "asset_saved"
	// EventAssetSaveFailed 表示本轮用户输入附件持久化失败。
	EventAssetSaveFailed EventType = "asset_save_failed"
	// EventRepositoryContextUnavailable 表示本轮 repository 事实本应获取但失败，已降级为空上下文。
	EventRepositoryContextUnavailable EventType = "repository_context_unavailable"
	// EventHookStarted 表示 hook 执行开始。
	EventHookStarted EventType = "hook_started"
	// EventHookFinished 表示 hook 执行结束。
	EventHookFinished EventType = "hook_finished"
	// EventHookFailed 表示 hook 执行失败。
	EventHookFailed EventType = "hook_failed"
	// EventHookBlocked 表示某个 hook 返回 block（是否生效由 payload.enforced 决定）。
	EventHookBlocked EventType = "hook_blocked"
	// EventHookNotification 表示异步 hook 触发的回灌通知（仅可观测，不改变主链状态）。
	EventHookNotification EventType = "hook_notification"
	// EventRepoHooksDiscovered 表示检测到仓库 hooks 配置文件。
	EventRepoHooksDiscovered EventType = "repo_hooks_discovered"
	// EventRepoHooksLoaded 表示仓库 hooks 已加载并进入执行链。
	EventRepoHooksLoaded EventType = "repo_hooks_loaded"
	// EventRepoHooksSkippedUntrusted 表示仓库未信任导致 repo hooks 被跳过。
	EventRepoHooksSkippedUntrusted EventType = "repo_hooks_skipped_untrusted"
	// EventRepoHooksTrustStoreInvalid 表示 trust store 缺失或损坏，已降级为 untrusted。
	EventRepoHooksTrustStoreInvalid EventType = "repo_hooks_trust_store_invalid"
	// EventRuntimeSnapshotUpdated 表示 runtime 统一状态快照已更新。
	EventRuntimeSnapshotUpdated EventType = "runtime_snapshot_updated"
	// EventFactsUpdated 表示运行事实快照已更新。
	EventFactsUpdated EventType = "facts_updated"
	// EventDecisionMade 表示 FinalDecider 已输出裁决。
	EventDecisionMade EventType = "decision_made"
	// EventSubAgentSnapshotUpdated 表示子代理状态快照已更新。
	EventSubAgentSnapshotUpdated EventType = "subagent_snapshot_updated"
	// EventTodoSnapshotUpdated 表示 todo 快照已更新。
	EventTodoSnapshotUpdated EventType = "todo_snapshot_updated"

	// EventCheckpointCreated 表示 pre-write checkpoint 已创建。
	EventCheckpointCreated EventType = "checkpoint_created"
	// EventCheckpointWarning 表示 checkpoint 创建过程中出现非致命告警。
	EventCheckpointWarning EventType = "checkpoint_warning"
	// EventCheckpointRestored 表示 checkpoint 已成功恢复。
	EventCheckpointRestored EventType = "checkpoint_restored"
	// EventCheckpointUndoRestore 表示 restore 已撤销。
	EventCheckpointUndoRestore EventType = "checkpoint_undo_restore"
	// EventBashSideEffect 表示 bash 命令在 workdir 内产生了文件变更。
	EventBashSideEffect EventType = "bash_side_effect"
)

// TokenUsagePayload 承载单轮 token 用量统计。
type TokenUsagePayload struct {
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	InputSource         string `json:"input_source,omitempty"`
	OutputSource        string `json:"output_source,omitempty"`
	HasUnknownUsage     bool   `json:"has_unknown_usage,omitempty"`
	SessionInputTokens  int    `json:"session_input_tokens"`
	SessionOutputTokens int    `json:"session_output_tokens"`
}

// CheckpointCreatedPayload 描述 checkpoint 创建成功事件。
type CheckpointCreatedPayload struct {
	CheckpointID         string `json:"checkpoint_id"`
	CodeCheckpointRef    string `json:"code_checkpoint_ref"`
	SessionCheckpointRef string `json:"session_checkpoint_ref"`
	CommitHash           string `json:"commit_hash"`
	Reason               string `json:"reason"`
}

// CheckpointWarningPayload 描述 checkpoint 创建过程中非致命告警。
type CheckpointWarningPayload struct {
	Error string `json:"error"`
	Phase string `json:"phase"`
}

// CheckpointRestoredPayload 描述 checkpoint 恢复成功事件。
type CheckpointRestoredPayload struct {
	CheckpointID      string `json:"checkpoint_id"`
	SessionID         string `json:"session_id"`
	GuardCheckpointID string `json:"guard_checkpoint_id"`
}

// CheckpointUndoRestorePayload 描述 restore 撤销事件。
type CheckpointUndoRestorePayload struct {
	GuardCheckpointID string `json:"guard_checkpoint_id"`
	SessionID         string `json:"session_id"`
}

// FileChange 描述一次文件变更的最小信息。
type FileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "added" | "modified" | "deleted"
}

// FileDiffEntry 描述单个文件的精确 diff（多文件工具下使用）。
// Kind 字段指示变更类型("added"/"modified"/"deleted")，向后兼容旧消费方（缺失时由 WasNew 折算）。
type FileDiffEntry struct {
	Path   string `json:"path"`
	Diff   string `json:"diff,omitempty"`
	WasNew bool   `json:"was_new,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// ToolDiffPayload 描述写工具修改了哪些文件。
// 单文件兼容字段（FilePath/Diff/WasNew）保留以支持现有消费方；多文件工具填充 Files+Diffs。
type ToolDiffPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	FilePath   string          `json:"file_path"`
	Diff       string          `json:"diff,omitempty"`
	WasNew     bool            `json:"was_new,omitempty"`
	Files      []FileChange    `json:"files,omitempty"`
	Diffs      []FileDiffEntry `json:"diffs,omitempty"`
}

// BashSideEffectPayload 描述 bash 命令在 workdir 内的文件变更。
type BashSideEffectPayload struct {
	ToolCallID                string       `json:"tool_call_id"`
	Command                   string       `json:"command,omitempty"`
	Changes                   []FileChange `json:"changes"`
	PreemptivelyCapturedPaths []string     `json:"preemptively_captured_paths,omitempty"`
	UncoveredPaths            []string     `json:"uncovered_paths,omitempty"`
}
