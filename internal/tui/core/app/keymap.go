package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Send          key.Binding
	Newline       key.Binding
	CancelAgent   key.Binding
	NewSession    key.Binding
	OpenWorkspace key.Binding
	NextPanel     key.Binding
	PrevPanel     key.Binding
	FocusInput    key.Binding
	ToggleHelp    key.Binding
	Quit          key.Binding
	ScrollUp      key.Binding
	ScrollDown    key.Binding
	PageUp        key.Binding
	PageDown      key.Binding
	Top           key.Binding
	Bottom        key.Binding
	PasteImage    key.Binding
	LogViewer     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "Send"),
		),
		Newline: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("Ctrl+J", "New line"),
		),
		CancelAgent: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("Ctrl+W", "Cancel"),
		),
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("Ctrl+N", "New session"),
		),
		OpenWorkspace: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("Ctrl+O", "Workspace"),
		),
		NextPanel: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "Next panel"),
		),
		PrevPanel: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("Shift+Tab", "Prev panel"),
		),
		FocusInput: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "Focus input"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("ctrl+q"),
			key.WithHelp("Ctrl+Q", "/help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("Ctrl+U", "/exit"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("Up/K", "Scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("Down/J", "Scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "b"),
			key.WithHelp("PgUp/B", "Page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "f"),
			key.WithHelp("PgDn/F", "Page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("G/Home", "Top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("Shift+G/End", "Bottom"),
		),
		PasteImage: key.NewBinding(
			key.WithKeys("ctrl+v"),
			key.WithHelp("Ctrl+V", "Paste image"),
		),
		LogViewer: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("Ctrl+L", "Log viewer"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Newline, k.CancelAgent, k.LogViewer, k.ToggleHelp, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Send, k.Newline, k.CancelAgent, k.NewSession},
		{k.OpenWorkspace},
		{k.FocusInput, k.NextPanel, k.PrevPanel},
		{k.ToggleHelp, k.Quit, k.PasteImage, k.ScrollUp},
		{k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.LogViewer},
	}
}
