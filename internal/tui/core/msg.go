package core

import "go-llm-demo/internal/tui/services"

type StreamChunkMsg struct {
	Content string
}

func (StreamChunkMsg) isMsg() {}

type StreamDoneMsg struct{}

func (StreamDoneMsg) isMsg() {}

type StreamErrorMsg struct {
	Err error
}

func (StreamErrorMsg) isMsg() {}

type StreamToolCallMsg struct {
	Call   services.ToolCall
	CallID string
}

func (StreamToolCallMsg) isMsg() {}

type ToolResultMsg struct {
	Result *services.ToolResult
	Call   services.ToolCall
	CallID string
}

func (ToolResultMsg) isMsg() {}

type ToolErrorMsg struct {
	Err    error
	CallID string
}

func (ToolErrorMsg) isMsg() {}

type ExitMsg struct{}

func (ExitMsg) isMsg() {}

type ShowHelpMsg struct{}

func (ShowHelpMsg) isMsg() {}

type HideHelpMsg struct{}

func (HideHelpMsg) isMsg() {}

type RefreshMemoryMsg struct{}

func (RefreshMemoryMsg) isMsg() {}

type RefreshTodosMsg struct{}

func (RefreshTodosMsg) isMsg() {}

type TodoUpdatedMsg struct{}

func (TodoUpdatedMsg) isMsg() {}
