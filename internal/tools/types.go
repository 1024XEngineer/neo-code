package tools

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	MicroCompactPolicy() MicroCompactPolicy
	Execute(ctx context.Context, call ToolCallInput) (ToolResult, error)
}

type ChunkEmitter func(chunk []byte)

type ToolCallInput struct {
	ID            string
	Name          string
	Arguments     []byte
	SessionID     string
	Workdir       string
	WorkspacePlan *security.WorkspaceExecutionPlan
	EmitChunk     ChunkEmitter
}

type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Metadata   map[string]any
}

type ToolSpec = providertypes.ToolSpec
