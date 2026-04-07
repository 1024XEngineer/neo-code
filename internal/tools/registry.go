package tools

import (
	"context"
	"errors"
	"sort"
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/tools/mcp"
)

type Registry struct {
	tools                map[string]Tool
	microCompactPolicies map[string]MicroCompactPolicy
	mcpRegistry          *mcp.Registry
	mcpFactory           *mcp.AdapterFactory
}

func NewRegistry() *Registry {
	return &Registry{
		tools:                map[string]Tool{},
		microCompactPolicies: map[string]MicroCompactPolicy{},
	}
}

// SetMCPRegistry 绑定 MCP registry，用于将远程工具纳入统一执行链。
func (r *Registry) SetMCPRegistry(registry *mcp.Registry) {
	if r == nil || registry == nil {
		return
	}
	r.mcpRegistry = registry
	r.mcpFactory = mcp.NewAdapterFactory(registry)
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

// MicroCompactPolicy 返回指定工具名的 micro compact 策略；未知工具按默认可压缩处理。
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

// ListAvailableSpecs returns all registered tool specs.
func (r *Registry) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	specs := r.GetSpecs()
	mcpAdapters, err := r.listMCPAdapters(ctx)
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
	if r == nil || r.mcpFactory == nil {
		return false
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(lowerName, "mcp.") {
		return false
	}
	for _, snapshot := range r.mcpFactoryBuildSnapshot() {
		for _, tool := range snapshot.Tools {
			if strings.EqualFold(mcpToolFullName(snapshot.ServerID, tool.Name), lowerName) {
				return true
			}
		}
	}
	return false
}

// listMCPAdapters 返回 MCP 快照对应的 adapter 列表。
func (r *Registry) listMCPAdapters(ctx context.Context) ([]*mcp.Adapter, error) {
	if r == nil || r.mcpFactory == nil {
		return nil, nil
	}
	return r.mcpFactory.BuildAdapters(ctx)
}

// resolveMCPAdapter 按完整工具名解析并返回对应 adapter。
func (r *Registry) resolveMCPAdapter(ctx context.Context, fullName string) (*mcp.Adapter, error) {
	adapters, err := r.listMCPAdapters(ctx)
	if err != nil {
		return nil, err
	}
	lowerName := strings.ToLower(strings.TrimSpace(fullName))
	if !strings.HasPrefix(lowerName, "mcp.") {
		return nil, errors.New("tool: not found")
	}
	for _, adapter := range adapters {
		if strings.EqualFold(adapter.FullName(), lowerName) {
			return adapter, nil
		}
	}
	return nil, errors.New("tool: not found")
}

// mcpFactoryBuildSnapshot 读取 MCP registry 快照，用于无上下文快速检查。
func (r *Registry) mcpFactoryBuildSnapshot() []mcp.ServerSnapshot {
	if r == nil || r.mcpRegistry == nil {
		return nil
	}
	return r.mcpRegistry.Snapshot()
}

func mcpToolFullName(serverID string, toolName string) string {
	return "mcp." + strings.ToLower(strings.TrimSpace(serverID)) + "." + strings.ToLower(strings.TrimSpace(toolName))
}
