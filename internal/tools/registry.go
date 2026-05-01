package tools

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/tools/mcp"
)

type Registry struct {
	tools                   map[string]Tool
	microCompactPolicies    map[string]MicroCompactPolicy
	microCompactSummarizers map[string]ContentSummarizer
	microCompactSummaryMu   sync.RWMutex
	mcpMu                   sync.RWMutex
	mcpRegistry             *mcp.Registry
	mcpFactory              *mcp.AdapterFactory
	mcpExposureFilter       mcp.ExposureFilter
	mcpExposureAudit        []mcp.ExposureDecision
}

func NewRegistry() *Registry {
	return &Registry{
		tools:                   map[string]Tool{},
		microCompactPolicies:    map[string]MicroCompactPolicy{},
		microCompactSummarizers: map[string]ContentSummarizer{},
	}
}

// SetMCPRegistry 绑定 MCP registry，用于将远程工具纳入统一执行链。
func (r *Registry) SetMCPRegistry(registry *mcp.Registry) {
	if r == nil || registry == nil {
		return
	}
	r.mcpMu.Lock()
	defer r.mcpMu.Unlock()
	r.mcpRegistry = registry
	r.mcpFactory = mcp.NewAdapterFactory(registry)
	if r.mcpExposureFilter == nil {
		r.mcpExposureFilter = mcp.NewExposureFilter(mcp.ExposureFilterConfig{})
	}
}

// ReplaceMCPRegistry 安全替换当前 MCP registry，先绑定新实例再在锁外关闭旧实例，避免并发读路径命中已关闭的 client；filter 非 nil 时同步更新暴露过滤器。
func (r *Registry) ReplaceMCPRegistry(registry *mcp.Registry, filter mcp.ExposureFilter) {
	if r == nil {
		return
	}
	r.mcpMu.Lock()
	oldRegistry := r.mcpRegistry
	r.mcpRegistry = registry
	if registry != nil {
		r.mcpFactory = mcp.NewAdapterFactory(registry)
	} else {
		r.mcpFactory = nil
	}
	if filter != nil {
		r.mcpExposureFilter = filter
	}
	r.mcpMu.Unlock()

	if oldRegistry != nil {
		_ = oldRegistry.Close()
	}
}

// SetMCPExposureFilter 绑定 MCP 暴露过滤器，仅影响模型可见 specs，不影响工具执行。
func (r *Registry) SetMCPExposureFilter(filter mcp.ExposureFilter) {
	if r == nil {
		return
	}
	r.mcpMu.Lock()
	defer r.mcpMu.Unlock()
	r.mcpExposureFilter = filter
}

// MCPExposureAuditSnapshot 返回最近一次 specs 过滤得到的 MCP 审计决策副本。
func (r *Registry) MCPExposureAuditSnapshot() []mcp.ExposureDecision {
	if r == nil || len(r.mcpExposureAudit) == 0 {
		return nil
	}
	cloned := make([]mcp.ExposureDecision, len(r.mcpExposureAudit))
	copy(cloned, r.mcpExposureAudit)
	return cloned
}

func (r *Registry) Register(tool Tool) {
	if tool == nil {
		return
	}
	name := strings.ToLower(tool.Name())
	r.tools[name] = tool
	switch tool.MicroCompactPolicy() {
	case MicroCompactPolicyPreserveHistory:
		r.microCompactPolicies[name] = MicroCompactPolicyPreserveHistory
	default:
		r.microCompactPolicies[name] = MicroCompactPolicyCompact
	}
}

func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.tools[strings.ToLower(name)]
	if !ok {
		return nil, errors.New("tool: not found")
	}
	return tool, nil
}

// Supports reports whether a tool is registered.
func (r *Registry) Supports(name string) bool {
	if _, err := r.Get(name); err == nil {
		return true
	}
	return r.supportsMCPTool(name)
}

// MicroCompactPolicy 返回指定工具的 micro compact 策略；未知工具按默认可压缩处理。
func (r *Registry) MicroCompactPolicy(name string) MicroCompactPolicy {
	if r == nil {
		return MicroCompactPolicyCompact
	}
	policy, ok := r.microCompactPolicies[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return MicroCompactPolicyCompact
	}
	if policy == MicroCompactPolicyPreserveHistory {
		return MicroCompactPolicyPreserveHistory
	}
	return MicroCompactPolicyCompact
}

// RegisterSummarizer 为指定工具注册内容摘要器；传入 nil 移除已有条目。
func (r *Registry) RegisterSummarizer(toolName string, summarizer ContentSummarizer) {
	if r == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	r.microCompactSummaryMu.Lock()
	defer r.microCompactSummaryMu.Unlock()
	if summarizer == nil {
		delete(r.microCompactSummarizers, name)
		return
	}
	r.microCompactSummarizers[name] = summarizer
}

// MicroCompactSummarizer 返回指定工具的内容摘要器；无注册时返回 nil。
func (r *Registry) MicroCompactSummarizer(name string) ContentSummarizer {
	if r == nil {
		return nil
	}
	r.microCompactSummaryMu.RLock()
	defer r.microCompactSummaryMu.RUnlock()
	if r.microCompactSummarizers == nil {
		return nil
	}
	return r.microCompactSummarizers[strings.ToLower(strings.TrimSpace(name))]
}

