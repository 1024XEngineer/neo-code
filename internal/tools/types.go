package tools

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
)

// Tool 定义所有内置/扩展工具的统一契约。
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	MicroCompactPolicy() MicroCompactPolicy
	Execute(ctx context.Context, call ToolCallInput) (ToolResult, error)
}

// ChunkEmitter 是工具执行过程中向上游发送流式分片的回调。
type ChunkEmitter func(chunk []byte) error

// SessionMutator 定义工具可调用的会话 Todo 读写能力。
type SessionMutator interface {
	ListTodos() []agentsession.TodoItem
	FindTodo(id string) (agentsession.TodoItem, bool)
	ReplaceTodos(items []agentsession.TodoItem) error
	AddTodo(item agentsession.TodoItem) error
	UpdateTodo(id string, patch agentsession.TodoPatch, expectedRevision int64) error
	SetTodoStatus(id string, status agentsession.TodoStatus, expectedRevision int64) error
	DeleteTodo(id string, expectedRevision int64) error
	ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error
	CompleteTodo(id string, artifacts []string, expectedRevision int64) error
	FailTodo(id string, reason string, expectedRevision int64) error
}

// ToolCallInput 承载一次工具调用所需的运行时上下文。
type ToolCallInput struct {
	ID            string
	Name          string
	Arguments     []byte
	SessionID     string
	Workdir       string
	WorkspacePlan *security.WorkspaceExecutionPlan
	// SessionMutator 仅对需要会话级写入的工具开放（例如 todo_write）。
	SessionMutator SessionMutator
	// EmitChunk 用于工具执行期间的流式输出回调。
	EmitChunk ChunkEmitter
}

// ToolResult 是工具执行完成后返回给 runtime 的统一结果结构。
type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Metadata   map[string]any
}

// ToolSpec 对齐 provider 层 tool schema 结构。
type ToolSpec = providertypes.ToolSpec
