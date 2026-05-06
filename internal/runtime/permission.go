package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
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

	defaultInlineSubAgentToolTimeout = 3 * time.Minute
	maxInlineSubAgentToolTimeout     = 10 * time.Minute
	minInlineSubAgentToolTimeout     = 30 * time.Second
	defaultDiagnoseToolTimeout       = 60 * time.Second
	defaultPermissionToolTimeout     = 20 * time.Second
)

// permissionExecutionInput 汇总一次工具执行与审批协作所需的上下文。
type permissionExecutionInput struct {
	RunID       string
	SessionID   string
	TaskID      string
	AgentID     string
	Capability  *security.CapabilityToken
	State       *runState
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
		ID:              input.Call.ID,
		Name:            input.Call.Name,
		Arguments:       []byte(input.Call.Arguments),
		Workdir:         input.Workdir,
		ReadOnly:        input.State != nil && isReadOnlyPlanningStage(resolvePlanningStageForState(input.State)),
		SessionID:       input.SessionID,
		TaskID:          input.TaskID,
		AgentID:         input.AgentID,
		CapabilityToken: input.Capability,
		EmitChunk: func(chunk []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return s.emit(ctx, EventToolChunk, input.RunID, input.SessionID, string(chunk))
		},
	}
	if input.State != nil {
		callInput.SessionMutator = newRuntimeSessionMutator(ctx, s, input.State)
	}
	callInput.SubAgentInvoker = newRuntimeSubAgentInvoker(s, input.RunID, input.SessionID, input.AgentID, input.Workdir)

	effectiveTimeout := resolveToolExecutionTimeout(input.Call, input.ToolTimeout)
	runCtx, cancel := context.WithTimeout(ctx, effectiveTimeout)
	result, execErr := s.toolManager.Execute(runCtx, callInput)
	cancel()
	if execErr == nil {
		return result, nil
	}

	var permissionErr *tools.PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		return result, execErr
	}

	beforePermissionHookOutput := s.runHookPoint(ctx, input.State, runtimehooks.HookPointBeforePermissionDecision, runtimehooks.HookContext{
		RunID:     strings.TrimSpace(input.RunID),
		SessionID: strings.TrimSpace(input.SessionID),
		Metadata: map[string]any{
			"tool_call_id": strings.TrimSpace(input.Call.ID),
			"tool_name":    strings.TrimSpace(input.Call.Name),
			"decision":     strings.TrimSpace(permissionErr.Decision()),
			"reason":       strings.TrimSpace(permissionErr.Reason()),
			"rule_id":      strings.TrimSpace(permissionErr.RuleID()),
			"workdir":      strings.TrimSpace(input.Workdir),
		},
	})
	if beforePermissionHookOutput.Blocked {
		reason := findHookBlockMessage(beforePermissionHookOutput)
		blockSource := findHookBlockSource(beforePermissionHookOutput)
		blockedResult := tools.NewErrorResult(input.Call.Name, hookErrorClassBlocked, reason, map[string]any{
			"hook_id":     beforePermissionHookOutput.BlockedBy,
			"hook_source": string(blockSource),
			"point":       string(runtimehooks.HookPointBeforePermissionDecision),
		})
		blockedResult.ToolCallID = input.Call.ID
		blockedResult.ErrorClass = hookErrorClassBlocked
		_ = s.emit(ctx, EventHookBlocked, strings.TrimSpace(input.RunID), strings.TrimSpace(input.SessionID), HookBlockedPayload{
			HookID:     strings.TrimSpace(beforePermissionHookOutput.BlockedBy),
			Source:     string(blockSource),
			Point:      string(runtimehooks.HookPointBeforePermissionDecision),
			ToolCallID: strings.TrimSpace(input.Call.ID),
			ToolName:   strings.TrimSpace(input.Call.Name),
			Reason:     reason,
			Enforced:   true,
		})
		return blockedResult, errors.New(reason)
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

	// 审批等待属于用户交互阶段，不应受工具执行超时约束；
	// 否则用户未及时响应会被误判为工具失败并进入调度重试/失败链路。
	var decision approvalflow.Decision
	var requestID string
	if err := s.enterTemporaryRunState(ctx, input.State, controlplane.RunStateWaitingPermission); err != nil {
		return result, err
	}
	defer func() {
		_ = s.leaveTemporaryRunState(ctx, input.State, controlplane.RunStateWaitingPermission)
	}()
	resolvedDecision, resolvedRequestID, waitErr := s.awaitPermissionDecision(ctx, input, permissionErr)
	if waitErr != nil {
		return result, waitErr
	}
	decision = resolvedDecision
	requestID = resolvedRequestID

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

	retryCtx, retryCancel := context.WithTimeout(ctx, effectiveTimeout)
	retryResult, retryErr := s.toolManager.Execute(retryCtx, callInput)
	retryCancel()
	return retryResult, retryErr
}

