//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

package security

import "context"

// Decision 表示权限判定结果。
type Decision string

const (
	// DecisionAllow 表示允许继续执行。
	DecisionAllow Decision = "allow"
	// DecisionDeny 表示拒绝执行。
	DecisionDeny Decision = "deny"
	// DecisionAsk 表示需要显式审批。
	DecisionAsk Decision = "ask"
)

// ActionType 表示统一动作类型。
type ActionType string

const (
	// ActionTypeBash 表示命令执行类动作。
	ActionTypeBash ActionType = "bash"
	// ActionTypeRead 表示读取类动作。
	ActionTypeRead ActionType = "read"
	// ActionTypeWrite 表示写入类动作。
	ActionTypeWrite ActionType = "write"
	// ActionTypeMCP 表示 MCP 工具调用类动作。
	ActionTypeMCP ActionType = "mcp"
)

// TargetType 表示被访问目标的类型。
type TargetType string

const (
	// TargetTypePath 表示文件路径。
	TargetTypePath TargetType = "path"
	// TargetTypeDirectory 表示目录。
	TargetTypeDirectory TargetType = "directory"
	// TargetTypeCommand 表示命令文本。
	TargetTypeCommand TargetType = "command"
	// TargetTypeURL 表示 URL。
	TargetTypeURL TargetType = "url"
	// TargetTypeMCP 表示 MCP server/tool。
	TargetTypeMCP TargetType = "mcp"
)

// ActionPayload 表示权限检查上下文。
type ActionPayload struct {
	// ToolName 是发起本次动作的工具名。
	ToolName string
	// Resource 是资源类别，例如 filesystem_read / webfetch / bash。
	Resource string
	// Operation 是操作名，例如 read / write / exec。
	Operation string
	// SessionID 是当前会话标识。
	SessionID string
	// Workdir 是当前工作目录。
	Workdir string
	// TargetType 是目标类型。
	TargetType TargetType
	// Target 是原始目标值。
	Target string
	// SandboxTargetType 是复核后的工作区目标类型。
	SandboxTargetType TargetType
	// SandboxTarget 是复核后的工作区目标。
	SandboxTarget string
}

// Action 表示统一安全输入。
type Action struct {
	// Type 是动作类型。
	Type ActionType
	// Payload 是动作上下文。
	Payload ActionPayload
}

// Rule 表示静态策略规则。
type Rule struct {
	// ID 是规则标识。
	ID string
	// Type 是动作类型。
	Type ActionType
	// Resource 是资源类别。
	Resource string
	// TargetPrefix 是命中的目标前缀。
	TargetPrefix string
	// Decision 是命中后的决策。
	Decision Decision
	// Reason 是规则说明。
	Reason string
}

// CheckResult 表示权限引擎输出。
type CheckResult struct {
	// Decision 是最终决策。
	Decision Decision
	// Action 是参与判定的动作。
	Action Action
	// Rule 是命中的规则。
	Rule *Rule
	// Reason 是最终说明。
	Reason string
}

// PermissionRememberScope 表示 session 级记忆范围。
type PermissionRememberScope string

const (
	// PermissionRememberNone 表示不记忆。
	PermissionRememberNone PermissionRememberScope = "none"
	// PermissionRememberOnce 表示仅一次。
	PermissionRememberOnce PermissionRememberScope = "once"
	// PermissionRememberAlways 表示当前 session 内持续放行。
	PermissionRememberAlways PermissionRememberScope = "always_session"
	// PermissionRememberReject 表示当前 session 内持续拒绝。
	PermissionRememberReject PermissionRememberScope = "reject_session"
)

// PermissionResolution 表示上游回写的审批决议。
type PermissionResolution struct {
	// RequestID 是审批请求标识。
	RequestID string
	// Allowed 表示是否放行。
	Allowed bool
	// Reason 是审批理由。
	Reason string
	// Scope 是 session 级记忆范围。
	Scope PermissionRememberScope
}

// PermissionEventType 表示审批事件类型。
type PermissionEventType string

const (
	// PermissionEventRequest 表示审批请求事件。
	PermissionEventRequest PermissionEventType = "PermissionRequest"
	// PermissionEventResolved 表示审批完成事件。
	PermissionEventResolved PermissionEventType = "PermissionResolved"
)

// PermissionRequest 表示审批请求事件负载。
type PermissionRequest struct {
	// RequestID 是审批请求标识。
	RequestID string
	// SessionID 是会话标识。
	SessionID string
	// Action 是触发审批的动作。
	Action Action
	// Decision 是当前引擎给出的决策。
	Decision Decision
	// RuleID 是命中规则标识。
	RuleID string
	// Reason 是审批原因。
	Reason string
}

// PermissionResolved 表示审批完成事件负载。
type PermissionResolved struct {
	// RequestID 是审批请求标识。
	RequestID string
	// SessionID 是会话标识。
	SessionID string
	// Action 是触发审批的动作。
	Action Action
	// Allowed 表示是否允许。
	Allowed bool
	// Scope 是记忆范围。
	Scope PermissionRememberScope
	// Reason 是结果说明。
	Reason string
}

// PolicyMatcher 定义策略命中层。
type PolicyMatcher interface {
	// Match 根据 action 命中静态策略并返回判定结果。
	Match(ctx context.Context, action Action) (CheckResult, error)
}

// PermissionMemoryStore 定义会话级记忆存储抽象。
type PermissionMemoryStore interface {
	// Remember 写入 session 级审批记忆。
	Remember(sessionID string, action Action, scope PermissionRememberScope, allowed bool) error
	// Resolve 返回 session 级命中结果。
	Resolve(sessionID string, action Action) (allowed bool, scope PermissionRememberScope, ok bool)
	// Clear 清理指定 session 下的所有记忆。
	Clear(sessionID string) error
}

// PermissionEventPublisher 定义审批事件透传层。
type PermissionEventPublisher interface {
	// PublishPermissionRequest 发布审批请求事件。
	PublishPermissionRequest(ctx context.Context, event PermissionRequest) error
	// PublishPermissionResolved 发布审批完成事件。
	PublishPermissionResolved(ctx context.Context, event PermissionResolved) error
}

// ToolSecurityGateway 定义 Tools 层统一安全入口。
type ToolSecurityGateway interface {
	// Evaluate 执行动作判定，综合策略、记忆与工作区复核输出结果。
	Evaluate(ctx context.Context, action Action) (CheckResult, error)
	// Remember 接收上游审批决议并写入 session 记忆。
	Remember(ctx context.Context, sessionID string, action Action, resolution PermissionResolution) error
}
