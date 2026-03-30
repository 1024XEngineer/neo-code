package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/provider"
)

const (
	slashPrefix              = "/"
	slashCommandProviderPick = "/provider"
	slashCommandModelPicker  = "/model"
	slashCommandAPIKeyInput  = "/apikey"

	slashUsageProvider = "/provider"
	slashUsageModel    = "/model"
	slashUsageAPIKey   = "/apikey"

	commandMenuTitle       = "Commands"
	providerPickerTitle    = "Select Provider"
	providerPickerSubtitle = "Up/Down choose, Enter confirm, Esc cancel"
	modelPickerTitle       = "Select Model"
	modelPickerSubtitle    = "Up/Down choose, Enter confirm, Esc cancel"
	apiKeyInputTitle       = "Set API Key Env"
	apiKeyInputSubtitle    = "Enter save, Esc cancel, leave empty to restore default"

	sidebarTitle      = "Sessions"
	sidebarFilterHint = "Type / to search"
	sidebarOpenHint   = "Enter to open"

	draftSessionTitle     = "Draft"
	emptyConversationText = "No conversation yet.\nAsk NeoCode to inspect or change code, or type / to browse local commands."
	emptyMessageText      = "(empty)"

	statusReady          = "Ready"
	statusRuntimeClosed  = "Runtime closed"
	statusThinking       = "Thinking"
	statusCanceling      = "Canceling"
	statusCanceled       = "Canceled"
	statusRunningTool    = "Running tool"
	statusToolFinished   = "Tool finished"
	statusToolError      = "Tool error"
	statusError          = "Error"
	statusDraft          = "New draft"
	statusRunning        = "Running"
	statusChooseProvider = "Choose a provider"
	statusChooseModel    = "Choose a model"
	statusEditAPIKeyEnv  = "Set API key env"

	focusLabelSessions   = "Sessions"
	focusLabelTranscript = "Transcript"
	focusLabelComposer   = "Composer"

	messageTagUser  = "[ YOU ]"
	messageTagAgent = "[ NEO ]"
	messageTagTool  = "[ TOOL ]"

	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
	roleEvent     = "event"
	roleError     = "error"
	roleSystem    = "system"
)

type slashCommand struct {
	Usage       string
	Description string
}

type commandSuggestion struct {
	Command slashCommand
	Match   bool
}

var builtinSlashCommands = []slashCommand{
	{Usage: slashUsageProvider, Description: "Open the interactive provider picker"},
	{Usage: slashUsageModel, Description: "Open the interactive model picker"},
	{Usage: slashUsageAPIKey, Description: "Open the API key env input"},
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

func newProviderPicker(items []provider.ProviderCatalogItem) list.Model {
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

func newModelPicker(models []provider.ModelDescriptor) list.Model {
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
	models, err := a.providerSvc.ListModels(context.Background())
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

func (a *App) openAPIKeyInput() {
	a.state.ActivePicker = pickerAPIKey
	a.state.StatusText = statusEditAPIKeyEnv
	a.apiKeyInput.SetValue(a.state.APIKeyEnvOverride)
	a.apiKeyInput.Placeholder = fallback(a.state.CurrentAPIKeyEnv, "Enter API key env name")
	a.focus = panelInput
	a.applyFocus()
	a.apiKeyInput.CursorEnd()
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

func runProviderSelection(providerSvc ProviderController, providerName string) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SelectProvider(context.Background(), providerName)
		if err != nil {
			return localCommandResultMsg{err: err}
		}
		return localCommandResultMsg{
			notice: fmt.Sprintf("[System] Current provider switched to %s.", selection.ProviderID),
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
			notice: fmt.Sprintf("[System] Current model switched to %s.", selection.ModelID),
		}
	}
}

func runAPIKeyEnvSelection(providerSvc ProviderController, envName string) tea.Cmd {
	return func() tea.Msg {
		trimmed := strings.TrimSpace(envName)
		if err := providerSvc.SetAPIKeyEnvOverride(context.Background(), trimmed); err != nil {
			return localCommandResultMsg{err: err}
		}
		if trimmed == "" {
			return localCommandResultMsg{
				notice: "[System] API key env restored to provider default.",
			}
		}
		return localCommandResultMsg{
			notice: fmt.Sprintf("[System] API key env override set to %s.", trimmed),
		}
	}
}