// resolveToolExecutionTimeout 为特定工具覆写默认超时策略，避免长耗时链路被统一短超时误杀。
func resolveToolExecutionTimeout(call providertypes.ToolCall, fallback time.Duration) time.Duration {
	base := fallback
	if base <= 0 {
		base = defaultPermissionToolTimeout
	}
	name := strings.TrimSpace(call.Name)
	if strings.EqualFold(name, tools.ToolNameDiagnose) {
		if base < defaultDiagnoseToolTimeout {
			return defaultDiagnoseToolTimeout
		}
		return base
	}
	if !strings.EqualFold(name, tools.ToolNameSpawnSubAgent) {
		return base
	}

	_, requested := parseSpawnSubAgentRuntimeOptions(call.Arguments)
	if requested <= 0 {
		if base > defaultInlineSubAgentToolTimeout {
			return base
		}
		return defaultInlineSubAgentToolTimeout
	}
	requested = clampDuration(requested, minInlineSubAgentToolTimeout, maxInlineSubAgentToolTimeout)
	if requested > base {
		return requested
	}
	return base
}

// parseSpawnSubAgentRuntimeOptions 提取 spawn_subagent 的运行模式与 timeout_sec 参数。
func parseSpawnSubAgentRuntimeOptions(raw string) (string, time.Duration) {
	if strings.TrimSpace(raw) == "" {
		return "", 0
	}
	var payload struct {
		Mode       string `json:"mode"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", 0
	}
	return strings.TrimSpace(payload.Mode), time.Duration(payload.TimeoutSec) * time.Second
}

// clampDuration 把持续时间限制在 [min,max] 区间，避免极值配置影响运行稳定性。
func clampDuration(value time.Duration, min time.Duration, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// awaitPermissionDecision 发出 permission_request 事件，并等待外部审批结果。
func (s *Service) awaitPermissionDecision(
	ctx context.Context,
	input permissionExecutionInput,
	permissionErr *tools.PermissionDecisionError,
) (approvalflow.Decision, string, error) {
	askMu, releaseAskLock := s.acquirePermissionAskLock(input.RunID, input.SessionID)
	defer releaseAskLock()
	askMu.Lock()
	defer askMu.Unlock()

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

// acquirePermissionAskLock 按运行维度获取审批串行锁，避免跨运行的审批互相阻塞。
func (s *Service) acquirePermissionAskLock(runID string, sessionID string) (*sync.Mutex, func()) {
	lockKey := permissionAskLockKey(runID, sessionID)

	s.permissionAskMapMu.Lock()
	if s.permissionAskLocks == nil {
		s.permissionAskLocks = make(map[string]*permissionAskLockEntry)
	}

	entry, ok := s.permissionAskLocks[lockKey]
	if !ok {
		entry = &permissionAskLockEntry{}
		s.permissionAskLocks[lockKey] = entry
	}
	entry.refs++
	s.permissionAskMapMu.Unlock()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			s.permissionAskMapMu.Lock()
			defer s.permissionAskMapMu.Unlock()

			current, exists := s.permissionAskLocks[lockKey]
			if !exists || current != entry {
				return
			}
			current.refs--
			if current.refs <= 0 {
				delete(s.permissionAskLocks, lockKey)
			}
		})
	}
	return &entry.mu, release
}

// permissionAskLockKey 生成审批串行锁键，优先按运行隔离，缺失时退化为会话或全局键。
func permissionAskLockKey(runID string, sessionID string) string {
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID != "" {
		return "run:" + trimmedRunID
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID != "" {
		return "session:" + trimmedSessionID
	}
	return "global"
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
