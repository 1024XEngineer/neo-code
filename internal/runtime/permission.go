package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/security"
	"neo-code/internal/tools"
)

// PermissionResolutionInput 描述一次来自界面的审批决定。
type PermissionResolutionInput struct {
	RequestID string
	Decision  PermissionResolutionDecision
}

// PermissionResolutionDecision 表示用户在审批弹层中的最终选择。
type PermissionResolutionDecision = approvalflow.Decision

const (
	permissionDecisionAllow = "allow"
	permissionDecisionDeny  = "deny"

	permissionResolvedApproved = "approved"
	permissionResolvedRejected = "rejected"
	permissionResolvedDenied   = "denied"

	permissionReasonApprovedByUser = "permission approved by user"
	permissionReasonRejectedByUser = "permission rejected by user"
	permissionRejectedErrorMessage = "tools: permission rejected by user"
	permissionRejectedContentFmt   = "tool error\ntool: %s\nreason: %s"

	permissionToolCategoryFilesystemRead  = "filesystem_read"
	permissionToolCategoryFilesystemWrite = "filesystem_write"
	permissionToolCategoryMCP             = "mcp"
)

// permissionExecutionInput 汇总一次工具执行与审批协作所需的上下文。
type permissionExecutionInput struct {
	RunID       string
	SessionID   string
	Call        providertypes.ToolCall
	Workdir     string
	ToolTimeout time.Duration
}

// ResolvePermission 接收 UI 的审批结果，并提交给等待中的工具调用。
func (s *Service) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return errors.New("runtime: permission request id is empty")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	switch input.Decision {
	case approvalflow.DecisionAllowOnce, approvalflow.DecisionAllowSession, approvalflow.DecisionReject:
		return s.approvalBroker.Resolve(requestID, input.Decision)
	default:
		return fmt.Errorf("runtime: unsupported permission decision %q", input.Decision)
	}
}

// executeToolCallWithPermission 执行工具调用，并在 ask/deny 路径上统一发出权限事件。
func (s *Service) executeToolCallWithPermission(ctx context.Context, input permissionExecutionInput) (tools.ToolResult, error) {
	callInput := tools.ToolCallInput{
		ID:        input.Call.ID,
		Name:      input.Call.Name,
		Arguments: []byte(input.Call.Arguments),
		Workdir:   input.Workdir,
		SessionID: input.SessionID,
		EmitChunk: func(chunk []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return s.emit(ctx, EventToolChunk, input.RunID, input.SessionID, string(chunk))
		},
	}

	runCtx, cancel := context.WithTimeout(ctx, input.ToolTimeout)
	result, execErr := s.toolManager.Execute(runCtx, callInput)
	cancel()
	if execErr == nil {
		return result, nil
	}

	var permissionErr *tools.PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		return result, execErr
	}
	if !strings.EqualFold(permissionErr.Decision(), string(security.DecisionAsk)) {
		s.emitPermissionResolved(
			ctx,
			input,
			permissionErr,
			"",
			permissionErr.Decision(),
			permissionResolutionStatus(permissionErr.Decision()),
			permissionErr.RememberScope(),
		)
		return result, execErr
	}

	decision, requestID, err := s.awaitPermissionDecision(ctx, input, permissionErr)
	if err != nil {
		return result, err
	}

	scope, err := rememberScopeFromDecision(decision)
	if err != nil {
		return result, err
	}
	if decision == approvalflow.DecisionReject {
		if scope == tools.SessionPermissionScopeReject {
			if rememberErr := s.toolManager.RememberSessionDecision(input.SessionID, permissionErr.Action(), scope); rememberErr != nil {
				return tools.ToolResult{}, rememberErr
			}
		}
		s.emitPermissionResolved(
			ctx,
			input,
			permissionErr,
			requestID,
			permissionDecisionDeny,
			permissionResolvedRejected,
			string(scope),
		)
		return tools.ToolResult{
			ToolCallID: input.Call.ID,
			Name:       input.Call.Name,
			Content:    fmt.Sprintf(permissionRejectedContentFmt, input.Call.Name, permissionReasonRejectedByUser),
			IsError:    true,
		}, errors.New(permissionRejectedErrorMessage)
	}

	if rememberErr := s.toolManager.RememberSessionDecision(input.SessionID, permissionErr.Action(), scope); rememberErr != nil {
		return tools.ToolResult{}, rememberErr
	}
	s.emitPermissionResolved(
		ctx,
		input,
		permissionErr,
		requestID,
		permissionDecisionAllow,
		permissionResolvedApproved,
		string(scope),
	)

	retryCtx, retryCancel := context.WithTimeout(ctx, input.ToolTimeout)
	retryResult, retryErr := s.toolManager.Execute(retryCtx, callInput)
	retryCancel()
	return retryResult, retryErr
}

