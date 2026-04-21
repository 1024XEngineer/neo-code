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

// StopReasonDecidedPayload 描述停止原因决策结果。
type StopReasonDecidedPayload struct {
	Reason StopReason `json:"reason"`
	Detail string     `json:"detail,omitempty"`
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

const (
	EventUserMessage         EventType = "user_message"
	EventAgentChunk          EventType = "agent_chunk"
	EventAgentDone           EventType = "agent_done"
	EventToolStart           EventType = "tool_start"
	EventToolResult          EventType = "tool_result"
	EventToolChunk           EventType = "tool_chunk"
	EventRunCanceled         EventType = "run_canceled"
	EventError               EventType = "error"
	EventToolCallThinking    EventType = "tool_call_thinking"
	EventProviderRetry       EventType = "provider_retry"
	EventPermissionRequested EventType = "permission_requested"
	EventPermissionResolved  EventType = "permission_resolved"
	EventCompactStart        EventType = "compact_start"
	EventCompactApplied      EventType = "compact_applied"
	EventCompactError        EventType = "compact_error"
	EventTokenUsage          EventType = "token_usage"
	EventSkillActivated      EventType = "skill_activated"
	EventSkillDeactivated    EventType = "skill_deactivated"
	EventSkillMissing        EventType = "skill_missing"
	EventPhaseChanged        EventType = "phase_changed"
	EventProgressEvaluated   EventType = "progress_evaluated"
	EventStopReasonDecided   EventType = "stop_reason_decided"
	EventTodoUpdated         EventType = "todo_updated"
	EventTodoConflict        EventType = "todo_conflict"
	EventTodoSummaryInjected EventType = "todo_summary_injected"
	EventInputNormalized     EventType = "input_normalized"
	EventAssetSaved          EventType = "asset_saved"
	EventAssetSaveFailed     EventType = "asset_save_failed"
)
