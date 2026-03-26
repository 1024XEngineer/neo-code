package tools

import (
	"context"
	"encoding/json"
)

// Tool exposes a model-callable action to Runtime.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, call Invocation) (Result, error)
}

// Invocation contains the normalized inputs to a tool execution.
type Invocation struct {
	ID        string
	Name      string
	Arguments json.RawMessage
	SessionID string
	Workdir   string
}

// Result is the standardized tool result sent back into Runtime.
type Result struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Metadata   map[string]any
}