func (r *Registry) GetSpecs() []providertypes.ToolSpec {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]providertypes.ToolSpec, 0, len(names))
	for _, name := range names {
		tool := r.tools[name]
		specs = append(specs, providertypes.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			Schema:      tool.Schema(),
		})
	}
	return specs
}

func (r *Registry) ListSchemas() []providertypes.ToolSpec {
	return r.GetSpecs()
}

// ListAvailableSpecs 返回当前上下文下模型可见的工具 specs。
func (r *Registry) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	specs := r.GetSpecs()
	mcpAdapters, err := r.listMCPAdapters(ctx, input)
	if err != nil {
		return nil, err
	}
	for _, adapter := range mcpAdapters {
		specs = append(specs, providertypes.ToolSpec{
			Name:        adapter.FullName(),
			Description: adapter.Description(),
			Schema:      adapter.Schema(),
		})
	}
	sort.Slice(specs, func(i, j int) bool {
		return strings.ToLower(specs[i].Name) < strings.ToLower(specs[j].Name)
	})
	return specs, nil
}

func (r *Registry) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	tool, err := r.Get(input.Name)
	if err == nil {
		result, execErr := tool.Execute(ctx, input)
		result.ToolCallID = input.ID
		if execErr != nil {
			result.IsError = true
			if strings.TrimSpace(result.Content) == "" {
				result.Content = FormatError(result.Name, NormalizeErrorReason(result.Name, execErr), "")
			}
			return result, execErr
		}
		return result, nil
	}

	adapter, resolveErr := r.resolveMCPAdapter(ctx, input.Name)
	if resolveErr != nil {
		content := FormatError(input.Name, NormalizeErrorReason(input.Name, resolveErr), "")
		return ToolResult{
			ToolCallID: input.ID,
			Name:       input.Name,
			Content:    content,
			IsError:    true,
		}, resolveErr
	}
	callResult, callErr := adapter.Call(ctx, input.Arguments)
	result := ToolResult{
		ToolCallID: input.ID,
		Name:       adapter.FullName(),
		Content:    strings.TrimSpace(callResult.Content),
		IsError:    callResult.IsError,
		Metadata: map[string]any{
			"mcp_server_id": adapter.ServerID(),
			"mcp_tool_name": adapter.ToolName(),
		},
	}
	for key, value := range callResult.Metadata {
		if shouldSkipMCPMetadataKey(key, result.Metadata) {
			continue
		}
		result.Metadata[key] = value
	}
	if callErr != nil {
		result.IsError = true
		if strings.TrimSpace(result.Content) == "" {
			result.Content = FormatError(result.Name, NormalizeErrorReason(result.Name, callErr), "")
		}
		result = ApplyOutputLimit(result, DefaultOutputLimitBytes)
		return result, callErr
	}
	if result.IsError {
		if strings.TrimSpace(result.Content) == "" {
			result.Content = FormatError(result.Name, "mcp tool returned isError=true", "")
		}
		result = ApplyOutputLimit(result, DefaultOutputLimitBytes)
		return result, errors.New("mcp: tool returned error result")
	}
	if result.Content == "" {
		result.Content = "ok"
	}
	result = ApplyOutputLimit(result, DefaultOutputLimitBytes)
	return result, nil
}

// RememberSessionDecision 对纯 Registry 管理器不生效，保留接口以满足 runtime 依赖。
func (r *Registry) RememberSessionDecision(sessionID string, action security.Action, scope SessionPermissionScope) error {
	return errors.New("tools: session permission memory is unsupported by registry manager")
}

// supportsMCPTool 判断指定工具名是否可由当前 MCP 快照解析。
func (r *Registry) supportsMCPTool(name string) bool {
	if r == nil {
		return false
	}
	r.mcpMu.RLock()
	defer r.mcpMu.RUnlock()
	if r.mcpFactory == nil {
		return false
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(lowerName, "mcp.") {
		return false
	}
	for _, snapshot := range r.mcpRegistry.Snapshot() {
		for _, tool := range snapshot.Tools {
			if strings.EqualFold(mcpToolFullName(snapshot.ServerID, tool.Name), lowerName) {
				return true
			}
		}
	}
	return false
}

// listMCPAdapters 返回 MCP 快照对应的 adapter 列表。
func (r *Registry) listMCPAdapters(ctx context.Context, input SpecListInput) ([]*mcp.Adapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	r.mcpMu.RLock()
	defer r.mcpMu.RUnlock()
	if r.mcpFactory == nil || r.mcpRegistry == nil {
		return nil, nil
	}
	snapshots := r.mcpRegistry.Snapshot()
	filteredSnapshots, decisions, err := r.filterMCPSnapshots(ctx, snapshots, mcp.ExposureFilterInput{
		SessionID: input.SessionID,
		Agent:     input.Agent,
		Query:     input.Query,
	})
	if err != nil {
		return nil, err
	}
	r.mcpExposureAudit = decisions
	return r.mcpFactory.BuildAdaptersFromSnapshots(ctx, filteredSnapshots)
}

// resolveMCPAdapter 按完整工具名解析并返回对应 adapter。
func (r *Registry) resolveMCPAdapter(ctx context.Context, fullName string) (*mcp.Adapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("tool: not found")
	}
	r.mcpMu.RLock()
	defer r.mcpMu.RUnlock()
	if r.mcpRegistry == nil {
		return nil, errors.New("tool: not found")
	}

	serverID, toolName, ok := parseMCPToolFullName(fullName)
	if !ok {
		return nil, errors.New("tool: not found")
	}

	snapshots := r.mcpRegistry.Snapshot()
	if r.isMCPToolPolicyDenied(ctx, snapshots, fullName) {
		return nil, errors.New("tool: not found")
	}

	for _, snapshot := range snapshots {
		if !strings.EqualFold(snapshot.ServerID, serverID) {
			continue
		}
		for _, descriptor := range snapshot.Tools {
			if strings.EqualFold(strings.TrimSpace(descriptor.Name), toolName) {
				return mcp.NewAdapter(r.mcpRegistry, snapshot.ServerID, descriptor)
			}
		}
	}
	return nil, errors.New("tool: not found")
}

