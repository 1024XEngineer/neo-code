package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"

	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
	tuiservices "neo-code/internal/tui/services"
)

func TestBuiltinSlashCommands(t *testing.T) {
	if len(builtinSlashCommands) == 0 {
		t.Error("builtinSlashCommands should not be empty")
	}

	found := false
	foundTodo := false
	foundSkills := false
	foundSkillUse := false
	foundStatus := false
	for _, cmd := range builtinSlashCommands {
		if cmd.Usage == slashUsageHelp {
			found = true
		}
		if strings.HasPrefix(cmd.Usage, "/todo") {
			foundTodo = true
		}
		if cmd.Usage == slashUsageSkills {
			foundSkills = true
		}
		if cmd.Usage == slashUsageSkillUse {
			foundSkillUse = true
		}
		if strings.EqualFold(cmd.Usage, "/status") {
			foundStatus = true
		}
	}
	if !found {
		t.Error("expected to find /help command")
	}
	if foundTodo {
		t.Error("did not expect /todo command in builtin slash commands")
	}
	if !foundSkills {
		t.Error("expected to find /skills command")
	}
	if !foundSkillUse {
		t.Error("expected to find /skill use command")
	}
	if foundStatus {
		t.Error("did not expect /status command in builtin slash commands")
	}
}

func TestNewSelectionPicker(t *testing.T) {
	items := []list.Item{
		selectionItem{id: "1", name: "Item 1", description: "Desc 1"},
	}
	picker := newSelectionPicker(items)
	if !picker.FilteringEnabled() {
		t.Fatalf("expected selection picker filtering to be enabled")
	}
	if !picker.ShowFilter() {
		t.Fatalf("expected selection picker to show search filter")
	}
}

func TestNewSelectionPickerItems(t *testing.T) {
	items := []selectionItem{
		{id: "1", name: "Item 1", description: "Desc 1"},
	}
	picker := newSelectionPickerItems(items)
	_ = picker
}

func TestReplacePickerItemsKeepsFilterEditingState(t *testing.T) {
	current := newSelectionPickerItems([]selectionItem{
		{id: "m-1", name: "model-one", description: "first"},
	})
	current.SetSize(48, 12)
	current.SetFilterText("model")
	current.SetFilterState(list.Filtering)

	replacePickerItems(&current, []selectionItem{
		{id: "m-2", name: "model-two", description: "second"},
	})

	if !current.SettingFilter() {
		t.Fatalf("expected picker to keep filtering state after replace")
	}
	if current.FilterValue() != "model" {
		t.Fatalf("expected picker to keep filter text, got %q", current.FilterValue())
	}
}

func TestReplacePickerItemsKeepsFilteringStateWithEmptyQuery(t *testing.T) {
	current := newSelectionPickerItems([]selectionItem{
		{id: "m-1", name: "model-one", description: "first"},
	})
	current.SetSize(48, 12)
	current.SetFilterText("")
	current.SetFilterState(list.Filtering)

	replacePickerItems(&current, []selectionItem{
		{id: "m-2", name: "model-two", description: "second"},
		{id: "m-3", name: "model-three", description: "third"},
	})

	if !current.SettingFilter() {
		t.Fatalf("expected picker to stay in filtering state")
	}
	if got := len(current.VisibleItems()); got != 2 {
		t.Fatalf("expected visible items to be preserved under empty filter, got %d", got)
	}
}

func TestNewCommandMenuModel(t *testing.T) {
	uiStyles := newStyles()
	delegate := commandMenuDelegate{styles: uiStyles}
	if delegate.Height() == 0 {
		t.Error("delegate should have height")
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"statusReady", statusReady},
		{"statusThinking", statusThinking},
		{"statusCanceling", statusCanceling},
		{"statusCanceled", statusCanceled},
		{"statusRunningTool", statusRunningTool},
		{"statusToolFinished", statusToolFinished},
		{"statusToolError", statusToolError},
		{"statusError", statusError},
		{"statusDraft", statusDraft},
		{"statusRunning", statusRunning},
		{"statusApplyingCommand", statusApplyingCommand},
		{"statusCompacting", statusCompacting},
		{"statusChooseProvider", statusChooseProvider},
		{"statusChooseModel", statusChooseModel},
		{"statusTodoCollapsed", statusTodoCollapsed},
		{"statusTodoExpanded", statusTodoExpanded},
		{"statusChooseHelp", statusChooseHelp},
		{"statusBrowseFile", statusBrowseFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Error("status constant should not be empty")
			}
		})
	}
}

