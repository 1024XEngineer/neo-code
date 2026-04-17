package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
)

// SpecListInput carries future session and agent context for tool filtering.
type SpecListInput struct {
	SessionID string
	Agent     string
	Query     string
}

// Manager is the runtime-facing tool execution and schema exposure boundary.
type Manager interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error)
	MicroCompactPolicy(name string) MicroCompactPolicy
	MicroCompactSummarizer(name string) ContentSummarizer
	// Execute 必须支持并发调用；runtime 可能在同一轮中并行调度多个工具调用。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	RememberSessionDecision(sessionID string, action security.Action, scope SessionPermissionScope) error
}

// Executor is the concrete tool execution layer under the manager.
type Executor interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error)
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	Supports(name string) bool
}

type microCompactPolicyExecutor interface {
	MicroCompactPolicy(name string) MicroCompactPolicy
}

type microCompactSummarizerExecutor interface {
	MicroCompactSummarizer(name string) ContentSummarizer
}

// WorkspaceSandbox enforces workspace-oriented constraints before execution.
type WorkspaceSandbox interface {
	Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error)
}

// NoopWorkspaceSandbox keeps the explicit sandbox stage in the execution chain
// without changing current behavior.
type NoopWorkspaceSandbox struct{}

// Check implements WorkspaceSandbox.
func (NoopWorkspaceSandbox) Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error) {
	return nil, ctx.Err()
}

var (
	// ErrPermissionDenied 标记工具请求被权限系统拒绝。
	ErrPermissionDenied = errors.New("tools: permission denied")
	// ErrPermissionApprovalRequired 标记工具请求需要用户审批。
	ErrPermissionApprovalRequired = errors.New("tools: permission approval required")
	// ErrCapabilityDenied 标记拒绝由 capability token 触发。
	ErrCapabilityDenied = errors.New("tools: capability denied")
)

// PermissionDecisionError reports a non-allow permission decision.
type PermissionDecisionError struct {
	decision security.Decision
	toolName string
	action   security.Action
	reason   string
	ruleID   string
	scope    SessionPermissionScope
}

// Error returns a stable error message for the blocked tool call.
func (e *PermissionDecisionError) Error() string {
	if e == nil {
		return ""
	}

	reason := strings.TrimSpace(e.reason)
	switch e.decision {
	case security.DecisionAsk:
		if reason == "" {
			reason = "permission approval required"
		}
	default:
		if reason == "" {
			reason = "permission denied"
		}
	}
	return "tools: " + reason
}

// Unwrap 返回可用于 errors.Is 判定的哨兵错误集合。
func (e *PermissionDecisionError) Unwrap() []error {
	if e == nil {
		return nil
	}
	switch e.decision {
	case security.DecisionAsk:
		return []error{ErrPermissionApprovalRequired}
	default:
		if strings.EqualFold(strings.TrimSpace(e.ruleID), security.CapabilityRuleID) {
			return []error{ErrPermissionDenied, ErrCapabilityDenied}
		}
		return []error{ErrPermissionDenied}
	}
}

// Decision returns the blocking engine decision.
func (e *PermissionDecisionError) Decision() string {
	if e == nil {
		return ""
	}
	return string(e.decision)
}

// ToolName returns the tool that was blocked.
func (e *PermissionDecisionError) ToolName() string {
	if e == nil {
		return ""
	}
	return e.toolName
}

// Action 返回触发权限决策时的结构化动作上下文。
func (e *PermissionDecisionError) Action() security.Action {
	if e == nil {
		return security.Action{}
	}
	return e.action
}

// Reason 返回权限网关给出的拒绝或审批原因。
func (e *PermissionDecisionError) Reason() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.reason)
}

// RuleID 返回命中规则的标识，未命中时为空字符串。
func (e *PermissionDecisionError) RuleID() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.ruleID)
}

// RememberScope 返回触发该权限结果时命中的会话记忆范围。
func (e *PermissionDecisionError) RememberScope() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(string(e.scope))
}

// DefaultManager routes tool calls through the permission engine, workspace
// sandbox, and executor.
type DefaultManager struct {
	executor         Executor
	engine           security.PermissionEngine
	sandbox          WorkspaceSandbox
	sessionDecisions *sessionPermissionMemory
	capabilityMu     sync.RWMutex
	capabilitySigner *security.CapabilitySigner
}

