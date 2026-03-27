package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Send        key.Binding
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
			key.WithKeys("enter", "ctrl+s"),
			key.WithHelp("enter", "send"),
		),
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "new"),
		),
		NextPanel: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		PrevPanel: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev panel"),
		),
		FocusInput: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "focus input"),
		),
		OpenSession: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open session"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("up/k", "up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("down/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "b"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "f"),
			key.WithHelp("pgdn", "page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.NewSession, k.NextPanel, k.ToggleHelp, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.NewSession, k.OpenSession, k.FocusInput},
		{k.NextPanel, k.PrevPanel, k.ToggleHelp, k.Quit},
		{k.ScrollUp, k.ScrollDown, k.PageUp, k.PageDown},
		{k.Top, k.Bottom},
	}
}
