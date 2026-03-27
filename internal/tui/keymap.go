package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap centralizes the high-value keyboard interactions for the TUI.
type KeyMap struct {
	Submit         key.Binding
	InsertNewline  key.Binding
	ClearComposer  key.Binding
	Paste          key.Binding
	ToggleSidebar  key.Binding
	SearchSessions key.Binding
	NewSession     key.Binding
	SwitchProvider key.Binding
	BrowseMode     key.Binding
	ComposeMode    key.Binding
	SidebarUp      key.Binding
	SidebarDown    key.Binding
	PrevCodeBlock  key.Binding
	NextCodeBlock  key.Binding
	CopyCodeBlock  key.Binding
	CopyMessage    key.Binding
	JumpLatest     key.Binding
	GoTop          key.Binding
	GoBottom       key.Binding
	PageScroll     key.Binding
	LineScroll     key.Binding
	ToggleHelp     key.Binding
	Quit           key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		InsertNewline: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("ctrl+j", "newline"),
		),
		ClearComposer: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "clear"),
		),
		Paste: key.NewBinding(
			key.WithKeys("ctrl+v"),
			key.WithHelp("ctrl+v", "paste"),
		),
		ToggleSidebar: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("ctrl+b", "sessions"),
		),
		SearchSessions: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search sessions"),
		),
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "new session"),
		),
		SwitchProvider: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "switch provider"),
		),
		BrowseMode: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "browse"),
		),
		ComposeMode: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "compose"),
		),
		SidebarUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("up/down", "move"),
		),
		SidebarDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("up/down", "move"),
		),
		PrevCodeBlock: key.NewBinding(
			key.WithKeys("["),
			key.WithHelp("[", "prev code"),
		),
		NextCodeBlock: key.NewBinding(
			key.WithKeys("]"),
			key.WithHelp("]", "next code"),
		),
		CopyCodeBlock: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy code"),
		),
		CopyMessage: key.NewBinding(
			key.WithKeys("Y"),
			key.WithHelp("Y", "copy message"),
		),
		JumpLatest: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "latest"),
		),
		GoTop: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g/home", "top"),
		),
		GoBottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G/end", "bottom"),
		),
		PageScroll: key.NewBinding(
			key.WithKeys("pgup", "pgdown"),
			key.WithHelp("pgup/pgdn", "page"),
		),
		LineScroll: key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("up/down", "line"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "q"),
			key.WithHelp("ctrl+c", "quit"),
		),
	}
}

// ShortHelp returns the compact footer help.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Submit,
		k.ToggleSidebar,
		k.CopyCodeBlock,
		k.Quit,
	}
}

// FullHelp returns the expanded help overlay bindings.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.Submit,
			k.InsertNewline,
			k.ClearComposer,
			k.Paste,
			k.ComposeMode,
			k.BrowseMode,
		},
		{
			k.ToggleSidebar,
			k.SearchSessions,
			k.NewSession,
			k.SwitchProvider,
		},
		{
			k.LineScroll,
			k.PageScroll,
			k.GoTop,
			k.GoBottom,
			k.JumpLatest,
		},
		{
			k.PrevCodeBlock,
			k.NextCodeBlock,
			k.CopyCodeBlock,
			k.CopyMessage,
		},
		{
			k.ToggleHelp,
			k.Quit,
		},
	}
}
