//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

package tools

import "context"

// ToolSpec 是暴露给模型的工具描述。
type ToolSpec struct {
	// Name 是工具名。
	Name string
	// Description 是工具说明。
	Description string
	// Schema 是工具参数 schema。
	Schema map[string]any
}

// SpecListInput 是工具列表查询输入。
type SpecListInput struct {
	// SessionID 是会话标识，用于按会话过滤工具能力。
	SessionID string
	// Agent 是调用方代理标识，用于策略判断与可观测打点。
	Agent string
}

// WorkspaceExecutionPlan 是工作区约束计划。
type WorkspaceExecutionPlan struct {
	// AllowedRoots 是允许访问的目录根集合。
	AllowedRoots []string
	// RequiresRewrite 表示是否需要重写目标路径。
	RequiresRewrite bool
}

// ChunkEmitter 是工具流式输出回调。
// 并发语义：
// - 单次 Execute 内按顺序调用；
// - 回调返回非 nil error 时，工具应停止继续发送分片并尽快中止执行。
// 内存语义：
// - 回调返回后不得继续持有传入的 chunk 引用。
type ChunkEmitter func(chunk []byte) error

// ToolCallInput 是一次工具调用输入。
type ToolCallInput struct {
	// ID 是工具调用标识。
	ID string
	// Name 是工具名。
	Name string
	// Arguments 是原始参数 JSON。
	Arguments []byte
	// SessionID 是所属会话标识。
	SessionID string
	// Workdir 是本次执行工作目录。
	Workdir string
	// WorkspacePlan 是沙箱层生成的工作区执行计划。
	WorkspacePlan *WorkspaceExecutionPlan
	// EmitChunk 是工具分片输出回调。
	EmitChunk ChunkEmitter
}

// ToolResult 是工具执行结果。
type ToolResult struct {
	// ToolCallID 是对应调用标识。
	ToolCallID string
	// Name 是工具名。
	Name string
	// Content 是工具输出。
	Content string
	// IsError 表示是否为业务错误结果。
	IsError bool
	// Metadata 是扩展元数据。
	Metadata map[string]any
}

// PermissionRememberScope 表示审批结果在 session 中的记忆范围。
type PermissionRememberScope string

const (
	// PermissionRememberNone 表示不记录。
	PermissionRememberNone PermissionRememberScope = "none"
	// PermissionRememberOnce 表示仅本次有效。
	PermissionRememberOnce PermissionRememberScope = "once"
	// PermissionRememberAlways 表示当前会话内持续放行。
	PermissionRememberAlways PermissionRememberScope = "always_session"
	// PermissionRememberReject 表示当前会话内持续拒绝。
	PermissionRememberReject PermissionRememberScope = "reject_session"
)

// PermissionResolution 表示一次审批决议。
type PermissionResolution struct {
	// RequestID 是审批请求标识。
	RequestID string
	// Allowed 表示是否允许继续执行。
	Allowed bool
	// Reason 是审批理由或来源说明。
	Reason string
	// Scope 表示 session 记忆范围。
	Scope PermissionRememberScope
}

// Manager 定义 runtime 侧当前工具边界。
type Manager interface {
	// ListAvailableSpecs 返回当前上下文可见工具列表。
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]ToolSpec, error)
	// Execute 执行一次工具调用。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
}

// ToolManagerV2 定义 #98 收口后的目标工具门面。
type ToolManagerV2 interface {
	// ListAvailableSpecs 返回当前上下文可见工具列表。
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]ToolSpec, error)
	// Execute 执行一次工具调用。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	// ResolvePermission 接收 UI/Runtime 回传的审批结果。
	ResolvePermission(ctx context.Context, resolution PermissionResolution) error
}

// PermissionMemoryStore 定义工具侧可替换的 session 记忆存储。
type PermissionMemoryStore interface {
	// Remember 记录一次审批记忆。
	Remember(sessionID string, actionKey string, scope PermissionRememberScope, allowed bool) error
	// Resolve 读取已命中的 session 记忆。
	Resolve(sessionID string, actionKey string) (allowed bool, scope PermissionRememberScope, ok bool)
	// Clear 清理会话下全部记忆。
	Clear(sessionID string) error
}

// MCPRegistryAdapter 定义 MCP server 生命周期与工具发现契约。
type MCPRegistryAdapter interface {
	// RegisterServer 注册一个 MCP server。
	RegisterServer(ctx context.Context, serverID string, source string, version string) error
	// UnregisterServer 注销一个 MCP server。
	UnregisterServer(ctx context.Context, serverID string) error
	// RefreshServerTools 刷新指定 server 的工具列表。
	RefreshServerTools(ctx context.Context, serverID string) error
	// ListServerTools 返回所有已注册 server 暴露的工具能力。
	ListServerTools(ctx context.Context) ([]ToolSpec, error)
}

// SubAgentOrchestrator 定义子任务隔离执行扩展契约。
type SubAgentOrchestrator interface {
	// Spawn 创建子任务。
	Spawn(ctx context.Context, task string, scope string) (agentID string, err error)
	// Wait 等待子任务结束。
	Wait(ctx context.Context, agentID string) (result ToolResult, err error)
	// Cancel 取消子任务。
	Cancel(ctx context.Context, agentID string) error
}
