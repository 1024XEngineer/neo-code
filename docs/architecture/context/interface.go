//go:build ignore
// +build ignore

package context

import "context"

// Message 是上下文契约中的消息结构。
type Message struct {
	// Role 是消息角色。
	Role string
	// Content 是消息正文。
	Content string
}

// Metadata 是当前已落地的上下文元数据。
type Metadata struct {
	// Workdir 是工作目录。
	Workdir string
	// Shell 是 shell 类型。
	Shell string
	// Provider 是当前 provider 名称。
	Provider string
	// Model 是当前模型名称。
	Model string
}

// BuildInput 是上下文构建输入。
type BuildInput struct {
	// Messages 是历史消息快照。
	Messages []Message
	// Metadata 是当前运行元数据。
	Metadata Metadata
	// TokenBudget [PROPOSED] 是上下文预算信息。
	TokenBudget *TokenBudget
	// TaskScope [PROPOSED] 是任务隔离范围。
	TaskScope *TaskScope
}

// BuildResult 是上下文构建输出。
type BuildResult struct {
	// SystemPrompt 是最终系统提示词。
	SystemPrompt string
	// Messages 是裁剪后的消息序列。
	Messages []Message
}

// Builder 定义上下文构建接口。
type Builder interface {
	// Build 组装 provider 调用所需上下文。
	// 输入语义：input 包含消息快照与运行元数据。
	// 并发约束：实现应支持并发调用。
	// 生命周期：每轮 provider 调用前由 runtime 调用。
	// 错误语义：返回规则读取失败、渲染失败或输入非法错误。
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// TokenBudget [PROPOSED] 描述上下文预算。
type TokenBudget struct {
	// ModelContextWindow 是模型窗口上限。
	ModelContextWindow int
	// EstimatedInputTokens 是当前输入估算 token。
	EstimatedInputTokens int
}

// TaskScope [PROPOSED] 描述子任务作用域。
type TaskScope struct {
	// ScopeID 是作用域标识。
	ScopeID string
	// ParentScopeID 是父作用域标识。
	ParentScopeID string
}
