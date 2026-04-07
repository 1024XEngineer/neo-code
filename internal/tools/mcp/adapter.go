package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const mcpToolNamePrefix = "mcp."

// AdapterFactory 基于 registry 快照构造 MCP tool 适配器集合。
type AdapterFactory struct {
	registry *Registry
}

// NewAdapterFactory 创建 MCP adapter 工厂。
func NewAdapterFactory(registry *Registry) *AdapterFactory {
	return &AdapterFactory{registry: registry}
}

// BuildAdapters 将当前所有 MCP tool 快照转换为 Adapter 列表。
func (f *AdapterFactory) BuildAdapters(ctx context.Context) ([]*Adapter, error) {
	if f == nil || f.registry == nil {
		return nil, errors.New("mcp: adapter factory registry is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshots := f.registry.Snapshot()
	if len(snapshots) == 0 {
		return nil, nil
	}

	result := make([]*Adapter, 0, len(snapshots)*2)
	for _, snapshot := range snapshots {
		for _, descriptor := range snapshot.Tools {
			adapter, err := NewAdapter(f.registry, snapshot.ServerID, descriptor)
			if err != nil {
				return nil, err
			}
			result = append(result, adapter)
		}
	}
	return result, nil
}

// Adapter 将单个 MCP tool 适配为统一调用描述。
type Adapter struct {
	registry    *Registry
	serverID    string
	toolName    string
	description string
	schema      map[string]any
}

// NewAdapter 创建指定 server/tool 的 MCP 适配器。
func NewAdapter(registry *Registry, serverID string, descriptor ToolDescriptor) (*Adapter, error) {
	if registry == nil {
		return nil, errors.New("mcp: registry is nil")
	}
	normalizedServerID := normalizeServerID(serverID)
	if normalizedServerID == "" {
		return nil, errors.New("mcp: server id is empty")
	}
	normalizedToolName := strings.TrimSpace(descriptor.Name)
	if normalizedToolName == "" {
		return nil, errors.New("mcp: descriptor tool name is empty")
	}

	return &Adapter{
		registry:    registry,
		serverID:    normalizedServerID,
		toolName:    normalizedToolName,
		description: strings.TrimSpace(descriptor.Description),
		schema:      ensureObjectSchema(descriptor.InputSchema),
	}, nil
}

// FullName 返回统一的 MCP tool 名称：mcp.<server_id>.<tool_name>。
func (a *Adapter) FullName() string {
	return composeToolName(a.serverID, a.toolName)
}

// ServerID 返回 MCP server 标识。
func (a *Adapter) ServerID() string {
	return a.serverID
}

// ToolName 返回 MCP tool 原始名称。
func (a *Adapter) ToolName() string {
	return a.toolName
}

// Description 返回工具描述，不存在时回退到稳定默认文案。
func (a *Adapter) Description() string {
	if strings.TrimSpace(a.description) != "" {
		return a.description
	}
	return fmt.Sprintf("MCP tool %s from server %s", a.toolName, a.serverID)
}

// Schema 返回 MCP 工具输入 schema 的标准对象结构。
func (a *Adapter) Schema() map[string]any {
	return cloneSchema(a.schema)
}

// Call 分发 MCP tool 调用并返回统一结果。
func (a *Adapter) Call(ctx context.Context, arguments []byte) (CallResult, error) {
	if a == nil || a.registry == nil {
		return CallResult{}, errors.New("mcp: adapter is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return CallResult{}, err
	}
	return a.registry.Call(ctx, a.serverID, a.toolName, arguments)
}

// composeToolName 组装统一的 MCP tool 名称，保持权限映射可预测。
func composeToolName(serverID string, toolName string) string {
	return mcpToolNamePrefix + normalizeServerID(serverID) + "." + strings.TrimSpace(toolName)
}

// ensureObjectSchema 确保 schema 至少是 object，避免上层 provider 解析异常。
func ensureObjectSchema(schema map[string]any) map[string]any {
	cloned := cloneSchema(schema)
	if len(cloned) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	if strings.TrimSpace(fmt.Sprintf("%v", cloned["type"])) == "" {
		cloned["type"] = "object"
	}
	if _, ok := cloned["properties"]; !ok {
		cloned["properties"] = map[string]any{}
	}
	return cloned
}
