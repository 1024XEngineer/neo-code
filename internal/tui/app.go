package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	agentruntime "neo-code/internal/runtime"
)

type App struct {
	state            UIState
	configManager    *config.Manager
	providerSvc      ProviderController
	runtime          agentruntime.Runtime
	keys             keyMap
	help             help.Model
	spinner          spinner.Model
	sessions         list.Model
	providerPicker   list.Model
	modelPicker      list.Model
	transcript       viewport.Model
	input            textarea.Model
	markdownRenderer markdownContentRenderer
	codeCopyBlocks   map[int]string
	pendingCopyID    int
	nowFn            func() time.Time
	lastInputEditAt  time.Time
	lastPasteLikeAt  time.Time
	inputBurstStart  time.Time
	inputBurstCount  int
	pasteMode        bool
	activeMessages   []provider.Message
	activities       []activityEntry
	fileCandidates   []string
	modelRefreshID   string
	focus            panel
	width            int
	height           int
	styles           styles
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
	markdownRenderer, err := newMarkdownRenderer()
	if err != nil {
		return App{}, err
	}
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
	input.CharLimit = 0
	input.ShowLineNumbers = false
	input.SetPromptFunc(composerPromptWidth, func(line int) string {
		return "> "
	})
	input.FocusedStyle.Base = lipgloss.NewStyle()
	input.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true)
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSubtle))
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.CursorLineNumber = lipgloss.NewStyle()
	input.BlurredStyle.Base = lipgloss.NewStyle()
	input.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true)
	input.BlurredStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorText))
	input.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorSubtle))
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.CursorLineNumber = lipgloss.NewStyle()
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	input.SetHeight(composerMinHeight)
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
		configManager:    configManager,
		providerSvc:      providerSvc,
		runtime:          runtime,
		keys:             keys,
		help:             h,
		spinner:          spin,
		sessions:         sessionList,
		providerPicker:   newProviderPicker(nil),
		modelPicker:      newModelPicker(nil),
		transcript:       viewport.New(0, 0),
		input:            input,
		markdownRenderer: markdownRenderer,
		codeCopyBlocks:   make(map[int]string),
		nowFn:            time.Now,
		focus:            panelInput,
		width:            128,
		height:           40,
		styles:           uiStyles,
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
	app.modelRefreshID = cfg.SelectedProvider
	if err := app.refreshFileCandidates(); err != nil {
		return App{}, err
	}
	app.resizeComponents()
	return app, nil
}

func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		ListenForRuntimeEvent(a.runtime.Events()),
		textarea.Blink,
		a.spinner.Tick,
	}
	if cmd := runModelCatalogRefresh(a.providerSvc, a.modelRefreshID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}
