package tools

import (
	"context"

	"github.com/dust/neo-code/internal/provider"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, call ToolCallInput) (ToolResult, error)
}

type ChunkEmitter func(chunk []byte)

type ToolCallInput struct {
	ID        string
	Name      string
	Arguments []byte
	SessionID string
	Workdir   string
	EmitChunk ChunkEmitter
}

type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Metadata   map[string]any
}

type ToolSpec = provider.ToolSpec