// NewManager creates a manager that wraps an executor with security checks.
func NewManager(executor Executor, engine security.PermissionEngine, sandbox WorkspaceSandbox) (*DefaultManager, error) {
	if executor == nil {
		return nil, errors.New("tools: executor is nil")
	}
	if engine == nil {
		return nil, errors.New("tools: permission engine is nil")
	}
	if sandbox == nil {
		sandbox = NoopWorkspaceSandbox{}
	}
	capabilitySigner, err := security.NewEphemeralCapabilitySigner()
	if err != nil {
		return nil, err
	}

	return &DefaultManager{
		executor:         executor,
		engine:           engine,
		sandbox:          sandbox,
		sessionDecisions: newSessionPermissionMemory(),
		capabilitySigner: capabilitySigner,
	}, nil
}

// SetCapabilitySigner 设置用于 capability token 验签的签名器。
func (m *DefaultManager) SetCapabilitySigner(signer *security.CapabilitySigner) error {
	if m == nil {
		return errors.New("tools: manager is nil")
	}
	if signer == nil {
		return errors.New("tools: capability signer is nil")
	}
	m.capabilityMu.Lock()
	defer m.capabilityMu.Unlock()
	m.capabilitySigner = signer
	return nil
}

// CapabilitySigner 返回当前 manager 使用的 capability 签名器。
func (m *DefaultManager) CapabilitySigner() *security.CapabilitySigner {
	if m == nil {
		return nil
	}
	m.capabilityMu.RLock()
	defer m.capabilityMu.RUnlock()
	return m.capabilitySigner
}

// capabilitySignerSnapshot 返回当前 capability signer 的并发安全快照。
func (m *DefaultManager) capabilitySignerSnapshot() *security.CapabilitySigner {
	if m == nil {
		return nil
	}
	m.capabilityMu.RLock()
	defer m.capabilityMu.RUnlock()
	return m.capabilitySigner
}

// ListAvailableSpecs returns the currently visible tool specs from the executor.
func (m *DefaultManager) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if m == nil || m.executor == nil {
		return nil, errors.New("tools: manager executor is nil")
	}
	return m.executor.ListAvailableSpecs(ctx, input)
}

// MicroCompactPolicy 返回工具的 micro compact 策略；无法判断时按默认可压缩处理。
func (m *DefaultManager) MicroCompactPolicy(name string) MicroCompactPolicy {
	if m == nil || m.executor == nil {
		return MicroCompactPolicyCompact
	}
	if source, ok := m.executor.(microCompactPolicyExecutor); ok {
		return source.MicroCompactPolicy(name)
	}
	return MicroCompactPolicyCompact
}

// MicroCompactSummarizer 返回工具的内容摘要器；未注册时返回 nil。
func (m *DefaultManager) MicroCompactSummarizer(name string) ContentSummarizer {
	if m == nil || m.executor == nil {
		return nil
	}
	if source, ok := m.executor.(microCompactSummarizerExecutor); ok {
		return source.MicroCompactSummarizer(name)
	}
	return nil
}

// Execute runs the tool if the permission engine allows it and the sandbox
// check passes.
func (m *DefaultManager) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	if m == nil || m.executor == nil {
		return ToolResult{}, errors.New("tools: manager executor is nil")
	}

	if !m.executor.Supports(input.Name) {
		return m.executor.Execute(ctx, input)
	}

	action, err := buildPermissionAction(input)
	if err != nil {
		result := NewErrorResult(input.Name, "invalid permission action", err.Error(), nil)
		result.ToolCallID = input.ID
		return result, err
	}
	if err := m.verifyCapabilityToken(action); err != nil {
		decision := capabilityDenyDecision(action, err.Error())
		m.auditCapabilityDecision(action, string(decision.Decision), decision.Reason)
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}

	decision, err := m.engine.Check(ctx, action)
	if err != nil {
		result := NewErrorResult(input.Name, "permission evaluation failed", err.Error(), nil)
		result.ToolCallID = input.ID
		return result, err
	}
	// deny 规则始终优先，避免 session 记忆覆盖硬性安全策略。
	if decision.Decision == security.DecisionDeny {
		if security.IsCapabilityDeniedResult(decision) {
			m.auditCapabilityDecision(action, string(decision.Decision), decision.Reason)
		}
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}
	// session 记忆仅用于自动处理 ask，不提升原本已 allow 的策略结果。
	if decision.Decision == security.DecisionAsk && m.sessionDecisions != nil {
		if rememberedDecision, rememberedScope, ok := m.sessionDecisions.resolve(input.SessionID, action); ok {
			decision = security.CheckResult{
				Decision: rememberedDecision,
				Action:   action,
				Reason:   sessionDecisionReason(rememberedScope),
			}
			if rememberedScope != "" {
				decision.Rule = &security.Rule{
					ID:       "session-memory:" + string(rememberedScope),
					Decision: rememberedDecision,
					Reason:   decision.Reason,
				}
			}
		}
	}
	if decision.Decision != security.DecisionAllow {
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}

	plan, err := m.sandbox.Check(ctx, action)
	if err != nil {
		result := NewErrorResult(input.Name, "workspace sandbox rejected action", err.Error(), actionMetadata(action))
		result.ToolCallID = input.ID
		return result, err
	}
	m.auditCapabilityDecision(action, string(security.DecisionAllow), "")

	if plan != nil {
		input.WorkspacePlan = plan
	}

	return m.executor.Execute(ctx, input)
}

