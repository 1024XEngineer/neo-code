package domain

import (
	"encoding/json"
	"strings"
)

// ToolCall represents a tool invocation request.
type ToolCall struct {
	Tool   string                 `json:"tool"`
	Params map[string]interface{} `json:"params"`
}

type ToolName string

const (
	ToolRead     ToolName = "Read"
	ToolWrite    ToolName = "Write"
	ToolEdit     ToolName = "Edit"
	ToolBash     ToolName = "Bash"
	ToolList     ToolName = "List"
	ToolGrep     ToolName = "Grep"
	ToolWebFetch ToolName = "Webfetch"
	ToolTodo     ToolName = "Todo"
)

func ParseToolName(input string) (ToolName, bool) {
	normalized := ToolName(strings.ToLower(strings.TrimSpace(input)))
	if normalized == "web_fetch" {
		normalized = ToolWebFetch
	}
	switch normalized {
	case ToolRead, ToolWrite, ToolEdit, ToolBash, ToolList, ToolGrep, ToolWebFetch, ToolTodo:
		return normalized, true
	default:
		return "", false
	}
}

type Tool interface {
	Definition() ToolDefinition
	Run(params map[string]interface{}) *ToolResult
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	ToolName string                 `json:"tool"`
	Success  bool                   `json:"success"`
	Output   string                 `json:"output,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ToolDefinition describes a tool and its parameter schema.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  []ToolParamSpec `json:"parameters"`
}

// ToolParamSpec describes a single tool parameter.
type ToolParamSpec struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Required     bool        `json:"required"`
	Description  string      `json:"description"`
	DefaultValue interface{} `json:"default,omitempty"`
	Enum         []string    `json:"enum,omitempty"`
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
