package services

import (
	"context"
	"time"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

// Runtime 定义 TUI 与运行时交互所需的最小契约。
type Runtime interface {
	Submit(ctx context.Context, input PrepareInput) error
	PrepareUserInput(ctx context.Context, input PrepareInput) (UserInput, error)
	Run(ctx context.Context, input UserInput) error
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	ExecuteSystemTool(ctx context.Context, input SystemToolInput) (tools.ToolResult, error)
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]agentsession.Summary, error)
	LoadSession(ctx context.Context, id string) (agentsession.Session, error)
	ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	ListSessionSkills(ctx context.Context, sessionID string) ([]SessionSkillState, error)
	ListAvailableSkills(ctx context.Context, sessionID string) ([]AvailableSkillState, error)
}

// EventType 标识运行时事件类型。
type EventType string

// RuntimeEvent 表示 TUI 消费的统一事件结构。
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

// UserInput 描述一次归一化后的用户输入。
type UserInput struct {
	SessionID string
	RunID     string
	Parts     []providertypes.ContentPart
	Workdir   string
	Mode      string
	TaskID    string
	AgentID   string
}

// UserImageInput 表示用户输入中的图片引用。
type UserImageInput struct {
	Path     string
	MimeType string
}

// PrepareInput 表示提交前的输入载荷。
type PrepareInput struct {
	SessionID string
	RunID     string
	Workdir   string
	Mode      string
	Text      string
	Images    []UserImageInput
}

// SystemToolInput 描述系统工具调用入参。
type SystemToolInput struct {
	SessionID string
	RunID     string
	Workdir   string
	ToolName  string
	Arguments []byte
}

// CompactInput 描述一次 compact 请求。
type CompactInput struct {
	SessionID string
	RunID     string
}

// CompactResult 描述 compact 成功后结果。
type CompactResult struct {
	Applied        bool
	BeforeChars    int
	AfterChars     int
	BeforeTokens   int
	SavedRatio     float64
	TriggerMode    string
	TranscriptID   string
	TranscriptPath string
}

// CompactErrorPayload 描述 compact 失败信息。
type CompactErrorPayload struct {
	TriggerMode string `json:"trigger_mode"`
	Message     string `json:"message"`
}

// PermissionResolutionInput 描述权限决策提交。
type PermissionResolutionInput struct {
	RequestID string
	Decision  PermissionResolutionDecision
}

// PermissionResolutionDecision 表示权限审批决策。
type PermissionResolutionDecision string

const (
	DecisionAllowOnce    PermissionResolutionDecision = "allow_once"
	DecisionAllowSession PermissionResolutionDecision = "allow_session"
	DecisionReject       PermissionResolutionDecision = "reject"
)

// PermissionRequestPayload 描述权限请求事件载荷。
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

// PermissionResolvedPayload 描述权限请求处理结果。
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

// SessionSkillState 描述会话技能状态。
type SessionSkillState struct {
	SkillID    string
	Missing    bool
	Descriptor *skills.Descriptor
}

// SessionSkillEventPayload 描述技能事件载荷。
type SessionSkillEventPayload struct {
	SkillID string `json:"skill_id"`
}

// AvailableSkillState 描述可用技能状态。
type AvailableSkillState struct {
	Descriptor skills.Descriptor
	Active     bool
}

// SessionLogEntry 描述日志查看器持久化条目。
type SessionLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// PhaseChangedPayload 描述阶段切换信息。
type PhaseChangedPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// StopReason 表示运行终止原因。
type StopReason string

