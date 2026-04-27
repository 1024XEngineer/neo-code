package gateway

import (
	"context"
	"time"

	"neo-code/internal/tools"
)

// RuntimeEventType 表示运行时事件类型。
type RuntimeEventType string

const (
	// RuntimeEventTypeRunProgress 表示运行过程事件。
	RuntimeEventTypeRunProgress RuntimeEventType = "run_progress"
	// RuntimeEventTypeRunDone 表示运行完成事件。
	RuntimeEventTypeRunDone RuntimeEventType = "run_done"
	// RuntimeEventTypeRunError 表示运行失败事件。
	RuntimeEventTypeRunError RuntimeEventType = "run_error"
)

// PermissionResolutionDecision 表示权限审批最终决策。
type PermissionResolutionDecision string

const (
	// PermissionResolutionAllowOnce 表示仅本次允许。
	PermissionResolutionAllowOnce PermissionResolutionDecision = "allow_once"
	// PermissionResolutionAllowSession 表示在当前会话持续允许。
	PermissionResolutionAllowSession PermissionResolutionDecision = "allow_session"
	// PermissionResolutionReject 表示拒绝本次审批。
	PermissionResolutionReject PermissionResolutionDecision = "reject"
)

// PermissionResolutionInput 表示一次权限审批决策输入。
type PermissionResolutionInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string `json:"subject_id,omitempty"`
	// RequestID 是待审批请求标识。
	RequestID string `json:"request_id"`
	// Decision 是审批决策值。
	Decision PermissionResolutionDecision `json:"decision"`
}

// RunInput 表示网关向下游运行端口发起 run 动作时的输入。
type RunInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是会话标识。
	SessionID string
	// RunID 是运行标识。
	RunID string
	// InputText 是文本输入。
	InputText string
	// InputParts 是多模态输入分片。
	InputParts []InputPart
	// Workdir 是请求级工作目录覆盖值。
	Workdir string
}

// CompactInput 表示网关向下游运行端口发起 compact 动作时的输入。
type CompactInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是会话标识。
	SessionID string
	// RunID 是运行标识。
	RunID string
}

// ExecuteSystemToolInput 表示 gateway.executeSystemTool 动作的下游输入。
type ExecuteSystemToolInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是会话标识，可选。
	SessionID string
	// RunID 是运行标识，可选。
	RunID string
	// Workdir 是请求级工作目录覆盖值，可选。
	Workdir string
	// ToolName 是要执行的系统工具名。
	ToolName string
	// Arguments 是工具参数 JSON 字节串。
	Arguments []byte
}

// SessionSkillMutationInput 表示会话技能启停动作输入。
type SessionSkillMutationInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是目标会话标识。
	SessionID string
	// SkillID 是目标技能标识。
	SkillID string
}

// ListSessionSkillsInput 表示查询会话激活技能列表动作输入。
type ListSessionSkillsInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是目标会话标识。
	SessionID string
}

// ListAvailableSkillsInput 表示查询可用技能列表动作输入。
type ListAvailableSkillsInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是可选会话标识，用于附带激活态。
	SessionID string
}

// CancelInput 表示 gateway.cancel 动作的下游输入。
type CancelInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是可选会话标识。
	SessionID string
	// RunID 是必须显式指定的目标运行标识。
	RunID string
}

// LoadSessionInput 表示 gateway.loadSession 动作的下游输入。
type LoadSessionInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是目标会话标识。
	SessionID string
}

// CompactResult 表示 compact 动作完成后返回的结果。
type CompactResult struct {
	// Applied 表示是否实际应用压缩结果。
	Applied bool
	// BeforeChars 是压缩前字符数。
	BeforeChars int
	// AfterChars 是压缩后字符数。
	AfterChars int
	// SavedRatio 是压缩节省比例。
	SavedRatio float64
	// TriggerMode 是触发模式标识。
	TriggerMode string
	// TranscriptID 是压缩产物标识。
	TranscriptID string
	// TranscriptPath 是压缩产物路径。
	TranscriptPath string
}

