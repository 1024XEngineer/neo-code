package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
)

const (
	slashPrefix           = "/"
	slashCommandHelp      = "/help"
	slashCommandExit      = "/exit"
	slashCommandClear     = "/clear"
	slashCommandCompact   = "/compact"
	slashCommandStatus    = "/status"
	slashCommandProvider  = "/provider"
	slashCommandModelPick = "/model"
	slashCommandCWD       = "/cwd"

	slashUsageHelp     = "/help"
	slashUsageExit     = "/exit"
	slashUsageClear    = "/clear"
	slashUsageCompact  = "/compact"
	slashUsageStatus   = "/status"
	slashUsageProvider = "/provider"
	slashUsageModel    = "/model"
	slashUsageWorkdir  = "/cwd"

	commandMenuTitle       = "Commands"
	providerPickerTitle    = "Select Provider"
	providerPickerSubtitle = "Up/Down choose, Enter confirm, Esc cancel"
	modelPickerTitle       = "Select Model"
	modelPickerSubtitle    = "Up/Down choose, Enter confirm, Esc cancel"

	sidebarTitle      = "Sessions"
	sidebarFilterHint = "Type / to search"
	sidebarOpenHint   = "Enter to open"
	activityTitle     = "Activity"
	activitySubtitle  = "Latest execution events"

	draftSessionTitle     = "Draft"
	emptyConversationText = "No conversation yet.\nAsk NeoCode to inspect or change code, or type /help to browse local commands."
	emptyMessageText      = "(empty)"

	statusReady           = "Ready"
	statusRuntimeClosed   = "Runtime closed"
	statusThinking        = "Thinking"
	statusCanceling       = "Canceling"
	statusCanceled        = "Canceled"
	statusRunningTool     = "Running tool"
	statusToolFinished    = "Tool finished"
	statusToolError       = "Tool error"
	statusError           = "Error"
	statusDraft           = "New draft"
	statusRunning         = "Running"
	statusApplyingCommand = "Applying local command"
	statusRunningCommand  = "Running command"
	statusCommandDone     = "Command finished"
	statusCompacting      = "Compacting context"
	statusChooseProvider  = "Choose a provider"
	statusChooseModel     = "Choose a model"

	focusLabelSessions   = "Sessions"
	focusLabelTranscript = "Transcript"
	focusLabelActivity   = "Activity"
	focusLabelComposer   = "Composer"

	activityPreviewEntries = 3
	maxActivityEntries     = 64

	messageTagUser  = "[ YOU ]"
	messageTagAgent = "[ NEO ]"
	messageTagTool  = "[ TOOL ]"
	copyCodeButton  = "[Copy code #%d]"

	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
	roleEvent     = "event"
	roleError     = "error"
	roleSystem    = "system"

	statusCodeCopied    = "Copied code block #%d"
	statusCodeCopyError = "Failed to copy code block"
)

type slashCommand struct {
	Usage       string
	Description string
}

type commandSuggestion struct {
	Command slashCommand
	Match   bool
}

type statusSnapshot struct {
	ActiveSessionID    string
	ActiveSessionTitle string
	IsAgentRunning     bool
	IsCompacting       bool
	CurrentProvider    string
	CurrentModel       string
	CurrentWorkdir     string
	CurrentTool        string
	ExecutionError     string
	FocusLabel         string
	PickerLabel        string
	MessageCount       int
}

var builtinSlashCommands = []slashCommand{
	{Usage: slashUsageHelp, Description: "Show slash command help"},
	{Usage: slashUsageClear, Description: "Clear the current draft transcript"},
	{Usage: slashUsageCompact, Description: "Compact the current session context"},
	{Usage: slashUsageStatus, Description: "Show current session and agent status"},
	{Usage: slashUsageWorkdir, Description: "Show or set current session workspace root (/cwd [path])"},
	{Usage: slashUsageProvider, Description: "Open the interactive provider picker"},
	{Usage: slashUsageModel, Description: "Open the interactive model picker"},
	{Usage: slashUsageExit, Description: "Exit NeoCode"},
}

func newSelectionPicker(items []list.Item) list.Model {
	delegate := list.NewDefaultDelegate()
	picker := list.New(items, delegate, 0, 0)
	picker.Title = ""
	picker.SetShowHelp(false)
	picker.SetShowStatusBar(false)
	picker.SetFilteringEnabled(false)
	picker.DisableQuitKeybindings()
	return picker
}