func TestFocusLabels(t *testing.T) {
	if focusLabelSessions == "" {
		t.Error("focusLabelSessions should not be empty")
	}
	if focusLabelTranscript == "" {
		t.Error("focusLabelTranscript should not be empty")
	}
	if focusLabelActivity == "" {
		t.Error("focusLabelActivity should not be empty")
	}
	if focusLabelTodo == "" {
		t.Error("focusLabelTodo should not be empty")
	}
	if focusLabelComposer == "" {
		t.Error("focusLabelComposer should not be empty")
	}
}

func TestMessageTags(t *testing.T) {
	if messageTagUser == "" {
		t.Error("messageTagUser should not be empty")
	}
	if messageTagAgent == "" {
		t.Error("messageTagAgent should not be empty")
	}
	if messageTagTool == "" {
		t.Error("messageTagTool should not be empty")
	}
}

func TestRoleConstants(t *testing.T) {
	if roleUser == "" {
		t.Error("roleUser should not be empty")
	}
	if roleAssistant == "" {
		t.Error("roleAssistant should not be empty")
	}
	if roleTool == "" {
		t.Error("roleTool should not be empty")
	}
}

func TestMaxActivityEntries(t *testing.T) {
	if maxActivityEntries == 0 {
		t.Error("maxActivityEntries should not be zero")
	}
}

type errorProviderService struct {
	err error
}

func (s errorProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
	return nil, s.err
}

func (s errorProviderService) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	return configstate.Selection{}, s.err
}

