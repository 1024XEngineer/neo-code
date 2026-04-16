package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/memo"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuistate "neo-code/internal/tui/state"
)

type panel = tuistate.Panel

const (
	panelTranscript panel = tuistate.PanelTranscript
	panelActivity   panel = tuistate.PanelActivity
	panelInput      panel = tuistate.PanelInput
)

type pickerMode = tuistate.PickerMode

const (
	pickerNone        pickerMode = tuistate.PickerNone
	pickerProvider    pickerMode = tuistate.PickerProvider
	pickerModel       pickerMode = tuistate.PickerModel
	pickerSession     pickerMode = tuistate.PickerSession
	pickerFile        pickerMode = tuistate.PickerFile
	pickerHelp        pickerMode = tuistate.PickerHelp
	pickerProviderAdd pickerMode = tuistate.PickerProviderAdd
)

type RuntimeMsg = tuistate.RuntimeMsg
type RuntimeClosedMsg = tuistate.RuntimeClosedMsg
type runFinishedMsg = tuistate.RunFinishedMsg
type modelCatalogRefreshMsg = tuistate.ModelCatalogRefreshMsg
type compactFinishedMsg = tuistate.CompactFinishedMsg
type localCommandResultMsg = tuistate.LocalCommandResultMsg
type workspaceCommandResultMsg = tuistate.WorkspaceCommandResultMsg
type permissionResolutionFinishedMsg = tuistate.PermissionResolutionFinishedMsg

type ProviderController interface {
	ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error)
	SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error)
	ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
}

// appServices 聚合 App 需要的服务依赖，避免与渲染状态混在同一层级。
type appServices struct {
	configManager *config.Manager
	providerSvc   ProviderController
	runtime       agentruntime.Runtime
	memoSvc       *memo.Service
}

// appComponents 聚合 Bubble Tea 组件与渲染器。
type appComponents struct {
	keys             keyMap
	help             help.Model
	spinner          spinner.Model
	commandMenu      list.Model
	commandMenuMeta  tuistate.CommandMenuMeta
	providerPicker   list.Model
	modelPicker      list.Model
	sessionPicker    list.Model
	helpPicker       list.Model
	fileBrowser      filepicker.Model
	progress         progress.Model
	transcript       viewport.Model
	activity         viewport.Model
	input            textarea.Model
	markdownRenderer markdownContentRenderer
}

// appRuntimeState 聚合运行期易变字段，降低 App 顶层字段密度。
type appRuntimeState struct {
	codeCopyBlocks           map[int]string
	pendingCopyID            int
	deferredEventCmd         tea.Cmd
	nowFn                    func() time.Time
	lastInputEditAt          time.Time
	lastPasteLikeAt          time.Time
	inputBurstStart          time.Time
	inputBurstCount          int
	pasteMode                bool
	activeMessages           []providertypes.Message
	activities               []tuistate.ActivityEntry
	fileCandidates           []string
	modelRefreshID           string
	focus                    panel
	runProgressValue         float64
	runProgressKnown         bool
	runProgressLabel         string
	pendingPermission        *permissionPromptState
	pendingImageAttachments  []pendingImageAttachment
	currentModelCapabilities modelCapabilityState
	providerAddForm          *providerAddFormState
}

type pendingImageAttachment struct {
	Path     string
	MimeType string
	Size     int64
	Name     string
}

type modelCapabilityState struct {
	supportsImageInput bool
	checked            bool
}

// providerAddFormState 保存添加新 provider 表单的状态。
type providerAddFormState struct {
	Step           int // 当前聚焦字段在“当前 driver 可见字段列表”中的索引
	Name           string
	Driver         string
	BaseURL        string
	APIStyle       string
	DeploymentMode string
	APIVersion     string
	APIKey         string
	Error          string
	ErrorIsHard    bool
	Submitting     bool
	Drivers        []string // 可选的 Driver 列表
}

type App struct {
	state tuistate.UIState
	appServices
	appComponents
	appRuntimeState
	width  int
	height int
	styles styles
}

func New(cfg *config.Config, configManager *config.Manager, runtime agentruntime.Runtime, providerSvc ProviderController) (App, error) {
	return NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   configManager,
		Runtime:         runtime,
		ProviderService: providerSvc,
	})
}

