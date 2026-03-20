package core

import "github.com/charmbracelet/bubbletea"

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

func Tick() tea.Cmd {
	return func() tea.Msg {
		return TickMsg{}
	}
}

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
