package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

const (
	subAgentToolDecisionPending  = "pending"
	stringPermissionDecisionAsk  = "ask"
	defaultSubAgentToolTimeout   = 20 * time.Second
	defaultSubAgentCapabilityTTL = 15 * time.Minute
)

// subAgentRuntimeToolExecutor 将 subagent 工具调用桥接到 runtime 的统一执行链路。
type subAgentRuntimeToolExecutor struct {
	service *Service
}

// newSubAgentRuntimeToolExecutor 创建子代理工具桥接器。
func newSubAgentRuntimeToolExecutor(service *Service) subagent.ToolExecutor {
	return &subAgentRuntimeToolExecutor{service: service}
}

// ListToolSpecs 返回子代理在当前会话可见的工具 schema，并按 allowlist 再过滤一层。
func (e *subAgentRuntimeToolExecutor) ListToolSpecs(
	ctx context.Context,
	input subagent.ToolSpecListInput,
) ([]providertypes.ToolSpec, error) {
	if e == nil || e.service == nil || e.service.toolManager == nil {
		return nil, errors.New("runtime: subagent tool executor is unavailable")
	}
	specs, err := e.service.toolManager.ListAvailableSpecs(ctx, tools.SpecListInput{
		SessionID: strings.TrimSpace(input.SessionID),
		Agent:     strings.TrimSpace(string(input.Role)),
	})
	if err != nil {
		return nil, err
	}
	return filterToolSpecsByAllowlist(specs, input.AllowedTools), nil
}

// ExecuteTool 执行一次子代理工具调用，并补齐 started/result/denied 事件。
func (e *subAgentRuntimeToolExecutor) ExecuteTool(
	ctx context.Context,
	input subagent.ToolExecutionInput,
) (subagent.ToolExecutionResult, error) {
	if e == nil || e.service == nil {
		return subagent.ToolExecutionResult{}, errors.New("runtime: subagent tool executor is unavailable")
	}
	startedAt := time.Now()
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultSubAgentToolTimeout
	}
	runID := strings.TrimSpace(input.RunID)
	sessionID := strings.TrimSpace(input.SessionID)
	taskID := strings.TrimSpace(input.TaskID)
	agentID := strings.TrimSpace(input.AgentID)
	workdir := strings.TrimSpace(input.Workdir)
	callName := strings.TrimSpace(input.Call.Name)

	payload := SubAgentToolCallEventPayload{
		Role:      input.Role,
		TaskID:    taskID,
		ToolName:  callName,
		Decision:  subAgentToolDecisionPending,
		ElapsedMS: 0,
	}
	e.emit(ctx, runID, sessionID, EventSubAgentToolCallStarted, payload)

	result, execErr := e.service.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:       runID,
		SessionID:   sessionID,
		TaskID:      taskID,
		AgentID:     agentID,
		Capability:  e.resolveCapabilityToken(input),
		Call:        input.Call,
		Workdir:     workdir,
		ToolTimeout: timeout,
	})

	output := subagent.ToolExecutionResult{
		ToolCallID: input.Call.ID,
		Name:       strings.TrimSpace(input.Call.Name),
		Content:    result.Content,
		IsError:    result.IsError,
		Decision:   permissionDecisionAllow,
		Metadata:   cloneToolMetadata(result.Metadata),
	}
	if strings.TrimSpace(result.ToolCallID) != "" {
		output.ToolCallID = strings.TrimSpace(result.ToolCallID)
	}
	if strings.TrimSpace(result.Name) != "" {
		output.Name = strings.TrimSpace(result.Name)
	}

	decision := resolveToolExecutionDecision(execErr)
	if execErr != nil {
		output.Decision = decision
		if strings.TrimSpace(output.Content) == "" {
			output.Content = strings.TrimSpace(execErr.Error())
		}
		output.IsError = true
	}

	eventPayload := SubAgentToolCallEventPayload{
		Role:      input.Role,
		TaskID:    taskID,
		ToolName:  output.Name,
		Decision:  decision,
		ElapsedMS: elapsedMilliseconds(startedAt),
		Truncated: toolResultTruncated(output.Metadata),
	}
	if execErr != nil {
		eventPayload.Error = strings.TrimSpace(execErr.Error())
	}

	eventType := EventSubAgentToolCallResult
	if strings.EqualFold(decision, permissionDecisionDeny) || strings.EqualFold(decision, stringPermissionDecisionAsk) {
		eventType = EventSubAgentToolCallDenied
	}
	e.emit(ctx, runID, sessionID, eventType, eventPayload)
	return output, execErr
}