// filterMCPSnapshots 在暴露阶段过滤 MCP snapshots，并在过滤失败时按 fail-closed 退化。
func (r *Registry) filterMCPSnapshots(
	ctx context.Context,
	snapshots []mcp.ServerSnapshot,
	input mcp.ExposureFilterInput,
) ([]mcp.ServerSnapshot, []mcp.ExposureDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if r == nil {
		return nil, nil, nil
	}
	filter := r.mcpExposureFilter
	if filter == nil {
		filter = mcp.NewExposureFilter(mcp.ExposureFilterConfig{})
	}
	filteredSnapshots, decisions, err := filter.Filter(ctx, snapshots, input)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, err
		}
		failClosed := buildMCPFilterErrorAudit(snapshots)
		r.mcpExposureAudit = failClosed
		return nil, failClosed, nil
	}
	return filteredSnapshots, decisions, nil
}

// isMCPToolPolicyDenied 判断指定 MCP 工具是否命中 policy_deny，命中则禁止执行。
func (r *Registry) isMCPToolPolicyDenied(ctx context.Context, snapshots []mcp.ServerSnapshot, fullName string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if r == nil || len(snapshots) == 0 {
		return false
	}
	filter := r.mcpExposureFilter
	if filter == nil {
		filter = mcp.NewExposureFilter(mcp.ExposureFilterConfig{})
	}
	_, decisions, err := filter.Filter(ctx, snapshots, mcp.ExposureFilterInput{})
	if err != nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(fullName))
	for _, decision := range decisions {
		if strings.EqualFold(decision.ToolFullName, normalized) && decision.Reason == mcp.ExposureFilterReasonPolicyDeny {
			return true
		}
	}
	return false
}

// buildMCPFilterErrorAudit 为过滤器异常生成 fail-closed 审计记录。
func buildMCPFilterErrorAudit(snapshots []mcp.ServerSnapshot) []mcp.ExposureDecision {
	if len(snapshots) == 0 {
		return nil
	}
	decisions := make([]mcp.ExposureDecision, 0)
	for _, snapshot := range snapshots {
		for _, tool := range snapshot.Tools {
			decisions = append(decisions, mcp.ExposureDecision{
				ServerID:     snapshot.ServerID,
				ToolName:     tool.Name,
				ToolFullName: mcpToolFullName(snapshot.ServerID, tool.Name),
				Allowed:      false,
				Reason:       mcp.ExposureFilterReasonFilterError,
			})
		}
	}
	return decisions
}

// mcpFactoryBuildSnapshot 读取 MCP registry 快照，用于无上下文快速检查。
func (r *Registry) mcpFactoryBuildSnapshot() []mcp.ServerSnapshot {
	if r == nil {
		return nil
	}
	r.mcpMu.RLock()
	defer r.mcpMu.RUnlock()
	if r.mcpRegistry == nil {
		return nil
	}
	return r.mcpRegistry.Snapshot()
}

func mcpToolFullName(serverID string, toolName string) string {
	return "mcp." + strings.ToLower(strings.TrimSpace(serverID)) + "." + strings.ToLower(strings.TrimSpace(toolName))
}

// parseMCPToolFullName 解析 mcp.<server>.<tool> 形式的完整工具名。
func parseMCPToolFullName(fullName string) (string, string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(fullName))
	if !strings.HasPrefix(normalized, "mcp.") {
		return "", "", false
	}
	parts := strings.SplitN(normalized, ".", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// shouldSkipMCPMetadataKey 过滤 MCP 远端透传 metadata 中会影响本地安全语义或覆盖保留键的字段。
func shouldSkipMCPMetadataKey(key string, existing map[string]any) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return true
	}
	if _, reserved := existing[normalized]; reserved {
		return true
	}
	switch normalized {
	case "workspace_write", "verification_performed", "verification_passed", "verification_scope":
		return true
	default:
		return false
	}
}