const (
	// StopReasonUserInterrupt 表示 runtime 当前协议中的用户中断原因。
	StopReasonUserInterrupt StopReason = "user_interrupt"
	// StopReasonFatalError 表示 runtime 当前协议中的不可恢复错误原因。
	StopReasonFatalError StopReason = "fatal_error"
	// StopReasonBudgetExceeded 表示 runtime 当前协议中的预算超限停止原因。
	StopReasonBudgetExceeded StopReason = "budget_exceeded"
	// StopReasonMaxTurnExceeded 表示 runtime 达到最大轮次上限后的受控停止原因。
	StopReasonMaxTurnExceeded StopReason = "max_turn_exceeded"
	// StopReasonVerificationFailed 表示验证失败。
	StopReasonVerificationFailed StopReason = "verification_failed"
	// StopReasonAccepted 表示双门控通过并被 acceptance 接受。
	StopReasonAccepted StopReason = "accepted"
	// StopReasonTodoNotConverged 表示 required todo 未收敛。
	StopReasonTodoNotConverged StopReason = "todo_not_converged"
	// StopReasonTodoWaitingExternal 表示 todo 等待外部输入。
	StopReasonTodoWaitingExternal StopReason = "todo_waiting_external"
	// StopReasonNoProgressAfterFinalIntercept 表示 final 被拦截后长期无进展。
	StopReasonNoProgressAfterFinalIntercept StopReason = "no_progress_after_final_intercept"
	// StopReasonMaxTurnExceededWithUnconvergedTodos 表示 max turn + todo 未收敛。
	StopReasonMaxTurnExceededWithUnconvergedTodos StopReason = "max_turn_exceeded_with_unconverged_todos"
	// StopReasonMaxTurnExceededWithFailedVerification 表示 max turn + verification 失败。
	StopReasonMaxTurnExceededWithFailedVerification StopReason = "max_turn_exceeded_with_failed_verification"
	// StopReasonVerificationConfigMissing 表示 verification 必需配置缺失。
	StopReasonVerificationConfigMissing StopReason = "verification_config_missing"
	// StopReasonVerificationExecutionDenied 表示 verification 命令被策略拒绝。
	StopReasonVerificationExecutionDenied StopReason = "verification_execution_denied"
	// StopReasonVerificationExecutionError 表示 verification 命令执行异常。
	StopReasonVerificationExecutionError StopReason = "verification_execution_error"
	// StopReasonRequiredTodoFailed 表示 required todo 已失败终止。
	StopReasonRequiredTodoFailed StopReason = "required_todo_failed"
)

// StopReasonDecidedPayload 描述停止原因决策结果。
type StopReasonDecidedPayload struct {
	Reason StopReason `json:"reason"`
	Detail string     `json:"detail,omitempty"`
}

// VerificationStartedPayload 描述 final 验证阶段开始。
type VerificationStartedPayload struct {
	CompletionPassed        bool   `json:"completion_passed"`
	CompletionBlockedReason string `json:"completion_blocked_reason,omitempty"`
}

// VerificationStageFinishedPayload 描述单个 verifier 阶段结果。
type VerificationStageFinishedPayload struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Summary    string `json:"summary,omitempty"`
	Reason     string `json:"reason,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
}

// VerificationFinishedPayload 描述验证流程结束结果。
type VerificationFinishedPayload struct {
	AcceptanceStatus string     `json:"acceptance_status"`
	StopReason       StopReason `json:"stop_reason,omitempty"`
	ErrorClass       string     `json:"error_class,omitempty"`
}

// VerificationCompletedPayload 描述验证通过并可完成。
type VerificationCompletedPayload struct {
	StopReason StopReason `json:"stop_reason,omitempty"`
}

// VerificationFailedPayload 描述验证失败信息。
type VerificationFailedPayload struct {
	StopReason StopReason `json:"stop_reason,omitempty"`
	ErrorClass string     `json:"error_class,omitempty"`
}

// AcceptanceDecidedPayload 描述 acceptance 引擎输出。
type AcceptanceDecidedPayload struct {
	Status                  string     `json:"status"`
	StopReason              StopReason `json:"stop_reason,omitempty"`
	ErrorClass              string     `json:"error_class,omitempty"`
	CompletionBlockedReason string     `json:"completion_blocked_reason,omitempty"`
	UserVisibleSummary      string     `json:"user_visible_summary,omitempty"`
	InternalSummary         string     `json:"internal_summary,omitempty"`
	ContinueHint            string     `json:"continue_hint,omitempty"`
}

// TokenUsagePayload 描述 runtime 当前 token_usage 事件载荷。
type TokenUsagePayload struct {
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	InputSource         string `json:"input_source,omitempty"`
	OutputSource        string `json:"output_source,omitempty"`
	HasUnknownUsage     bool   `json:"has_unknown_usage,omitempty"`
	SessionInputTokens  int    `json:"session_input_tokens"`
	SessionOutputTokens int    `json:"session_output_tokens"`
}

// TodoViewItem 描述 todo 列表中单项快照。
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

// TodoSummary 描述 todo 收敛摘要。
type TodoSummary struct {
	Total             int `json:"total"`
	RequiredTotal     int `json:"required_total"`
	RequiredCompleted int `json:"required_completed"`
	RequiredFailed    int `json:"required_failed"`
	RequiredOpen      int `json:"required_open"`
}

// TodoEventPayload 描述 todo 相关事件载荷。
type TodoEventPayload struct {
	Action  string         `json:"action"`
	Reason  string         `json:"reason,omitempty"`
	Items   []TodoViewItem `json:"items,omitempty"`
	Summary TodoSummary    `json:"summary,omitempty"`
}

// RuntimeSnapshot 描述 runtime 统一状态快照。
type RuntimeSnapshot struct {
	RunID     string           `json:"run_id"`
	SessionID string           `json:"session_id"`
	Phase     string           `json:"phase,omitempty"`
	TaskKind  string           `json:"task_kind,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
	Todos     TodoSnapshot     `json:"todos"`
	Facts     FactsSnapshot    `json:"facts"`
	Decision  DecisionSnapshot `json:"decision,omitempty"`
	SubAgents SubAgentSnapshot `json:"subagents,omitempty"`
}

