//go:build ignore
// +build ignore

package tools

import "context"

// ToolSpec 是暴露给模型的工具描述。
type ToolSpec struct {
	// Name 是工具名。
	Name string
}

// SpecListInput 是工具列表查询输入。
type SpecListInput struct {
	// SessionID 是会话标识。
	SessionID string
}

// ToolCallInput 是一次工具调用输入。
type ToolCallInput struct {
	// ID 是工具调用标识。
	ID string
	// Name 是工具名。
	Name string
	// Arguments 是原始参数。
	Arguments []byte
}

// ToolResult 是工具执行结果。
type ToolResult struct {
	// ToolCallID 是对应调用标识。
	ToolCallID string
	// Content 是工具输出。
	Content string
	// IsError 表示是否错误结果。
	IsError bool
}

// Manager 定义 runtime 侧工具边界。
type Manager interface {
	// ListAvailableSpecs 返回当前上下文可见工具列表。
	// 输入语义：input 提供会话上下文。
	// 并发约束：应支持并发读取。
	// 生命周期：每轮调用 provider 前调用。
	// 错误语义：返回注册表读取失败或权限过滤失败。
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]ToolSpec, error)
	// Execute 执行一次工具调用。
	// 输入语义：input 为模型发起的工具调用。
	// 并发约束：执行链路线程安全，同名工具可并发。
	// 生命周期：每个工具调用返回一次结果。
	// 错误语义：系统错误通过 error 返回，业务失败通过 ToolResult.IsError 表达。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
}

// SubAgentOrchestrator [PROPOSED] 定义子任务隔离执行契约。
type SubAgentOrchestrator interface {
	// Spawn 创建子任务。
	// 输入语义：task 是任务文本，scope 是隔离范围。
	// 并发约束：支持并发创建多个子任务。
	// 生命周期：返回的 agentID 用于后续等待或取消。
	// 错误语义：返回调度失败或参数非法。
	Spawn(ctx context.Context, task string, scope string) (agentID string, err error)
}
