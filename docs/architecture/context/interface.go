//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

package context

import (
	"context"

	"neo-code/internal/provider"
)

// Metadata 描述上下文基础构建所需的运行时元数据。
type Metadata struct {
	// Workdir 是当前工作目录，用于生成与路径相关的上下文提示。
	Workdir string
	// Shell 是当前 shell 类型，用于约束命令生成风格。
	Shell string
	// Provider 是当前模型提供商标识。
	Provider string
	// Model 是当前模型标识。
	Model string
}

// BuildInput 是基础上下文构建输入。
type BuildInput struct {
	// Messages 是历史消息快照，语义必须与 provider.Message 保持一致。
	Messages []provider.Message
	// Metadata 是本轮构建的运行时元数据。
	Metadata Metadata
}

// AdvancedBuildInput 是高级上下文构建输入。
type AdvancedBuildInput struct {
	// Base 是基础构建输入。
	Base BuildInput
	// LoopState 是当前回合编排状态快照。
	LoopState LoopState
	// TokenBudget 是上下文预算信息。
	TokenBudget TokenBudget
	// WorkspaceMap 是显式工作区映射。
	WorkspaceMap WorkspaceMap
	// TaskScope 是子任务隔离范围。
	TaskScope TaskScope
}

// BuildResult 是上下文构建输出。
type BuildResult struct {
	// SystemPrompt 是最终系统提示词。
	SystemPrompt string
	// Messages 是裁剪与重排后的消息序列。
	Messages []provider.Message
}

// BasicBuilder 定义基础上下文构建接口。
type BasicBuilder interface {
	// Build 组装基础上下文。
	// 职责：基于消息快照与基础元数据生成 Provider 可消费输入。
	// 输入语义：input 仅包含基础字段，不承载预算与作用域扩展。
	// 并发约束：实现必须支持并发调用且不依赖共享可变状态。
	// 生命周期：每轮 Provider 请求前可调用一次或多次。
	// 错误语义：返回规则读取失败、模板渲染失败或输入非法错误。
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// AdvancedBuilder 定义高级上下文构建接口。
type AdvancedBuilder interface {
	// BuildAdvanced 组装高级上下文。
	// 职责：在基础构建之上叠加预算控制、工作区映射与作用域隔离策略。
	// 输入语义：input 包含 Base 与扩展字段，扩展字段必须整体参与策略计算。
	// 并发约束：实现必须支持并发调用且不得污染会话外部状态。
	// 生命周期：用于需要精细预算与隔离控制的编排阶段。
	// 错误语义：返回预算校验失败、规则冲突或输入非法错误。
	BuildAdvanced(ctx context.Context, input AdvancedBuildInput) (BuildResult, error)
}

// LoopState 描述当前编排状态。
type LoopState struct {
	// TurnIndex 是当前回合序号。
	TurnIndex int
	// MaxTurns 是回合上限。
	MaxTurns int
}

// TokenBudget 描述上下文预算。
type TokenBudget struct {
	// ModelContextWindow 是模型上下文窗口上限。
	ModelContextWindow int
	// EstimatedInputTokens 是当前输入估算 token。
	EstimatedInputTokens int
	// ReserveOutputTokens 是为模型输出预留的 token。
	ReserveOutputTokens int
}

// WorkspaceMap 描述工作区映射信息。
type WorkspaceMap struct {
	// ActiveWorkdir 是当前激活工作目录。
	ActiveWorkdir string
	// AllowedRoots 是允许访问的根目录列表。
	AllowedRoots []string
}

// TaskScope 描述子任务作用域。
type TaskScope struct {
	// ScopeID 是当前作用域标识。
	ScopeID string
	// ParentScopeID 是父级作用域标识。
	ParentScopeID string
}