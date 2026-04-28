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
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
}

// PermissionResolvedPayload 描述权限请求处理结果。
type PermissionResolvedPayload struct {
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
	ResolvedAs    string
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
)

// StopReasonDecidedPayload 描述停止原因决策结果。
type StopReasonDecidedPayload struct {
	Reason StopReason `json:"reason"`
	Detail string     `json:"detail,omitempty"`
}

// VerificationStartedPayload 描述 final 验证阶段开始。
type VerificationStartedPayload struct {
	CompletionPassed bool `json:"completion_passed"`
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
	Status             string     `json:"status"`
	StopReason         StopReason `json:"stop_reason,omitempty"`
	ErrorClass         string     `json:"error_class,omitempty"`
	UserVisibleSummary string     `json:"user_visible_summary,omitempty"`
	InternalSummary    string     `json:"internal_summary,omitempty"`
	ContinueHint       string     `json:"continue_hint,omitempty"`
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

// TodoEventPayload 描述 todo 相关事件载荷。
type TodoEventPayload struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
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
	Point      string `json:"point"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Enforced   bool   `json:"enforced"`
}

const (
	EventUserMessage               EventType = "user_message"
	EventAgentChunk                EventType = "agent_chunk"
	EventAgentDone                 EventType = "agent_done"
	EventToolStart                 EventType = "tool_start"
	EventToolResult                EventType = "tool_result"
	EventToolChunk                 EventType = "tool_chunk"
	EventRunCanceled               EventType = "run_canceled"
	EventError                     EventType = "error"
	EventToolCallThinking          EventType = "tool_call_thinking"
	EventPermissionRequested       EventType = "permission_requested"
	EventPermissionResolved        EventType = "permission_resolved"
	EventCompactStart              EventType = "compact_start"
	EventCompactApplied            EventType = "compact_applied"
	EventCompactError              EventType = "compact_error"
	EventTokenUsage                EventType = "token_usage"
	EventSkillActivated            EventType = "skill_activated"
	EventSkillDeactivated          EventType = "skill_deactivated"
	EventSkillMissing              EventType = "skill_missing"
	EventPhaseChanged              EventType = "phase_changed"
	EventProgressEvaluated         EventType = "progress_evaluated"
	EventStopReasonDecided         EventType = "stop_reason_decided"
	EventVerificationStarted       EventType = "verification_started"
	EventVerificationStageFinished EventType = "verification_stage_finished"
	EventVerificationFinished      EventType = "verification_finished"
	EventVerificationCompleted     EventType = "verification_completed"
	EventVerificationFailed        EventType = "verification_failed"
	EventAcceptanceDecided         EventType = "acceptance_decided"
	EventTodoUpdated               EventType = "todo_updated"
	EventTodoConflict              EventType = "todo_conflict"
	EventTodoSummaryInjected       EventType = "todo_summary_injected"
	EventInputNormalized           EventType = "input_normalized"
	EventAssetSaved                EventType = "asset_saved"
	EventAssetSaveFailed           EventType = "asset_save_failed"
	EventHookStarted               EventType = "hook_started"
	EventHookFinished              EventType = "hook_finished"
	EventHookFailed                EventType = "hook_failed"
	EventHookBlocked               EventType = "hook_blocked"
)
