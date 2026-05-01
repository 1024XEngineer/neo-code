package tools

import (
	"context"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/subagent"
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

// SubAgentRunInput 描述一次通过工具触发的子代理即时执行请求。
type SubAgentRunInput struct {
	RunID                 string
	SessionID             string
	CallerAgent           string
	ParentCapabilityToken *security.CapabilityToken
	Role                  subagent.Role
	TaskType              subagent.TaskType
	TaskID                string
	Goal                  string
	ExpectedOut           string
	Workdir               string
	MaxSteps              int
	Timeout               time.Duration
	AllowedTools          []string
	AllowedPaths          []string
}

// SubAgentRunResult 描述子代理执行完成后的结构化结果。
type SubAgentRunResult struct {
	Role       subagent.Role
	TaskID     string
	State      subagent.State
	StopReason subagent.StopReason
	StepCount  int
	Output     subagent.Output
	Error      string
}

// SubAgentInvoker 定义工具层触发子代理执行的最小桥接接口。
type SubAgentInvoker interface {
	Run(ctx context.Context, input SubAgentRunInput) (SubAgentRunResult, error)
}

// ToolCallInput 承载一次工具调用所需的运行时上下文。
type ToolCallInput struct {
	ID              string
	Name            string
	Arguments       []byte
	SessionID       string
	TaskID          string
	AgentID         string
	Workdir         string
	ReadOnly        bool
	CapabilityToken *security.CapabilityToken
	WorkspacePlan   *security.WorkspaceExecutionPlan
	// SessionMutator 仅对需要会话级写入的工具开放（例如 todo_write）。
	SessionMutator SessionMutator
	// SubAgentInvoker 为 spawn_subagent 等工具提供即时子代理执行入口。
	SubAgentInvoker SubAgentInvoker
	// EmitChunk 用于工具执行期间的流式输出回调。
	EmitChunk ChunkEmitter
}

// ToolResult 是工具执行完成后返回给 runtime 的统一结果结构。
type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	// ErrorClass 表示机器可读的错误分类（例如 hook_blocked/permission_denied）。
	ErrorClass string
	Metadata   map[string]any
	Facts      ToolExecutionFacts
}

// ToolExecutionFacts 描述工具执行产出的结构化运行事实，供 runtime 做写入/验证控制。
type ToolExecutionFacts struct {
	WorkspaceWrite        bool
	VerificationPerformed bool
	VerificationPassed    bool
	VerificationScope     string
}

// ToolSpec 对齐 provider 层 tool schema 结构。
type ToolSpec = providertypes.ToolSpec
