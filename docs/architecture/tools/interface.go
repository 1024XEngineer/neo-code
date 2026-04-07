//go:build ignore
// +build ignore

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
type ChunkEmitter func(chunk []byte)

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

// Manager 定义 runtime 侧工具边界。
type Manager interface {
	// ListAvailableSpecs 返回当前上下文可见工具列表。
	// 职责：向模型暴露可调用工具能力。
	// 输入语义：input 提供会话与代理上下文。
	// 并发约束：应支持并发读取且保证返回结果一致语义。
	// 生命周期：每轮 provider 调用前调用。
	// 错误语义：返回注册表读取失败、权限过滤失败或策略计算失败。
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]ToolSpec, error)
	// Execute 执行一次工具调用。
	// 职责：执行权限校验、沙箱校验并调用具体工具。
	// 输入语义：input 为模型发起的工具调用，包含执行上下文与约束计划。
	// 并发约束：执行链路线程安全，同名工具可并发，不同会话互不影响。
	// 生命周期：每个工具调用返回一次结果或错误。
	// 错误语义：系统错误通过 error 返回，业务失败通过 ToolResult.IsError 表达。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
}

// SubAgentOrchestrator 定义子任务隔离执行扩展契约。
type SubAgentOrchestrator interface {
	// Spawn 创建子任务。
	// 职责：创建隔离子代理执行任务。
	// 输入语义：task 为任务文本，scope 为隔离范围标识。
	// 并发约束：支持并发创建多个子任务。
	// 生命周期：返回的 agentID 用于后续等待或取消。
	// 错误语义：返回调度失败、参数非法或资源不足错误。
	Spawn(ctx context.Context, task string, scope string) (agentID string, err error)
	// Wait 等待子任务结束。
	// 职责：阻塞直到子任务完成或超时。
	// 输入语义：agentID 为子任务标识。
	// 并发约束：支持并发等待不同子任务。
	// 生命周期：通常在主循环需要结果时调用。
	// 错误语义：返回超时、取消、任务不存在或子任务失败错误。
	Wait(ctx context.Context, agentID string) (result ToolResult, err error)
	// Cancel 取消子任务。
	// 职责：中止指定子任务执行。
	// 输入语义：agentID 为子任务标识。
	// 并发约束：应幂等且线程安全。
	// 生命周期：用户取消或门禁触发时调用。
	// 错误语义：返回取消失败或任务不存在错误。
	Cancel(ctx context.Context, agentID string) error
}
