package gateway

import (
	"context"
	"time"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
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

// ListSessionTodosInput 表示查询会话 Todo 快照动作输入。
type ListSessionTodosInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是目标会话标识。
	SessionID string
}

// GetRuntimeSnapshotInput 表示查询运行时统一快照动作输入。
type GetRuntimeSnapshotInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// RequestID 是客户端请求标识。
	RequestID string
	// SessionID 是目标会话标识。
	SessionID string
}

// CreateSessionInput 表示 gateway.createSession 动作的下游输入。
type CreateSessionInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是可选会话标识，留空时由 runtime 生成。
	SessionID string
}

// DeleteSessionInput 表示 gateway.deleteSession 动作的下游输入。
type DeleteSessionInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是目标会话标识。
	SessionID string
}

// RenameSessionInput 表示 gateway.renameSession 动作的下游输入。
type RenameSessionInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是目标会话标识。
	SessionID string
	// Title 是新标题。
	Title string
}

// FileEntry 表示文件树中的单个条目。
type FileEntry struct {
	// Name 是文件/目录名。
	Name string `json:"name"`
	// Path 是相对路径。
	Path string `json:"path"`
	// IsDir 表示是否为目录。
	IsDir bool `json:"is_dir"`
	// Size 是文件大小（字节）。
	Size int64 `json:"size,omitempty"`
	// ModTime 是修改时间。
	ModTime string `json:"mod_time,omitempty"`
}

// ListFilesInput 表示 gateway.listFiles 动作的下游输入。
type ListFilesInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是可选会话标识。
	SessionID string
	// Workdir 是工作目录。
	Workdir string
	// Path 是相对子路径。
	Path string
}

// ModelEntry 表示可用模型条目。
type ModelEntry struct {
	// ID 是模型标识。
	ID string `json:"id"`
	// Name 是模型展示名称。
	Name string `json:"name"`
	// Provider 是模型供应商。
	Provider string `json:"provider"`
}

// ListModelsInput 表示 gateway.listModels 动作的下游输入。
type ListModelsInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是可选会话标识。
	SessionID string
}

// SetSessionModelInput 表示 gateway.setSessionModel 动作的下游输入。
type SetSessionModelInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是目标会话标识。
	SessionID string
	// ProviderID 是可选 provider 标识，为空时沿用会话或全局当前 provider。
	ProviderID string
	// ModelID 是目标模型标识。
	ModelID string
}

// GetSessionModelInput 表示 gateway.getSessionModel 动作的下游输入。
type GetSessionModelInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// SessionID 是目标会话标识。
	SessionID string
}

// SessionModelResult 表示 getSessionModel 返回的模型信息。
type SessionModelResult struct {
	// ProviderID 是 provider 标识。
	ProviderID string `json:"provider_id,omitempty"`
	// ModelID 是模型标识。
	ModelID string `json:"model_id"`
	// ModelName 是模型展示名称。
	ModelName string `json:"model_name,omitempty"`
	// Provider 是模型供应商。
	Provider string `json:"provider,omitempty"`
}

// ProviderOption 表示前端管理面可见的 provider 及模型候选。
type ProviderOption struct {
	// ID 是 provider 标识。
	ID string `json:"id"`
	// Name 是展示名称。
	Name string `json:"name"`
	// Driver 是 provider 驱动类型。
	Driver string `json:"driver"`
	// BaseURL 是 provider 基础地址。
	BaseURL string `json:"base_url,omitempty"`
	// APIKeyEnv 是用于读取 API Key 的环境变量名。
	APIKeyEnv string `json:"api_key_env"`
	// Source 表示 provider 来源（builtin/custom）。
	Source string `json:"source"`
	// Selected 表示当前是否为全局选中 provider。
	Selected bool `json:"selected"`
	// Models 是该 provider 当前可用模型候选。
	Models []providertypes.ModelDescriptor `json:"models,omitempty"`
}

// ListProvidersInput 表示 provider 列表查询输入。
type ListProvidersInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
}

// CreateProviderInput 表示新增自定义 provider 的输入。
type CreateProviderInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// Name 是 provider 名称。
	Name string `json:"name"`
	// Driver 是 provider 驱动。
	Driver string `json:"driver"`
	// BaseURL 是 provider 基础地址。
	BaseURL string `json:"base_url,omitempty"`
	// ChatAPIMode 是 OpenAI 兼容 provider 的 chat API 模式。
	ChatAPIMode string `json:"chat_api_mode,omitempty"`
	// ChatEndpointPath 是 OpenAI 兼容 provider 的 chat 端点。
	ChatEndpointPath string `json:"chat_endpoint_path,omitempty"`
	// APIKeyEnv 是 API Key 环境变量名。
	APIKeyEnv string `json:"api_key_env"`
	// APIKey 是待写入用户环境变量的密钥值。
	APIKey string `json:"api_key,omitempty"`
	// ModelSource 表示模型来源（discover/manual）。
	ModelSource string `json:"model_source,omitempty"`
	// DiscoveryEndpointPath 是模型发现端点。
	DiscoveryEndpointPath string `json:"discovery_endpoint_path,omitempty"`
	// Models 是手工模型列表。
	Models []providertypes.ModelDescriptor `json:"models,omitempty"`
}

