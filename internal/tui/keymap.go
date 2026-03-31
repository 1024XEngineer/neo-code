package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Send        key.Binding
	Newline     key.Binding
	CancelAgent key.Binding
	NewSession  key.Binding
	NextPanel   key.Binding
	PrevPanel   key.Binding
	FocusInput  key.Binding
	OpenSession key.Binding
	ToggleHelp  key.Binding
	Quit        key.Binding
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Top         key.Binding
	Bottom      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "发送（输入框）"),
		),
		Newline: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("Ctrl+J", "换行（输入框）"),
		),
		CancelAgent: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("Ctrl+W", "中止"),
		),
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("Ctrl+N", "新会话"),
		),
		NextPanel: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "下个面板"),
		),
		PrevPanel: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("Shift+Tab", "上个面板"),
		),
		FocusInput: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "聚焦输入框"),
		),
		OpenSession: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "打开会话"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("ctrl+q"),
			key.WithHelp("Ctrl+Q", "帮助"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("Ctrl+U", "退出"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("Up/K", "向上滚动"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("Down/J", "向下滚动"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "b"),
			key.WithHelp("PgUp/B", "向上翻页"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "f"),
			key.WithHelp("PgDn/F", "向下翻页"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("G/Home", "跳到顶部"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("Shift+G/End", "跳到底部"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Newline, k.CancelAgent, k.ToggleHelp, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.Newline, k.CancelAgent, k.NewSession},
		{k.OpenSession, k.FocusInput, k.NextPanel, k.PrevPanel},
		{k.ToggleHelp, k.Quit, k.ScrollUp, k.ScrollDown},
		{k.PageUp, k.PageDown, k.Top, k.Bottom},
	}
}