// verifyCapabilityToken 校验 capability token 的签名、绑定关系与时效性。
func (m *DefaultManager) verifyCapabilityToken(action security.Action) error {
	token := action.Payload.CapabilityToken
	if token == nil {
		return nil
	}
	signer := m.capabilitySignerSnapshot()
	if signer == nil {
		return errors.New("capability signer is unavailable")
	}
	if err := signer.Verify(*token); err != nil {
		return fmt.Errorf("invalid capability token signature: %w", err)
	}

	normalized := token.Normalize()
	taskID := strings.TrimSpace(action.Payload.TaskID)
	if taskID == "" {
		return errors.New("capability token requires non-empty action task_id")
	}
	if normalized.TaskID != taskID {
		return errors.New("capability token task_id does not match action")
	}
	agentID := strings.TrimSpace(action.Payload.AgentID)
	if agentID == "" {
		return errors.New("capability token requires non-empty action agent_id")
	}
	if normalized.AgentID != agentID {
		return errors.New("capability token agent_id does not match action")
	}
	if err := normalized.ValidateAt(time.Now().UTC()); err != nil {
		return fmt.Errorf("invalid capability token: %w", err)
	}
	return nil
}

// capabilityDenyDecision 构造 capability 拒绝时统一的权限结果结构。
func capabilityDenyDecision(action security.Action, reason string) security.CheckResult {
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = "capability token denied"
	}
	return security.CheckResult{
		Decision: security.DecisionDeny,
		Action:   action,
		Rule: &security.Rule{
			ID:       security.CapabilityRuleID,
			Type:     action.Type,
			Resource: action.Payload.Resource,
			Decision: security.DecisionDeny,
			Reason:   trimmedReason,
		},
		Reason: trimmedReason,
	}
}

// auditCapabilityDecision 记录 capability 的 allow/deny 决策日志，便于追踪任务权限收敛。
func (m *DefaultManager) auditCapabilityDecision(action security.Action, decision string, reason string) {
	if action.Payload.CapabilityToken == nil {
		return
	}
	taskID := strings.TrimSpace(action.Payload.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(action.Payload.CapabilityToken.TaskID)
	}
	agentID := strings.TrimSpace(action.Payload.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(action.Payload.CapabilityToken.AgentID)
	}
	log.Printf(
		"tools capability audit: decision=%s task_id=%s agent_id=%s tool=%s reason=%s",
		strings.TrimSpace(decision),
		taskID,
		agentID,
		strings.TrimSpace(action.Payload.ToolName),
		strings.TrimSpace(reason),
	)
}

// RememberSessionDecision 记录会话内权限记忆，用于后续同类 action 快速决策。
func (m *DefaultManager) RememberSessionDecision(sessionID string, action security.Action, scope SessionPermissionScope) error {
	if m == nil {
		return errors.New("tools: manager is nil")
	}
	if m.sessionDecisions == nil {
		m.sessionDecisions = newSessionPermissionMemory()
	}
	return m.sessionDecisions.remember(sessionID, action, scope)
}