// TodoSnapshot 描述统一 Todo 快照。
type TodoSnapshot struct {
	Items   []TodoViewItem `json:"items,omitempty"`
	Summary TodoSummary    `json:"summary,omitempty"`
}

// FactsSnapshot 描述统一事实快照。
type FactsSnapshot struct {
	RuntimeFacts map[string]any `json:"runtime_facts"`
}

// DecisionSnapshot 描述最终裁决快照。
type DecisionSnapshot struct {
	Status              string           `json:"status,omitempty"`
	StopReason          string           `json:"stop_reason,omitempty"`
	MissingFacts        []map[string]any `json:"missing_facts,omitempty"`
	RequiredNextActions []map[string]any `json:"required_next_actions,omitempty"`
	UserVisibleSummary  string           `json:"user_visible_summary,omitempty"`
	InternalSummary     string           `json:"internal_summary,omitempty"`
}

// SubAgentSnapshot 描述子代理事实聚合快照。
type SubAgentSnapshot struct {
	StartedCount   int `json:"started_count"`
	CompletedCount int `json:"completed_count"`
	FailedCount    int `json:"failed_count"`
}

// RuntimeSnapshotUpdatedPayload 描述 runtime_snapshot_updated 事件。
type RuntimeSnapshotUpdatedPayload struct {
	Reason   string          `json:"reason,omitempty"`
	Snapshot RuntimeSnapshot `json:"snapshot"`
}

// FactsUpdatedPayload 描述 facts_updated 事件。
type FactsUpdatedPayload struct {
	Reason string        `json:"reason,omitempty"`
	Facts  FactsSnapshot `json:"facts"`
}

// DecisionMadePayload 描述 decision_made 事件。
type DecisionMadePayload struct {
	Status              string           `json:"status"`
	StopReason          string           `json:"stop_reason,omitempty"`
	MissingFacts        []map[string]any `json:"missing_facts,omitempty"`
	RequiredNextActions []map[string]any `json:"required_next_actions,omitempty"`
	UserVisibleSummary  string           `json:"user_visible_summary,omitempty"`
	InternalSummary     string           `json:"internal_summary,omitempty"`
}

// SubAgentSnapshotUpdatedPayload 描述 subagent_snapshot_updated 事件。
type SubAgentSnapshotUpdatedPayload struct {
	Reason   string           `json:"reason,omitempty"`
	SubAgent SubAgentSnapshot `json:"subagent"`
}

