package core

import "go-llm-demo/internal/tui/services"

type StreamChunkMsg struct {
	Content string
}

func (StreamChunkMsg) isMsg() {}

type StreamReadyMsg struct {
	Stream <-chan string
}

func (StreamReadyMsg) isMsg() {}

type StreamDoneMsg struct{}

func (StreamDoneMsg) isMsg() {}

type StreamErrorMsg struct {
	Err error
}

func (StreamErrorMsg) isMsg() {}

type ToolResultMsg struct {
	Result *services.ToolResult
	Call   services.ToolCall
}

func (ToolResultMsg) isMsg() {}

type ToolErrorMsg struct {
	Err error
}

func (ToolErrorMsg) isMsg() {}

type ExitMsg struct{}

func (ExitMsg) isMsg() {}

type RefreshMemoryMsg struct{}

func (RefreshMemoryMsg) isMsg() {}
