package core

import (
	"github.com/charmbracelet/bubbletea"
	"go-llm-demo/internal/server/infra/tools"
)

type Msg interface{ isMsg() }

type (
	InitMsg struct{}

	ResizeMsg struct {
		Width  int
		Height int
	}

	InputMsg struct {
		Value string
	}

	CodeLineMsg struct {
		Line string
	}

	CodeDelimiterMsg struct {
		Delim string
	}

	SubmitMsg struct{}

	CancelMsg struct{}

	StreamChunkMsg struct {
		Content string
	}

	StreamDoneMsg struct{}

	StreamErrorMsg struct {
		Err error
	}

	CommandMsg struct {
		Name string
		Args []string
	}

	SwitchModelMsg struct {
		Model string
	}

	MemoryStatsMsg struct {
		Stats interface{}
	}

	ShowHelpMsg struct{}

	HideHelpMsg struct{}

	ExitMsg struct{}

	RefreshMemoryMsg struct{}

	// 工具执行结果消息
	ToolResultMsg struct {
		Result *tools.ToolResult
	}

	// 工具执行错误消息
	ToolErrorMsg struct {
		Err error
	}
)

func (InitMsg) isMsg()          {}
func (ResizeMsg) isMsg()        {}
func (InputMsg) isMsg()         {}
func (CodeLineMsg) isMsg()      {}
func (CodeDelimiterMsg) isMsg() {}
func (SubmitMsg) isMsg()        {}
func (CancelMsg) isMsg()        {}
func (StreamChunkMsg) isMsg()   {}
func (StreamDoneMsg) isMsg()    {}
func (StreamErrorMsg) isMsg()   {}
func (CommandMsg) isMsg()       {}
func (SwitchModelMsg) isMsg()   {}
func (MemoryStatsMsg) isMsg()   {}
func (ShowHelpMsg) isMsg()      {}
func (HideHelpMsg) isMsg()      {}
func (ExitMsg) isMsg()          {}
func (RefreshMemoryMsg) isMsg() {}

type TickMsg struct{}

func (TickMsg) isMsg() {}

// Tick 返回一个会发出 TickMsg 的命令。
func Tick() tea.Cmd {
	return func() tea.Msg {
		return TickMsg{}
	}
}

// Chunk 返回一个会发出流式内容消息的命令。
func Chunk(content string) tea.Cmd {
	return func() tea.Msg {
		return StreamChunkMsg{Content: content}
	}
}

// Done 返回一个表示流结束的命令。
func Done() tea.Cmd {
	return func() tea.Msg {
		return StreamDoneMsg{}
	}
}

// CmdErr 返回一个会发出流错误消息的命令。
func CmdErr(err error) tea.Cmd {
	return func() tea.Msg {
		return StreamErrorMsg{Err: err}
	}
}
