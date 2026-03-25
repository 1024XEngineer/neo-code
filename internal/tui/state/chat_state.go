package state

import (
	"time"

	"go-llm-demo/internal/tui/services"
)

type Message struct {
	Role       string
	Content    string
	ToolCalls  []services.ChatToolCall
	ToolCallID string
	Timestamp  time.Time
	Streaming  bool
}

type PendingApproval struct {
	Call     services.ToolCall
	ToolType string
	Target   string
	CallID   string
}

type ChatState struct {
	Messages        []Message
	HistoryTurns    int
	Generating      bool
	ActiveModel     string
	MemoryStats     services.MemoryStats
	CommandHistory  []string
	CmdHistIndex    int
	WorkspaceRoot   string
	ToolExecuting   bool
	PendingApproval *PendingApproval
	APIKeyReady     bool
	ConfigPath      string
}
