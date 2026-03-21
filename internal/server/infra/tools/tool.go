package tools

import (
	"encoding/json"
	"go-llm-demo/internal/server/domain"
)

type ToolResult = domain.ToolResult
type Tool = domain.Tool

// GlobalRegistry 是ToolRegistry的单例实例。
var GlobalRegistry = &ToolRegistry{
	tools: make(map[string]domain.Tool),
}

// ToolRegistry 管理工具的注册和检索。
type ToolRegistry struct {
	tools map[string]Tool
}

// Register 向注册表添加一个工具。
func (r *ToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get 根据名称从注册表中检索一个工具。
func (r *ToolRegistry) Get(name string) Tool {
	return r.tools[name]
}

// ListTools 返回所有已注册工具名称的切片。
func (r *ToolRegistry) ListTools() []string {
	keys := make([]string, 0, len(r.tools))
	for k := range r.tools {
		keys = append(keys, k)
	}
	return keys
}

// JsonMarshalIndent 用于缩进JSON编码
func JsonMarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

// Initialize 注册所有标准工具。
func Initialize() {
	GlobalRegistry.Register(&ReadTool{})
	GlobalRegistry.Register(&WriteTool{})
	GlobalRegistry.Register(&EditTool{})
	GlobalRegistry.Register(&BashTool{})
	GlobalRegistry.Register(&ListTool{})
	GlobalRegistry.Register(&GrepTool{})
}