// DeleteProviderInput 表示删除自定义 provider 的输入。
type DeleteProviderInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// ProviderID 是 provider 标识。
	ProviderID string `json:"provider_id"`
}

// SelectProviderModelInput 表示全局选择 provider/model 的输入。
type SelectProviderModelInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// ProviderID 是 provider 标识。
	ProviderID string `json:"provider_id"`
	// ModelID 是模型标识。
	ModelID string `json:"model_id,omitempty"`
}

// ProviderSelectionResult 表示 provider/model 选择结果。
type ProviderSelectionResult struct {
	// ProviderID 是 provider 标识。
	ProviderID string `json:"provider_id"`
	// ModelID 是模型标识。
	ModelID string `json:"model_id"`
}

// MCPServerEntry 表示前端管理面可见的 MCP server 配置。
type MCPServerEntry = config.MCPServerConfig

// ListMCPServersInput 表示 MCP server 列表查询输入。
type ListMCPServersInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
}

// UpsertMCPServerInput 表示新增或更新 MCP server 的输入。
type UpsertMCPServerInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// Server 是完整 MCP server 配置。
	Server MCPServerEntry `json:"server"`
}

// SetMCPServerEnabledInput 表示启停 MCP server 的输入。
type SetMCPServerEnabledInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// ID 是 MCP server 标识。
	ID string `json:"id"`
	// Enabled 表示是否启用。
	Enabled bool `json:"enabled"`
}

// DeleteMCPServerInput 表示删除 MCP server 的输入。
type DeleteMCPServerInput struct {
	// SubjectID 是请求方身份主体标识。
	SubjectID string
	// ID 是 MCP server 标识。
	ID string `json:"id"`
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
	// Provider 是会话当前 provider。
	Provider string `json:"provider,omitempty"`
	// Model 是会话当前模型。
	Model string `json:"model,omitempty"`
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

// TodoViewItem 描述会话 Todo 单项快照。
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

// TodoSummary 描述会话 Todo 汇总信息。
type TodoSummary struct {
	Total             int `json:"total"`
	RequiredTotal     int `json:"required_total"`
	RequiredCompleted int `json:"required_completed"`
	RequiredFailed    int `json:"required_failed"`
	RequiredOpen      int `json:"required_open"`
}

// TodoSnapshot 描述会话 Todo 快照。
type TodoSnapshot struct {
	Items   []TodoViewItem `json:"items,omitempty"`
	Summary TodoSummary    `json:"summary,omitempty"`
}

// RuntimeSnapshot 描述 runtime 对外暴露的统一运行状态快照。
type RuntimeSnapshot struct {
	RunID     string         `json:"run_id,omitempty"`
	SessionID string         `json:"session_id"`
	Phase     string         `json:"phase,omitempty"`
	TaskKind  string         `json:"task_kind,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
	Todos     TodoSnapshot   `json:"todos"`
	Facts     map[string]any `json:"facts,omitempty"`
	Decision  map[string]any `json:"decision,omitempty"`
	SubAgents map[string]any `json:"subagents,omitempty"`
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
	// ListSessionTodos 返回指定会话的 Todo 快照。
	ListSessionTodos(ctx context.Context, input ListSessionTodosInput) (TodoSnapshot, error)
	// GetRuntimeSnapshot 返回指定会话的统一运行时快照。
	GetRuntimeSnapshot(ctx context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error)
	// CreateSession 创建并返回可用会话标识。
	CreateSession(ctx context.Context, input CreateSessionInput) (string, error)
	// DeleteSession 删除/归档指定会话。
	DeleteSession(ctx context.Context, input DeleteSessionInput) (bool, error)
	// RenameSession 重命名指定会话。
	RenameSession(ctx context.Context, input RenameSessionInput) error
	// ListFiles 列出工作目录文件树。
	ListFiles(ctx context.Context, input ListFilesInput) ([]FileEntry, error)
	// ListModels 列出可用模型。
	ListModels(ctx context.Context, input ListModelsInput) ([]ModelEntry, error)
	// SetSessionModel 设置会话模型。
	SetSessionModel(ctx context.Context, input SetSessionModelInput) error
	// GetSessionModel 获取当前会话模型。
	GetSessionModel(ctx context.Context, input GetSessionModelInput) (SessionModelResult, error)
}

// ManagementRuntimePort 定义前端管理面访问配置能力的可选下游端口。
type ManagementRuntimePort interface {
	// ListProviders 列出可管理 provider。
	ListProviders(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error)
	// CreateProvider 创建自定义 provider。
	CreateProvider(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error)
	// DeleteProvider 删除自定义 provider。
	DeleteProvider(ctx context.Context, input DeleteProviderInput) error
	// SelectProviderModel 设置全局 provider/model。
	SelectProviderModel(ctx context.Context, input SelectProviderModelInput) (ProviderSelectionResult, error)
	// ListMCPServers 列出 MCP server 配置。
	ListMCPServers(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error)
	// UpsertMCPServer 新增或更新 MCP server 配置。
	UpsertMCPServer(ctx context.Context, input UpsertMCPServerInput) error
	// SetMCPServerEnabled 启停 MCP server。
	SetMCPServerEnabled(ctx context.Context, input SetMCPServerEnabledInput) error
	// DeleteMCPServer 删除 MCP server。
	DeleteMCPServer(ctx context.Context, input DeleteMCPServerInput) error
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