func newProviderPicker(items []config.ProviderCatalogItem) list.Model {
	listItems := make([]list.Item, 0, len(items))
	for _, item := range items {
		listItems = append(listItems, providerItem{
			id:          item.ID,
			name:        item.Name,
			description: item.Description,
		})
	}
	return newSelectionPicker(listItems)
}

func newModelPicker(models []config.ModelDescriptor) list.Model {
	items := make([]list.Item, 0, len(models))
	for _, option := range models {
		items = append(items, modelItem{
			id:          option.ID,
			name:        option.Name,
			description: option.Description,
		})
	}
	return newSelectionPicker(items)
}

func replacePickerItems(current list.Model, next list.Model) list.Model {
	next.SetSize(current.Width(), current.Height())
	return next
}

func (a *App) refreshProviderPicker() error {
	items, err := a.providerSvc.ListProviders(context.Background())
	if err != nil {
		return err
	}

	a.providerPicker = replacePickerItems(a.providerPicker, newProviderPicker(items))
	a.selectCurrentProvider(a.state.CurrentProvider)
	return nil
}

func (a *App) refreshModelPicker() error {
	models, err := a.providerSvc.ListModelsSnapshot(context.Background())
	if err != nil {
		return err
	}

	a.modelPicker = replacePickerItems(a.modelPicker, newModelPicker(models))
	a.selectCurrentModel(a.state.CurrentModel)
	return nil
}

func (a *App) openProviderPicker() {
	a.state.ActivePicker = pickerProvider
	a.state.StatusText = statusChooseProvider
	a.input.Blur()
	a.selectCurrentProvider(a.state.CurrentProvider)
}

func (a *App) openModelPicker() {
	a.state.ActivePicker = pickerModel
	a.state.StatusText = statusChooseModel
	a.input.Blur()
	a.selectCurrentModel(a.state.CurrentModel)
}

func (a *App) closePicker() {
	a.state.ActivePicker = pickerNone
	a.focus = panelInput
	a.applyFocus()
}

func (a *App) selectCurrentProvider(providerID string) {
	items := a.providerPicker.Items()
	for idx, item := range items {
		candidate, ok := item.(providerItem)
		if ok && strings.EqualFold(candidate.id, providerID) {
			a.providerPicker.Select(idx)
			return
		}
	}
	if len(items) > 0 {
		a.providerPicker.Select(0)
	}
}

func (a *App) selectCurrentModel(modelID string) {
	items := a.modelPicker.Items()
	for idx, item := range items {
		candidate, ok := item.(modelItem)
		if ok && strings.EqualFold(candidate.id, modelID) {
			a.modelPicker.Select(idx)
			return
		}
	}
	if len(items) > 0 {
		a.modelPicker.Select(0)
	}
}

func (a App) matchingSlashCommands(input string) []commandSuggestion {
	if !strings.HasPrefix(input, slashPrefix) {
		return nil
	}

	query := strings.ToLower(strings.TrimSpace(input))
	if isCompleteSlashCommand(query) {
		return nil
	}
	out := make([]commandSuggestion, 0, len(builtinSlashCommands))
	for _, command := range builtinSlashCommands {
		normalized := strings.ToLower(command.Usage)
		match := query == slashPrefix || strings.HasPrefix(normalized, query)
		if query == slashPrefix || match || strings.Contains(normalized, query) {
			out = append(out, commandSuggestion{Command: command, Match: match})
		}
	}
	return out
}

func isCompleteSlashCommand(input string) bool {
	for _, command := range builtinSlashCommands {
		if strings.EqualFold(strings.TrimSpace(command.Usage), strings.TrimSpace(input)) {
			return true
		}
	}
	return false
}

func runProviderSelection(providerSvc ProviderController, providerName string) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SelectProvider(context.Background(), providerName)
		if err != nil {
			return localCommandResultMsg{err: err}
		}
		return localCommandResultMsg{
			notice:          fmt.Sprintf("[System] Current provider switched to %s.", selection.ProviderID),
			providerChanged: true,
		}
	}
}

func runModelSelection(providerSvc ProviderController, modelID string) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SetCurrentModel(context.Background(), modelID)
		if err != nil {
			return localCommandResultMsg{err: err}
		}
		return localCommandResultMsg{
			notice:       fmt.Sprintf("[System] Current model switched to %s.", selection.ModelID),
			modelChanged: true,
		}
	}
}