// awaitPermissionDecision 发出 permission_request 事件，并等待外部审批结果。
func (s *Service) awaitPermissionDecision(
	ctx context.Context,
	input permissionExecutionInput,
	permissionErr *tools.PermissionDecisionError,
) (approvalflow.Decision, string, error) {
	s.permissionAskMu.Lock()
	defer s.permissionAskMu.Unlock()

	requestID, resultCh, err := s.approvalBroker.Open()
	if err != nil {
		return "", "", err
	}
	defer s.approvalBroker.Close(requestID)

	s.emit(ctx, EventPermissionRequested, input.RunID, input.SessionID, PermissionRequestPayload{
		RequestID:    requestID,
		ToolCallID:   input.Call.ID,
		ToolName:     input.Call.Name,
		ToolCategory: permissionToolCategory(permissionErr.Action()),
		ActionType:   string(permissionErr.Action().Type),
		Operation:    permissionErr.Action().Payload.Operation,
		TargetType:   string(permissionErr.Action().Payload.TargetType),
		Target:       permissionErr.Action().Payload.Target,
		Decision:     permissionErr.Decision(),
		Reason:       permissionErr.Reason(),
		RuleID:       permissionErr.RuleID(),
	})

	select {
	case <-ctx.Done():
		return "", requestID, ctx.Err()
	case decision := <-resultCh:
		return decision, requestID, nil
	}
}

// emitPermissionResolved 将权限处理结果统一映射为 runtime 事件。
func (s *Service) emitPermissionResolved(
	ctx context.Context,
	input permissionExecutionInput,
	permissionErr *tools.PermissionDecisionError,
	requestID string,
	decision string,
	resolvedAs string,
	rememberScope string,
) {
	reason := strings.TrimSpace(permissionErr.Reason())
	if strings.TrimSpace(requestID) != "" {
		switch resolvedAs {
		case permissionResolvedApproved:
			reason = permissionReasonApprovedByUser
		case permissionResolvedRejected:
			reason = permissionReasonRejectedByUser
		}
	}
	if reason == "" {
		reason = strings.TrimSpace(permissionErr.Error())
	}

	s.emit(ctx, EventPermissionResolved, input.RunID, input.SessionID, PermissionResolvedPayload{
		RequestID:     requestID,
		ToolCallID:    input.Call.ID,
		ToolName:      input.Call.Name,
		ToolCategory:  permissionToolCategory(permissionErr.Action()),
		ActionType:    string(permissionErr.Action().Type),
		Operation:     permissionErr.Action().Payload.Operation,
		TargetType:    string(permissionErr.Action().Payload.TargetType),
		Target:        permissionErr.Action().Payload.Target,
		Decision:      decision,
		Reason:        reason,
		RuleID:        permissionErr.RuleID(),
		RememberScope: rememberScope,
		ResolvedAs:    resolvedAs,
	})
}

// rememberScopeFromDecision 将审批结果映射为工具层的会话记忆范围。
func rememberScopeFromDecision(decision PermissionResolutionDecision) (tools.SessionPermissionScope, error) {
	switch decision {
	case approvalflow.DecisionAllowOnce:
		return tools.SessionPermissionScopeOnce, nil
	case approvalflow.DecisionAllowSession:
		return tools.SessionPermissionScopeAlways, nil
	case approvalflow.DecisionReject:
		return tools.SessionPermissionScopeReject, nil
	default:
		return "", fmt.Errorf("runtime: unsupported permission decision %q", decision)
	}
}

// permissionResolutionStatus 负责将工具层决策映射为 resolved_as 字段。
func permissionResolutionStatus(decision string) string {
	if strings.EqualFold(strings.TrimSpace(decision), string(security.DecisionAsk)) {
		return permissionResolvedRejected
	}
	return permissionResolvedDenied
}

// permissionToolCategory 将安全动作收敛为用于审批展示的工具分类标签。
func permissionToolCategory(action security.Action) string {
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	switch action.Type {
	case security.ActionTypeRead:
		if strings.HasPrefix(resource, "filesystem_") {
			return permissionToolCategoryFilesystemRead
		}
	case security.ActionTypeWrite:
		if strings.HasPrefix(resource, "filesystem_") {
			return permissionToolCategoryFilesystemWrite
		}
	case security.ActionTypeMCP:
		target := strings.ToLower(strings.TrimSpace(action.Payload.Target))
		if serverIdentity := security.CanonicalMCPServerIdentity(target); serverIdentity != "" {
			return serverIdentity
		}
		return permissionToolCategoryMCP
	}
	if resource != "" {
		return resource
	}
	return strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
}
