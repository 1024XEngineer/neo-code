package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"neo-code/internal/provider"
	"neo-code/internal/security"
	"neo-code/internal/tools"
)

// PermissionResolutionInput 描述一次来自界面的权限审批决定。
type PermissionResolutionInput struct {
	RequestID string
	Decision  PermissionResolutionDecision
}

// PermissionResolutionDecision 表示用户在权限提示中的最终选择。
type PermissionResolutionDecision string

const (
	PermissionResolutionAllowOnce    PermissionResolutionDecision = "allow_once"
	PermissionResolutionAllowSession PermissionResolutionDecision = "allow_session"
	PermissionResolutionReject       PermissionResolutionDecision = "reject"
)

type permissionExecutionInput struct {
	RunID       string
	SessionID   string
	Call        provider.ToolCall
	Workdir     string
	ToolTimeout time.Duration
}

type pendingPermissionRequest struct {
	RequestID string
	RunID     string
	SessionID string
	Call      provider.ToolCall
	Action    security.Action
	ResultCh  chan PermissionResolutionDecision
	Submitted bool
}

var runtimePendingPermissions = struct {
	mu     sync.Mutex
	nextID uint64
	byRun  map[*Service]*pendingPermissionRequest
}{
	byRun: make(map[*Service]*pendingPermissionRequest),
}

// ResolvePermission 接收 UI 的审批决定，并唤醒 runtime 中等待的工具调用。
func (s *Service) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return errors.New("runtime: permission request id is empty")
	}
	decision := normalizePermissionResolutionDecision(input.Decision)
	if decision == "" {
		return fmt.Errorf("runtime: unsupported permission decision %q", input.Decision)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	runtimePendingPermissions.mu.Lock()
	pending := runtimePendingPermissions.byRun[s]
	if pending == nil || pending.RequestID != requestID {
		runtimePendingPermissions.mu.Unlock()
		return fmt.Errorf("runtime: permission request %q not found", requestID)
	}
	// Submitted 标记用于避免重复提交同一个 request 导致阻塞。
	if pending.Submitted {
		runtimePendingPermissions.mu.Unlock()
		return nil
	}
	pending.Submitted = true
	resultCh := pending.ResultCh
	runtimePendingPermissions.mu.Unlock()

	// 非阻塞提交，避免 UI 重复触发导致写满 channel 后长时间卡住。
	select {
	case resultCh <- decision:
		return nil
	default:
		return nil
	}
}

// executeToolCallWithPermission 执行工具调用并处理 ask 决策的显式审批闭环。
func (s *Service) executeToolCallWithPermission(ctx context.Context, input permissionExecutionInput) (tools.ToolResult, error) {
	callInput := tools.ToolCallInput{
		ID:        input.Call.ID,
		Name:      input.Call.Name,
		Arguments: []byte(input.Call.Arguments),
		Workdir:   input.Workdir,
		SessionID: input.SessionID,
		EmitChunk: func(chunk []byte) {
			s.emit(ctx, EventToolChunk, input.RunID, input.SessionID, string(chunk))
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
		return result, execErr
	}

	decision, scope, requestID, err := s.awaitPermissionDecision(ctx, input, permissionErr)
	if err != nil {
		return result, err
	}

	if decision == PermissionResolutionReject {
		if scope == tools.SessionPermissionScopeReject {
			if rememberErr := s.toolManager.RememberSessionDecision(input.SessionID, permissionErr.Action(), scope); rememberErr != nil {
				return tools.ToolResult{}, rememberErr
			}
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
			Decision:      "deny",
			Reason:        "permission rejected by user",
			RuleID:        permissionErr.RuleID(),
			RememberScope: string(scope),
			ResolvedAs:    "rejected",
		})
		return tools.ToolResult{
			ToolCallID: input.Call.ID,
			Name:       input.Call.Name,
			Content:    "tool error\ntool: " + input.Call.Name + "\nreason: permission rejected by user",
			IsError:    true,
		}, errors.New("tools: permission rejected by user")
	}

	if rememberErr := s.toolManager.RememberSessionDecision(input.SessionID, permissionErr.Action(), scope); rememberErr != nil {
		return tools.ToolResult{}, rememberErr
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
		Decision:      "allow",
		Reason:        "permission approved by user",
		RuleID:        permissionErr.RuleID(),
		RememberScope: string(scope),
		ResolvedAs:    "approved",
	})

	retryCtx, retryCancel := context.WithTimeout(ctx, input.ToolTimeout)
	retryResult, retryErr := s.toolManager.Execute(retryCtx, callInput)
	retryCancel()
	return retryResult, retryErr
}