func (s errorProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

func (s errorProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

func (s errorProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	return configstate.Selection{}, s.err
}

func (s errorProviderService) CreateCustomProvider(
	ctx context.Context,
	input configstate.CreateCustomProviderInput,
) (configstate.Selection, error) {
	return configstate.Selection{}, s.err
}

func TestExecuteLocalCommandErrors(t *testing.T) {
	app, _ := newTestApp(t)
	snapshot := app.currentStatusSnapshot()

	if _, err := executeLocalCommand(context.Background(), app.configManager, app.providerSvc, snapshot, ""); err == nil {
		t.Fatalf("expected empty command error")
	}
	if _, err := executeLocalCommand(context.Background(), app.configManager, app.providerSvc, snapshot, "/unknown"); err == nil {
		t.Fatalf("expected unknown command error")
	}
}

func TestExecuteLocalCommandHelpAndStatusRemoved(t *testing.T) {
	app, _ := newTestApp(t)
	snapshot := app.currentStatusSnapshot()

	helpText, err := executeLocalCommand(context.Background(), app.configManager, app.providerSvc, snapshot, "/help")
	if err != nil {
		t.Fatalf("executeLocalCommand(/help) error = %v", err)
	}
	if !strings.Contains(helpText, "Available slash commands:") {
		t.Fatalf("expected help output, got %q", helpText)
	}

	if _, err := executeLocalCommand(context.Background(), app.configManager, app.providerSvc, snapshot, "/status"); err == nil {
		t.Fatalf("expected /status to be removed")
	}
}

func TestExecuteProviderCommandValidation(t *testing.T) {
	app, _ := newTestApp(t)
	if _, err := executeProviderCommand(context.Background(), app.providerSvc, ""); err == nil {
		t.Fatalf("expected usage error")
	}
}

func TestExecuteProviderCommandSuccess(t *testing.T) {
	app, _ := newTestApp(t)
	value := app.state.CurrentProvider
	if strings.TrimSpace(value) == "" {
		t.Fatalf("expected provider id to be set")
	}

	message, err := executeProviderCommand(context.Background(), app.providerSvc, value)
	if err != nil {
		t.Fatalf("executeProviderCommand error = %v", err)
	}
	if !strings.Contains(message, value) {
		t.Fatalf("expected provider id in message, got %q", message)
	}
}

func TestExecuteProviderCommandPropagatesError(t *testing.T) {
	providerSvc := errorProviderService{err: errors.New("boom")}
	if _, err := executeProviderCommand(context.Background(), providerSvc, "any"); err == nil {
		t.Fatalf("expected provider error")
	}
}

func TestRunProviderSelectionCmd(t *testing.T) {
	app, _ := newTestApp(t)
	cmd := runProviderSelection(app.providerSvc, app.state.CurrentProvider)
	if cmd == nil {
		t.Fatalf("expected cmd")
	}
	msg := cmd()
	result, ok := msg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", msg)
	}
	if !result.ProviderChanged || !strings.Contains(result.Notice, app.state.CurrentProvider) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunModelSelectionCmd(t *testing.T) {
	app, _ := newTestApp(t)
	cmd := runModelSelection(app.providerSvc, app.state.CurrentModel)
	if cmd == nil {
		t.Fatalf("expected cmd")
	}
	msg := cmd()
	result, ok := msg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", msg)
	}
	if !result.ModelChanged || !strings.Contains(result.Notice, app.state.CurrentModel) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunModelCatalogRefreshCmd(t *testing.T) {
	app, _ := newTestApp(t)
	cmd := runModelCatalogRefresh(app.providerSvc, app.state.CurrentProvider)
	if cmd == nil {
		t.Fatalf("expected refresh cmd")
	}
	msg := cmd()
	result, ok := msg.(modelCatalogRefreshMsg)
	if !ok {
		t.Fatalf("expected modelCatalogRefreshMsg, got %T", msg)
	}
	if !strings.EqualFold(result.ProviderID, app.state.CurrentProvider) {
		t.Fatalf("unexpected provider id: %s", result.ProviderID)
	}
}

func TestRefreshModelPickerPrefersRuntimeModelCatalog(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	app.state.CurrentModel = "local-model"

	runtimeSource := &runtimeModelCatalogSourceStub{
		Runtime: app.runtime,
		models: []providertypes.ModelDescriptor{
			{ID: "remote-a", Name: "Remote A"},
			{ID: "remote-b", Name: "Remote B"},
		},
		selectedModelID: "remote-b",
	}
	app.runtime = runtimeSource

	if err := app.refreshModelPicker(); err != nil {
		t.Fatalf("refreshModelPicker() error = %v", err)
	}
	if runtimeSource.lastSessionID != "session-1" {
		t.Fatalf("expected runtime model query to use active session id, got %q", runtimeSource.lastSessionID)
	}
	if app.state.CurrentModel != "remote-b" {
		t.Fatalf("expected selected_model_id to update current model, got %q", app.state.CurrentModel)
	}
	selected, ok := app.modelPicker.SelectedItem().(selectionItem)
	if !ok {
		t.Fatalf("expected selected picker item, got %T", app.modelPicker.SelectedItem())
	}
	if selected.id != "remote-b" {
		t.Fatalf("expected picker selection to follow selected_model_id, got %q", selected.id)
	}
}

func TestRefreshModelPickerPropagatesRuntimeCatalogError(t *testing.T) {
	app, _ := newTestApp(t)
	app.runtime = &runtimeModelCatalogSourceStub{
		Runtime: app.runtime,
		err:     errors.New("list models failed"),
	}

	if err := app.refreshModelPicker(); err == nil || !strings.Contains(err.Error(), "list models failed") {
		t.Fatalf("expected runtime model catalog error, got %v", err)
	}
}

func TestRefreshHelpPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.refreshHelpPicker()
	if len(app.helpPicker.Items()) != len(builtinSlashCommands) {
		t.Fatalf("expected %d help items, got %d", len(builtinSlashCommands), len(app.helpPicker.Items()))
	}
}

func TestOpenHelpPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.helpPicker.SetFilterText("model")
	app.openHelpPicker()
	if app.state.ActivePicker != pickerHelp {
		t.Fatalf("expected help picker to open")
	}
	if app.state.StatusText != statusChooseHelp {
		t.Fatalf("expected help picker status, got %q", app.state.StatusText)
	}
	if app.helpPicker.FilterValue() != "" {
		t.Fatalf("expected help picker filter to be reset, got %q", app.helpPicker.FilterValue())
	}
	if !app.helpPicker.SettingFilter() {
		t.Fatalf("expected help picker search box to be focused")
	}
}

type runtimeModelCatalogSourceStub struct {
	tuiservices.Runtime
	models           []providertypes.ModelDescriptor
	selectedModelID  string
	err              error
	lastSessionID    string
}

func (s *runtimeModelCatalogSourceStub) ListModels(
	_ context.Context,
	sessionID string,
) ([]providertypes.ModelDescriptor, string, error) {
	s.lastSessionID = strings.TrimSpace(sessionID)
	if s.err != nil {
		return nil, "", s.err
	}
	return append([]providertypes.ModelDescriptor(nil), s.models...), strings.TrimSpace(s.selectedModelID), nil
}