type capabilitySignerProvider interface {
	CapabilitySigner() *security.CapabilitySigner
}

// resolveCapabilityToken 生成并签发子代理工具调用的 capability token，用于在权限链路硬执行能力边界。
func (e *subAgentRuntimeToolExecutor) resolveCapabilityToken(input subagent.ToolExecutionInput) *security.CapabilityToken {
	if input.CapabilityToken != nil {
		token := input.CapabilityToken.Normalize()
		return &token
	}
	if e == nil || e.service == nil {
		return nil
	}
	toolsList := normalizeAllowlistToList(input.Capability.AllowedTools)
	pathsList := normalizePathAllowlist(input.Capability.AllowedPaths)
	if len(toolsList) == 0 && len(pathsList) == 0 {
		return nil
	}

	signerProvider, ok := e.service.toolManager.(capabilitySignerProvider)
	if !ok {
		return nil
	}
	signer := signerProvider.CapabilitySigner()
	if signer == nil {
		return nil
	}

	toolName := strings.TrimSpace(input.Call.Name)
	if len(toolsList) == 0 && toolName != "" {
		toolsList = []string{toolName}
	}
	if len(toolsList) == 0 {
		return nil
	}
	now := time.Now().UTC()
	token := security.CapabilityToken{
		ID:            fmt.Sprintf("subagent-%d-%s", now.UnixNano(), strings.TrimSpace(input.TaskID)),
		TaskID:        strings.TrimSpace(input.TaskID),
		AgentID:       strings.TrimSpace(input.AgentID),
		IssuedAt:      now,
		ExpiresAt:     now.Add(defaultSubAgentCapabilityTTL),
		AllowedTools:  toolsList,
		AllowedPaths:  pathsList,
		NetworkPolicy: security.NetworkPolicy{Mode: security.NetworkPermissionAllowAll},
	}
	signed, err := signer.Sign(token)
	if err != nil {
		return nil
	}
	return &signed
}

// resolveToolExecutionDecision 根据工具执行错误映射统一的权限决策结果。
func resolveToolExecutionDecision(execErr error) string {
	if execErr == nil {
		return permissionDecisionAllow
	}
	var permissionErr *tools.PermissionDecisionError
	if errors.As(execErr, &permissionErr) {
		return permissionErr.Decision()
	}
	if isSubAgentPermissionDeniedError(execErr) {
		return permissionDecisionDeny
	}
	return "error"
}

// emit 发出子代理工具调用事件，失败路径按 best-effort 忽略。
func (e *subAgentRuntimeToolExecutor) emit(
	ctx context.Context,
	runID string,
	sessionID string,
	eventType EventType,
	payload SubAgentToolCallEventPayload,
) {
	if e == nil || e.service == nil {
		return
	}
	_ = e.service.emit(ctx, eventType, strings.TrimSpace(runID), strings.TrimSpace(sessionID), payload)
}

// filterToolSpecsByAllowlist 按工具名白名单过滤 schema 列表，白名单为空时默认拒绝全部。
func filterToolSpecsByAllowlist(specs []providertypes.ToolSpec, allowlist []string) []providertypes.ToolSpec {
	if len(specs) == 0 {
		return nil
	}
	normalizedAllowlist := normalizeAllowlist(allowlist)
	if len(normalizedAllowlist) == 0 {
		return nil
	}
	filtered := make([]providertypes.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		name := strings.ToLower(strings.TrimSpace(spec.Name))
		if _, ok := normalizedAllowlist[name]; !ok {
			continue
		}
		filtered = append(filtered, spec)
	}
	return filtered
}

// normalizeAllowlist 规整工具白名单，便于大小写无关匹配。
func normalizeAllowlist(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		result[normalized] = struct{}{}
	}
	return result
}

// normalizeAllowlistToList 规整白名单并输出稳定顺序列表，便于写入 capability token。
func normalizeAllowlistToList(items []string) []string {
	seen := normalizeAllowlist(items)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; !ok {
			continue
		}
		out = append(out, normalized)
		delete(seen, normalized)
	}
	return out
}

// normalizePathAllowlist 规整路径白名单并去重，避免 capability token 带入空路径。
func normalizePathAllowlist(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// cloneToolMetadata 深拷贝工具元数据，避免后续修改污染事件载荷。
func cloneToolMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

// toolResultTruncated 从工具 metadata 提取截断标记。
func toolResultTruncated(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	value, ok := metadata["truncated"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// elapsedMilliseconds 返回从起始时刻到当前的毫秒耗时。
func elapsedMilliseconds(startedAt time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(startedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed.Milliseconds()
}
