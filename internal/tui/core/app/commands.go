package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	tuicommands "neo-code/internal/tui/core/commands"
	tuistatus "neo-code/internal/tui/core/status"
	tuiservices "neo-code/internal/tui/services"
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

	commandMenuTitle       = "Suggestions"
	providerPickerTitle    = "Select Provider"
	providerPickerSubtitle = "Up/Down choose, Enter confirm, Esc cancel"
	modelPickerTitle       = "Select Model"
	modelPickerSubtitle    = "Up/Down choose, Enter confirm, Esc cancel"
	filePickerTitle        = "Browse Files"
	filePickerSubtitle     = "Navigate folders, Enter choose file, Esc cancel"

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
	statusBrowseFile      = "Browse workspace files"

	focusLabelSessions   = "Sessions"
	focusLabelTranscript = "Transcript"
	focusLabelActivity   = "Activity"
	focusLabelComposer   = "Composer"

	maxActivityEntries = 64

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

type slashCommand = tuicommands.SlashCommand
type commandSuggestion = tuicommands.CommandSuggestion

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

func newCommandMenuModel(uiStyles styles) list.Model {
	delegate := commandMenuDelegate{styles: uiStyles}
	menu := list.New([]list.Item{}, delegate, 0, 0)
	menu.Title = ""
	menu.SetShowTitle(false)
	menu.SetShowHelp(false)
	menu.SetShowStatusBar(false)
	menu.SetShowPagination(false)
	menu.SetShowFilter(false)
	menu.SetFilteringEnabled(false)
	menu.DisableQuitKeybindings()
	return menu
}

func newSelectionPickerItems(items []selectionItem) list.Model {
	listItems := make([]list.Item, 0, len(items))
	for _, item := range items {
		listItems = append(listItems, item)
	}
	return newSelectionPicker(listItems)
}

func mapProviderItems(items []config.ProviderCatalogItem) []selectionItem {
	mapped := make([]selectionItem, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, selectionItem{
			id:   item.ID,
			name: item.Name,
		})
	}
	return mapped
}

func mapModelItems(models []providertypes.ModelDescriptor) []selectionItem {
	mapped := make([]selectionItem, 0, len(models))
	for _, option := range models {
		mapped = append(mapped, selectionItem{
			id:          option.ID,
			name:        option.Name,
			description: option.Description,
		})
	}
	return mapped
}

func replacePickerItems(current *list.Model, items []selectionItem) {
	next := newSelectionPickerItems(items)
	next.SetSize(current.Width(), current.Height())
	*current = next
}

func (a *App) refreshProviderPicker() error {
	items, err := a.providerSvc.ListProviders(context.Background())
	if err != nil {
		return err
	}

	replacePickerItems(&a.providerPicker, mapProviderItems(items))
	selectPickerItemByID(&a.providerPicker, a.state.CurrentProvider)
	return nil
}

func (a *App) refreshModelPicker() error {
	models, err := a.providerSvc.ListModelsSnapshot(context.Background())
	if err != nil {
		return err
	}

	replacePickerItems(&a.modelPicker, mapModelItems(models))
	selectPickerItemByID(&a.modelPicker, a.state.CurrentModel)
	return nil
}

func (a *App) openProviderPicker() {
	a.openPicker(pickerProvider, statusChooseProvider, &a.providerPicker, a.state.CurrentProvider)
}

func (a *App) openModelPicker() {
	a.openPicker(pickerModel, statusChooseModel, &a.modelPicker, a.state.CurrentModel)
}

func (a *App) openPicker(mode pickerMode, statusText string, picker *list.Model, selectedID string) {
	a.state.ActivePicker = mode
	a.state.StatusText = statusText
	a.input.Blur()
	selectPickerItemByID(picker, selectedID)
}

func (a *App) closePicker() {
	a.state.ActivePicker = pickerNone
	a.focus = panelInput
	a.applyFocus()
}

func selectPickerItemByID(picker *list.Model, selectedID string) {
	items := picker.Items()
	for idx, item := range items {
		candidate, ok := item.(selectionItem)
		if ok && strings.EqualFold(candidate.id, selectedID) {
			picker.Select(idx)
			return
		}
	}
	if len(items) > 0 {
		picker.Select(0)
	}
}

func (a *App) selectCurrentProvider(providerID string) {
	selectPickerItemByID(&a.providerPicker, providerID)
}

func (a *App) selectCurrentModel(modelID string) {
	selectPickerItemByID(&a.modelPicker, modelID)
}

func (a App) matchingSlashCommands(input string) []commandSuggestion {
	return tuicommands.MatchSlashCommands(input, slashPrefix, builtinSlashCommands)
}

func isCompleteSlashCommand(input string) bool {
	return tuicommands.IsCompleteSlashCommand(input, builtinSlashCommands)
}

func runProviderSelection(providerSvc ProviderController, providerName string) tea.Cmd {
	return tuiservices.SelectProviderCmd(
		providerSvc,
		providerName,
		func(selection config.ProviderSelection, err error) tea.Msg {
			if err != nil {
				return localCommandResultMsg{Err: err}
			}
			return localCommandResultMsg{
				Notice:          fmt.Sprintf("[System] Current provider switched to %s.", selection.ProviderID),
				ProviderChanged: true,
			}
		},
	)
}

func runModelSelection(providerSvc ProviderController, modelID string) tea.Cmd {
	return tuiservices.SelectModelCmd(
		providerSvc,
		modelID,
		func(selection config.ProviderSelection, err error) tea.Msg {
			if err != nil {
				return localCommandResultMsg{Err: err}
			}
			return localCommandResultMsg{
				Notice:       fmt.Sprintf("[System] Current model switched to %s.", selection.ModelID),
				ModelChanged: true,
			}
		},
	)
}

func runLocalCommand(configManager *config.Manager, providerSvc ProviderController, snapshot tuistatus.Snapshot, raw string) tea.Cmd {
	return tuiservices.RunLocalCommandCmd(
		func(ctx context.Context) (string, error) {
			return executeLocalCommand(ctx, configManager, providerSvc, snapshot, raw)
		},
		func(notice string, err error) tea.Msg {
			result := localCommandResultMsg{Notice: notice, Err: err}
			if err == nil {
				cfg := configManager.Get()
				result.ProviderChanged = !strings.EqualFold(snapshot.CurrentProvider, cfg.SelectedProvider)
				result.ModelChanged = !strings.EqualFold(snapshot.CurrentModel, cfg.CurrentModel)
			}
			return result
		},
	)
}

func runModelCatalogRefresh(providerSvc ProviderController, providerID string) tea.Cmd {
	return tuiservices.RefreshModelCatalogCmd(
		providerSvc,
		providerID,
		func(providerID string, models []providertypes.ModelDescriptor, err error) tea.Msg {
			return modelCatalogRefreshMsg{
				ProviderID: providerID,
				Models:     models,
				Err:        err,
			}
		},
	)
}

func executeLocalCommand(ctx context.Context, configManager *config.Manager, providerSvc ProviderController, snapshot tuistatus.Snapshot, raw string) (string, error) {
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

func executeStatusCommand(snapshot tuistatus.Snapshot) string {
	return tuistatus.Format(snapshot, draftSessionTitle)
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
	return tuicommands.SplitFirstWord(input)
}

func isWorkspaceSlashCommand(raw string) bool {
	return tuicommands.IsWorkspaceSlashCommand(raw, slashCommandCWD)
}

func parseWorkspaceSlashCommand(raw string) (string, error) {
	return tuicommands.ParseWorkspaceSlashCommand(raw, slashCommandCWD)
}
