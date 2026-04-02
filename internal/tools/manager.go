package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/security"
)

// SpecListInput carries future session and agent context for tool filtering.
type SpecListInput struct {
	SessionID string
	Agent     string
}

// Manager is the runtime-facing tool execution and schema exposure boundary.
type Manager interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]provider.ToolSpec, error)
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
}

// Executor is the concrete tool execution layer under the manager.
type Executor interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]provider.ToolSpec, error)
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	Supports(name string) bool
}

// WorkspaceSandbox enforces workspace-oriented constraints before execution.
type WorkspaceSandbox interface {
	Check(ctx context.Context, action security.Action) error
}

// NoopWorkspaceSandbox keeps the explicit sandbox stage in the execution chain
// without changing current behavior.
type NoopWorkspaceSandbox struct{}

// Check implements WorkspaceSandbox.
func (NoopWorkspaceSandbox) Check(ctx context.Context, action security.Action) error {
	return ctx.Err()
}

// PermissionDecisionError reports a non-allow permission decision.
type PermissionDecisionError struct {
	decision security.Decision
	toolName string
	action   security.Action
	reason   string
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

// DefaultManager routes tool calls through the permission engine, workspace
// sandbox, and executor.
type DefaultManager struct {
	executor Executor
	engine   security.PermissionEngine
	sandbox  WorkspaceSandbox
}

// NewManager creates a manager that wraps an executor with security checks.
func NewManager(executor Executor, engine security.PermissionEngine, sandbox WorkspaceSandbox) (*DefaultManager, error) {
	if executor == nil {
		return nil, errors.New("tools: executor is nil")
	}
	if engine == nil {
		defaultEngine, err := security.NewStaticGateway(security.DecisionAllow, nil)
		if err != nil {
			return nil, err
		}
		engine = defaultEngine
	}
	if sandbox == nil {
		sandbox = NoopWorkspaceSandbox{}
	}

	return &DefaultManager{
		executor: executor,
		engine:   engine,
		sandbox:  sandbox,
	}, nil
}

// ListAvailableSpecs returns the currently visible tool specs from the executor.
func (m *DefaultManager) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]provider.ToolSpec, error) {
	if m == nil || m.executor == nil {
		return nil, errors.New("tools: manager executor is nil")
	}
	return m.executor.ListAvailableSpecs(ctx, input)
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

	decision, err := m.engine.Check(ctx, action)
	if err != nil {
		result := NewErrorResult(input.Name, "permission evaluation failed", err.Error(), nil)
		result.ToolCallID = input.ID
		return result, err
	}
	if decision.Decision != security.DecisionAllow {
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}

	if err := m.sandbox.Check(ctx, action); err != nil {
		result := NewErrorResult(input.Name, "workspace sandbox rejected action", err.Error(), actionMetadata(action))
		result.ToolCallID = input.ID
		return result, err
	}

	return m.executor.Execute(ctx, input)
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
	return &PermissionDecisionError{
		decision: decision.Decision,
		toolName: decision.Action.Payload.ToolName,
		action:   decision.Action,
		reason:   decision.Reason,
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
