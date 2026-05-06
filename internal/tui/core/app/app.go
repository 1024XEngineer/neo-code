package tui

import (
	"context"
	"time"

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
	agentsession "neo-code/internal/session"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiservices "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

type logEntry struct {
	Timestamp time.Time
	Level     string
	Source    string
	Message   string
}

type panel = tuistate.Panel

const (
	panelTranscript panel = tuistate.PanelTranscript
	panelActivity   panel = tuistate.PanelActivity
	panelTodo       panel = tuistate.PanelTodo
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
	pickerModelScope  pickerMode = tuistate.PickerModelScopeGuide
)

type RuntimeMsg = tuistate.RuntimeMsg
type RuntimeClosedMsg = tuistate.RuntimeClosedMsg
type runFinishedMsg = tuistate.RunFinishedMsg
type modelCatalogRefreshMsg = tuistate.ModelCatalogRefreshMsg
type compactFinishedMsg = tuistate.CompactFinishedMsg
type localCommandResultMsg = tuistate.LocalCommandResultMsg
type permissionResolutionFinishedMsg = tuistate.PermissionResolutionFinishedMsg

type ProviderController interface {
	ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error)
	SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error)
	ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
	CreateCustomProvider(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
}

// appServices 聚合 App 需要的服务依赖，避免与渲染状态混在同一层级。
type appServices struct {
	configManager *config.Manager
	providerSvc   ProviderController
	runtime       tuiservices.Runtime
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
	progress         progress.Model
	transcript       viewport.Model
	activity         viewport.Model
	todo             viewport.Model
	input            textarea.Model
	markdownRenderer markdownContentRenderer
}

// appRuntimeState 聚合运行期易变字段，降低 App 顶层字段密度。
type appRuntimeState struct {
	deferredEventCmd          tea.Cmd
	deferredLogPersistCmd     tea.Cmd
	nowFn                     func() time.Time
	lastInputEditAt           time.Time
	lastPasteLikeAt           time.Time
	pasteSessionStartedAt     time.Time
	pasteSessionUntil         time.Time
	pendingCtrlVPasteEcho     string
	pendingCtrlVEchoUntil     time.Time
	deferredPastedTextLoadCmd tea.Cmd
	loadingPastedText         bool
	pendingSendAfterPasteLoad bool
	skipNextSendPasteLoadWait bool
	pasteTxnActive            bool
	pasteTxnBuffer            string
	pasteTxnVersion           int
	pasteTxnTokenInjected     bool
	pasteTxnInjectedToken     string
	inputBurstStart           time.Time
	inputBurstCount           int
	pasteMode                 bool
	pasteSessionBase          string
	lastSummarizedPasteText   string
	lastSummarizedPasteAt     time.Time
	lastSummarizedPasteToken  string
	lastPastedContent         string
	lastPastedContentAt       time.Time
	activeMessages            []providertypes.Message
	activities                []tuistate.ActivityEntry
	todoItems                 []todoViewItem
	todoFilter                todoFilter
	todoSelectedIndex         int
	todoPanelVisible          bool
	todoCollapsed             bool
	fileCandidates            []string
	modelRefreshID            string
	focus                     panel
	runProgressValue          float64
	runProgressKnown          bool
	runProgressLabel          string
	lastUserMessageRunID      string
	pendingPermission         *permissionPromptState
	queuedIntervention        *queuedInterventionInput
	pendingAutoPermission     *autoPermissionApprovalState
	pendingFullAccessPrompt   *fullAccessPromptState
	startupWakeSubmitInput    *startupWakeSubmitInput
	fullAccessModeEnabled     bool
	pendingImageAttachments   []pendingImageAttachment
	pendingTextPastes         []pendingTextPaste
	providerAddForm           *providerAddFormState
	modelScopeGuide           *modelScopeGuideState
	layoutCached              bool
	cachedWidth               int
	cachedHeight              int
	viewDirty                 bool
	logViewerVisible          bool
	logViewerOffset           int
	logViewerPrevStatus       string
	logEntries                []logEntry
	logPersistDirty           bool
	logPersistVersion         int
	transcriptContent         string
	transcriptScrollbarDrag   bool
	startupScreenLocked       bool
	suppressAssistantForRun   string

	textSelection struct {
		active    bool
		dragging  bool
		startLine int
		startCol  int
		endLine   int
		endCol    int
	}

	footerErrorLast    string
	footerErrorText    string
	footerErrorUntil   time.Time
	deferredFooterTick tea.Cmd
}

type pendingImageAttachment struct {
	Path     string
	MimeType string
	Size     int64
	Name     string
}