// NewWithMemo 创建带 memo 服务的 TUI App。
func NewWithMemo(cfg *config.Config, configManager *config.Manager, runtime agentruntime.Runtime, providerSvc ProviderController, memoSvc *memo.Service) (App, error) {
	return NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   configManager,
		Runtime:         runtime,
		ProviderService: providerSvc,
		MemoSvc:         memoSvc,
	})
}

// NewWithBootstrap 通过 bootstrap 层完成依赖装配，再构建可运行的 TUI App。
func NewWithBootstrap(options tuibootstrap.Options) (App, error) {
	container, err := tuibootstrap.Build(options)
	if err != nil {
		return App{}, err
	}
	return newApp(container)
}

// newApp 根据 bootstrap 装配结果初始化 App 状态与组件。
func newApp(container tuibootstrap.Container) (App, error) {
	cfg := container.Config
	configManager := container.ConfigManager
	runtime := container.Runtime
	providerSvc := container.ProviderService

	uiStyles := newStyles()
	markdownRenderer, err := newMarkdownRenderer()
	if err != nil {
		return App{}, err
	}
	keys := newKeyMap()

	input := textarea.New()
	input.Placeholder = "Ask NeoCode to inspect, edit, or build. Type / to browse commands."
	input.CharLimit = 0
	input.ShowLineNumbers = false
	input.SetPromptFunc(composerPromptWidth, func(line int) string {
		return "> "
	})
	input.FocusedStyle.Base = lipgloss.NewStyle()
	input.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(lightText)).Bold(true)
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(lightText))
	input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(oliveGray))
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.CursorLineNumber = lipgloss.NewStyle()
	input.BlurredStyle.Base = lipgloss.NewStyle()
	input.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(lightText)).Bold(true)
	input.BlurredStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(lightText))
	input.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(oliveGray))
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.CursorLineNumber = lipgloss.NewStyle()
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(purpleAccent))
	input.SetHeight(composerMinHeight)
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(purpleAccent))

	h := help.New()
	h.ShowAll = false

	commandMenu := newCommandMenuModel(uiStyles)

	fileBrowser := filepicker.New()
	fileBrowser.SetHeight(10)
	fileBrowser.AutoHeight = false
	fileBrowser.ShowPermissions = false
	fileBrowser.ShowSize = false
	fileBrowser.FileAllowed = true
	fileBrowser.DirAllowed = true
	fileBrowser.CurrentDirectory = cfg.Workdir

	progressBar := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
	progressBar.Width = 22

	app := App{
		state: tuistate.UIState{
			StatusText:      statusReady,
			CurrentProvider: cfg.SelectedProvider,
			CurrentModel:    cfg.CurrentModel,
			// Workdir 在启动阶段由 config 校验过，此处直接使用。
			CurrentWorkdir:     cfg.Workdir,
			ActiveSessionTitle: draftSessionTitle,
			Focus:              panelInput,
		},
		appServices: appServices{
			configManager: configManager,
			providerSvc:   providerSvc,
			runtime:       runtime,
			memoSvc:       container.MemoSvc,
		},
		appComponents: appComponents{
			keys:             keys,
			help:             h,
			spinner:          spin,
			commandMenu:      commandMenu,
			providerPicker:   newSelectionPickerItems(nil),
			modelPicker:      newSelectionPickerItems(nil),
			sessionPicker:    newSelectionPickerItems(nil),
			helpPicker:       newHelpPickerItems(nil),
			fileBrowser:      fileBrowser,
			progress:         progressBar,
			transcript:       viewport.New(0, 0),
			activity:         viewport.New(0, 0),
			input:            input,
			markdownRenderer: markdownRenderer,
		},
		appRuntimeState: appRuntimeState{
			codeCopyBlocks: make(map[int]string),
			nowFn:          time.Now,
			focus:          panelInput,
		},
		width:  128,
		height: 40,
		styles: uiStyles,
	}

	app.syncActiveSessionTitle()
	app.syncConfigState(configManager.Get())
	if err := app.refreshProviderPicker(); err != nil {
		return App{}, err
	}
	if err := app.refreshModelPicker(); err != nil {
		return App{}, err
	}
	app.refreshHelpPicker()
	app.selectCurrentProvider(cfg.SelectedProvider)
	app.selectCurrentModel(cfg.CurrentModel)
	app.modelRefreshID = cfg.SelectedProvider
	if err := app.refreshFileCandidates(); err != nil {
		return App{}, err
	}
	app.applyComponentLayout(true)
	app.refreshCommandMenu()
	app.rebuildActivity()
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
