package domain

import "encoding/json"

type Tool interface {
	Name() string
	Description() string
	Run(params map[string]interface{}) *ToolResult
}

// ToolResult 表示执行工具的结果。
type ToolResult struct {
	ToolName string                 `json:"tool"`
	Success  bool                   `json:"success"`
	Output   string                 `json:"output,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

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
