package core

import (
	tea "github.com/charmbracelet/bubbletea"
	"go-llm-demo/internal/tui/services"
)

func Chunk(content string) tea.Cmd {
	return func() tea.Msg {
		return StreamChunkMsg{Content: content}
	}
}

func Done() tea.Cmd {
	return func() tea.Msg {
		return StreamDoneMsg{}
	}
}

func CmdErr(err error) tea.Cmd {
	return func() tea.Msg {
		return StreamErrorMsg{Err: err}
	}
}

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

type ToolResultMsg struct {
	Result *services.ToolResult
}

func (ToolResultMsg) isMsg() {}

type ToolErrorMsg struct {
	Err error
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

type streamNextChunk struct {
	stream <-chan string
}

func (streamNextChunk) isMsg() {}

var StreamDone = func() tea.Msg { return StreamDoneMsg{} }
