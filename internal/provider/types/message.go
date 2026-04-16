package types

import "strings"

// RoleSystem 标识系统消息。
const RoleSystem = "system"

// RoleUser 标识用户消息。
const RoleUser = "user"

// RoleAssistant 标识助手消息。
const RoleAssistant = "assistant"

// RoleTool 标识工具结果消息。
const RoleTool = "tool"

// Message 表示对话中的单条消息。
type Message struct {
	Role         string            `json:"role"`
	Parts        []ContentPart     `json:"parts,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	IsError      bool              `json:"is_error,omitempty"`
	ToolMetadata map[string]string `json:"tool_metadata,omitempty"`
}

// IsEmpty checks if the message has no content parts and no tool calls.
func (m *Message) IsEmpty() bool {
	if len(m.ToolCalls) > 0 {
		return false
	}

	for _, part := range m.Parts {
		if part.Kind != ContentPartText || strings.TrimSpace(part.Text) != "" {
			return false
		}
	}

	return true
}

// Validate ensures the message is well-formed.
func (m *Message) Validate() error {
	return ValidateParts(m.Parts)
}

// ToolCall 表示模型发起的工具调用请求。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec 表示暴露给模型的可调用工具描述。
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}