type pendingTextPaste struct {
	Token     string
	FilePath  string
	LineCount int
	Loaded    bool
}

type queuedInterventionInput struct {
	Text   string
	Images []tuiservices.UserImageInput
}

type startupWakeSubmitInput struct {
	Text    string
	Workdir string
}

// providerAddFormState 保存添加新 provider 表单的状态。
type providerAddFormState struct {
	Stage                 providerAddFormStage
	Step                  int
	Name                  string
	Driver                string
	ModelSource           string
	ChatAPIMode           string
	BaseURL               string
	ChatEndpointPath      string
	DiscoveryEndpointPath string
	ManualModelsJSON      string
	APIKeyEnv             string
	APIKey                string
	Error                 string
	ErrorIsHard           bool
	Submitting            bool
	Drivers               []string
	ModelSources          []string
	ChatAPIModes          []string
}

type providerAddFormStage int

const (
	providerAddFormStageFields providerAddFormStage = iota
	providerAddFormStageManualModels
)

// modelScopeGuideStep 定义 ModelScope 半引导流程中的阶段。
type modelScopeGuideStep int

const (
	modelScopeGuideStepGuide modelScopeGuideStep = iota
	modelScopeGuideStepLogin
	modelScopeGuideStepToken
	modelScopeGuideStepPasteToken
)

// modelScopeGuideState 保存 ModelScope 半引导流程的运行时状态。
type modelScopeGuideState struct {
	ProviderID string
	APIKeyEnv  string
	GuidePath  string
	Step       modelScopeGuideStep
	Token      string
	Submitting bool
	Error      string
	Notice     string
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

func New(cfg *config.Config, configManager *config.Manager, runtime tuiservices.Runtime, providerSvc ProviderController) (App, error) {
	return NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   configManager,
		Runtime:         runtime,
		ProviderService: providerSvc,
	})
}

// NewWithMemo 创建带 memo 服务的 TUI App。
func NewWithMemo(cfg *config.Config, configManager *config.Manager, runtime tuiservices.Runtime, providerSvc ProviderController, memoSvc *memo.Service) (App, error) {
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
	h.ShortSeparator = " | "
	h.Styles.ShortKey = lipgloss.NewStyle().
		Foreground(lipgloss.Color(purpleAccent)).
		Bold(true)
	h.Styles.ShortDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText2))
	h.Styles.ShortSeparator = lipgloss.NewStyle().
		Foreground(lipgloss.Color(midGray))
	h.Styles.FullKey = h.Styles.ShortKey.Copy()
	h.Styles.FullDesc = h.Styles.ShortDesc.Copy()
	h.Styles.FullSeparator = h.Styles.ShortSeparator.Copy()
	h.Styles.Ellipsis = lipgloss.NewStyle().
		Foreground(lipgloss.Color(oliveGray))

	commandMenu := newCommandMenuModel(uiStyles)

	progressBar := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
	progressBar.Width = 22

	app := App{
		state: tuistate.UIState{
			StatusText:       statusReady,
			CurrentProvider:  cfg.SelectedProvider,
			CurrentModel:     cfg.CurrentModel,
			CurrentAgentMode: string(agentsession.AgentModeBuild),
			// CurrentWorkdir 初始化为启动配置中的工作目录，避免启动阶段丢失目录上下文。
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
			progress:         progressBar,
			transcript:       viewport.New(0, 0),
			activity:         viewport.New(0, 0),
			todo:             viewport.New(0, 0),
			input:            input,
			markdownRenderer: markdownRenderer,
		},
		appRuntimeState: appRuntimeState{
			nowFn:        time.Now,
			focus:        panelInput,
			todoFilter:   todoFilterAll,
			layoutCached: true,
			cachedWidth:  128,
			cachedHeight: 40,
			// 初始进入草稿态时锁定启动页，直到发送或切换 session 才退出。
			startupScreenLocked: true,
		},
		width:  128,
		height: 40,
		styles: uiStyles,
	}

	app.setCurrentAgentMode(app.state.CurrentAgentMode)
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

type tickMsg time.Time

func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		ListenForRuntimeEvent(a.runtime.Events()),
		textarea.Blink,
		a.spinner.Tick,
		appTickCmd(),
	}
	if a.startupWakeSubmitInput != nil {
		cmds = append(cmds, emitStartupWakeSubmitCmd(*a.startupWakeSubmitInput))
	}
	if cmd := runModelCatalogRefresh(a.providerSvc, a.modelRefreshID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func appTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
