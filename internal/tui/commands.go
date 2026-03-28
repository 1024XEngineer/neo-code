package tui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dust/neo-code/internal/config"
)

const (
	slashPrefix             = "/"
	slashCommandSet         = "/set"
	slashCommandModelPicker = "/model"

	slashUsageSetURL = "/set url <url>"
	slashUsageSetKey = "/set key <key>"
	slashUsageModel  = "/model"

	commandMenuTitle    = "Commands"
	modelPickerTitle    = "Select Model"
	modelPickerSubtitle = "Up/Down choose, Enter confirm, Esc cancel"

	sidebarTitle      = "Sessions"
	sidebarFilterHint = "按 \"/\" 筛选"
	sidebarOpenHint   = "Enter 打开"

	draftSessionTitle     = "Draft"
	emptyConversationText = "No conversation yet.\nAsk NeoCode to inspect or change code, or type / to browse local commands."
	emptyMessageText      = "(empty)"

	statusReady           = "Ready"
	statusRuntimeClosed   = "Runtime closed"
	statusThinking        = "Thinking"
	statusRunningTool     = "Running tool"
	statusToolFinished    = "Tool finished"
	statusToolError       = "Tool error"
	statusError           = "Error"
	statusDraft           = "New draft"
	statusRunning         = "Running"
	statusApplyingCommand = "Applying local command"
	statusChooseModel     = "Choose a model"

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
	{Usage: slashUsageSetURL, Description: "Set the API Base URL"},
	{Usage: slashUsageSetKey, Description: "Update the API Key"},
	{Usage: slashUsageModel, Description: "Open the interactive model picker"},
}

func newModelPicker() list.Model {
	catalog := config.BuiltinModelCatalog()
	items := make([]list.Item, 0, len(catalog))
	for _, option := range catalog {
		items = append(items, modelItem{name: option.Name, description: option.Description})
	}

	delegate := list.NewDefaultDelegate()
	picker := list.New(items, delegate, 0, 0)
	picker.Title = ""
	picker.SetShowHelp(false)
	picker.SetShowStatusBar(false)
	picker.SetFilteringEnabled(false)
	picker.DisableQuitKeybindings()
	return picker
}

func (a *App) openModelPicker() {
	a.state.ShowModelPicker = true
	a.state.StatusText = statusChooseModel
	a.input.Blur()
	a.selectCurrentModel(a.state.CurrentModel)
}

func (a *App) closeModelPicker() {
	a.state.ShowModelPicker = false
	a.focus = panelInput
	a.applyFocus()
}

func (a *App) selectCurrentModel(model string) {
	items := a.modelPicker.Items()
	for idx, item := range items {
		candidate, ok := item.(modelItem)
		if ok && strings.EqualFold(candidate.name, model) {
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

func runLocalCommand(configManager *config.Manager, raw string) tea.Cmd {
	return func() tea.Msg {
		notice, err := executeLocalCommand(context.Background(), configManager, raw)
		return localCommandResultMsg{notice: notice, err: err}
	}
}

func runModelSelection(configManager *config.Manager, model string) tea.Cmd {
	return func() tea.Msg {
		notice, err := setCurrentModel(context.Background(), configManager, model)
		return localCommandResultMsg{notice: notice, err: err}
	}
}

func executeLocalCommand(ctx context.Context, configManager *config.Manager, raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty command")
	}

	if !strings.EqualFold(fields[0], slashCommandSet) {
		return "", fmt.Errorf("unknown command %q", fields[0])
	}
	if len(fields) < 3 {
		return "", fmt.Errorf("usage: %s | %s | %s", slashUsageSetURL, slashUsageSetKey, slashUsageModel)
	}

	value := strings.TrimSpace(strings.Join(fields[2:], " "))
	if value == "" {
		return "", fmt.Errorf("command value is empty")
	}

	switch strings.ToLower(fields[1]) {
	case "url":
		if _, err := url.ParseRequestURI(value); err != nil {
			return "", fmt.Errorf("invalid url: %w", err)
		}
		if err := configManager.Update(ctx, func(cfg *config.Config) error {
			selectedName := strings.TrimSpace(cfg.SelectedProvider)
			for i := range cfg.Providers {
				if strings.EqualFold(strings.TrimSpace(cfg.Providers[i].Name), selectedName) {
					cfg.Providers[i].BaseURL = value
					return nil
				}
			}
			return fmt.Errorf("selected provider %q not found", cfg.SelectedProvider)
		}); err != nil {
			return "", err
		}
		cfg := configManager.Get()
		return fmt.Sprintf("[System] Base URL updated for %s -> %s", cfg.SelectedProvider, value), nil
	case "key":
		cfg := configManager.Get()
		selected, err := cfg.SelectedProviderConfig()
		if err != nil {
			return "", err
		}
		if err := configManager.UpsertEnv(selected.APIKeyEnv, value); err != nil {
			return "", fmt.Errorf("persist api key: %w", err)
		}
		if err := configManager.OverloadManagedEnvironment(); err != nil {
			return "", fmt.Errorf("reload managed env: %w", err)
		}
		if _, err := configManager.Reload(ctx); err != nil {
			return "", fmt.Errorf("reload config: %w", err)
		}
		return fmt.Sprintf("[System] %s updated and loaded.", selected.APIKeyEnv), nil
	case "model":
		return setCurrentModel(ctx, configManager, value)
	default:
		return "", fmt.Errorf("unsupported /set field %q", fields[1])
	}
}

func setCurrentModel(ctx context.Context, configManager *config.Manager, model string) (string, error) {
	if err := configManager.Update(ctx, func(cfg *config.Config) error {
		cfg.CurrentModel = model
		selectedName := strings.TrimSpace(cfg.SelectedProvider)
		for i := range cfg.Providers {
			if strings.EqualFold(strings.TrimSpace(cfg.Providers[i].Name), selectedName) {
				cfg.Providers[i].Model = model
				return nil
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if _, err := configManager.Reload(ctx); err != nil {
		return "", fmt.Errorf("reload config: %w", err)
	}

	return fmt.Sprintf("[System] Current model switched to %s.", model), nil
}