// awaitPermissionDecision 发送 permission_request 事件，并等待 UI 回传审批结果。
func (s *Service) awaitPermissionDecision(
	ctx context.Context,
	input permissionExecutionInput,
	permissionErr *tools.PermissionDecisionError,
) (PermissionResolutionDecision, tools.SessionPermissionScope, string, error) {
	request := registerPendingPermission(s, input, permissionErr.Action())
	defer clearPendingPermission(s, request.RequestID)

	s.emit(ctx, EventPermissionRequest, input.RunID, input.SessionID, PermissionRequestPayload{
		RequestID:    request.RequestID,
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
		return "", "", request.RequestID, ctx.Err()
	case decision := <-request.ResultCh:
		scope, err := rememberScopeFromDecision(decision)
		if err != nil {
			return "", "", request.RequestID, err
		}
		return decision, scope, request.RequestID, nil
	}
}

// registerPendingPermission 注册待审批请求并返回 request id。
func registerPendingPermission(s *Service, input permissionExecutionInput, action security.Action) pendingPermissionRequest {
	runtimePendingPermissions.mu.Lock()
	defer runtimePendingPermissions.mu.Unlock()

	runtimePendingPermissions.nextID++
	request := pendingPermissionRequest{
		RequestID: fmt.Sprintf("perm-%d", runtimePendingPermissions.nextID),
		RunID:     input.RunID,
		SessionID: input.SessionID,
		Call:      input.Call,
		Action:    action,
		ResultCh:  make(chan PermissionResolutionDecision, 1),
	}
	runtimePendingPermissions.byRun[s] = &request
	return request
}

// clearPendingPermission 清理待审批请求，避免跨请求误用。
func clearPendingPermission(s *Service, requestID string) {
	runtimePendingPermissions.mu.Lock()
	defer runtimePendingPermissions.mu.Unlock()

	current := runtimePendingPermissions.byRun[s]
	if current != nil && current.RequestID == requestID {
		delete(runtimePendingPermissions.byRun, s)
	}
}

// normalizePermissionResolutionDecision 将审批决定归一化为受支持枚举值。
func normalizePermissionResolutionDecision(decision PermissionResolutionDecision) PermissionResolutionDecision {
	switch strings.ToLower(strings.TrimSpace(string(decision))) {
	case "allow_once", "once", "y", "yes":
		return PermissionResolutionAllowOnce
	case "allow_session", "always", "always_session", "a":
		return PermissionResolutionAllowSession
	case "reject", "deny", "n", "no", "r":
		return PermissionResolutionReject
	default:
		return ""
	}
}

// rememberScopeFromDecision 将审批决定映射到工具层权限记忆范围。
func rememberScopeFromDecision(decision PermissionResolutionDecision) (tools.SessionPermissionScope, error) {
	switch decision {
	case PermissionResolutionAllowOnce:
		return tools.SessionPermissionScopeOnce, nil
	case PermissionResolutionAllowSession:
		return tools.SessionPermissionScopeAlways, nil
	case PermissionResolutionReject:
		return tools.SessionPermissionScopeReject, nil
	default:
		return "", fmt.Errorf("runtime: unsupported permission decision %q", decision)
	}
}

// permissionToolCategory 将 action 归一化为工具类别标签，供审批展示和记忆使用。
func permissionToolCategory(action security.Action) string {
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	switch action.Type {
	case security.ActionTypeRead:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_read"
		}
	case security.ActionTypeWrite:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_write"
		}
	}
	if resource != "" {
		return resource
	}
	return strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
}
