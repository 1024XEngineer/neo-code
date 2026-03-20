package tools

import (
	"encoding/json"
)

// Tool 定义了所有工具必须实现的接口。
type Tool interface {
	// Name 返回工具的唯一名称。
	Name() string
	// Description 返回工具的人类可读描述。
	Description() string
	// Run 执行工具并返回ToolResult。
	// 参数以map[string]interface{}形式传递，以匹配预期的AI调用格式。
	Run(params map[string]interface{}) *ToolResult
}

// ToolResult 表示执行工具的结果。
type ToolResult struct {
	// ToolName 是生成此结果的工具的名称。
	ToolName string `json:"tool"`
	// Success 指示工具是否成功执行。
	Success bool `json:"success"`
	// Output 包含工具的成功输出（如果有）。
	Output string `json:"output,omitempty"`
	// Error 包含工具失败时的错误信息。
	Error string `json:"error,omitempty"`
	// Metadata 包含关于执行的附加信息。
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ToolRegistry 管理工具的注册和检索。
type ToolRegistry struct {
	tools map[string]Tool
}

// GlobalRegistry 是ToolRegistry的单例实例。
var GlobalRegistry = &ToolRegistry{
	tools: make(map[string]Tool),
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

// MarshalJSON 自定义ToolResult的JSON编码以省略空字段。
func (tr *ToolResult) MarshalJSON() ([]byte, error) {
	type Alias ToolResult
	return json.Marshal(&struct {
		*Alias
		Output string `json:"output,omitempty"`
		Error  string `json:"error,omitempty"`
	}{
		Alias:  (*Alias)(tr),
		Output: tr.Output,
		Error:  tr.Error,
	})
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