// SubAgentEventPayload 描述子代理执行生命周期事件载荷。
type SubAgentEventPayload struct {
	Role       string `json:"role"`
	TaskID     string `json:"task_id"`
	State      string `json:"state"`
	StopReason string `json:"stop_reason,omitempty"`
	Step       int    `json:"step,omitempty"`
	QueueSize  int    `json:"queue_size,omitempty"`
	Running    int    `json:"running,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Delta      string `json:"delta,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SubAgentToolCallEventPayload 描述子代理工具调用事件载荷。
type SubAgentToolCallEventPayload struct {
	Role      string `json:"role"`
	TaskID    string `json:"task_id"`
	ToolName  string `json:"tool_name"`
	Decision  string `json:"decision"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

// InputNormalizedPayload 描述输入归一化摘要。
type InputNormalizedPayload struct {
	TextLength int `json:"text_length"`
	ImageCount int `json:"image_count"`
}

// AssetSavedPayload 描述附件保存成功信息。
type AssetSavedPayload struct {
	Index    int    `json:"index"`
	Path     string `json:"path,omitempty"`
	AssetID  string `json:"asset_id"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// AssetSaveFailedPayload 描述附件保存失败信息。
type AssetSaveFailedPayload struct {
	Index   int    `json:"index"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// HookEventPayload 描述 hook 生命周期事件。
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

// HookBlockedPayload 描述 hook 阻断事件。
type HookBlockedPayload struct {
	HookID     string `json:"hook_id"`
	Source     string `json:"source,omitempty"`
	Point      string `json:"point"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Enforced   bool   `json:"enforced"`
}

// HookNotificationPayload 描述异步 hook 回灌通知事件。
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
	EventUserMessage                EventType = "user_message"
	EventAgentChunk                 EventType = "agent_chunk"
	EventAgentDone                  EventType = "agent_done"
	EventToolStart                  EventType = "tool_start"
	EventToolResult                 EventType = "tool_result"
	EventToolChunk                  EventType = "tool_chunk"
	EventRunCanceled                EventType = "run_canceled"
	EventError                      EventType = "error"
	EventToolCallThinking           EventType = "tool_call_thinking"
	EventPermissionRequested        EventType = "permission_requested"
	EventPermissionResolved         EventType = "permission_resolved"
	EventCompactStart               EventType = "compact_start"
	EventCompactApplied             EventType = "compact_applied"
	EventCompactError               EventType = "compact_error"
	EventTokenUsage                 EventType = "token_usage"
	EventSkillActivated             EventType = "skill_activated"
	EventSkillDeactivated           EventType = "skill_deactivated"
	EventSkillMissing               EventType = "skill_missing"
	EventPhaseChanged               EventType = "phase_changed"
	EventProgressEvaluated          EventType = "progress_evaluated"
	EventStopReasonDecided          EventType = "stop_reason_decided"
	EventVerificationStarted        EventType = "verification_started"
	EventVerificationStageFinished  EventType = "verification_stage_finished"
	EventVerificationFinished       EventType = "verification_finished"
	EventVerificationCompleted      EventType = "verification_completed"
	EventVerificationFailed         EventType = "verification_failed"
	EventAcceptanceDecided          EventType = "acceptance_decided"
	EventTodoUpdated                EventType = "todo_updated"
	EventTodoConflict               EventType = "todo_conflict"
	EventTodoSummaryInjected        EventType = "todo_summary_injected"
	EventInputNormalized            EventType = "input_normalized"
	EventAssetSaved                 EventType = "asset_saved"
	EventAssetSaveFailed            EventType = "asset_save_failed"
	EventHookStarted                EventType = "hook_started"
	EventHookFinished               EventType = "hook_finished"
	EventHookFailed                 EventType = "hook_failed"
	EventHookBlocked                EventType = "hook_blocked"
	EventHookNotification           EventType = "hook_notification"
	EventRepoHooksDiscovered        EventType = "repo_hooks_discovered"
	EventRepoHooksLoaded            EventType = "repo_hooks_loaded"
	EventRepoHooksSkippedUntrusted  EventType = "repo_hooks_skipped_untrusted"
	EventRepoHooksTrustStoreInvalid EventType = "repo_hooks_trust_store_invalid"
	EventSubAgentStarted            EventType = "subagent_started"
	EventSubAgentProgress           EventType = "subagent_progress"
	EventSubAgentRetried            EventType = "subagent_retried"
	EventSubAgentBlocked            EventType = "subagent_blocked"
	EventSubAgentCompleted          EventType = "subagent_completed"
	EventSubAgentFailed             EventType = "subagent_failed"
	EventSubAgentCanceled           EventType = "subagent_canceled"
	EventSubAgentFinished           EventType = "subagent_finished"
	EventSubAgentToolCallStarted    EventType = "subagent_tool_call_started"
	EventSubAgentToolCallResult     EventType = "subagent_tool_call_result"
	EventSubAgentToolCallDenied     EventType = "subagent_tool_call_denied"
	EventRuntimeSnapshotUpdated     EventType = "runtime_snapshot_updated"
	EventFactsUpdated               EventType = "facts_updated"
	EventDecisionMade               EventType = "decision_made"
	EventSubAgentSnapshotUpdated    EventType = "subagent_snapshot_updated"
	EventTodoSnapshotUpdated        EventType = "todo_snapshot_updated"
)