// RuntimeEvent 表示运行端口推送给网关的统一事件。
type RuntimeEvent struct {
	// Type 是事件类型。
	Type RuntimeEventType `json:"type"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// Payload 是事件扩展载荷。
	Payload any `json:"payload,omitempty"`
}

// ToolCall 表示助手消息中的工具调用元数据。
type ToolCall struct {
	// ID 是工具调用标识。
	ID string `json:"id"`
	// Name 是工具名。
	Name string `json:"name"`
	// Arguments 是工具参数 JSON 字符串。
	Arguments string `json:"arguments"`
}

// SessionMessage 表示会话消息快照中的单条消息。
type SessionMessage struct {
	// Role 是消息角色。
	Role string `json:"role"`
	// Content 是消息内容。
	Content string `json:"content"`
	// ToolCalls 是 assistant 发起的工具调用元数据。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 是工具消息关联的调用标识。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// IsError 表示该消息是否为错误结果。
	IsError bool `json:"is_error,omitempty"`
}

// Session 表示网关视角的会话详情。
type Session struct {
	// ID 是会话标识。
	ID string `json:"id"`
	// Title 是会话标题。
	Title string `json:"title"`
	// CreatedAt 是会话创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 是会话更新时间。
	UpdatedAt time.Time `json:"updated_at"`
	// Workdir 是会话工作目录。
	Workdir string `json:"workdir,omitempty"`
	// Messages 是会话消息快照。
	Messages []SessionMessage `json:"messages,omitempty"`
}

// SessionSummary 表示会话列表项摘要。
type SessionSummary struct {
	// ID 是会话标识。
	ID string `json:"id"`
	// Title 是会话标题。
	Title string `json:"title"`
	// CreatedAt 是会话创建时间。
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt 是会话更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// SkillSource 描述技能来源元数据。
type SkillSource struct {
	// Kind 表示技能来源类型（local/builtin）。
	Kind string `json:"kind"`
	// Layer 表示技能来源层级（project/global）。
	Layer string `json:"layer,omitempty"`
	// RootDir 表示来源根目录。
	RootDir string `json:"root_dir,omitempty"`
	// SkillDir 表示技能目录。
	SkillDir string `json:"skill_dir,omitempty"`
	// FilePath 表示技能入口文件路径。
	FilePath string `json:"file_path,omitempty"`
}

// SkillDescriptor 描述技能元信息。
type SkillDescriptor struct {
	// ID 是技能唯一标识。
	ID string `json:"id"`
	// Name 是技能展示名称。
	Name string `json:"name,omitempty"`
	// Description 是技能说明。
	Description string `json:"description,omitempty"`
	// Version 是技能版本号。
	Version string `json:"version,omitempty"`
	// Source 是技能来源信息。
	Source SkillSource `json:"source"`
	// Scope 是技能激活作用域。
	Scope string `json:"scope,omitempty"`
}

// SessionSkillState 描述会话技能状态。
type SessionSkillState struct {
	// SkillID 是技能标识。
	SkillID string `json:"skill_id"`
	// Missing 表示技能已在会话中激活但当前注册表不可见。
	Missing bool `json:"missing,omitempty"`
	// Descriptor 是技能描述信息（可选）。
	Descriptor *SkillDescriptor `json:"descriptor,omitempty"`
}

// AvailableSkillState 描述可见技能状态。
type AvailableSkillState struct {
	// Descriptor 是技能描述信息。
	Descriptor SkillDescriptor `json:"descriptor"`
	// Active 表示该技能在当前会话是否激活。
	Active bool `json:"active"`
}

// RuntimePort 定义网关访问运行时编排的下游端口契约。
type RuntimePort interface {
	// Run 启动一次运行编排。
	Run(ctx context.Context, input RunInput) error
	// Compact 对指定会话触发一次手动压缩。
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	// ExecuteSystemTool 执行一次系统工具调用。
	ExecuteSystemTool(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error)
	// ActivateSessionSkill 在指定会话中激活一个技能。
	ActivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error
	// DeactivateSessionSkill 在指定会话中停用一个技能。
	DeactivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error
	// ListSessionSkills 查询指定会话的激活技能列表。
	ListSessionSkills(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error)
	// ListAvailableSkills 查询当前可用技能列表。
	ListAvailableSkills(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error)
	// ResolvePermission 向运行时提交一次权限审批决策。
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
	// CancelRun 按 run_id 精确取消运行态任务。
	CancelRun(ctx context.Context, input CancelInput) (bool, error)
	// Events 返回统一运行事件流。
	Events() <-chan RuntimeEvent
	// ListSessions 返回会话摘要列表。
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	// LoadSession 加载指定会话详情。
	LoadSession(ctx context.Context, input LoadSessionInput) (Session, error)
}

// Gateway 定义网关主契约。
type Gateway interface {
	// Serve 启动网关服务并绑定运行端口。
	Serve(ctx context.Context, runtimePort RuntimePort) error
	// Close 优雅关闭网关服务。
	Close(ctx context.Context) error
}

// TransportAdapter defines the shared lifecycle contract for gateway transports.
type TransportAdapter interface {
	// ListenAddress returns the listening address for this transport.
	ListenAddress() string
	// Serve starts the transport and binds it to the runtime port.
	Serve(ctx context.Context, runtimePort RuntimePort) error
	// Close gracefully shuts down the transport.
	Close(ctx context.Context) error
}