func runLocalCommand(configManager *config.Manager, providerSvc ProviderController, snapshot statusSnapshot, raw string) tea.Cmd {
	return func() tea.Msg {
		notice, err := executeLocalCommand(context.Background(), configManager, providerSvc, snapshot, raw)
		result := localCommandResultMsg{notice: notice, err: err}
		if err == nil {
			cfg := configManager.Get()
			result.providerChanged = !strings.EqualFold(snapshot.CurrentProvider, cfg.SelectedProvider)
			result.modelChanged = !strings.EqualFold(snapshot.CurrentModel, cfg.CurrentModel)
		}
		return result
	}
}

func runModelCatalogRefresh(providerSvc ProviderController, providerID string) tea.Cmd {
	providerID = strings.TrimSpace(providerID)
	if providerSvc == nil || providerID == "" {
		return nil
	}

	return func() tea.Msg {
		models, err := providerSvc.ListModels(context.Background())
		return modelCatalogRefreshMsg{
			providerID: providerID,
			models:     models,
			err:        err,
		}
	}
}

func executeLocalCommand(ctx context.Context, configManager *config.Manager, providerSvc ProviderController, snapshot statusSnapshot, raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty command")
	}

	switch strings.ToLower(fields[0]) {
	case slashCommandHelp:
		return slashHelpText(), nil
	case slashCommandStatus:
		return executeStatusCommand(snapshot), nil
	case slashCommandProvider:
		return executeProviderCommand(ctx, providerSvc, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), slashCommandProvider)))
	default:
		return "", fmt.Errorf("unknown command %q", fields[0])
	}
}

func executeStatusCommand(snapshot statusSnapshot) string {
	sessionID := snapshot.ActiveSessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "<draft>"
	}
	sessionTitle := snapshot.ActiveSessionTitle
	if strings.TrimSpace(sessionTitle) == "" {
		sessionTitle = draftSessionTitle
	}
	running := "no"
	if snapshot.IsAgentRunning || snapshot.IsCompacting {
		running = "yes"
	}
	currentTool := snapshot.CurrentTool
	if strings.TrimSpace(currentTool) == "" {
		currentTool = "<none>"
	}
	errorText := snapshot.ExecutionError
	if strings.TrimSpace(errorText) == "" {
		errorText = "<none>"
	}
	picker := snapshot.PickerLabel
	if strings.TrimSpace(picker) == "" {
		picker = "none"
	}

	lines := []string{
		"Status:",
		"Session: " + sessionTitle,
		"Session ID: " + sessionID,
		"Running: " + running,
		"Provider: " + snapshot.CurrentProvider,
		"Model: " + snapshot.CurrentModel,
		"Workdir: " + snapshot.CurrentWorkdir,
		"Focus: " + snapshot.FocusLabel,
		"Picker: " + picker,
		"Current Tool: " + currentTool,
		fmt.Sprintf("Messages: %d", snapshot.MessageCount),
		"Error: " + errorText,
	}
	return strings.Join(lines, "\n")
}

func executeProviderCommand(ctx context.Context, providerSvc ProviderController, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("usage: %s", slashUsageProvider)
	}
	selection, err := providerSvc.SelectProvider(ctx, value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[System] Current provider switched to %s.", selection.ProviderID), nil
}

func slashHelpText() string {
	lines := []string{"Available slash commands:"}
	for _, command := range builtinSlashCommands {
		lines = append(lines, fmt.Sprintf("%s - %s", command.Usage, command.Description))
	}
	return strings.Join(lines, "\n")
}

func splitFirstWord(input string) (string, string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	index := strings.IndexAny(input, " \t")
	if index < 0 {
		return input, ""
	}
	return input[:index], strings.TrimSpace(input[index+1:])
}

func isWorkspaceSlashCommand(raw string) bool {
	command, _ := splitFirstWord(strings.ToLower(strings.TrimSpace(raw)))
	return command == slashCommandCWD
}

func parseWorkspaceSlashCommand(raw string) (string, error) {
	command, args := splitFirstWord(strings.TrimSpace(raw))
	switch strings.ToLower(command) {
	case slashCommandCWD:
		return strings.TrimSpace(args), nil
	default:
		return "", fmt.Errorf("unknown command %q", command)
	}
}
