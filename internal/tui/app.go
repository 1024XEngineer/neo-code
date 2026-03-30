package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	agentruntime "neo-code/internal/runtime"
)

type App struct {
	state          UIState
	configManager  *config.Manager
	providerSvc    ProviderController
	runtime        agentruntime.Runtime
	keys           keyMap
	help           help.Model
	spinner        spinner.Model
	sessions       list.Model
	providerPicker list.Model
	modelPicker    list.Model
	apiKeyInput    textinput.Model
	transcript     viewport.Model
	input          textarea.Model
	activeMessages []provider.Message
	codeBlocks     []codeBlockTarget
	transcriptRect rect
	focus          panel
	width          int
	height         int
	styles         styles
}

func New(cfg *config.Config, configManager *config.Manager, runtime agentruntime.Runtime, providerSvc ProviderController) (App, error) {
	if configManager == nil {
		return App{}, fmt.Errorf("tui: config manager is nil")
	}
	if providerSvc == nil {
		return App{}, fmt.Errorf("tui: provider service is nil")
	}
	if cfg == nil {
		snapshot := configManager.Get()
		cfg = &snapshot
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
	sessionList.FilterInput.Prompt = "Filter: "
	sessionList.FilterInput.Placeholder = "Type to search sessions"

	input := textarea.New()
	input.Placeholder = "Ask NeoCode to inspect, edit, or build. Type / to browse commands."
	input.Prompt = "> "
	input.CharLimit = 24000
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.SetHeight(1)
	input.FocusedStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSubtle))
	input.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true)
	input.BlurredStyle = input.FocusedStyle
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary))

	apiKeyInput := textinput.New()
	apiKeyInput.Prompt = ""
	apiKeyInput.Placeholder = "Enter API key env name"
	apiKeyInput.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	apiKeyInput.CharLimit = 256

	h := help.New()
	h.ShowAll = false

	app := App{
		state: UIState{
			StatusText:         statusReady,
			CurrentProvider:    cfg.SelectedProvider,
			CurrentModel:       cfg.CurrentModel,
			APIKeyEnvOverride:  cfg.APIKeyEnvOverride,
			CurrentWorkdir:     cfg.Workdir,
			ActiveSessionTitle: draftSessionTitle,
			Focus:              panelInput,
		},
		configManager:  configManager,
		providerSvc:    providerSvc,
		runtime:        runtime,
		keys:           keys,
		help:           h,
		spinner:        spin,
		sessions:       sessionList,
		providerPicker: newProviderPicker(nil),
		modelPicker:    newModelPicker(nil),
		apiKeyInput:    apiKeyInput,
		transcript:     viewport.New(0, 0),
		input:          input,
		focus:          panelInput,
		width:          128,
		height:         40,
		styles:         uiStyles,
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
	if err := app.refreshProviderPicker(); err != nil {
		return App{}, err
	}
	if err := app.refreshModelPicker(); err != nil {
		return App{}, err
	}
	app.selectCurrentProvider(cfg.SelectedProvider)
	app.selectCurrentModel(cfg.CurrentModel)
	app.resizeComponents()
	return app, nil
}

func (a App) Init() tea.Cmd {
	return tea.Batch(ListenForRuntimeEvent(a.runtime.Events()), textarea.Blink, a.spinner.Tick)
}