func blockedToolResult(input ToolCallInput, decision security.CheckResult) ToolResult {
	reason := "permission denied"
	if decision.Decision == security.DecisionAsk {
		reason = "permission approval required"
	}
	if strings.TrimSpace(decision.Reason) != "" {
		reason = strings.TrimSpace(decision.Reason)
	}

	result := NewErrorResult(input.Name, reason, permissionDetails(decision), permissionMetadata(decision))
	result.ToolCallID = input.ID
	return result
}

func permissionErrorFromDecision(decision security.CheckResult) error {
	ruleID := ""
	if decision.Rule != nil {
		ruleID = decision.Rule.ID
	}
	return &PermissionDecisionError{
		decision: decision.Decision,
		toolName: decision.Action.Payload.ToolName,
		action:   decision.Action,
		reason:   decision.Reason,
		ruleID:   ruleID,
		scope:    extractRememberScope(decision),
	}
}

// extractRememberScope 从决策规则中提取会话记忆范围。
func extractRememberScope(decision security.CheckResult) SessionPermissionScope {
	if decision.Rule == nil {
		return ""
	}
	ruleID := strings.TrimSpace(decision.Rule.ID)
	switch ruleID {
	case "session-memory:" + string(SessionPermissionScopeOnce):
		return SessionPermissionScopeOnce
	case "session-memory:" + string(SessionPermissionScopeAlways):
		return SessionPermissionScopeAlways
	case "session-memory:" + string(SessionPermissionScopeReject):
		return SessionPermissionScopeReject
	default:
		return ""
	}
}

// sessionDecisionReason 生成会话记忆命中的统一原因文本。
func sessionDecisionReason(scope SessionPermissionScope) string {
	switch scope {
	case SessionPermissionScopeOnce:
		return "session permission remembered: once"
	case SessionPermissionScopeAlways:
		return "session permission remembered: always(session)"
	case SessionPermissionScopeReject:
		return "session permission remembered: reject"
	default:
		return "session permission remembered"
	}
}

func permissionMetadata(decision security.CheckResult) map[string]any {
	metadata := actionMetadata(decision.Action)
	metadata["permission_decision"] = string(decision.Decision)
	if decision.Rule != nil && strings.TrimSpace(decision.Rule.ID) != "" {
		metadata["permission_rule_id"] = decision.Rule.ID
	}
	return metadata
}

func actionMetadata(action security.Action) map[string]any {
	metadata := map[string]any{
		"permission_action_type": string(action.Type),
		"permission_resource":    action.Payload.Resource,
		"permission_operation":   action.Payload.Operation,
	}
	if action.Payload.TargetType != "" {
		metadata["permission_target_type"] = string(action.Payload.TargetType)
	}
	if action.Payload.Target != "" {
		metadata["permission_target"] = action.Payload.Target
	}
	if action.Payload.SandboxTargetType != "" {
		metadata["permission_sandbox_target_type"] = string(action.Payload.SandboxTargetType)
	}
	if action.Payload.SandboxTarget != "" {
		metadata["permission_sandbox_target"] = action.Payload.SandboxTarget
	}
	if strings.TrimSpace(action.Payload.TaskID) != "" {
		metadata["permission_task_id"] = strings.TrimSpace(action.Payload.TaskID)
	}
	if strings.TrimSpace(action.Payload.AgentID) != "" {
		metadata["permission_agent_id"] = strings.TrimSpace(action.Payload.AgentID)
	}
	return metadata
}

func permissionDetails(decision security.CheckResult) string {
	parts := make([]string, 0, 5)
	parts = append(parts, "type: "+string(decision.Action.Type))
	if strings.TrimSpace(decision.Action.Payload.Resource) != "" {
		parts = append(parts, "resource: "+decision.Action.Payload.Resource)
	}
	if strings.TrimSpace(decision.Action.Payload.Operation) != "" {
		parts = append(parts, "operation: "+decision.Action.Payload.Operation)
	}
	if decision.Action.Payload.TargetType != "" && strings.TrimSpace(decision.Action.Payload.Target) != "" {
		parts = append(parts, fmt.Sprintf("%s: %s", decision.Action.Payload.TargetType, decision.Action.Payload.Target))
	}
	if strings.TrimSpace(decision.Reason) != "" {
		parts = append(parts, "policy: "+strings.TrimSpace(decision.Reason))
	}
	return strings.Join(parts, "\n")
}
