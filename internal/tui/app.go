package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	agentruntime "github.com/dust/neo-code/internal/runtime"
)

type App struct {
	state          UIState
	configManager  *config.Manager
	runtime        agentruntime.Runtime
	keys           keyMap
	help           help.Model
	spinner        spinner.Model
	sessions       list.Model
	modelPicker    list.Model
	transcript     viewport.Model
	input          textinput.Model
	activeMessages []provider.Message
	focus          panel
	width          int
	height         int
	styles         styles
}

func New(cfg *config.Config, configManager *config.Manager, runtime agentruntime.Runtime) (App, error) {
	if cfg == nil {
		defaultCfg := config.Default()
		cfg = defaultCfg
	}
	if configManager == nil {
		return App{}, fmt.Errorf("tui: config manager is nil")
	}

	uiStyles := newStyles()
	keys := newKeyMap()
	delegate := sessionDelegate{styles: uiStyles}
	sessionList := list.New([]list.Item{}, delegate, 0, 0)
	sessionList.Title = ""
	sessionList.SetShowTitle(false)
	sessionList.SetShowHelp(false)
	sessionList.SetShowStatusBar(false)
	sessionList.SetShowFilter(false)
	sessionList.SetShowPagination(false)
	sessionList.SetFilteringEnabled(true)
	sessionList.DisableQuitKeybindings()
	sessionList.FilterInput.Prompt = "筛选："
	sessionList.FilterInput.Placeholder = "输入关键词…"

	input := textinput.New()
	input.Placeholder = "Ask NeoCode to inspect, edit, or build. Type / to browse commands."
	input.Prompt = ""
	input.CharLimit = 24000
	input.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSubtle))
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary))

	h := help.New()
	h.ShowAll = false

	app := App{
		state: UIState{
			StatusText:         statusReady,
			CurrentProvider:    cfg.SelectedProvider,
			CurrentModel:       cfg.CurrentModel,
			CurrentWorkdir:     cfg.Workdir,
			ActiveSessionTitle: draftSessionTitle,
			Focus:              panelInput,
		},
		configManager: configManager,
		runtime:       runtime,
		keys:          keys,
		help:          h,
		spinner:       spin,
		sessions:      sessionList,
		modelPicker:   newModelPicker(),
		transcript:    viewport.New(0, 0),
		input:         input,
		focus:         panelInput,
		width:         128,
		height:        40,
		styles:        uiStyles,
	}

	if err := app.refreshSessions(); err != nil {
		return App{}, err
	}
	if len(app.state.Sessions) > 0 {
		app.state.ActiveSessionID = app.state.Sessions[0].ID
		if err := app.refreshMessages(); err != nil {
			return App{}, err
		}
	}
	app.syncActiveSessionTitle()
	app.syncConfigState(configManager.Get())
	app.selectCurrentModel(cfg.CurrentModel)
	app.resizeComponents()
	return app, nil
}

func (a App) Init() tea.Cmd {
	return tea.Batch(ListenForRuntimeEvent(a.runtime.Events()), textinput.Blink, a.spinner.Tick)
}
