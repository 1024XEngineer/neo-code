package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	agentruntime "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

type stubProviderService struct {
	providers      []configstate.ProviderOption
	models         []providertypes.ModelDescriptor
	listErr        error
	listModelsErr  error
	selectErr      error
	selectDelay    time.Duration
	selectResponse configstate.Selection
	selectHook     func(providerID string)
	setModelHook   func(modelID string)
	setModelErr    error
	createErr      error
	createResponse configstate.Selection
}

func (s stubProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.providers, nil
}

func (s stubProviderService) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	if s.selectHook != nil {
		s.selectHook(providerID)
	}
	if s.selectDelay > 0 {
		timer := time.NewTimer(s.selectDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return configstate.Selection{}, ctx.Err()
		case <-timer.C:
		}
	}
	if s.selectErr != nil {
		return configstate.Selection{}, s.selectErr
	}
	if strings.TrimSpace(s.selectResponse.ProviderID) != "" || strings.TrimSpace(s.selectResponse.ModelID) != "" {
		return s.selectResponse, nil
	}

	modelID := ""
	if len(s.models) > 0 {
		modelID = s.models[0].ID
	}
	return configstate.Selection{ProviderID: providerID, ModelID: modelID}, nil
}

func (s stubProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if s.listModelsErr != nil {
		return nil, s.listModelsErr
	}
	return s.models, nil
}

func (s stubProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if s.listModelsErr != nil {
		return nil, s.listModelsErr
	}
	return s.models, nil
}

func (s stubProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	if s.setModelHook != nil {
		s.setModelHook(modelID)
	}
	if s.setModelErr != nil {
		return configstate.Selection{}, s.setModelErr
	}
	providerID := ""
	if len(s.providers) > 0 {
		providerID = s.providers[0].ID
	}
	return configstate.Selection{ProviderID: providerID, ModelID: modelID}, nil
}

func (s stubProviderService) CreateCustomProvider(
	ctx context.Context,
	input configstate.CreateCustomProviderInput,
) (configstate.Selection, error) {
	if s.createErr != nil {
		return configstate.Selection{}, s.createErr
	}
	if strings.TrimSpace(s.createResponse.ProviderID) != "" || strings.TrimSpace(s.createResponse.ModelID) != "" {
		return s.createResponse, nil
	}
	modelID := ""
	if len(s.models) > 0 {
		modelID = s.models[0].ID
	}
	return configstate.Selection{ProviderID: input.Name, ModelID: modelID}, nil
}

type stubRuntime struct {
	events             chan agentruntime.RuntimeEvent
	prepareInputs      []agentruntime.PrepareInput
	prepareErr         error
	preparedOutput     agentruntime.UserInput
	runInputs          []agentruntime.UserInput
	systemToolCalls    []agentruntime.SystemToolInput
	systemToolRes      tools.ToolResult
	systemToolErr      error
	resolveCalls       []agentruntime.PermissionResolutionInput
	resolveErr         error
	cancelInvoked      bool
	listSessions       []agentsession.Summary
	listSessionsErr    error
	loadSessions       map[string]agentsession.Session
	loadSessionErr     error
	logEntriesBySID    map[string][]agentruntime.SessionLogEntry
	loadLogErr         error
	saveLogErr         error
	activateSkillCalls []struct {
		SessionID string
		SkillID   string
	}
	activateSkillErr     error
	deactivateSkillCalls []struct {
		SessionID string
		SkillID   string
	}
	deactivateSkillErr    error
	sessionSkillsResult   []agentruntime.SessionSkillState
	sessionSkillsErr      error
	availableSkillsResult []agentruntime.AvailableSkillState
	availableSkillsErr    error
}

type snapshotRuntime struct {
	*stubRuntime
	sessionContext any
	sessionUsage   any
	runSnapshot    any
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{
		events:          make(chan agentruntime.RuntimeEvent),
		logEntriesBySID: make(map[string][]agentruntime.SessionLogEntry),
	}
}

func (s *stubRuntime) PrepareUserInput(ctx context.Context, input agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	s.prepareInputs = append(s.prepareInputs, input)
	if s.prepareErr != nil {
		return agentruntime.UserInput{}, s.prepareErr
	}
	if len(s.preparedOutput.Parts) > 0 {
		return s.preparedOutput, nil
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = "session-prepared"
	}
	content := strings.TrimSpace(input.Text)
	if content == "" {
		content = "image input"
	}
	return agentruntime.UserInput{
		SessionID: sessionID,
		RunID:     strings.TrimSpace(input.RunID),
		Parts:     []providertypes.ContentPart{providertypes.NewTextPart(content)},
		Workdir:   strings.TrimSpace(input.Workdir),
	}, nil
}

func (s *stubRuntime) Submit(ctx context.Context, input agentruntime.PrepareInput) error {
	prepared, err := s.PrepareUserInput(ctx, input)
	if err != nil {
		return err
	}
	return s.Run(ctx, prepared)
}

func (s *stubRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	s.runInputs = append(s.runInputs, input)
	return nil
}

func (s *stubRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (s *stubRuntime) ExecuteSystemTool(ctx context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	s.systemToolCalls = append(s.systemToolCalls, input)
	if s.systemToolErr != nil {
		return tools.ToolResult{}, s.systemToolErr
	}
	return s.systemToolRes, nil
}

func (s *stubRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	s.resolveCalls = append(s.resolveCalls, input)
	return s.resolveErr
}

func (s *stubRuntime) CancelActiveRun() bool {
	s.cancelInvoked = true
	return true
}

func (s *stubRuntime) Events() <-chan agentruntime.RuntimeEvent {
	return s.events
}

func (s *stubRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	if s.listSessionsErr != nil {
		return nil, s.listSessionsErr
	}
	return s.listSessions, nil
}

func (s *stubRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if s.loadSessionErr != nil {
		return agentsession.Session{}, s.loadSessionErr
	}
	if s.loadSessions != nil {
		if session, ok := s.loadSessions[id]; ok {
			return session, nil
		}
	}
	return agentsession.NewWithWorkdir("draft", ""), nil
}

func (s *stubRuntime) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	s.activateSkillCalls = append(s.activateSkillCalls, struct {
		SessionID string
		SkillID   string
	}{
		SessionID: sessionID,
		SkillID:   skillID,
	})
	return s.activateSkillErr
}

func (s *stubRuntime) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	s.deactivateSkillCalls = append(s.deactivateSkillCalls, struct {
		SessionID string
		SkillID   string
	}{
		SessionID: sessionID,
		SkillID:   skillID,
	})
	return s.deactivateSkillErr
}

func (s *stubRuntime) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	if s.sessionSkillsErr != nil {
		return nil, s.sessionSkillsErr
	}
	return append([]agentruntime.SessionSkillState(nil), s.sessionSkillsResult...), nil
}

func (s *stubRuntime) ListAvailableSkills(ctx context.Context, sessionID string) ([]agentruntime.AvailableSkillState, error) {
	if s.availableSkillsErr != nil {
		return nil, s.availableSkillsErr
	}
	return append([]agentruntime.AvailableSkillState(nil), s.availableSkillsResult...), nil
}

func (s *stubRuntime) LoadSessionLogEntries(ctx context.Context, sessionID string) ([]agentruntime.SessionLogEntry, error) {
	if s.loadLogErr != nil {
		return nil, s.loadLogErr
	}
	entries := s.logEntriesBySID[strings.TrimSpace(sessionID)]
	return append([]agentruntime.SessionLogEntry(nil), entries...), nil
}

func (s *stubRuntime) SaveSessionLogEntries(
	ctx context.Context,
	sessionID string,
	entries []agentruntime.SessionLogEntry,
) error {
	if s.saveLogErr != nil {
		return s.saveLogErr
	}
	s.logEntriesBySID[strings.TrimSpace(sessionID)] = append([]agentruntime.SessionLogEntry(nil), entries...)
	return nil
}

func (s *stubRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	return agentsession.NewWithWorkdir("draft", workdir), nil
}

func messageText(message providertypes.Message) string {
	return renderMessagePartsForDisplay(message.Parts)
}
func (s *snapshotRuntime) GetSessionContext(ctx context.Context, sessionID string) (any, error) {
	return s.sessionContext, nil
}

func (s *snapshotRuntime) GetSessionUsage(ctx context.Context, sessionID string) (any, error) {
	return s.sessionUsage, nil
}

func (s *snapshotRuntime) GetRunSnapshot(ctx context.Context, runID string) (any, error) {
	return s.runSnapshot, nil
}

func newDefaultAppConfig() *config.Config {
	cfg := config.StaticDefaults()
	cfg.Providers = config.DefaultProviders()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}
	return cfg
}

func newTestAppWithProviderService(t *testing.T, providerSvc ProviderController) (App, *stubRuntime) {
	t.Helper()

	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:          *cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: providerSvc,
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	return app, runtime
}

func newTestApp(t *testing.T) (App, *stubRuntime) {
	t.Helper()

	cfg := newDefaultAppConfig()
	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		providerCfg := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   providerCfg.Name,
				Name: providerCfg.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: providerCfg.Model, Name: providerCfg.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: providerCfg.Model, Name: providerCfg.Model}}
	}

	app, runtime := newTestAppWithProviderService(t, stubProviderService{providers: providers, models: models})
	app.startupScreenLocked = false
	app.layoutCached = true
	app.cachedWidth = app.width
	app.cachedHeight = app.height
	return app, runtime
}

func TestStartupKeyEscFocusesInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected Esc to keep startup screen locked")
	}
	if next.focus != panelInput {
		t.Fatalf("expected Esc to focus input panel from startup")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatalf("expected Esc not to quit from startup")
		}
	}
}

func TestCtrlCCopiesSelectionInsteadOfQuitting(t *testing.T) {
	app, _ := newTestApp(t)
	app.transcriptContent = "hello world"
	app.transcript.SetContent("hello world")
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 0
	app.textSelection.endLine = 0
	app.textSelection.endCol = 5
	if !app.hasTextSelection() {
		t.Fatalf("expected selection precondition")
	}

	originalClipboardWriteAll := clipboardWriteAll
	defer func() { clipboardWriteAll = originalClipboardWriteAll }()
	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatalf("expected Ctrl+C with selection not to quit")
		}
	}

	next := model.(App)
	if copied != "hello" {
		t.Fatalf("expected copied selection \"hello\", got %q", copied)
	}
	if next.state.StatusText != "Copied selected text" {
		t.Fatalf("expected copied status, got %q", next.state.StatusText)
	}
	if next.hasTextSelection() {
		t.Fatalf("expected selection to clear after copy")
	}
}

func TestStartupSlashTransitionsToComposer(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected startup to remain locked before first send")
	}
	if got := next.input.Value(); got != "/" {
		t.Fatalf("expected composer input '/', got %q", got)
	}
	if got := next.state.InputText; got != "/" {
		t.Fatalf("expected state input '/', got %q", got)
	}
	if len(next.commandMenu.Items()) == 0 {
		t.Fatalf("expected slash suggestions to be visible")
	}
}

func TestStartupRegularInputDismissesStartup(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	originalReadText := readClipboardText
	defer func() { readClipboardText = originalReadText }()
	readClipboardText = func() (string, error) { return "", errors.New("clipboard unavailable") }

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected startup to remain locked before first send")
	}
	if got := next.input.Value(); got != "h" {
		t.Fatalf("expected composer input to receive typed rune, got %q", got)
	}
}

func TestStartupCtrlONavigatesToWorkspaceBrowser(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected opening workspace picker not to unlock startup")
	}
	if next.state.ActivePicker != pickerFile {
		t.Fatalf("expected file picker active, got %v", next.state.ActivePicker)
	}
}

func TestStartupCtrlNStartsDraftSession(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActiveSessionID = "session-1"
	app.state.ActiveSessionTitle = "Old Session"
	app.input.SetValue("old content")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected Ctrl+N to return to startup screen")
	}
	if next.state.ActiveSessionID != "" {
		t.Fatalf("expected draft session id to be empty")
	}
	if next.state.ActiveSessionTitle != draftSessionTitle {
		t.Fatalf("expected draft session title, got %q", next.state.ActiveSessionTitle)
	}
	if got := next.input.Value(); got != "" {
		t.Fatalf("expected composer reset, got %q", got)
	}
}

func TestCtrlNRelocksStartupFromActiveDraft(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = false
	app.state.ActiveSessionID = "session-2"
	app.state.ActiveSessionTitle = "Session 2"
	app.input.SetValue("keep me?")
	app.state.InputText = "keep me?"
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("history")}},
	}

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	next := model.(App)
	if !next.startupScreenLocked {
		t.Fatalf("expected Ctrl+N to relock startup screen from active draft")
	}
	if next.state.ActiveSessionID != "" {
		t.Fatalf("expected active session cleared, got %q", next.state.ActiveSessionID)
	}
	if len(next.activeMessages) != 0 {
		t.Fatalf("expected transcript history cleared for startup draft")
	}
}

func TestSubmitProviderAddFormRequiresCustomDriverBaseURL(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()

	app.providerAddForm.Name = "custom-gateway"
	app.providerAddForm.Driver = "custom-driver"
	app.providerAddForm.APIKeyEnv = "CUSTOM_GATEWAY_API_KEY"
	app.providerAddForm.APIKey = "test-key"
	app.providerAddForm.BaseURL = ""

	cmd := app.submitProviderAddForm()
	if cmd != nil {
		t.Fatalf("expected nil command for invalid custom driver form")
	}
	if !strings.Contains(app.providerAddForm.Error, "Base URL is required for custom driver") {
		t.Fatalf("expected base URL validation error, got %q", app.providerAddForm.Error)
	}
}

func TestSubmitProviderAddFormAsyncSuccess(t *testing.T) {
	prevSupportsUserEnvPersistence := supportsUserEnvPersistence
	supportsUserEnvPersistence = func() bool { return false }
	t.Cleanup(func() { supportsUserEnvPersistence = prevSupportsUserEnvPersistence })

	providerName := "team-gateway"
	modelID := "gateway-model"
	service := stubProviderService{
		providers: []configstate.ProviderOption{
			{
				ID:   providerName,
				Name: providerName,
				Models: []providertypes.ModelDescriptor{
					{ID: modelID, Name: modelID},
				},
			},
		},
		models: []providertypes.ModelDescriptor{{ID: modelID, Name: modelID}},
		createResponse: configstate.Selection{
			ProviderID: providerName,
			ModelID:    modelID,
		},
	}
	app, _ := newTestAppWithProviderService(t, service)
	app.startProviderAddForm()

	app.providerAddForm.Name = providerName
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://team-gateway.example.com/v1"
	app.providerAddForm.APIKeyEnv = "TEAM_GATEWAY_API_KEY"
	app.providerAddForm.APIKey = "sk-test-123"

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command for provider add")
	}
	if !app.providerAddForm.Submitting {
		t.Fatalf("expected form to enter submitting state")
	}

	msg := cmd()
	result, ok := msg.(providerAddResultMsg)
	if !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if !strings.Contains(result.Warning, "current process only") {
		t.Fatalf("expected non-persistent env warning, got %q", result.Warning)
	}

	next, _ := app.Update(result)
	app = next.(App)
	if app.providerAddForm != nil {
		t.Fatalf("expected provider add form to close on success")
	}
	if app.state.CurrentProvider != providerName {
		t.Fatalf("expected current provider %q, got %q", providerName, app.state.CurrentProvider)
	}
	if app.state.CurrentModel != modelID {
		t.Fatalf("expected current model %q, got %q", modelID, app.state.CurrentModel)
	}
	var foundPersistenceNotice bool
	for _, entry := range app.activities {
		if entry.Title == "Provider key persistence" && strings.Contains(entry.Detail, "current process only") {
			foundPersistenceNotice = true
			break
		}
	}
	if !foundPersistenceNotice {
		t.Fatal("expected provider key persistence activity notice")
	}
}

func TestSubmitProviderAddFormRedactsSensitiveError(t *testing.T) {
	secretKey := "sk-secret-456"
	service := stubProviderService{
		createErr: errors.New("authentication failed for key " + secretKey),
	}
	app, _ := newTestAppWithProviderService(t, service)
	app.startProviderAddForm()

	app.providerAddForm.Name = "redact-gateway"
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://redact-gateway.example.com/v1"
	app.providerAddForm.APIKeyEnv = "REDACT_GATEWAY_API_KEY"
	app.providerAddForm.APIKey = secretKey

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command for provider add failure")
	}
	next, _ := app.Update(cmd())
	app = next.(App)
	if app.providerAddForm == nil {
		t.Fatalf("expected form to stay open on failure")
	}
	if strings.Contains(app.providerAddForm.Error, secretKey) {
		t.Fatalf("expected error to redact api key, got %q", app.providerAddForm.Error)
	}
	if !strings.Contains(app.providerAddForm.Error, "[REDACTED]") {
		t.Fatalf("expected redaction marker in error, got %q", app.providerAddForm.Error)
	}
}

func TestSubmitProviderAddFormTransitionsToManualStageWhenModelSourceManual(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()

	app.providerAddForm.Name = "manual-stage-gateway"
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.ModelSource = config.ModelSourceManual
	app.providerAddForm.APIKeyEnv = "MANUAL_STAGE_GATEWAY_API_KEY"
	app.providerAddForm.APIKey = "sk-manual-stage"

	cmd := app.submitProviderAddForm()
	if cmd != nil {
		t.Fatalf("expected no async command when entering manual JSON stage")
	}
	if app.providerAddForm == nil {
		t.Fatalf("expected provider form to remain open")
	}
	if app.providerAddForm.Stage != providerAddFormStageManualModels {
		t.Fatalf("expected form stage manual models, got %v", app.providerAddForm.Stage)
	}
	if strings.TrimSpace(app.providerAddForm.ManualModelsJSON) != "" {
		t.Fatalf("expected manual model json buffer to stay empty, got %q", app.providerAddForm.ManualModelsJSON)
	}
	if app.state.StatusText != "Fill manual model JSON" {
		t.Fatalf("expected manual stage status text, got %q", app.state.StatusText)
	}
}

func TestSubmitProviderAddFormRequiresAPIKeyEnv(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Name = "gateway"
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://example.com/v1"
	app.providerAddForm.APIKey = "test-key"
	app.providerAddForm.APIKeyEnv = ""

	cmd := app.submitProviderAddForm()
	if cmd != nil {
		t.Fatalf("expected nil command for invalid env key")
	}
	if !strings.Contains(app.providerAddForm.Error, "API Key Env is required") {
		t.Fatalf("expected env key validation error, got %q", app.providerAddForm.Error)
	}
}

func TestSubmitProviderAddFormRejectsProtectedAPIKeyEnv(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Name = "gateway"
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://example.com/v1"
	app.providerAddForm.APIKey = "test-key"
	app.providerAddForm.APIKeyEnv = "PATH"

	cmd := app.submitProviderAddForm()
	if cmd != nil {
		t.Fatalf("expected nil command for protected env key")
	}
	if !strings.Contains(app.providerAddForm.Error, "is protected") {
		t.Fatalf("expected protected env key validation error, got %q", app.providerAddForm.Error)
	}
}

func TestTrimLastRune(t *testing.T) {
	if got := trimLastRune(""); got != "" {
		t.Fatalf("trimLastRune(empty) = %q, want empty", got)
	}
	if got := trimLastRune("ab"); got != "a" {
		t.Fatalf("trimLastRune(ascii) = %q, want a", got)
	}
	if got := trimLastRune("\u4f60\u597d"); got != "\u4f60" {
		t.Fatalf("trimLastRune(utf8) = %q, want %q", got, "\u4f60")
	}
}

func TestAppUpdateBasic(t *testing.T) {
	app, _ := newTestApp(t)

	windowMsg := tea.WindowSizeMsg{Width: 100, Height: 30}
	model, cmd := app.Update(windowMsg)
	if model == nil {
		t.Error("Update returned nil model for WindowSizeMsg")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for WindowSizeMsg")
	}

	app.state.StatusText = ""
	closedMsg := RuntimeClosedMsg{}
	model, cmd = app.Update(closedMsg)
	if model == nil {
		t.Error("Update returned nil model for RuntimeClosedMsg")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for RuntimeClosedMsg")
	}
	if app.state.StatusText != statusRuntimeClosed {
		t.Errorf("Expected status %s, got %s", statusRuntimeClosed, app.state.StatusText)
	}

	runErrMsg := runFinishedMsg{Err: errors.New("test error")}
	model, cmd = app.Update(runErrMsg)
	if model == nil {
		t.Error("Update returned nil model for runFinishedMsg with error")
	}
	app = model.(App)
	if cmd != nil {
		if _, ok := cmd().(tickMsg); !ok {
			t.Errorf("expected optional tick cmd for footer toast, got %T", cmd())
		}
	}

	canceledMsg := runFinishedMsg{Err: context.Canceled}
	model, cmd = app.Update(canceledMsg)
	if model == nil {
		t.Error("Update returned nil model for runFinishedMsg with canceled error")
	}
	app = model.(App)
	if cmd != nil {
		if _, ok := cmd().(tickMsg); !ok {
			t.Errorf("expected optional tick cmd for footer toast, got %T", cmd())
		}
	}
}

func TestRefreshSessionPickerSelectsActiveSession(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	runtime.listSessions = []agentsession.Summary{
		{ID: "session-1", Title: "Session One", UpdatedAt: now.Add(-time.Minute)},
		{ID: "session-2", Title: "Session Two", UpdatedAt: now},
	}
	app.state.ActiveSessionID = "session-2"

	if err := app.refreshSessionPicker(); err != nil {
		t.Fatalf("refreshSessionPicker() error = %v", err)
	}
	if len(app.sessionPicker.Items()) != 2 {
		t.Fatalf("expected 2 session items, got %d", len(app.sessionPicker.Items()))
	}
	if got := app.sessionPicker.Index(); got != 1 {
		t.Fatalf("expected active session index 1, got %d", got)
	}
}

func TestParsePermissionShortcutFromKeyInput(t *testing.T) {
	if decision, ok := parsePermissionShortcut("y"); !ok || decision != agentruntime.DecisionAllowOnce {
		t.Fatalf("expected allow_once, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("a"); !ok || decision != agentruntime.DecisionAllowSession {
		t.Fatalf("expected allow_session, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("n"); !ok || decision != agentruntime.DecisionReject {
		t.Fatalf("expected reject, got %v (ok=%v)", decision, ok)
	}
	if _, ok := parsePermissionShortcut("x"); ok {
		t.Fatalf("expected unsupported key to return false")
	}
}

func TestRuntimeEventPermissionRequestHandler(t *testing.T) {
	app, _ := newTestApp(t)

	payload := agentruntime.PermissionRequestPayload{
		RequestID: "perm-1",
		ToolName:  "bash",
		Operation: "write",
		Target:    "file.txt",
	}
	handled := runtimeEventPermissionRequestHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected handler to return false")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-1" {
		t.Fatalf("expected pending permission request to be set")
	}
	if app.state.StatusText != statusPermissionRequired {
		t.Fatalf("expected permission required status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventPermissionResolvedHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-2"},
	}

	payload := agentruntime.PermissionResolvedPayload{
		RequestID:  "perm-2",
		ToolName:   "bash",
		Decision:   "allow",
		ResolvedAs: "approved",
	}
	handled := runtimeEventPermissionResolvedHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected handler to return false")
	}
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared")
	}
	if app.state.StatusText != "Permission approved" {
		t.Fatalf("expected resolved status text, got %s", app.state.StatusText)
	}
}

func TestUpdatePermissionResolveFlow(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-3"},
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if model == nil {
		t.Fatalf("expected non-nil model")
	}
	app = model.(App)
	if cmd == nil {
		t.Fatalf("expected command to resolve permission")
	}
	if app.state.StatusText != statusPermissionSubmitting {
		t.Fatalf("expected submitting status, got %s", app.state.StatusText)
	}

	msg := cmd()
	if len(runtime.resolveCalls) != 1 || runtime.resolveCalls[0].RequestID != "perm-3" {
		t.Fatalf("expected ResolvePermission to be called")
	}
	if runtime.resolveCalls[0].Decision != agentruntime.DecisionAllowOnce {
		t.Fatalf("unexpected decision forwarded: %s", runtime.resolveCalls[0].Decision)
	}

	next, _ := app.Update(msg)
	app = next.(App)
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared after submit")
	}
	if app.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status, got %s", app.state.StatusText)
	}
}

func TestUpdatePermissionResolvedError(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-4"},
		Submitting: true,
	}

	model, _ := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-4",
		Decision:  string(agentruntime.DecisionAllowOnce),
		Err:       errors.New("boom"),
	})
	app = model.(App)

	if app.pendingPermission == nil || app.pendingPermission.Submitting {
		t.Fatalf("expected pending permission to remain but leave submitting state")
	}
	if app.state.StatusText != "boom" {
		t.Fatalf("expected failure status, got %s", app.state.StatusText)
	}
}

func TestRunResolvePermissionCommand(t *testing.T) {
	runtime := newStubRuntime()
	cmd := runResolvePermission(runtime, "perm-5", agentruntime.DecisionAllowSession)
	if cmd == nil {
		t.Fatalf("expected command")
	}
	msg := cmd()
	resolved, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if resolved.RequestID != "perm-5" || resolved.Decision != string(agentruntime.DecisionAllowSession) {
		t.Fatalf("unexpected resolved msg: %#v", resolved)
	}
	if len(runtime.resolveCalls) != 1 {
		t.Fatalf("expected resolve call recorded")
	}
}

func TestRenderPermissionPromptInUpdateFlow(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{
			RequestID: "perm-6",
			ToolName:  "bash",
			Operation: "write",
			Target:    "file.txt",
		},
	}
	got := app.renderPermissionPrompt()
	if !strings.Contains(got, "Permission request: bash (write)") {
		t.Fatalf("expected permission prompt header, got %q", got)
	}
}

func TestUpdatePermissionResolutionFinishedMsgIgnoresMismatch(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-7"},
	}
	model, cmd := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-8",
		Decision:  string(agentruntime.DecisionAllowOnce),
	})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-7" {
		t.Fatalf("expected pending permission to remain")
	}
}

func TestRuntimeEventPermissionRequestUsesToolName(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.PermissionRequestPayload{
		RequestID: "perm-9",
		ToolName:  "webfetch",
	}
	runtimeEventPermissionRequestHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if app.pendingPermission == nil || app.pendingPermission.Request.ToolName != "webfetch" {
		t.Fatalf("expected pending permission tool to be set")
	}
}

func TestUpdatePermissionRejectFlow(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-10"},
	}
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatalf("expected resolve cmd")
	}
	app = model.(App)
	msg := cmd()
	next, _ := app.Update(msg)
	app = next.(App)
	if len(runtime.resolveCalls) != 1 || runtime.resolveCalls[0].Decision != agentruntime.DecisionReject {
		t.Fatalf("expected reject decision to be submitted")
	}
	if app.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventToolResultHandlerUpdatesMessages(t *testing.T) {
	app, _ := newTestApp(t)
	result := tools.ToolResult{
		Name:       "bash",
		Content:    "ok",
		IsError:    false,
		ToolCallID: "tool-1",
	}
	handled := runtimeEventToolResultHandler(&app, agentruntime.RuntimeEvent{Payload: result})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	var toolMsg *providertypes.Message
	for i := len(app.activeMessages) - 1; i >= 0; i-- {
		if app.activeMessages[i].Role == roleTool {
			toolMsg = &app.activeMessages[i]
			break
		}
	}
	if toolMsg == nil || messageText(*toolMsg) != "ok" {
		t.Fatalf("unexpected tool message list: %#v", app.activeMessages)
	}
}

func TestRuntimeEventToolResultHandlerError(t *testing.T) {
	app, _ := newTestApp(t)
	result := tools.ToolResult{
		Name:       "bash",
		Content:    "boom",
		IsError:    true,
		ToolCallID: "tool-2",
	}
	handled := runtimeEventToolResultHandler(&app, agentruntime.RuntimeEvent{Payload: result})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	if app.state.StatusText != statusToolError {
		t.Fatalf("expected tool error status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventAgentDoneHandlerAppendsMessage(t *testing.T) {
	app, _ := newTestApp(t)
	payload := providertypes.Message{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")}}
	handled := runtimeEventAgentDoneHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected message appended")
	}
}

func TestParseFenceOpenLine(t *testing.T) {
	info, ok := parseFenceOpenLine("```go")
	if !ok || info != "go" {
		t.Fatalf("expected fence info, got %q ok=%v", info, ok)
	}
	info, ok = parseFenceOpenLine(" not a fence")
	if ok || info != "" {
		t.Fatalf("expected no fence")
	}
}

func TestIsFenceCloseLine(t *testing.T) {
	if !isFenceCloseLine("```") {
		t.Fatalf("expected fence close")
	}
	if isFenceCloseLine("```go") {
		t.Fatalf("expected not fence close")
	}
}

func TestIsIndentedCodeLine(t *testing.T) {
	if !isIndentedCodeLine("\tcode") {
		t.Fatalf("expected tab-indented code")
	}
	if !isIndentedCodeLine("    code") {
		t.Fatalf("expected space-indented code")
	}
	if isIndentedCodeLine("code") {
		t.Fatalf("expected non-indented line")
	}
}

func TestTrimCodeIndent(t *testing.T) {
	if got := trimCodeIndent("\tcode"); got != "code" {
		t.Fatalf("expected trimmed tab indent, got %q", got)
	}
	if got := trimCodeIndent("    code"); got != "code" {
		t.Fatalf("expected trimmed space indent, got %q", got)
	}
	if got := trimCodeIndent("code"); got != "code" {
		t.Fatalf("expected unchanged line, got %q", got)
	}
}

func TestSplitMarkdownSegmentsFenced(t *testing.T) {
	content := "hello\n```go\nfmt.Println(\"ok\")\n```\nworld"
	segments := splitMarkdownSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	if segments[1].Kind != markdownSegmentCode || segments[1].Code == "" {
		t.Fatalf("expected code segment")
	}
}

func TestSplitMarkdownSegmentsIndented(t *testing.T) {
	content := "hello\n    code line\nworld"
	segments := splitMarkdownSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	foundCode := false
	for _, seg := range segments {
		if seg.Kind == markdownSegmentCode && seg.Code != "" {
			foundCode = true
		}
	}
	if !foundCode {
		t.Fatalf("expected indented code segment")
	}
}

func TestSplitIndentedCodeSegmentsDoesNotGuessByKeywords(t *testing.T) {
	content := "func main() {\nreturn 1\n}\nplain text"
	segments := splitIndentedCodeSegments(content)
	if len(segments) != 1 {
		t.Fatalf("expected plain text segment only, got %d", len(segments))
	}
	if segments[0].Kind != markdownSegmentText {
		t.Fatalf("expected text segment, got kind=%v", segments[0].Kind)
	}
}

func TestSplitMarkdownSegmentsMarkdownSyntaxNotMisclassifiedAsCode(t *testing.T) {
	content := "# Title\n- item one\n- item two\n\n**bold** and `inline`"
	segments := splitMarkdownSegments(content)
	if len(segments) != 1 {
		t.Fatalf("expected markdown to stay as one text segment, got %d", len(segments))
	}
	if segments[0].Kind != markdownSegmentText {
		t.Fatalf("expected text segment, got kind=%v", segments[0].Kind)
	}
}

func TestExtractFencedCodeBlocks(t *testing.T) {
	content := "text\n```go\nfmt.Println(\"ok\")\n```\nend"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 1 || blocks[0] == "" {
		t.Fatalf("expected one code block")
	}
}

func TestTokenRangeFirstToken(t *testing.T) {
	start, end, token, ok := tokenRange("  /help now", tokenSelectorFirst)
	if !ok {
		t.Fatalf("expected token range to be found")
	}
	if token != "/help" {
		t.Fatalf("expected first token to be /help, got %q", token)
	}
	if start < 0 || end <= start {
		t.Fatalf("expected valid range, got %d-%d", start, end)
	}
}

func TestTokenRangeLastToken(t *testing.T) {
	start, end, token, ok := tokenRange("one two three", tokenSelectorLast)
	if !ok {
		t.Fatalf("expected token range to be found")
	}
	if token != "three" {
		t.Fatalf("expected last token to be three, got %q", token)
	}
	if start < 0 || end <= start {
		t.Fatalf("expected valid range, got %d-%d", start, end)
	}
}

func TestCollectFileSuggestionMatches(t *testing.T) {
	candidates := []string{"README.md", "docs/guide.md", "internal/app.go"}
	matches := collectFileSuggestionMatches("read", candidates, 2)
	if len(matches) == 0 {
		t.Fatalf("expected matches for read")
	}
}

func TestViewSmallWindow(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 60
	app.height = 20

	view := app.View()
	if !strings.Contains(view, "Window too small") {
		t.Fatalf("expected small window warning, got %q", view)
	}
	if !strings.Contains(view, "60x30") {
		t.Fatalf("expected 60x30 minimum size hint, got %q", view)
	}
}

func TestComputeLayoutStackedAndWide(t *testing.T) {
	app, _ := newTestApp(t)

	app.width = 90
	app.height = 40
	layout := app.computeLayout()
	if layout.contentWidth <= 0 {
		t.Fatalf("expected positive content width, got %+v", layout)
	}

	app.width = 140
	app.height = 40
	layout = app.computeLayout()
	if layout.contentWidth <= 0 {
		t.Fatalf("expected positive content width, got %+v", layout)
	}
}

func TestStatusBadgeVariants(t *testing.T) {
	app, _ := newTestApp(t)

	errorBadge := app.statusBadge("Error occurred")
	if strings.TrimSpace(errorBadge) == "" {
		t.Fatalf("expected error badge to render")
	}

	cancelBadge := app.statusBadge("Canceled")
	if strings.TrimSpace(cancelBadge) == "" {
		t.Fatalf("expected cancel badge to render")
	}

	app.state.IsAgentRunning = true
	runningBadge := app.statusBadge("Running")
	if strings.TrimSpace(runningBadge) == "" {
		t.Fatalf("expected running badge to render")
	}

	app.state.IsAgentRunning = false
	okBadge := app.statusBadge("Ready")
	if strings.TrimSpace(okBadge) == "" {
		t.Fatalf("expected success badge to render")
	}
}

func TestHelpHeightAndRenderHelp(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120

	app.state.ShowHelp = false
	helpHeight := app.helpHeight(80)
	if helpHeight <= 0 {
		t.Fatalf("expected help height to be positive")
	}
	rendered := app.renderHelp(80)
	if strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected renderHelp output")
	}

	app.state.ShowHelp = true
	helpHeight = app.helpHeight(80)
	if helpHeight <= 0 {
		t.Fatalf("expected help height to be positive when help is shown")
	}
}

func TestNewWithBootstrapSuccess(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   provider.Name,
				Name: provider.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: provider.Model, Name: provider.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	runtime := newStubRuntime()
	app, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: stubProviderService{providers: providers, models: models},
	})
	if err != nil {
		t.Fatalf("NewWithBootstrap() error = %v", err)
	}

	cmd := app.Init()
	if cmd == nil {
		t.Fatalf("expected Init() to return command")
	}
}

func TestNewWithBootstrapMissingDependencies(t *testing.T) {
	cfg := newDefaultAppConfig()

	manager := config.NewManager(config.NewLoader(t.TempDir(), cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   manager,
		Runtime:         nil,
		ProviderService: stubProviderService{},
	}); err == nil {
		t.Fatalf("expected error for nil runtime")
	}

	if _, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   nil,
		Runtime:         newStubRuntime(),
		ProviderService: stubProviderService{},
	}); err == nil {
		t.Fatalf("expected error for nil config manager")
	}
}

func TestStartupScreenTickDoesNotAnimateLogo(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true

	model, cmd := app.Update(tickMsg(time.Unix(1_700_000_000, 0)))
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected no follow-up tick when only startup screen is visible")
	}
}

func TestNewUsesBootstrap(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   provider.Name,
				Name: provider.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: provider.Model, Name: provider.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	app, err := New(cfg, manager, newStubRuntime(), stubProviderService{providers: providers, models: models})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.state.CurrentProvider == "" {
		t.Fatalf("expected CurrentProvider to be set")
	}
}

func TestRuntimeEventUserMessageHandler(t *testing.T) {
	app, _ := newTestApp(t)
	event := agentruntime.RuntimeEvent{RunID: "run-1"}
	handled := runtimeEventUserMessageHandler(&app, event)
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.ActiveRunID != "run-1" {
		t.Fatalf("expected run id to be set")
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected thinking status")
	}
}

func TestRuntimeEventUserMessageHandlerDeduplicatesByRunID(t *testing.T) {
	app, _ := newTestApp(t)
	payload := providertypes.Message{
		Role:  roleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("same content")},
	}
	event := agentruntime.RuntimeEvent{RunID: "run-1", Payload: payload}

	handled := runtimeEventUserMessageHandler(&app, event)
	if !handled {
		t.Fatalf("expected first user message to be rendered")
	}
	if len(app.activeMessages) != 1 {
		t.Fatalf("expected one user message, got %d", len(app.activeMessages))
	}

	handled = runtimeEventUserMessageHandler(&app, event)
	if handled {
		t.Fatalf("expected duplicate run id to be ignored")
	}
	if len(app.activeMessages) != 1 {
		t.Fatalf("expected one user message after duplicate event, got %d", len(app.activeMessages))
	}

	handled = runtimeEventUserMessageHandler(&app, agentruntime.RuntimeEvent{RunID: "run-2", Payload: payload})
	if !handled {
		t.Fatalf("expected same content with new run id to be rendered")
	}
	if len(app.activeMessages) != 2 {
		t.Fatalf("expected two user messages after new run id, got %d", len(app.activeMessages))
	}
}

func TestRuntimeEventRunContextHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.RuntimeRunContextPayload{
		Provider: "p1",
		Model:    "m1",
		Workdir:  "/tmp",
	}
	event := agentruntime.RuntimeEvent{RunID: "run-2", SessionID: "s1", Payload: payload}
	handled := runtimeEventRunContextHandler(&app, event)
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentProvider != "p1" || app.state.CurrentModel != "m1" {
		t.Fatalf("expected provider/model to update")
	}
}

func TestUpdateCtrlVPasteWithoutImageDoesNotMutateInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("seed")
	app.state.InputText = "seed"

	originalReadImage := readClipboardImage
	originalReadText := readClipboardText
	defer func() {
		readClipboardImage = originalReadImage
		readClipboardText = originalReadText
	}()
	readClipboardImage = func() ([]byte, error) { return nil, errors.New("clipboard image unavailable") }
	readClipboardText = func() (string, error) { return "", errors.New("clipboard text unavailable") }

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != "seed" {
		t.Fatalf("expected Ctrl+V without image payload to keep input unchanged, got %q", app.input.Value())
	}
}

func TestUpdateCtrlVPasteInsertsRawTextAndSuppressesTerminalEcho(t *testing.T) {
	app, _ := newTestApp(t)
	pasted := "line-1\nline-2\nline-3\nline-4\nline-5\nline-6"

	originalReadImage := readClipboardImage
	originalReadText := readClipboardText
	defer func() {
		readClipboardImage = originalReadImage
		readClipboardText = originalReadText
	}()
	readClipboardImage = func() ([]byte, error) { return nil, errors.New("no image") }
	readClipboardText = func() (string, error) { return pasted, nil }

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	expected := "[paste 6 LINE]"
	if app.input.Value() != expected {
		t.Fatalf("expected Ctrl+V to insert summarized pasted text %q immediately, got %q", expected, app.input.Value())
	}
	if app.pendingCtrlVPasteEcho == "" {
		t.Fatalf("expected pending ctrl+v echo buffer to be populated")
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line-1\nline-2\n"), Paste: false})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != expected {
		t.Fatalf("expected echoed raw chunk to be swallowed, got %q", app.input.Value())
	}
}

func TestUpdateSingleRuneInputDoesNotPrimeFromClipboard(t *testing.T) {
	app, _ := newTestApp(t)
	clipboardText := strings.Join([]string{
		"xackage tui",
		"import (",
		`"strings"`,
		`"time"`,
		")",
	}, "\n")

	originalReadText := readClipboardText
	defer func() { readClipboardText = originalReadText }()
	readClipboardText = func() (string, error) { return clipboardText, nil }

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("p"),
		Paste: false,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if app.input.Value() != "p" {
		t.Fatalf("expected single-rune input to stay as regular typing, got %q", app.input.Value())
	}
}

func TestUpdateEnterDoesNotSendWhilePasteEchoPending(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	app.nowFn = func() time.Time { return now }
	app.pendingCtrlVPasteEcho = "long pasted payload"
	app.pendingCtrlVEchoUntil = now.Add(time.Second)
	app.input.SetValue("long pasted payload")
	app.state.InputText = app.input.Value()

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if len(runtime.prepareInputs) != 0 {
		t.Fatalf("expected enter to avoid send while paste echo pending, got %d submits", len(runtime.prepareInputs))
	}
}

func TestUpdateCtrlVPasteSuppressesWhitespaceMismatchedEchoChunk(t *testing.T) {
	app, _ := newTestApp(t)
	pasted := strings.Join([]string{
		"lastInputEd\t todoFilter\t todoFilter",
		"todoSelectedIndex\t int",
		"todoPanelVisible\t bool",
		"inputBurstCount\t int",
		"pasteMode\t bool",
	}, "\n")

	originalReadImage := readClipboardImage
	originalReadText := readClipboardText
	defer func() {
		readClipboardImage = originalReadImage
		readClipboardText = originalReadText
	}()
	readClipboardImage = func() ([]byte, error) { return nil, errors.New("no image") }
	readClipboardText = func() (string, error) { return pasted, nil }

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != "[paste 5 LINE]" {
		t.Fatalf("expected immediate summarized pasted text to match clipboard content, got %q", app.input.Value())
	}

	// Simulate terminal echo chunk where tabs are expanded to spaces.
	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("lastInputEd    todoFilter"), Paste: false})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != "[paste 5 LINE]" {
		t.Fatalf("expected whitespace-mismatched echoed raw chunk to be swallowed, got %q", app.input.Value())
	}
}

func TestUpdatePasteEventKeepsLongTextAsInput(t *testing.T) {
	app, _ := newTestApp(t)

	lineCount := 5
	lines := make([]string, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i+1))
	}
	pasted := strings.Join(lines, "\n")

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	expected := "[paste 5 LINE]"
	if app.input.Value() != expected {
		t.Fatalf("expected pasted clipboard text to stay summarized as %q, got %q", expected, app.input.Value())
	}
}

func TestUpdatePasteEventSuppressesDuplicateSummaryToken(t *testing.T) {
	app, _ := newTestApp(t)
	now := time.Unix(1_720_000_000, 0)
	app.nowFn = func() time.Time { return now }

	pasted := "line-1\nline-2\nline-3\nline-4\nline-5"
	msg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	}

	model, cmd := app.Update(msg)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	model, cmd = app.Update(msg)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if got := app.input.Value(); got != "[paste 5 LINE]" {
		t.Fatalf("expected duplicate paste event to keep a single summary token, got %q", got)
	}
	if len(app.pendingTextPastes) != 1 {
		t.Fatalf("expected one pending text paste entry after duplicate suppression, got %d", len(app.pendingTextPastes))
	}
}

func TestUpdatePasteEventSuppressesDuplicateSummaryTokenAcrossContentVariants(t *testing.T) {
	app, _ := newTestApp(t)
	now := time.Unix(1_720_000_000, 0)
	app.nowFn = func() time.Time { return now }

	first := "line-1\nline-2\nline-3\nline-4\nline-5"
	second := "line-1 \nline-2\nline-3\nline-4\nline-5"

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(first),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	now = now.Add(2 * time.Second)
	model, cmd = app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(second),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if got := app.input.Value(); got != "[paste 5 LINE]" {
		t.Fatalf("expected duplicate paste variants to keep a single summary token, got %q", got)
	}
	if len(app.pendingTextPastes) != 1 {
		t.Fatalf("expected one pending text paste entry for duplicate variants, got %d", len(app.pendingTextPastes))
	}
}

func TestUpdatePasteEventSingleSessionPinsSingleTokenAcrossChunks(t *testing.T) {
	app, _ := newTestApp(t)
	now := time.Unix(1_720_000_000, 0)
	app.nowFn = func() time.Time { return now }

	makeLines := func(prefix string, n int) string {
		lines := make([]string, 0, n)
		for i := 0; i < n; i++ {
			lines = append(lines, fmt.Sprintf("%s-%d", prefix, i+1))
		}
		return strings.Join(lines, "\n")
	}

	first := makeLines("big", 386)
	second := makeLines("small", 3)
	third := makeLines("tiny", 2)

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(first),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	model, cmd = app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(second),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	model, cmd = app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(third),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if got := app.input.Value(); got != "[paste 386 LINE]" {
		t.Fatalf("expected one pinned token in single paste session, got %q", got)
	}
	if len(app.pendingTextPastes) != 1 {
		t.Fatalf("expected one pending paste record for pinned session token, got %d", len(app.pendingTextPastes))
	}
}

func TestUpdatePasteEventSuppressesDuplicateSingleLineContent(t *testing.T) {
	app, _ := newTestApp(t)
	now := time.Unix(1_720_000_000, 0)
	app.nowFn = func() time.Time { return now }

	msg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("single-line-paste"),
		Paste: true,
	}

	model, cmd := app.Update(msg)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	model, cmd = app.Update(msg)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if got := app.input.Value(); got != "single-line-paste" {
		t.Fatalf("expected duplicate single-line paste to be suppressed, got %q", got)
	}
}

func TestUpdateSingleLineCtrlVPasteEnterDoesNotReinsertPaste(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Unix(1_720_000_000, 0)
	app.nowFn = func() time.Time { return now }

	pasted := "single-line-paste"
	originalReadImage := readClipboardImage
	originalReadText := readClipboardText
	defer func() {
		readClipboardImage = originalReadImage
		readClipboardText = originalReadText
	}()
	readClipboardImage = func() ([]byte, error) { return nil, errors.New("no image") }
	readClipboardText = func() (string, error) { return pasted, nil }

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if got := app.input.Value(); got != pasted {
		t.Fatalf("expected single-line Ctrl+V paste inserted once, got %q", got)
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if got := app.input.Value(); got != pasted {
		t.Fatalf("expected Enter during echo window not to reinsert paste, got %q", got)
	}
	if len(runtime.prepareInputs) != 0 {
		t.Fatalf("expected Enter during echo window not to submit yet, got %d submits", len(runtime.prepareInputs))
	}

	now = now.Add(3 * time.Second)
	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if len(runtime.prepareInputs) != 1 || runtime.prepareInputs[0].Text != pasted {
		t.Fatalf("expected delayed Enter to submit single-line paste once, got %+v", runtime.prepareInputs)
	}
}

func TestUpdatePasteEventConvertsClipboardFilesAsReferences(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	first := filepath.Join(root, "docs", "a.txt")
	second := filepath.Join(root, "README.md")
	if err := os.MkdirAll(filepath.Dir(first), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(first, []byte("a"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(first + "\n" + second),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	input := app.input.Value()
	if !strings.Contains(input, "@docs/a.txt") || !strings.Contains(input, "@README.md") {
		t.Fatalf("expected pasted files to become references, got %q", input)
	}
}

func TestUpdatePasteEventSingleImagePathAsAttachment(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	imagePath := filepath.Join(root, "clip.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(imagePath),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one image attachment from pasted path, got %d", app.getImageAttachmentCount())
	}
	if app.input.Value() != "" {
		t.Fatalf("expected input to stay empty when pasting image path as attachment, got %q", app.input.Value())
	}
}

func TestUpdateLongPasteSendsRawText(t *testing.T) {
	app, runtime := newTestApp(t)

	lineCount := 6
	lines := make([]string, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i+1))
	}
	pasted := strings.Join(lines, "\n")

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	})
	var loadReadyMsg tea.Msg
	if cmd != nil {
		emitted := cmd()
		switch msg := emitted.(type) {
		case pastedTextLoadReadyMsg:
			loadReadyMsg = msg
		case tea.BatchMsg:
			for _, child := range msg {
				if child == nil {
					continue
				}
				if ready, ok := child().(pastedTextLoadReadyMsg); ok {
					loadReadyMsg = ready
					break
				}
			}
		}
	}
	app = model.(App)

	if app.input.Value() != "[paste 6 LINE]" {
		t.Fatalf("expected summarized pasted text in input, got %q", app.input.Value())
	}
	if app.state.StatusText != statusLoadingPastedText {
		t.Fatalf("expected loading status immediately after long paste detection, got %q", app.state.StatusText)
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.StatusText != statusLoadingPastedText {
		t.Fatalf("expected loading status before resolving pasted placeholders, got %q", app.state.StatusText)
	}
	if len(runtime.prepareInputs) != 0 {
		t.Fatalf("expected send to defer while pasted placeholders are loading")
	}
	if loadReadyMsg == nil {
		t.Fatalf("expected deferred load command message before send")
	}
	if cmd != nil {
		t.Fatalf("expected Enter while loading pasted text to wait for existing load completion")
	}

	model, cmd = app.Update(loadReadyMsg)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input after send, got %d", len(runtime.prepareInputs))
	}
	if runtime.prepareInputs[0].Text != pasted {
		t.Fatalf("expected raw pasted text in prepare input")
	}
}

func TestUpdateEnterWithPastePlaceholderShowsLoadingAndAutoSends(t *testing.T) {
	app, runtime := newTestApp(t)
	pasted := "line-1\nline-2\nline-3\nline-4\nline-5\nline-6"

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	})
	var delayed tea.Msg
	if cmd != nil {
		emitted := cmd()
		switch msg := emitted.(type) {
		case pastedTextLoadReadyMsg:
			delayed = msg
		case tea.BatchMsg:
			for _, child := range msg {
				if child == nil {
					continue
				}
				if ready, ok := child().(pastedTextLoadReadyMsg); ok {
					delayed = ready
					break
				}
			}
		}
	}
	app = model.(App)

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.StatusText != statusLoadingPastedText {
		t.Fatalf("expected loading status while resolving pasted text, got %q", app.state.StatusText)
	}
	if !app.loadingPastedText || !app.pendingSendAfterPasteLoad {
		t.Fatalf("expected loading flags to be enabled")
	}
	if delayed == nil {
		t.Fatalf("expected deferred load-ready message")
	}
	if cmd != nil {
		t.Fatalf("expected no extra load-ready command from Enter while paste loading is already active")
	}

	model, cmd = app.Update(delayed)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected deferred send to run once, got %d", len(runtime.prepareInputs))
	}
	if runtime.prepareInputs[0].Text != pasted {
		t.Fatalf("expected deferred send text to match pasted content")
	}
	if app.loadingPastedText || app.pendingSendAfterPasteLoad {
		t.Fatalf("expected loading flags cleared after deferred send")
	}
}

func TestUpdatePasteLoadingClearsAfterLoadReadyWithoutSend(t *testing.T) {
	app, _ := newTestApp(t)
	pasted := "line-1\nline-2\nline-3\nline-4\nline-5\nline-6"

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	})
	var delayed tea.Msg
	if cmd != nil {
		emitted := cmd()
		switch msg := emitted.(type) {
		case pastedTextLoadReadyMsg:
			delayed = msg
		case tea.BatchMsg:
			for _, child := range msg {
				if child == nil {
					continue
				}
				if ready, ok := child().(pastedTextLoadReadyMsg); ok {
					delayed = ready
					break
				}
			}
		}
	}
	app = model.(App)
	if app.state.StatusText != statusLoadingPastedText {
		t.Fatalf("expected loading status after long paste, got %q", app.state.StatusText)
	}
	if delayed == nil {
		t.Fatalf("expected load-ready command after long paste")
	}

	model, cmd = app.Update(delayed)
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.loadingPastedText {
		t.Fatalf("expected loading flag cleared when load-ready event arrives")
	}
	if app.state.StatusText != statusReady {
		t.Fatalf("expected status to return to ready after load completion, got %q", app.state.StatusText)
	}
}

func TestUpdatePastedTextLoadReadyReschedulesWhenPasteTxnActive(t *testing.T) {
	app, _ := newTestApp(t)
	app.loadingPastedText = true
	app.pasteTxnActive = true

	model, cmd := app.Update(pastedTextLoadReadyMsg{})
	app = model.(App)
	if !app.loadingPastedText {
		t.Fatalf("expected loading state to remain when paste txn is active")
	}
	if cmd == nil {
		t.Fatalf("expected re-schedule command when pasted text is not ready")
	}
}

func TestUpdatePastedTextLoadReadyAppendsSeparatorBeforeDeferredSend(t *testing.T) {
	app, runtime := newTestApp(t)
	app.loadingPastedText = true
	app.pendingSendAfterPasteLoad = true
	app.input.SetValue("hello")
	app.state.InputText = app.input.Value()

	model, cmd := app.Update(pastedTextLoadReadyMsg{})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected deferred send to run once, got %d", len(runtime.prepareInputs))
	}
	if runtime.prepareInputs[0].Text != "hello" {
		t.Fatalf("unexpected deferred send payload: %q", runtime.prepareInputs[0].Text)
	}
	if app.loadingPastedText || app.pendingSendAfterPasteLoad {
		t.Fatalf("expected loading flags cleared after deferred send")
	}
}

func TestPasteSessionHelpers(t *testing.T) {
	app, _ := newTestApp(t)
	now := time.Now()

	app.extendPasteSession(now, 0)
	if app.pasteSessionStartedAt.IsZero() || app.pasteSessionUntil.IsZero() {
		t.Fatalf("expected paste session window to be initialized")
	}
	if !app.inPasteSessionWindow(now.Add(200 * time.Millisecond)) {
		t.Fatalf("expected to stay within paste session window")
	}

	app.markPasteSessionToken(" token ")
	if !app.pasteTxnTokenInjected || app.pasteTxnInjectedToken != "token" {
		t.Fatalf("expected token to be normalized and recorded")
	}

	app.pendingTextPastes = []pendingTextPaste{{Token: "token", Loaded: false}}
	if token, ok := app.reuseSinglePasteSessionToken(now.Add(100 * time.Millisecond)); !ok || token != "token" {
		t.Fatalf("expected to reuse pinned paste token within session window")
	}

	app.pasteSessionUntil = now.Add(100 * time.Millisecond)
	if _, ok := app.reuseSinglePasteSessionToken(now.Add(2 * time.Second)); ok {
		t.Fatalf("expected token reuse to stop outside paste session window")
	}

	app.pendingCtrlVPasteEcho = "echo"
	if app.shouldCompletePastedTextLoading(now.Add(100 * time.Millisecond)) {
		t.Fatalf("expected pending ctrl+v echo to block completion")
	}
	app.pendingCtrlVPasteEcho = ""
	if !app.shouldCompletePastedTextLoading(now.Add(200 * time.Millisecond)) {
		t.Fatalf("expected completion after ctrl+v echo settles")
	}
}

func TestUpdateDirectPasteEventKeepsIncomingPayload(t *testing.T) {
	app, _ := newTestApp(t)
	pasted := "a\nb\nc\nd\ne"

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: true,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	expected := "[paste 5 LINE]"
	if app.input.Value() != expected {
		t.Fatalf("expected direct paste event to keep incoming summarized text %q, got %q", expected, app.input.Value())
	}
}

func TestUpdatePasteLikeRunesWithoutPasteFlagStayRaw(t *testing.T) {
	app, _ := newTestApp(t)
	pasted := "line-1\nline-2\nline-3\nline-4\nline-5"

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(pasted),
		Paste: false,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	model, cmd = app.Update(pasteTxnFlushMsg{Version: app.pasteTxnVersion})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	expected := "[paste 5 LINE]"
	if app.input.Value() != expected {
		t.Fatalf("expected paste-like rune burst to stay summarized as %q, got %q", expected, app.input.Value())
	}
}

func TestUpdatePasteLikeRunesChunksAppendRawText(t *testing.T) {
	app, _ := newTestApp(t)

	chunk := "a1\na2\na3\na4"
	for i := 0; i < 4; i++ {
		model, cmd := app.Update(tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune(chunk),
			Paste: false,
		})
		if cmd != nil {
			_ = cmd()
		}
		app = model.(App)
	}
	model, cmd := app.Update(pasteTxnFlushMsg{Version: app.pasteTxnVersion})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	expected := "[paste 13 LINE]"
	if app.input.Value() != expected {
		t.Fatalf("expected chunked paste to append summarized text %q, got %q", expected, app.input.Value())
	}
}

func TestUpdatePasteTxnAvoidsPartialPrefixRenderBeforeRawClipboardInsert(t *testing.T) {
	app, _ := newTestApp(t)
	clipboardText := strings.Join([]string{
		"lastInputEd\ttodoFilter\ttodoFilter",
		"todoSelectedIndex\tint",
		"todoPanelVisible\tbool",
		"inputBurstCount\tint",
		"pasteMode\tbool",
		"pasteSessionBase\tstring",
		"activeMessages\t[]providertypes.Message",
		"activities\t[]tuistate.ActivityEntry",
		"todoItems\t[]todoViewItem",
		"todoFilter\ttodoFilter",
	}, "\n")
	originalReadText := readClipboardText
	defer func() { readClipboardText = originalReadText }()
	readClipboardText = func() (string, error) { return clipboardText, nil }

	model, cmd := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("lastInputEd"),
		Paste: false,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != "[paste 10 LINE]" {
		t.Fatalf("expected first chunk to prime clipboard summarized payload insert, got %q", app.input.Value())
	}

	rest := "\ttodoFilter\n\ttodoSelectedIndex\n\ttodoPanelVisible\n\ttodoItems"
	model, cmd = app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(rest),
		Paste: false,
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if app.input.Value() != "[paste 10 LINE]" {
		t.Fatalf("expected full clipboard payload to remain summarized, got %q", app.input.Value())
	}
}

func TestMaybeCollapseLongPasteAfterInputFallbackCollapses(t *testing.T) {
	app, _ := newTestApp(t)
	app.pasteMode = true

	before := "prefix "
	inserted := strings.Repeat("A", 120) + "\n" + strings.Repeat("B", 120) + "\n" + strings.Repeat("C", 120) + "\n" + strings.Repeat("D", 120)
	after := before + inserted
	app.input.SetValue(after)
	app.state.InputText = after

	app.maybeCollapseLongPasteAfterInput(before, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if app.input.Value() != "prefix [paste 4 LINE]" {
		t.Fatalf("expected fallback collapse to replace long multiline paste, got %q", app.input.Value())
	}
}

func TestUpdateSendWhileBusyQueuesInterventionAndDispatchesAfterCancel(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.IsAgentRunning = true
	app.state.ActiveSessionID = "s-1"
	app.state.ActiveRunID = "run-old"
	app.input.SetValue("follow up request")
	app.state.InputText = "follow up request"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if !runtime.cancelInvoked {
		t.Fatalf("expected busy send to invoke runtime cancellation")
	}
	if app.queuedIntervention == nil || app.queuedIntervention.Text != "follow up request" {
		t.Fatalf("expected queued intervention payload, got %+v", app.queuedIntervention)
	}
	if app.state.StatusText != statusInterventionCanceling {
		t.Fatalf("expected intervention canceling status, got %q", app.state.StatusText)
	}
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if !strings.Contains(plain, messageTagUser+" queue") {
		t.Fatalf("expected queued intervention marker in transcript, got %q", plain)
	}
	if !strings.Contains(plain, "follow up request") {
		t.Fatalf("expected queued intervention text in transcript, got %q", plain)
	}

	model, cmd = app.Update(RuntimeMsg{Event: agentruntime.RuntimeEvent{
		Type:      agentruntime.EventRunCanceled,
		SessionID: "s-1",
		RunID:     "run-old",
	}})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, child := range batch {
					if child != nil {
						_ = child()
						if len(runtime.prepareInputs) > 0 {
							break
						}
					}
				}
			}
		}
	}
	app = model.(App)

	if app.queuedIntervention != nil {
		dispatchCmd := app.dispatchQueuedInterventionIfIdle()
		if dispatchCmd != nil {
			_ = dispatchCmd()
		}
	}
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one queued intervention submit, got %d", len(runtime.prepareInputs))
	}
	if runtime.prepareInputs[0].Text != "follow up request" {
		t.Fatalf("expected queued intervention text to submit, got %q", runtime.prepareInputs[0].Text)
	}
	plain = copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if strings.Contains(plain, messageTagUser+" queue") {
		t.Fatalf("expected queued intervention marker cleared after dispatch, got %q", plain)
	}
	if !app.state.IsAgentRunning {
		t.Fatalf("expected app to re-enter running state for queued intervention")
	}
}

func TestUpdateEnterWhileRunningTreatsRecentPasteAsNewline(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	app.nowFn = func() time.Time { return now }
	app.state.IsAgentRunning = true
	app.input.SetValue("line one")
	app.state.InputText = "line one"
	app.pasteMode = true
	app.lastPasteLikeAt = now
	app.lastInputEditAt = now

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if runtime.cancelInvoked {
		t.Fatalf("expected Enter to stay in composer newline path, but runtime cancel was invoked")
	}
	if app.queuedIntervention != nil {
		t.Fatalf("expected no queued intervention when Enter is treated as newline")
	}
	if got := app.input.Value(); got != "line one\n" {
		t.Fatalf("expected newline inserted while running, got %q", got)
	}
}

func TestUpdateEnterSessionOpensSessionPicker(t *testing.T) {
	app, runtime := newTestApp(t)
	runtime.listSessions = []agentsession.Summary{
		{ID: "s1", Title: "Session 1", UpdatedAt: time.Now()},
	}
	app.input.SetValue("/session")
	app.state.InputText = "/session"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.state.ActivePicker != pickerSession {
		t.Fatalf("expected session picker to open")
	}
	if app.state.StatusText != statusChooseSession {
		t.Fatalf("expected status %q, got %q", statusChooseSession, app.state.StatusText)
	}
}

func TestUpdateEnterImageReferencePath(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("@image:/path/does-not-exist.png")
	app.state.InputText = "@image:/path/does-not-exist.png"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.input.Value() != "" {
		t.Fatalf("expected input to be reset after image reference handling")
	}
	if strings.TrimSpace(app.state.StatusText) == "" {
		t.Fatalf("expected status text to reflect image reference failure")
	}
}

func TestUpdateSendWithUnsupportedImageInputDoesNotPreBlock(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingImageAttachments = []pendingImageAttachment{
		{Name: "a.png", MimeType: "image/png", Path: "/tmp/a.png", Size: 1},
	}
	app.providerSvc = stubProviderService{
		providers: []configstate.ProviderOption{{ID: app.state.CurrentProvider, Name: app.state.CurrentProvider}},
		models: []providertypes.ModelDescriptor{{
			ID:   app.state.CurrentModel,
			Name: app.state.CurrentModel,
			CapabilityHints: providertypes.ModelCapabilityHints{
				ImageInput: providertypes.ModelCapabilityStateUnsupported,
			},
		}},
	}
	app.input.SetValue("hello")
	app.state.InputText = "hello"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if !app.state.IsAgentRunning {
		t.Fatalf("expected send not to be pre-blocked by model capability hints")
	}
	if app.hasImageAttachments() {
		t.Fatalf("expected pending image attachments to be cleared after send")
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("unexpected status text: %q", app.state.StatusText)
	}
	if app.input.Value() != "" || app.state.InputText != "" {
		t.Fatalf("expected input to reset after send, got input=%q state=%q", app.input.Value(), app.state.InputText)
	}
	if len(runtime.prepareInputs) != 1 || len(runtime.prepareInputs[0].Images) != 1 {
		t.Fatalf("expected image to flow into prepare pipeline, got %+v", runtime.prepareInputs)
	}
}

func TestUpdateSendWithImageAttachmentsUsesPreparePipeline(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingImageAttachments = []pendingImageAttachment{
		{Name: "a.png", MimeType: "image/png", Path: "/tmp/a.png", Size: 1},
	}
	app.providerSvc = stubProviderService{
		providers: []configstate.ProviderOption{{ID: app.state.CurrentProvider, Name: app.state.CurrentProvider}},
		models: []providertypes.ModelDescriptor{{
			ID:   app.state.CurrentModel,
			Name: app.state.CurrentModel,
			CapabilityHints: providertypes.ModelCapabilityHints{
				ImageInput: providertypes.ModelCapabilityStateSupported,
			},
		}},
	}
	app.input.SetValue("hello")
	app.state.InputText = "hello"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if !app.state.IsAgentRunning {
		t.Fatalf("expected send to enter running state")
	}
	if app.hasImageAttachments() {
		t.Fatalf("expected pending image attachments to be cleared after send")
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("unexpected status text: %q", app.state.StatusText)
	}
	if app.input.Value() != "" {
		t.Fatalf("expected input to be reset after send, got %q", app.input.Value())
	}
	if app.state.InputText != "" {
		t.Fatalf("expected state input text to reset after send, got %q", app.state.InputText)
	}
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input, got %+v", runtime.prepareInputs)
	}
	if len(runtime.prepareInputs[0].Images) != 1 || runtime.prepareInputs[0].Images[0].MimeType != "image/png" {
		t.Fatalf("expected image metadata to flow through prepare input, got %+v", runtime.prepareInputs[0].Images)
	}
	if len(runtime.runInputs) != 1 {
		t.Fatalf("expected one runtime run input, got %+v", runtime.runInputs)
	}
}

func TestUpdateSendAmpersandInputAsPlainText(t *testing.T) {
	app, runtime := newTestApp(t)
	app.input.SetValue("& git status")
	app.state.InputText = "& git status"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if !app.state.IsAgentRunning {
		t.Fatalf("expected ampersand-prefixed text to enter normal send flow")
	}
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input, got %+v", runtime.prepareInputs)
	}
	if runtime.prepareInputs[0].Text != "& git status" {
		t.Fatalf("expected ampersand text to be forwarded unchanged, got %q", runtime.prepareInputs[0].Text)
	}
}

func TestConfigureStartupWakeInputRejectsEmptyText(t *testing.T) {
	app, _ := newTestApp(t)

	if err := app.ConfigureStartupWakeInput("   ", ""); err == nil {
		t.Fatal("expected empty startup wake input error")
	}
}

func TestUpdateStartupWakeSubmitMsgTriggersSubmitPipeline(t *testing.T) {
	app, runtime := newTestApp(t)
	workdir := t.TempDir()

	if err := app.ConfigureStartupWakeInput("hello from wake", workdir); err != nil {
		t.Fatalf("ConfigureStartupWakeInput() error = %v", err)
	}
	if app.startupWakeSubmitInput == nil {
		t.Fatal("expected startup wake input to be configured")
	}

	model, cmd := app.Update(startupWakeSubmitMsg{Input: *app.startupWakeSubmitInput})
	if cmd == nil {
		t.Fatal("expected startup wake submit command")
	}
	_ = cmd()
	app = model.(App)

	if app.startupWakeSubmitInput != nil {
		t.Fatal("expected startup wake input to be cleared after first submit")
	}
	if !app.state.IsAgentRunning {
		t.Fatal("expected app to enter running state")
	}
	if app.state.CurrentWorkdir != workdir {
		t.Fatalf("current workdir = %q, want %q", app.state.CurrentWorkdir, workdir)
	}
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input, got %+v", runtime.prepareInputs)
	}
	if runtime.prepareInputs[0].Text != "hello from wake" {
		t.Fatalf("prepare input text = %q, want %q", runtime.prepareInputs[0].Text, "hello from wake")
	}
	if runtime.prepareInputs[0].Workdir != workdir {
		t.Fatalf("prepare input workdir = %q, want %q", runtime.prepareInputs[0].Workdir, workdir)
	}
	if app.input.Value() != "" || app.state.InputText != "" {
		t.Fatalf("expected startup wake submit to clear input, got input=%q state=%q", app.input.Value(), app.state.InputText)
	}
}

func TestUpdateStartupWakeSubmitMsgWithEmptyTextNoop(t *testing.T) {
	app, runtime := newTestApp(t)

	model, cmd := app.Update(startupWakeSubmitMsg{Input: startupWakeSubmitInput{Text: "  "}})
	app = model.(App)
	if cmd != nil {
		t.Fatal("expected nil command for empty startup wake input")
	}
	if app.state.IsAgentRunning {
		t.Fatal("expected app to remain idle")
	}
	if len(runtime.prepareInputs) != 0 {
		t.Fatalf("expected no prepare input, got %+v", runtime.prepareInputs)
	}
}

func TestUpdateSendWithInlineImageReferenceUsesPreparePipeline(t *testing.T) {
	app, runtime := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	imagePath := filepath.Join(root, "burn.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	app.providerSvc = stubProviderService{
		providers: []configstate.ProviderOption{{ID: app.state.CurrentProvider, Name: app.state.CurrentProvider}},
		models: []providertypes.ModelDescriptor{{
			ID:   app.state.CurrentModel,
			Name: app.state.CurrentModel,
			CapabilityHints: providertypes.ModelCapabilityHints{
				ImageInput: providertypes.ModelCapabilityStateSupported,
			},
		}},
	}

	app.input.SetValue("analyze @image:burn.png")
	app.state.InputText = "analyze @image:burn.png"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)

	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input, got %+v", runtime.prepareInputs)
	}
	if runtime.prepareInputs[0].Text != "analyze" {
		t.Fatalf("expected inline image token removed from text, got %q", runtime.prepareInputs[0].Text)
	}
	if len(runtime.prepareInputs[0].Images) != 1 || runtime.prepareInputs[0].Images[0].MimeType != "" {
		t.Fatalf("expected one promoted image in prepare input, got %+v", runtime.prepareInputs[0].Images)
	}
	if len(runtime.runInputs) != 1 {
		t.Fatalf("expected one runtime run input, got %+v", runtime.runInputs)
	}
}

func TestUpdatePickerSessionEnterActivatesSelectedSession(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	runtime.listSessions = []agentsession.Summary{
		{ID: "s1", Title: "One", UpdatedAt: now.Add(-time.Minute)},
		{ID: "s2", Title: "Two", UpdatedAt: now},
	}
	runtime.loadSessions = map[string]agentsession.Session{
		"s2": {
			ID:      "s2",
			Title:   "Two",
			Workdir: app.state.CurrentWorkdir,
			Messages: []providertypes.Message{
				{Role: roleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			},
		},
	}
	if err := app.refreshSessionPicker(); err != nil {
		t.Fatalf("refreshSessionPicker() error = %v", err)
	}
	app.openPicker(pickerSession, statusChooseSession, &app.sessionPicker, "")
	app.sessionPicker.Select(1)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.state.ActiveSessionID != "s2" || app.state.ActiveSessionTitle != "Two" {
		t.Fatalf("expected selected session to be activated, got id=%q title=%q", app.state.ActiveSessionID, app.state.ActiveSessionTitle)
	}
	if len(app.activeMessages) != 1 {
		t.Fatalf("expected messages to refresh from selected session")
	}
}

func TestUpdatePickerSessionEnterWhileBusyRejectsSwitch(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	runtime.listSessions = []agentsession.Summary{
		{ID: "s1", Title: "One", UpdatedAt: now.Add(-time.Minute)},
		{ID: "s2", Title: "Two", UpdatedAt: now},
	}
	if err := app.refreshSessionPicker(); err != nil {
		t.Fatalf("refreshSessionPicker() error = %v", err)
	}
	app.state.ActiveSessionID = "s1"
	app.state.ActiveSessionTitle = "One"
	app.state.IsAgentRunning = true
	app.openPicker(pickerSession, statusChooseSession, &app.sessionPicker, "s1")
	app.sessionPicker.Select(1)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected nil cmd for rejected session switch")
	}
	app = model.(App)
	if app.state.ActiveSessionID != "s1" {
		t.Fatalf("expected active session to remain unchanged, got %q", app.state.ActiveSessionID)
	}
	if !strings.Contains(app.state.ExecutionError, sessionSwitchBusyMessage) {
		t.Fatalf("expected busy session switch error, got %q", app.state.ExecutionError)
	}
}

func TestUpdatePickerModelFilterAppliesImmediately(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelPicker.SetItems([]list.Item{
		selectionItem{id: "alpha", name: "alpha-model", description: "a"},
		selectionItem{id: "beta", name: "beta-model", description: "b"},
	})
	app.openPicker(pickerModel, statusChooseModel, &app.modelPicker, "alpha")

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected inline filter update to return nil cmd")
	}
	if app.modelPicker.FilterValue() != "z" {
		t.Fatalf("expected filter text to update, got %q", app.modelPicker.FilterValue())
	}
	if got := len(app.modelPicker.VisibleItems()); got != 0 {
		t.Fatalf("expected 0 matched items after filtering, got %d", got)
	}
}

func TestUpdatePickerModelAllowsArrowNavigationWhileFiltering(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelPicker.SetItems([]list.Item{
		selectionItem{id: "m1", name: "model-one", description: "first"},
		selectionItem{id: "m2", name: "model-two", description: "second"},
	})
	app.openPicker(pickerModel, statusChooseModel, &app.modelPicker, "m1")

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd for arrow navigation while filtering")
	}
	selected, ok := app.modelPicker.SelectedItem().(selectionItem)
	if !ok {
		t.Fatalf("expected selected model item type, got %T", app.modelPicker.SelectedItem())
	}
	if selected.id != "m2" {
		t.Fatalf("expected selection to move to m2, got %q", selected.id)
	}
}

func TestUpdatePickerModelFilterUsesFuzzyMatching(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelPicker.SetItems([]list.Item{
		selectionItem{id: "qwen", name: "qwen-turbo", description: "fast"},
		selectionItem{id: "glm", name: "glm-5.1", description: "alt"},
	})
	app.openPicker(pickerModel, statusChooseModel, &app.modelPicker, "qwen")

	model, _ := app.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	app = model.(App)
	model, _ = app.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	app = model.(App)
	model, _ = app.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	app = model.(App)

	if app.modelPicker.FilterValue() != "qtr" {
		t.Fatalf("expected filter text qtr, got %q", app.modelPicker.FilterValue())
	}
	if got := len(app.modelPicker.VisibleItems()); got != 1 {
		t.Fatalf("expected fuzzy filter to keep one item, got %d", got)
	}
	selected, ok := app.modelPicker.SelectedItem().(selectionItem)
	if !ok {
		t.Fatalf("expected selected model item type, got %T", app.modelPicker.SelectedItem())
	}
	if selected.id != "qwen" {
		t.Fatalf("expected fuzzy match to select qwen, got %q", selected.id)
	}
}

func TestActivateSessionByIDNotFound(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.Sessions = []agentsession.Summary{{ID: "s1", Title: "one"}}
	if err := app.activateSessionByID("missing"); err == nil {
		t.Fatalf("expected session not found error")
	}
}

func TestHandleImmediateSlashCommandSession(t *testing.T) {
	app, runtime := newTestApp(t)
	runtime.listSessions = []agentsession.Summary{
		{ID: "s1", Title: "Session 1", UpdatedAt: time.Now()},
	}
	handled, cmd := app.handleImmediateSlashCommand("/session")
	if !handled {
		t.Fatalf("expected /session to be handled immediately")
	}
	if cmd != nil {
		_ = cmd()
	}
	if app.state.ActivePicker != pickerSession {
		t.Fatalf("expected session picker opened by immediate slash command")
	}
}

func TestHandleImmediateSlashCommandSessionWhileBusy(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.IsAgentRunning = true

	handled, cmd := app.handleImmediateSlashCommand("/session")
	if !handled {
		t.Fatalf("expected /session to be handled immediately")
	}
	if cmd != nil {
		t.Fatalf("expected busy /session to avoid returning cmd")
	}
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected session picker to stay closed while busy")
	}
	if !strings.Contains(app.state.ExecutionError, sessionSwitchBusyMessage) {
		t.Fatalf("expected busy session switch error, got %q", app.state.ExecutionError)
	}
}

func TestRuntimeEventToolStatusHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.RuntimeToolStatusPayload{ToolCallID: "tool-1", ToolName: "bash", Status: string(tuistate.ToolLifecyclePlanned)}
	handled := runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentTool != "bash" {
		t.Fatalf("expected current tool to be set")
	}
	payload.Status = string(tuistate.ToolLifecycleSucceeded)
	_ = runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if app.state.CurrentTool != "" {
		t.Fatalf("expected current tool to be cleared")
	}
}

func TestRuntimeEventUsageHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.RuntimeUsagePayload{Run: agentruntime.RuntimeUsageSnapshot{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}}
	handled := runtimeEventUsageHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.TokenUsage.RunTotalTokens != 3 {
		t.Fatalf("expected token usage to update")
	}
}

func TestRuntimeEventTokenUsageHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.TokenUsage.RunInputTokens = 2
	app.state.TokenUsage.RunOutputTokens = 3
	app.state.TokenUsage.RunTotalTokens = 5

	payload := agentruntime.TokenUsagePayload{
		InputTokens:         7,
		OutputTokens:        11,
		SessionInputTokens:  17,
		SessionOutputTokens: 19,
		HasUnknownUsage:     true,
	}
	handled := runtimeEventTokenUsageHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.TokenUsage.RunInputTokens != 9 ||
		app.state.TokenUsage.RunOutputTokens != 14 ||
		app.state.TokenUsage.RunTotalTokens != 23 {
		t.Fatalf("unexpected run token usage: %+v", app.state.TokenUsage)
	}
	if app.state.TokenUsage.SessionInputTokens != 17 ||
		app.state.TokenUsage.SessionOutputTokens != 19 ||
		app.state.TokenUsage.SessionTotalTokens != 36 {
		t.Fatalf("unexpected session token usage: %+v", app.state.TokenUsage)
	}
	if runtimeEventTokenUsageHandler(&app, agentruntime.RuntimeEvent{Payload: "invalid"}) {
		t.Fatalf("invalid token usage payload should return false")
	}
}

func TestRuntimeEventToolCallThinkingHandler(t *testing.T) {
	app, _ := newTestApp(t)
	handled := runtimeEventToolCallThinkingHandler(&app, agentruntime.RuntimeEvent{Payload: "bash"})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentTool != "bash" {
		t.Fatalf("expected current tool to be set")
	}
}

func TestRuntimeEventToolStartHandler(t *testing.T) {
	app, _ := newTestApp(t)
	call := providertypes.ToolCall{Name: "bash"}
	handled := runtimeEventToolStartHandler(&app, agentruntime.RuntimeEvent{Payload: call})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected running tool status")
	}
}

func TestRuntimeEventToolChunkHandler(t *testing.T) {
	app, _ := newTestApp(t)
	_ = runtimeEventToolChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "chunk"})
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected running tool status")
	}
}

func TestRuntimeEventAgentChunkHandler(t *testing.T) {
	app, _ := newTestApp(t)
	handled := runtimeEventAgentChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "hello"})
	if !handled {
		t.Fatalf("expected true")
	}
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected message appended")
	}
}

func TestRuntimeEventRunCanceledHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveRunID = "run-3"
	runtimeEventRunCanceledHandler(&app, agentruntime.RuntimeEvent{})
	if app.state.StatusText != statusCanceled {
		t.Fatalf("expected canceled status")
	}
	if app.state.ActiveRunID != "" {
		t.Fatalf("expected run id cleared")
	}
}

func TestRuntimeEventErrorHandler(t *testing.T) {
	app, _ := newTestApp(t)
	runtimeEventErrorHandler(&app, agentruntime.RuntimeEvent{Payload: "boom"})
	if app.state.StatusText != "boom" {
		t.Fatalf("expected status to be set to error")
	}
}

func TestRuntimeEventCompactDoneHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.CompactResult{TriggerMode: "auto", SavedRatio: 0.5, BeforeChars: 10, AfterChars: 5, TranscriptPath: "path"}
	handled := runtimeEventCompactDoneHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected true")
	}
	if !strings.Contains(app.state.StatusText, "Compact(") {
		t.Fatalf("expected compact status")
	}
}

func TestRuntimeEventCompactErrorHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.CompactErrorPayload{TriggerMode: "auto", Message: "fail"}
	handled := runtimeEventCompactErrorHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected true")
	}
	if app.state.ExecutionError == "" {
		t.Fatalf("expected error message")
	}
}

func TestAppendAssistantAndInlineMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.appendAssistantChunk("hi")
	app.appendAssistantChunk(" there")
	if len(app.activeMessages) == 0 || !strings.Contains(messageText(app.activeMessages[len(app.activeMessages)-1]), "there") {
		t.Fatalf("expected assistant chunk to append")
	}
	app.appendInlineMessage(roleSystem, "  note ")
	if len(app.activeMessages) < 2 {
		t.Fatalf("expected inline message appended")
	}
}

func TestShouldHandleTabAsInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.focus = panelInput
	app.state.ActivePicker = pickerNone
	app.input.SetValue("/he")
	if !app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
		t.Fatalf("expected tab to be handled as input")
	}
	app.input.SetValue("")
	if app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
		t.Fatalf("expected tab to be ignored for empty input")
	}
}

func TestSlashTabCompletionDoesNotMoveInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 28
	app.focus = panelInput
	app.state.ActivePicker = pickerNone
	app.input.SetValue("/he")
	app.state.InputText = "/he"
	app.applyComponentLayout(true)
	app.refreshCommandMenu()
	if !app.commandMenuHasSuggestions() {
		t.Fatalf("expected slash suggestions before tab completion")
	}
	_, inputYBefore, _, _ := app.inputBounds()

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = model.(App)
	_, inputYAfterTab, _, _ := app.inputBounds()
	if inputYAfterTab != inputYBefore {
		t.Fatalf("expected input Y to stay stable after slash tab completion, before=%d after=%d", inputYBefore, inputYAfterTab)
	}
	if got := strings.TrimSpace(app.input.Value()); got != slashUsageHelp {
		t.Fatalf("expected completed slash command %q, got %q", slashUsageHelp, got)
	}
	if app.commandMenuHasSuggestions() {
		t.Fatalf("expected command menu to clear for complete slash command")
	}
}

func TestManualSlashCompletionDoesNotMoveInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 28
	app.focus = panelInput
	app.state.ActivePicker = pickerNone
	app.input.SetValue("/hel")
	app.state.InputText = "/hel"
	app.applyComponentLayout(true)
	app.refreshCommandMenu()
	if !app.commandMenuHasSuggestions() {
		t.Fatalf("expected slash suggestions before manual completion")
	}
	_, inputYBefore, _, _ := app.inputBounds()

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	app = model.(App)
	_, inputYAfter, _, _ := app.inputBounds()
	if inputYAfter != inputYBefore {
		t.Fatalf("expected input Y to stay stable after manual slash completion, before=%d after=%d", inputYBefore, inputYAfter)
	}
	if got := strings.TrimSpace(app.input.Value()); got != slashUsageHelp {
		t.Fatalf("expected input value %q, got %q", slashUsageHelp, got)
	}
	if app.commandMenuHasSuggestions() {
		t.Fatalf("expected command menu to clear for complete slash command")
	}
}

func TestFocusNextPrev(t *testing.T) {
	app, _ := newTestApp(t)
	app.focus = panelTranscript
	app.focusNext()
	if app.focus == panelTranscript {
		t.Fatalf("expected focus to move")
	}
	app.focusPrev()
}

func TestHandleViewportKeys(t *testing.T) {
	app, _ := newTestApp(t)
	app.transcript.SetContent("line1\nline2\nline3")
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyDown})
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyUp})
}

func TestUpdateEnterHelpOpensHelpPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("/help")
	app.state.InputText = "/help"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected non-nil model")
	}
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected no async cmd when opening help picker")
	}
	if app.state.ActivePicker != pickerHelp {
		t.Fatalf("expected help picker to be active")
	}
	if app.state.StatusText != statusChooseHelp {
		t.Fatalf("expected status %q, got %q", statusChooseHelp, app.state.StatusText)
	}
	if len(app.helpPicker.Items()) != len(builtinSlashCommands) {
		t.Fatalf("expected %d help options, got %d", len(builtinSlashCommands), len(app.helpPicker.Items()))
	}
}

func TestUpdatePickerHelpSelectionOpensModelPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.refreshHelpPicker()
	app.openHelpPicker()
	selectPickerItemByID(&app.helpPicker, slashCommandModelPick)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if cmd != nil {
		_ = cmd()
	}
	if app.state.ActivePicker != pickerModel {
		t.Fatalf("expected model picker to open from help selection")
	}
}

func TestUpdatePickerHelpSelectionRunsSlashCommand(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentWorkdir = t.TempDir()
	app.refreshHelpPicker()
	app.openHelpPicker()
	selectPickerItemByID(&app.helpPicker, slashUsageMemo)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected help picker to close after selecting /memo")
	}
	if cmd == nil {
		t.Fatalf("expected cmd after selecting /memo")
	}
}

func TestRunSlashCommandSelectionModelReturnsRefreshCmd(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelRefreshID = ""

	cmd := app.runSlashCommandSelection(slashCommandModelPick)
	if app.state.ActivePicker != pickerModel {
		t.Fatalf("expected model picker to open")
	}
	if cmd == nil {
		t.Fatalf("expected model refresh cmd")
	}
	msg := cmd()
	if _, ok := msg.(modelCatalogRefreshMsg); !ok {
		t.Fatalf("expected modelCatalogRefreshMsg, got %T", msg)
	}
}

func TestRunSlashCommandSelectionProviderRefreshError(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = errorProviderService{err: errors.New("provider refresh failed")}

	cmd := app.runSlashCommandSelection(slashCommandProvider)
	if cmd != nil {
		t.Fatalf("expected nil cmd when provider refresh fails")
	}
	if !strings.Contains(app.state.StatusText, "provider refresh failed") {
		t.Fatalf("expected provider refresh error status, got %q", app.state.StatusText)
	}
}

func TestRunSlashCommandSelectionModelRefreshError(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = errorProviderService{err: errors.New("model refresh failed")}

	cmd := app.runSlashCommandSelection(slashCommandModelPick)
	if cmd != nil {
		t.Fatalf("expected nil cmd when model refresh fails")
	}
	if !strings.Contains(app.state.StatusText, "model refresh failed") {
		t.Fatalf("expected model refresh error status, got %q", app.state.StatusText)
	}
}

func TestRunSlashCommandSelectionWorkspaceAndLocal(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = ""
	app.state.CurrentWorkdir = t.TempDir()

	// /unknown-local is not handled by handleImmediateSlashCommand and is not in the direct switch cases.
	// It should therefore execute through runLocalCommand and return a localCommandResultMsg.
	localCmd := app.runSlashCommandSelection("/unknown-local")
	if localCmd == nil {
		t.Fatalf("expected local slash cmd for /unknown-local")
	}
}

func TestHandleImmediateSlashCommandDefault(t *testing.T) {
	app, _ := newTestApp(t)
	handled, cmd := app.handleImmediateSlashCommand("/unknown")
	if handled || cmd != nil {
		t.Fatalf("expected unknown slash command to be ignored")
	}
}

func TestHandleImmediateSlashCommandCompactBranches(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"

	handled, cmd := app.handleImmediateSlashCommand(slashCommandCompact + " now")
	if !handled || cmd != nil {
		t.Fatalf("expected compact with args to be handled without cmd")
	}
	if !strings.Contains(app.state.StatusText, "usage:") {
		t.Fatalf("expected usage error for compact with args")
	}

	app.state.ExecutionError = ""
	app.state.IsCompacting = true
	handled, cmd = app.handleImmediateSlashCommand(slashCommandCompact)
	if !handled || cmd != nil {
		t.Fatalf("expected compact busy branch to return handled with nil cmd")
	}
	if !strings.Contains(app.state.StatusText, "already running") {
		t.Fatalf("expected busy message")
	}

	app.state.IsCompacting = false
	app.state.IsAgentRunning = false
	app.state.StatusText = ""
	handled, cmd = app.handleImmediateSlashCommand(slashCommandCompact)
	if !handled || cmd == nil {
		t.Fatalf("expected compact success branch to return cmd")
	}
	msg := cmd()
	if _, ok := msg.(compactFinishedMsg); !ok {
		t.Fatalf("expected compactFinishedMsg, got %T", msg)
	}
	if len(runtime.resolveCalls) != 0 {
		t.Fatalf("compact should not resolve permissions")
	}
}

func TestHandleMemoCommandsRouteToSystemTools(t *testing.T) {
	app, runtime := newTestApp(t)

	runtime.systemToolRes = tools.ToolResult{Content: "ok"}
	cmd := app.handleMemoCommand()
	if cmd == nil {
		t.Fatalf("expected /memo command")
	}
	msg := cmd()
	model, _ := app.Update(msg)
	app = model.(App)
	if len(runtime.systemToolCalls) != 1 {
		t.Fatalf("expected one system tool call for /memo")
	}
	if runtime.systemToolCalls[0].ToolName != tools.ToolNameMemoList {
		t.Fatalf("unexpected tool for /memo: %s", runtime.systemToolCalls[0].ToolName)
	}
	if app.state.StatusText != "ok" {
		t.Fatalf("expected status from tool result, got %q", app.state.StatusText)
	}

	cmd = app.handleRememberCommand("persist this")
	if cmd == nil {
		t.Fatalf("expected /remember command")
	}
	_ = cmd()
	if len(runtime.systemToolCalls) != 2 {
		t.Fatalf("expected one additional system tool call for /remember")
	}
	if runtime.systemToolCalls[1].ToolName != tools.ToolNameMemoRemember {
		t.Fatalf("unexpected tool for /remember: %s", runtime.systemToolCalls[1].ToolName)
	}

	cmd = app.handleForgetCommand("keyword")
	if cmd == nil {
		t.Fatalf("expected /forget command")
	}
	_ = cmd()
	if len(runtime.systemToolCalls) != 3 {
		t.Fatalf("expected one additional system tool call for /forget")
	}
	if runtime.systemToolCalls[2].ToolName != tools.ToolNameMemoRemove {
		t.Fatalf("unexpected tool for /forget: %s", runtime.systemToolCalls[2].ToolName)
	}
}

func TestHandleMemoCommandMapsUnsupportedActionErrorToUserFriendlyMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "legacy sentinel",
			err:  agentruntime.ErrUnsupportedActionInGatewayMode,
		},
		{
			name: "gateway rpc unsupported_action",
			err: &agentruntime.GatewayRPCError{
				Method:      protocol.MethodGatewayExecuteSystemTool,
				Code:        protocol.JSONRPCCodeMethodNotFound,
				GatewayCode: protocol.GatewayCodeUnsupportedAction,
				Message:     "method not found",
			},
		},
		{
			name: "gateway rpc method_not_found",
			err: &agentruntime.GatewayRPCError{
				Method:  protocol.MethodGatewayExecuteSystemTool,
				Code:    protocol.JSONRPCCodeMethodNotFound,
				Message: "method not found",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			app, runtime := newTestApp(t)
			runtime.systemToolErr = tt.err

			cmd := app.handleMemoCommand()
			if cmd == nil {
				t.Fatalf("expected /memo command")
			}
			model, _ := app.Update(cmd())
			app = model.(App)

			status := strings.ToLower(strings.TrimSpace(app.state.StatusText))
			if strings.Contains(status, "unsupported_action_in_gateway_mode") {
				t.Fatalf("expected sentinel to be hidden from UI, got %q", app.state.StatusText)
			}
			if !strings.Contains(status, "gateway") {
				t.Fatalf("expected gateway upgrade hint, got %q", app.state.StatusText)
			}
		})
	}
}

func TestHandleRememberAndForgetValidation(t *testing.T) {
	app, _ := newTestApp(t)

	if cmd := app.handleRememberCommand("   "); cmd != nil {
		t.Fatalf("expected nil cmd for empty /remember")
	}
	if len(app.activeMessages) == 0 || !strings.Contains(messageText(app.activeMessages[len(app.activeMessages)-1]), "Usage") {
		t.Fatalf("expected usage message for empty /remember")
	}

	if cmd := app.handleForgetCommand("   "); cmd != nil {
		t.Fatalf("expected nil cmd for empty /forget")
	}
	if len(app.activeMessages) == 0 || !strings.Contains(messageText(app.activeMessages[len(app.activeMessages)-1]), "Usage") {
		t.Fatalf("expected usage message for empty /forget")
	}
}

func TestHandleSkillsSlashCommands(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-skills"
	runtime.availableSkillsResult = []agentruntime.AvailableSkillState{
		{
			Descriptor: skills.Descriptor{
				ID:          "go-review",
				Description: "review go code",
				Source:      skills.Source{Kind: skills.SourceKindLocal},
				Scope:       skills.ScopeSession,
				Version:     "v1",
			},
			Active: true,
		},
	}

	handled, cmd := app.handleImmediateSlashCommand("/skills")
	if !handled || cmd == nil {
		t.Fatalf("expected /skills command to return async cmd")
	}
	model, _ := app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Available skills:") {
		t.Fatalf("expected available skill notice, got %q", app.state.StatusText)
	}
	if len(app.activeMessages) == 0 || !strings.Contains(messageText(app.activeMessages[len(app.activeMessages)-1]), "go-review") {
		t.Fatalf("expected transcript to include listed skill")
	}
}

func TestHandleSkillUseOffAndActiveCommands(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-skills"
	runtime.sessionSkillsResult = []agentruntime.SessionSkillState{
		{SkillID: "go-review", Descriptor: &skills.Descriptor{ID: "go-review", Description: "review"}},
	}

	handled, cmd := app.handleImmediateSlashCommand("/skill use go-review")
	if !handled || cmd == nil {
		t.Fatalf("expected /skill use to produce command")
	}
	model, _ := app.Update(cmd())
	app = model.(App)
	if len(runtime.activateSkillCalls) != 1 || runtime.activateSkillCalls[0].SkillID != "go-review" {
		t.Fatalf("unexpected activate calls: %+v", runtime.activateSkillCalls)
	}
	if !strings.Contains(app.state.StatusText, "Skill activated") {
		t.Fatalf("expected activate notice, got %q", app.state.StatusText)
	}

	handled, cmd = app.handleImmediateSlashCommand("/skill off go-review")
	if !handled || cmd == nil {
		t.Fatalf("expected /skill off to produce command")
	}
	model, _ = app.Update(cmd())
	app = model.(App)
	if len(runtime.deactivateSkillCalls) != 1 || runtime.deactivateSkillCalls[0].SkillID != "go-review" {
		t.Fatalf("unexpected deactivate calls: %+v", runtime.deactivateSkillCalls)
	}
	if !strings.Contains(app.state.StatusText, "Skill deactivated") {
		t.Fatalf("expected deactivate notice, got %q", app.state.StatusText)
	}

	handled, cmd = app.handleImmediateSlashCommand("/skill active")
	if !handled || cmd == nil {
		t.Fatalf("expected /skill active to produce command")
	}
	model, _ = app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Active skills:") {
		t.Fatalf("expected active skill listing, got %q", app.state.StatusText)
	}
}

func TestHandleSkillCommandValidationAndGatewayErrors(t *testing.T) {
	app, runtime := newTestApp(t)

	handled, cmd := app.handleImmediateSlashCommand("/skill use go-review")
	if !handled || cmd != nil {
		t.Fatalf("expected missing session branch handled without cmd")
	}
	if !strings.Contains(app.state.StatusText, "requires an active session") {
		t.Fatalf("expected missing session hint, got %q", app.state.StatusText)
	}

	app.state.ActiveSessionID = "session-skills"
	handled, cmd = app.handleImmediateSlashCommand("/skills now")
	if !handled || cmd != nil {
		t.Fatalf("expected /skills with args to reject usage")
	}
	if !strings.Contains(app.state.StatusText, "usage: /skills") {
		t.Fatalf("expected /skills usage error, got %q", app.state.StatusText)
	}

	runtime.activateSkillErr = &agentruntime.GatewayRPCError{
		Method:      protocol.MethodGatewayActivateSessionSkill,
		Code:        protocol.JSONRPCCodeMethodNotFound,
		GatewayCode: protocol.GatewayCodeUnsupportedAction,
		Message:     "method not found",
	}
	handled, cmd = app.handleImmediateSlashCommand("/skill use go-review")
	if !handled || cmd == nil {
		t.Fatalf("expected /skill use to produce cmd on gateway error")
	}
	model, _ := app.Update(cmd())
	app = model.(App)
	if !strings.Contains(strings.ToLower(app.state.StatusText), "gateway") {
		t.Fatalf("expected gateway unsupported hint, got %q", app.state.StatusText)
	}
}

func TestUpdateCompactFinishedAndRefreshMessagesError(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-error"
	runtime.loadSessionErr = errors.New("load session failed")

	model, _ := app.Update(compactFinishedMsg{Err: errors.New("compact failed")})
	app = model.(App)
	if app.state.IsCompacting {
		t.Fatalf("expected compacting state to be cleared")
	}
	if app.state.ExecutionError != "load session failed" {
		t.Fatalf("expected refresh message error to win, got %q", app.state.ExecutionError)
	}
	if len(app.activeMessages) == 0 || app.activeMessages[len(app.activeMessages)-1].Role != roleError {
		t.Fatalf("expected inline error message appended")
	}
}

func TestFormatPermissionPromptToolOnly(t *testing.T) {
	lines := formatPermissionPromptLines(permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{ToolName: "bash"},
	})
	if len(lines) == 0 || !strings.Contains(lines[0], "Permission request: bash") {
		t.Fatalf("expected tool-only prompt header, got %#v", lines)
	}
}

func TestStartDraftSessionResetsRunState(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	app.state.ActiveSessionTitle = "Session 1"
	app.state.ActiveRunID = "run-1"
	app.state.CurrentTool = "bash"
	app.state.ToolStates = []tuistate.ToolState{{ToolCallID: "tool-1", ToolName: "bash"}}
	app.state.RunContext = tuistate.ContextWindowState{Provider: "openai"}
	app.state.TokenUsage = tuistate.TokenUsageState{RunTotalTokens: 123}
	app.activities = []tuistate.ActivityEntry{{Title: "activity"}}
	app.state.CurrentWorkdir = t.TempDir()
	app.startupScreenLocked = false

	app.startDraftSession()

	if app.state.ActiveRunID != "" {
		t.Fatalf("expected run id to be reset")
	}
	if app.state.CurrentTool != "" {
		t.Fatalf("expected current tool to be reset")
	}
	if len(app.state.ToolStates) != 0 {
		t.Fatalf("expected tool states to be reset")
	}
	if app.state.ActiveSessionID != "" || app.state.ActiveSessionTitle != draftSessionTitle {
		t.Fatalf("expected draft session state")
	}
	if len(app.activities) != 0 {
		t.Fatalf("expected activities to be cleared")
	}
	if app.startupScreenLocked {
		t.Fatalf("expected startupScreenLocked to stay unlocked after new session")
	}
}
func TestSetCurrentWorkdir(t *testing.T) {
	app, _ := newTestApp(t)

	t.Run("accepts absolute path", func(t *testing.T) {
		dir := t.TempDir()
		app.setCurrentWorkdir(dir)
		if app.state.CurrentWorkdir != filepath.Clean(dir) {
			t.Fatalf("expected %q, got %q", filepath.Clean(dir), app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores empty", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores whitespace", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("   ")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores relative path", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("relative/path")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})
}

func TestNoteInputEditTracksPasteHeuristics(t *testing.T) {
	app, _ := newTestApp(t)
	base := time.Now()

	app.noteInputEdit("a", "ab", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")}, base)
	if app.lastInputEditAt.IsZero() {
		t.Fatalf("expected lastInputEditAt to be updated")
	}

	app.noteInputEdit("ab", "ab\ncd", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\ny")}, base.Add(time.Millisecond))
	if !app.pasteMode {
		t.Fatalf("expected pasteMode to be enabled for multiline runes")
	}

	app.noteInputEdit("ab\ncd", "ab\ncd", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}, base.Add(2*time.Millisecond))
	if app.inputBurstCount == 0 {
		t.Fatalf("expected burst count to remain when text unchanged path is skipped")
	}
}

func TestActivateSelectedSession(t *testing.T) {
	app, runtime := newTestApp(t)
	runtime.loadSessions = map[string]agentsession.Session{
		"s-active": {
			ID:      "s-active",
			Title:   "Active Session",
			Workdir: app.state.CurrentWorkdir,
			Messages: []providertypes.Message{
				{Role: roleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			},
		},
	}

	app.sessionPicker.SetItems([]list.Item{
		sessionItem{Summary: agentsession.Summary{ID: "s-active", Title: "Active Session"}},
	})
	app.sessionPicker.Select(0)

	if err := app.activateSelectedSession(); err != nil {
		t.Fatalf("activateSelectedSession() error = %v", err)
	}
	if app.state.ActiveSessionID != "s-active" || app.state.ActiveSessionTitle != "Active Session" {
		t.Fatalf("expected selected session to become active")
	}
	if len(app.activeMessages) != 1 {
		t.Fatalf("expected active messages to be refreshed from session")
	}
}

func TestRefreshMessagesHidesToolCallOnlyEntriesOnSessionReload(t *testing.T) {
	app, runtime := newTestApp(t)
	app.width = 120
	app.height = 32
	app.applyComponentLayout(true)
	app.state.ActiveSessionID = "s1"

	runtime.loadSessions = map[string]agentsession.Session{
		"s1": {
			ID:      "s1",
			Title:   "S1",
			Workdir: app.state.CurrentWorkdir,
			Messages: []providertypes.Message{
				{Role: roleAssistant, ToolCalls: []providertypes.ToolCall{{Name: "bash"}}},
				{Role: roleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool result payload")}},
				{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("final answer")}},
			},
		},
	}

	if err := app.refreshMessages(); err != nil {
		t.Fatalf("refreshMessages() error = %v", err)
	}
	app.rebuildTranscript()
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")

	if strings.Contains(plain, "Tool calls:") {
		t.Fatalf("expected tool-call-only helper text hidden on session reload, got %q", plain)
	}
	if strings.Contains(plain, "tool result payload") {
		t.Fatalf("expected tool result payload hidden on session reload, got %q", plain)
	}
	if !strings.Contains(plain, "final answer") {
		t.Fatalf("expected assistant content to remain visible, got %q", plain)
	}
}

func TestMouseHandlersAndBounds(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 40
	app.activities = []tuistate.ActivityEntry{{Kind: "test", Title: "activity"}}
	app.applyComponentLayout(true)
	app.setTranscriptContent(strings.Repeat("line\n", 160))

	tx, ty, _, _ := app.transcriptBounds()
	if !app.isMouseWithinTranscript(tea.MouseMsg{X: tx, Y: ty}) {
		t.Fatalf("expected transcript bounds hit")
	}
	if app.isMouseWithinTranscript(tea.MouseMsg{X: tx - 1, Y: ty - 1}) {
		t.Fatalf("expected transcript bounds miss")
	}

	if app.handleTranscriptMouse(tea.MouseMsg{X: tx - 1, Y: ty - 1, Action: tea.MouseActionRelease}) {
		t.Fatalf("expected outside transcript release to return false")
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: tx, Y: ty, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript wheel down to be handled")
	}
	sx, sy, sw, sh := app.transcriptScrollbarBounds()
	if sw != transcriptScrollbarWidth {
		t.Fatalf("expected transcript scrollbar width %d, got %d", transcriptScrollbarWidth, sw)
	}
	if app.isMouseWithinTranscript(tea.MouseMsg{X: sx, Y: sy}) {
		t.Fatalf("expected scrollbar column not to be counted as transcript content")
	}
	offsetBeforeDrag := app.transcript.YOffset
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: sx + max(0, sw-1), Y: sy + max(1, sh/2), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript scrollbar press to be handled")
	}
	if !app.transcriptScrollbarDrag {
		t.Fatalf("expected scrollbar drag mode to start after press")
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: sx + max(0, sw-1), Y: sy + sh - 1, Action: tea.MouseActionMotion,
	}) {
		t.Fatalf("expected transcript scrollbar drag motion to be handled")
	}
	if app.transcript.YOffset <= offsetBeforeDrag {
		t.Fatalf("expected drag motion to move transcript offset, got %d <= %d", app.transcript.YOffset, offsetBeforeDrag)
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: sx + max(0, sw-1), Y: sy + sh - 1, Action: tea.MouseActionRelease,
	}) {
		t.Fatalf("expected transcript scrollbar release to be handled")
	}
	if app.transcriptScrollbarDrag {
		t.Fatalf("expected scrollbar drag mode to stop after release")
	}

	ix, iy, _, _ := app.inputBounds()
	if !app.isMouseWithinInput(tea.MouseMsg{X: ix, Y: iy}) {
		t.Fatalf("expected input bounds hit")
	}
	app.focus = panelTranscript
	if !app.handleInputMouse(tea.MouseMsg{
		X: ix, Y: iy, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected input wheel to be handled")
	}
	if app.focus != panelInput {
		t.Fatalf("expected input panel to gain focus")
	}

	ax, ay, _, _ := app.activityBounds()
	if app.isMouseWithinActivity(tea.MouseMsg{X: ax, Y: ay}) {
		t.Fatalf("expected activity bounds miss when activity panel is disabled")
	}
	app.focus = panelTranscript
	if app.handleActivityMouse(tea.MouseMsg{
		X: ax, Y: ay, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected activity wheel to be ignored when activity panel is disabled")
	}
	if app.focus != panelTranscript {
		t.Fatalf("expected focus to stay on transcript when activity panel is disabled")
	}
}

func TestComposerHelpers(t *testing.T) {
	app, _ := newTestApp(t)
	if got := app.composerBoxWidth(88); got != 88 {
		t.Fatalf("composerBoxWidth() = %d, want 88", got)
	}

	app.input.SetHeight(1)
	app.input.SetValue("line1\nline2")
	before := app.input.Height()
	app.growComposerForNewline()
	if app.input.Height() <= before {
		t.Fatalf("expected growComposerForNewline to increase height")
	}

	app.input.SetHeight(composerMaxHeight)
	app.input.SetValue("line")
	app.normalizeComposerHeight()
	if app.input.Height() < composerMinHeight || app.input.Height() > composerMaxHeight {
		t.Fatalf("normalizeComposerHeight should keep height in clamp range")
	}

	app.input.SetValue("")
	app.normalizeComposerHeight()
	if app.input.Height() != composerMinHeight {
		t.Fatalf("expected empty composer to reset to min height, got %d", app.input.Height())
	}
}

func TestApplyComponentLayoutUsesStartupPromptWidthForInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.state.IsAgentRunning = false
	app.state.IsCompacting = false
	app.activeMessages = nil
	app.input.SetValue("")
	app.width = 120
	app.height = 40

	if !app.shouldRenderStartupScreen() {
		t.Fatalf("expected startup screen precondition")
	}
	lay := app.computeLayout()
	fullWidth := app.composerInnerWidth(lay.contentWidth)

	app.applyComponentLayout(false)

	if app.input.Width() >= fullWidth {
		t.Fatalf("expected startup input width to be narrower than full width (%d), got %d", fullWidth, app.input.Width())
	}
}

func TestCurrentProviderAddFieldAndInputHandling(t *testing.T) {
	app, _ := newTestApp(t)
	if got := currentProviderAddField(nil); got != providerAddFieldName {
		t.Fatalf("currentProviderAddField(nil) = %v, want name field", got)
	}

	app.startProviderAddForm()
	app.providerAddForm.Step = 999
	if got := currentProviderAddField(app.providerAddForm); got != providerAddFieldAPIKey {
		t.Fatalf("expected out-of-range step to clamp to last visible field, got %v", got)
	}

	app.providerAddForm.Step = 0
	model, cmd := app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd != nil {
		t.Fatalf("expected nil cmd for rune input")
	}
	ptr, ok := model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Name != "a" {
		t.Fatalf("expected name field append, got %q", app.providerAddForm.Name)
	}

	app.providerAddForm.Step = 7 // api key env
	model, cmd = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\x00', 'D', 'E', 'E', 'P'}})
	if cmd != nil {
		t.Fatalf("expected nil cmd for env key rune input")
	}
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.APIKeyEnv != "DEEP" {
		t.Fatalf("expected control chars filtered from env key, got %q", app.providerAddForm.APIKeyEnv)
	}

	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyTab})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Step == 0 {
		t.Fatalf("expected tab to switch field")
	}

	app.providerAddForm.Step = 1 // driver
	driverBefore := app.providerAddForm.Driver
	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyDown})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Driver == driverBefore {
		t.Fatalf("expected key down to switch driver")
	}

	app.providerAddForm.Step = 2 // model source
	modelSourceBefore := app.providerAddForm.ModelSource
	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyDown})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.ModelSource == modelSourceBefore {
		t.Fatalf("expected key down to switch model source")
	}

	app.providerAddForm.Step = 0
	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyBackspace})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Name != "" {
		t.Fatalf("expected backspace to remove name content")
	}

	app.providerAddForm.Name = "\u4f60\u597d"
	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyBackspace})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Name != "\u4f60" {
		t.Fatalf("expected UTF-8 safe backspace result, got %q", app.providerAddForm.Name)
	}

	model, cmd = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(providerAddResultMsg); !ok {
			t.Fatalf("expected providerAddResultMsg from submit cmd, got %T", msg)
		}
	}
}

func TestHandleProviderAddFormInputSubmittingNoop(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Submitting = true

	model, cmd := app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd != nil {
		t.Fatalf("expected nil cmd while submitting")
	}
	ptr, ok := model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Name != "" {
		t.Fatalf("expected no mutation while submitting")
	}
}

func TestListenForRuntimeEvent(t *testing.T) {
	eventCh := make(chan agentruntime.RuntimeEvent, 1)
	event := agentruntime.RuntimeEvent{RunID: "run-listen"}
	eventCh <- event

	cmd := ListenForRuntimeEvent(eventCh)
	msg := cmd()
	runtimeMsg, ok := msg.(RuntimeMsg)
	if !ok {
		t.Fatalf("expected RuntimeMsg, got %T", msg)
	}
	forwarded, ok := runtimeMsg.Event.(agentruntime.RuntimeEvent)
	if !ok {
		t.Fatalf("expected runtime event payload, got %T", runtimeMsg.Event)
	}
	if forwarded.RunID != "run-listen" {
		t.Fatalf("expected forwarded runtime event")
	}

	close(eventCh)
	cmd = ListenForRuntimeEvent(eventCh)
	msg = cmd()
	if _, ok := msg.(RuntimeClosedMsg); !ok {
		t.Fatalf("expected RuntimeClosedMsg after channel close, got %T", msg)
	}
}

func TestUpdateRuntimeMsgWithInvalidEventTypeSchedulesNextListen(t *testing.T) {
	app, _ := newTestApp(t)

	updated, cmd := app.Update(RuntimeMsg{Event: "not-runtime-event"})
	if updated == nil {
		t.Fatalf("expected updated model")
	}
	if cmd == nil {
		t.Fatalf("expected follow-up listen command")
	}
}

func TestBuildProviderAddRequest(t *testing.T) {
	t.Run("validates required fields", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{}); !strings.Contains(err, "Name is required") {
			t.Fatalf("expected missing name error, got %q", err)
		}
		if _, err := buildProviderAddRequest(providerAddFormState{Name: "demo"}); !strings.Contains(err, "Driver is required") {
			t.Fatalf("expected missing driver error, got %q", err)
		}
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:   "demo",
			Driver: provider.DriverGemini,
		}); !strings.Contains(err, "Model Source") {
			t.Fatalf("expected missing model source error, got %q", err)
		}
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "demo",
			Driver:      provider.DriverGemini,
			ModelSource: config.ModelSourceManual,
		}); !strings.Contains(err, "API Key is required") {
			t.Fatalf("expected missing key error, got %q", err)
		}
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "demo",
			Driver:      provider.DriverGemini,
			ModelSource: config.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "",
		}); !strings.Contains(err, "API Key Env is required") {
			t.Fatalf("expected missing env key error, got %q", err)
		}
	})

	t.Run("openai compat discover mode requires discovery endpoint path", func(t *testing.T) {
		_, err := buildProviderAddRequest(providerAddFormState{
			Name:        "openai-compat",
			Driver:      provider.DriverOpenAICompat,
			ModelSource: config.ModelSourceDiscover,
			APIKey:      "k",
			APIKeyEnv:   "OPENAI_COMPAT_API_KEY",
		})
		if !strings.Contains(err, "requires discovery_endpoint_path") {
			t.Fatalf("expected missing discovery endpoint error, got %q", err)
		}
	})

	t.Run("openai compat discover mode normalizes discovery settings", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat-discover",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			ChatAPIMode:           provider.ChatAPIModeResponses,
			ChatEndpointPath:      "/chat/completions",
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_DISCOVER_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.ModelSource != config.ModelSourceDiscover {
			t.Fatalf("expected discover model source, got %q", req.ModelSource)
		}
		if req.DiscoveryEndpointPath != provider.DiscoveryEndpointPathModels {
			t.Fatalf("expected default discovery endpoint, got %q", req.DiscoveryEndpointPath)
		}
		if req.ChatEndpointPath != "/chat/completions" {
			t.Fatalf("expected default chat endpoint, got %q", req.ChatEndpointPath)
		}
		if req.ChatAPIMode != provider.ChatAPIModeResponses {
			t.Fatalf("expected chat api mode responses, got %q", req.ChatAPIMode)
		}
	})

	t.Run("openai compat defaults chat api mode to chat_completions", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat-default-mode",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_DEFAULT_MODE_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.ChatAPIMode != provider.ChatAPIModeChatCompletions {
			t.Fatalf("expected default chat api mode, got %q", req.ChatAPIMode)
		}
	})

	t.Run("openai compat fills chat endpoint by chat api mode when empty", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat-responses-endpoint",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			ChatAPIMode:           provider.ChatAPIModeResponses,
			ChatEndpointPath:      "",
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_RESPONSES_ENDPOINT_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.ChatAPIMode != provider.ChatAPIModeResponses {
			t.Fatalf("expected chat api mode responses, got %q", req.ChatAPIMode)
		}
		if req.ChatEndpointPath != "/responses" {
			t.Fatalf("expected responses endpoint path, got %q", req.ChatEndpointPath)
		}
	})

	t.Run("openai compat keeps explicit custom endpoint path", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat-custom-endpoint",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			ChatAPIMode:           provider.ChatAPIModeChatCompletions,
			ChatEndpointPath:      "/v1/text/chatcompletion_v2",
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_CUSTOM_ENDPOINT_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.ChatEndpointPath != "/v1/text/chatcompletion_v2" {
			t.Fatalf("expected custom endpoint path preserved, got %q", req.ChatEndpointPath)
		}
		if req.ChatAPIMode != provider.ChatAPIModeChatCompletions {
			t.Fatalf("expected chat api mode completions, got %q", req.ChatAPIMode)
		}
	})

	t.Run("strips control chars from env key before validation", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "\x00OPENAI_COMPAT_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.APIKeyEnv != "OPENAI_COMPAT_API_KEY" {
			t.Fatalf("expected sanitized env key, got %q", req.APIKeyEnv)
		}
	})

	t.Run("rejects protected env key", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "PATH",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		}); !strings.Contains(err, "protected") {
			t.Fatalf("expected protected env key error, got %q", err)
		}
	})

	t.Run("gemini applies default base url", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "gemini",
			Driver:                provider.DriverGemini,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "GEMINI_GATEWAY_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.BaseURL != config.GeminiDefaultBaseURL {
			t.Fatalf("expected gemini normalization, got %+v", req)
		}
	})

	t.Run("rejects invalid discovery endpoint path", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_API_KEY",
			DiscoveryEndpointPath: "https://api.example.com/models",
		}); !strings.Contains(err, "relative path") {
			t.Fatalf("expected invalid discovery endpoint path error, got %q", err)
		}
	})

	t.Run("rejects invalid chat endpoint path", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
			ChatEndpointPath:      "https://api.example.com/chat/completions",
		}); !strings.Contains(err, "relative path") {
			t.Fatalf("expected invalid chat endpoint path error, got %q", err)
		}
	})

	t.Run("custom driver requires base url", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "custom",
			Driver:      "custom-driver",
			ModelSource: config.ModelSourceDiscover,
			APIKey:      "k",
			APIKeyEnv:   "CUSTOM_DRIVER_API_KEY",
			BaseURL:     "",
		}); !strings.Contains(err, "Base URL is required for custom driver") {
			t.Fatalf("expected custom base url error, got %q", err)
		}
	})

	t.Run("manual source clears discovery settings", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "manual",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           config.ModelSourceManual,
			APIKey:                "k",
			APIKeyEnv:             "MANUAL_GATEWAY_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
			ManualModelsJSON:      `[{"id":"manual-model","name":"Manual Model"}]`,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.DiscoveryEndpointPath != "" {
			t.Fatalf("expected manual mode to clear discovery settings, got %+v", req)
		}
	})
}

func TestParseProviderAddManualModelsJSONRejectsNonPositiveNumericFields(t *testing.T) {
	_, err := parseProviderAddManualModelsJSON(`[{"id":"m1","name":"Model 1","context_window":0}]`)
	if err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("expected context_window validation error, got %v", err)
	}

	_, err = parseProviderAddManualModelsJSON(`[{"id":"m1","name":"Model 1","max_output_tokens":0}]`)
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("expected max_output_tokens validation error, got %v", err)
	}
}

func TestRefreshRuntimeSourceSnapshot(t *testing.T) {
	app, runtime := newTestApp(t)
	snapshot := &snapshotRuntime{
		stubRuntime: runtime,
		sessionContext: map[string]any{
			"SessionID": "sess-1",
			"Provider":  "provider-x",
			"Model":     "model-x",
			"Workdir":   "/tmp/work",
			"Mode":      "agent",
		},
		sessionUsage: map[string]any{
			"InputTokens":  11,
			"OutputTokens": 7,
			"TotalTokens":  18,
		},
		runSnapshot: map[string]any{
			"RunID":     "run-9",
			"SessionID": "sess-1",
			"Context": map[string]any{
				"Provider": "provider-y",
				"Model":    "model-y",
				"Workdir":  "/tmp/run",
				"Mode":     "run",
			},
			"ToolStates": []any{
				map[string]any{"ToolCallID": "tool-1", "ToolName": "bash", "Status": "running"},
			},
			"Usage": map[string]any{
				"InputTokens":  3,
				"OutputTokens": 4,
				"TotalTokens":  7,
			},
			"SessionUsage": map[string]any{
				"InputTokens":  20,
				"OutputTokens": 9,
				"TotalTokens":  29,
			},
		},
	}
	app.runtime = snapshot
	app.state.ActiveSessionID = "sess-1"
	app.state.ActiveRunID = "run-9"

	app.refreshRuntimeSourceSnapshot()

	if app.state.RunContext.Provider != "provider-y" || app.state.RunContext.Model != "model-y" {
		t.Fatalf("expected run snapshot context to override run context, got %+v", app.state.RunContext)
	}
	if len(app.state.ToolStates) != 1 || app.state.ToolStates[0].ToolCallID != "tool-1" {
		t.Fatalf("expected tool states from run snapshot")
	}
	if app.state.TokenUsage.RunTotalTokens != 7 || app.state.TokenUsage.SessionTotalTokens != 29 {
		t.Fatalf("expected usage values from run snapshot, got %+v", app.state.TokenUsage)
	}
}

func TestUpdatePickerProviderAndModelEnter(t *testing.T) {
	app, _ := newTestApp(t)

	app.providerPicker.SetItems([]list.Item{
		selectionItem{id: "provider-a", name: "provider-a", description: "provider-a"},
	})
	app.openPicker(pickerProvider, statusChooseProvider, &app.providerPicker, "provider-a")
	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil || cmd == nil {
		t.Fatalf("expected provider enter to return command")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close after provider enter")
	}

	app.modelPicker.SetItems([]list.Item{
		selectionItem{id: "model-a", name: "model-a", description: "model-a"},
	})
	app.openPicker(pickerModel, statusChooseModel, &app.modelPicker, "model-a")
	model, cmd = app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil || cmd == nil {
		t.Fatalf("expected model enter to return command")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close after model enter")
	}
}

func TestUpdatePickerProviderEnterStartsModelScopeGuide(t *testing.T) {
	app, _ := newTestApp(t)
	t.Setenv(config.ModelScopeDefaultAPIKeyEnv, "")

	prevLookup := lookupProviderUserEnvVar
	lookupProviderUserEnvVar = func(key string) (string, bool, error) {
		return "", false, nil
	}
	t.Cleanup(func() {
		lookupProviderUserEnvVar = prevLookup
	})

	var openedTarget string
	prevOpen := openExternalResource
	openExternalResource = func(target string) error {
		openedTarget = target
		return nil
	}
	t.Cleanup(func() {
		openExternalResource = prevOpen
	})

	app.providerPicker.SetItems([]list.Item{
		selectionItem{id: config.ModelScopeName, name: config.ModelScopeName, description: "modelscope"},
	})
	app.openPicker(pickerProvider, statusChooseProvider, &app.providerPicker, config.ModelScopeName)
	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected modelscope guide open command")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerModelScope {
		t.Fatalf("expected modelscope guide picker, got %v", app.state.ActivePicker)
	}
	if app.modelScopeGuide == nil {
		t.Fatalf("expected modelscope guide state initialized")
	}

	openMsg, ok := cmd().(modelScopeGuideOpenResultMsg)
	if !ok {
		t.Fatalf("expected modelscope open result msg, got %T", cmd())
	}
	if openMsg.Error != "" {
		t.Fatalf("expected open success, got error %q", openMsg.Error)
	}
	if openedTarget != modelScopeLoginURL {
		t.Fatalf("expected fallback login url %q, got %q", modelScopeLoginURL, openedTarget)
	}
}

func TestUpdatePickerProviderEnterSkipsModelScopeGuideWhenTokenExists(t *testing.T) {
	app, _ := newTestApp(t)
	t.Setenv(config.ModelScopeDefaultAPIKeyEnv, "exists-token")

	app.providerPicker.SetItems([]list.Item{
		selectionItem{id: config.ModelScopeName, name: config.ModelScopeName, description: "modelscope"},
	})
	app.openPicker(pickerProvider, statusChooseProvider, &app.providerPicker, config.ModelScopeName)
	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected provider selection command")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close")
	}
	if app.modelScopeGuide != nil {
		t.Fatalf("expected modelscope guide not to start when token exists")
	}
}

func TestHandleModelScopeGuideInputSubmitSuccess(t *testing.T) {
	app, _ := newTestAppWithProviderService(t, stubProviderService{
		models: []providertypes.ModelDescriptor{{ID: config.ModelScopeDefaultModel, Name: "Qwen"}},
	})
	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  "MODELSCOPE_TEST_TOKEN",
		Step:       modelScopeGuideStepPasteToken,
		Token:      "token-value",
	}
	app.state.ActivePicker = pickerModelScope

	prevPersist := persistProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error { return nil }
	t.Cleanup(func() {
		persistProviderUserEnvVar = prevPersist
	})

	model, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected token submit command")
	}
	appPtr, ok := model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *appPtr
	if !app.modelScopeGuide.Submitting {
		t.Fatalf("expected guide submitting state")
	}

	msg, ok := cmd().(modelScopeGuideSubmitResultMsg)
	if !ok {
		t.Fatalf("expected submit result msg, got %T", cmd())
	}
	if msg.Error != "" {
		t.Fatalf("expected submit success, got error %q", msg.Error)
	}

	nextCmd := app.handleModelScopeGuideSubmitResultMsg(msg)
	if app.modelScopeGuide != nil {
		t.Fatalf("expected guide to close after success")
	}
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker none after success")
	}
	if !strings.EqualFold(app.state.CurrentProvider, config.ModelScopeName) {
		t.Fatalf("expected current provider modelscope, got %q", app.state.CurrentProvider)
	}
	if nextCmd == nil {
		t.Fatalf("expected model catalog refresh command")
	}
}

func TestHandleModelScopeGuideSubmitResultMsgAuthFallback(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  "MODELSCOPE_TEST_TOKEN",
		Step:       modelScopeGuideStepPasteToken,
		Token:      "bad-token",
	}
	app.state.ActivePicker = pickerModelScope

	var openedTarget string
	prevOpen := openExternalResource
	openExternalResource = func(target string) error {
		openedTarget = target
		return nil
	}
	t.Cleanup(func() {
		openExternalResource = prevOpen
	})

	cmd := app.handleModelScopeGuideSubmitResultMsg(modelScopeGuideSubmitResultMsg{
		Error: "verify token: request failed with 401 unauthorized",
	})
	if cmd == nil {
		t.Fatalf("expected fallback auth open command")
	}
	if app.modelScopeGuide == nil {
		t.Fatalf("expected guide to stay open on error")
	}
	if app.modelScopeGuide.Step != modelScopeGuideStepToken {
		t.Fatalf("expected guide step rollback to token page, got %v", app.modelScopeGuide.Step)
	}

	openMsg, ok := cmd().(modelScopeGuideOpenResultMsg)
	if !ok {
		t.Fatalf("expected open result msg, got %T", cmd())
	}
	if openMsg.Error != "" {
		t.Fatalf("expected auth page open success, got %q", openMsg.Error)
	}
	if openedTarget != modelScopeAuthURL {
		t.Fatalf("expected auth url %q, got %q", modelScopeAuthURL, openedTarget)
	}
}

func TestRunModelScopeGuideSubmitRollsBackSelectionWhenVerifyFails(t *testing.T) {
	var selectedProviders []string
	var selectedModels []string
	app, _ := newTestApp(t)
	app.providerSvc = stubProviderService{
		models:        []providertypes.ModelDescriptor{{ID: config.ModelScopeDefaultModel, Name: "Qwen"}},
		listModelsErr: errors.New("modelscope verify failed"),
		selectHook: func(providerID string) {
			selectedProviders = append(selectedProviders, providerID)
		},
		setModelHook: func(modelID string) {
			selectedModels = append(selectedModels, modelID)
		},
	}
	before := app.configManager.Get()
	envName := "MODELSCOPE_TEST_TOKEN_VERIFY_FAIL"
	t.Setenv(envName, "previous-token")

	msg := app.runModelScopeGuideSubmit(config.ModelScopeName, envName, "new-token")()
	result, ok := msg.(modelScopeGuideSubmitResultMsg)
	if !ok {
		t.Fatalf("expected submit result msg, got %T", msg)
	}
	if !strings.Contains(result.Error, "verify token") {
		t.Fatalf("expected verify token error, got %q", result.Error)
	}
	if got := os.Getenv(envName); got != "previous-token" {
		t.Fatalf("expected env to rollback, got %q", got)
	}
	if len(selectedProviders) < 2 {
		t.Fatalf("expected select provider to include rollback call, got %+v", selectedProviders)
	}
	if !strings.EqualFold(selectedProviders[0], config.ModelScopeName) {
		t.Fatalf("expected first provider selection to modelscope, got %+v", selectedProviders)
	}
	if !strings.EqualFold(selectedProviders[1], before.SelectedProvider) {
		t.Fatalf("expected rollback provider %q, got %+v", before.SelectedProvider, selectedProviders)
	}
	if len(selectedModels) == 0 || !strings.EqualFold(selectedModels[0], before.CurrentModel) {
		t.Fatalf("expected rollback model %q, got %+v", before.CurrentModel, selectedModels)
	}
}

func TestRunModelScopeGuideSubmitRollsBackSelectionWhenPersistFails(t *testing.T) {
	var selectedProviders []string
	var selectedModels []string
	providerSvc := stubProviderService{
		models: []providertypes.ModelDescriptor{{ID: config.ModelScopeDefaultModel, Name: "Qwen"}},
		selectHook: func(providerID string) {
			selectedProviders = append(selectedProviders, providerID)
		},
		setModelHook: func(modelID string) {
			selectedModels = append(selectedModels, modelID)
		},
	}
	app, _ := newTestAppWithProviderService(t, providerSvc)
	before := app.configManager.Get()
	envName := "MODELSCOPE_TEST_TOKEN_PERSIST_FAIL"
	t.Setenv(envName, "previous-token")

	prevPersist := persistProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error {
		return errors.New("persist failed")
	}
	t.Cleanup(func() {
		persistProviderUserEnvVar = prevPersist
	})

	msg := app.runModelScopeGuideSubmit(config.ModelScopeName, envName, "new-token")()
	result, ok := msg.(modelScopeGuideSubmitResultMsg)
	if !ok {
		t.Fatalf("expected submit result msg, got %T", msg)
	}
	if !strings.Contains(result.Error, "persist token env") {
		t.Fatalf("expected persist token env error, got %q", result.Error)
	}
	if got := os.Getenv(envName); got != "previous-token" {
		t.Fatalf("expected env to rollback, got %q", got)
	}
	if len(selectedProviders) < 2 {
		t.Fatalf("expected select provider to include rollback call, got %+v", selectedProviders)
	}
	if !strings.EqualFold(selectedProviders[0], config.ModelScopeName) {
		t.Fatalf("expected first provider selection to modelscope, got %+v", selectedProviders)
	}
	if !strings.EqualFold(selectedProviders[1], before.SelectedProvider) {
		t.Fatalf("expected rollback provider %q, got %+v", before.SelectedProvider, selectedProviders)
	}
	if len(selectedModels) == 0 || !strings.EqualFold(selectedModels[0], before.CurrentModel) {
		t.Fatalf("expected rollback model %q, got %+v", before.CurrentModel, selectedModels)
	}
}

func TestMaybeStartModelScopeGuideFromProviderGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)

	if cmd, started := app.maybeStartModelScopeGuideFromProvider("openai"); started || cmd != nil {
		t.Fatalf("expected non-modelscope provider not to start guide")
	}

	t.Setenv(config.ModelScopeDefaultAPIKeyEnv, "exists-token")
	if cmd, started := app.maybeStartModelScopeGuideFromProvider(config.ModelScopeName); started || cmd != nil {
		t.Fatalf("expected guide not to start when process env token exists")
	}
}

func TestMaybeStartModelScopeGuideFromProviderSkipsWhenUserEnvExists(t *testing.T) {
	app, _ := newTestApp(t)
	t.Setenv(config.ModelScopeDefaultAPIKeyEnv, "")

	prevLookup := lookupProviderUserEnvVar
	lookupProviderUserEnvVar = func(key string) (string, bool, error) {
		return "persisted-token", true, nil
	}
	t.Cleanup(func() {
		lookupProviderUserEnvVar = prevLookup
	})

	if cmd, started := app.maybeStartModelScopeGuideFromProvider(config.ModelScopeName); started || cmd != nil {
		t.Fatalf("expected guide not to start when user env token exists")
	}
}

func TestMaybeStartModelScopeGuideFromProviderUsesGuideHTMLFile(t *testing.T) {
	app, _ := newTestApp(t)
	t.Setenv(config.ModelScopeDefaultAPIKeyEnv, "")

	prevLookup := lookupProviderUserEnvVar
	lookupProviderUserEnvVar = func(key string) (string, bool, error) {
		return "", false, nil
	}
	t.Cleanup(func() {
		lookupProviderUserEnvVar = prevLookup
	})

	guidePath := filepath.Join(app.configManager.BaseDir(), "modelscope-guide.html")
	if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
		t.Fatalf("mkdir guide dir: %v", err)
	}
	if err := os.WriteFile(guidePath, []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write guide html: %v", err)
	}

	var opened string
	prevOpen := openExternalResource
	openExternalResource = func(target string) error {
		opened = target
		return nil
	}
	t.Cleanup(func() {
		openExternalResource = prevOpen
	})

	cmd, started := app.maybeStartModelScopeGuideFromProvider(config.ModelScopeName)
	if !started || cmd == nil {
		t.Fatalf("expected modelscope guide to start with local guide html")
	}
	if app.modelScopeGuide == nil || app.modelScopeGuide.GuidePath != guidePath {
		t.Fatalf("expected guide path %q, got %+v", guidePath, app.modelScopeGuide)
	}

	if _, ok := cmd().(modelScopeGuideOpenResultMsg); !ok {
		t.Fatalf("expected open result msg from guide open command")
	}
	if opened != guidePath {
		t.Fatalf("expected guide html opened first, got %q", opened)
	}
}

func TestMaybeStartModelScopeGuideFromProviderSkipsWhenProviderConfigMissing(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	filtered := make([]config.ProviderConfig, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if !strings.EqualFold(providerCfg.Name, config.ModelScopeName) {
			filtered = append(filtered, providerCfg)
		}
	}
	cfg.Providers = filtered
	if len(cfg.Providers) == 0 {
		t.Fatalf("expected at least one provider after filtering modelscope")
	}
	cfg.SelectedProvider = cfg.Providers[0].Name
	cfg.CurrentModel = cfg.Providers[0].Model

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:        *cfg,
		ConfigManager: manager,
		Runtime:       runtime,
		ProviderService: stubProviderService{
			providers: []configstate.ProviderOption{
				{
					ID:   cfg.Providers[0].Name,
					Name: cfg.Providers[0].Name,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	if cmd, started := app.maybeStartModelScopeGuideFromProvider(config.ModelScopeName); started || cmd != nil {
		t.Fatalf("expected guide not to start when modelscope provider config is missing")
	}
}

func TestResolveModelScopeGuidePathRejectsDirectory(t *testing.T) {
	app, _ := newTestApp(t)
	guidePath := filepath.Join(app.configManager.BaseDir(), "modelscope-guide.html")
	if err := os.MkdirAll(guidePath, 0o755); err != nil {
		t.Fatalf("mkdir guide dir: %v", err)
	}

	if got := app.resolveModelScopeGuidePath(); got != "" {
		t.Fatalf("expected empty guide path when candidate is directory, got %q", got)
	}
}

func TestResolveModelScopeGuidePathReturnsEmptyWhenBaseDirMissing(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	manager := config.NewManager(config.NewLoader(filepath.Join(t.TempDir(), "missing-config-root"), cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:        *cfg,
		ConfigManager: manager,
		Runtime:       runtime,
		ProviderService: stubProviderService{
			providers: []configstate.ProviderOption{
				{ID: cfg.SelectedProvider, Name: cfg.SelectedProvider},
			},
		},
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	if got := app.resolveModelScopeGuidePath(); got != "" {
		t.Fatalf("expected empty guide path when base dir is missing, got %q", got)
	}
}

func TestHandleModelScopeGuideInputGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)

	if model, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatalf("expected nil guide to ignore input")
	} else if _, ok := model.(*App); !ok {
		t.Fatalf("expected *App model when guide is nil, got %T", model)
	}

	app.modelScopeGuide = &modelScopeGuideState{Submitting: true, Step: modelScopeGuideStepPasteToken}
	if model, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatalf("expected submitting guide to ignore input")
	} else if _, ok := model.(*App); !ok {
		t.Fatalf("expected *App model when submitting, got %T", model)
	}

	app.modelScopeGuide = &modelScopeGuideState{Step: modelScopeGuideStepToken}
	app.state.ActivePicker = pickerModelScope
	model, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("expected cancel command to be nil")
	}
	ptr, ok := model.(*App)
	if !ok {
		t.Fatalf("expected *App after cancel, got %T", model)
	}
	if ptr.modelScopeGuide != nil || ptr.state.ActivePicker != pickerNone {
		t.Fatalf("expected guide canceled and picker closed")
	}
}

func TestHandleModelScopeGuideInputStepTransitionsAndEditing(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  config.ModelScopeDefaultAPIKeyEnv,
		Step:       modelScopeGuideStepGuide,
		GuidePath:  "",
	}

	var opened []string
	prevOpen := openExternalResource
	openExternalResource = func(target string) error {
		opened = append(opened, target)
		return nil
	}
	t.Cleanup(func() {
		openExternalResource = prevOpen
	})

	cases := []struct {
		name   string
		step   modelScopeGuideStep
		target string
	}{
		{name: "guide fallback opens login", step: modelScopeGuideStepGuide, target: modelScopeLoginURL},
		{name: "login opens login page", step: modelScopeGuideStepLogin, target: modelScopeLoginURL},
		{name: "token opens token page", step: modelScopeGuideStepToken, target: modelScopeTokenURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app.modelScopeGuide.Step = tc.step
			_, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatalf("expected open command for step %v", tc.step)
			}
			_ = cmd()
			if got := opened[len(opened)-1]; got != tc.target {
				t.Fatalf("expected target %q, got %q", tc.target, got)
			}
		})
	}

	app.modelScopeGuide.Step = modelScopeGuideStepPasteToken
	app.modelScopeGuide.Token = ""
	_, cmd := app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected empty token submit to return nil command")
	}
	if !strings.Contains(app.modelScopeGuide.Error, "Token is required") {
		t.Fatalf("expected token required error, got %q", app.modelScopeGuide.Error)
	}

	app.modelScopeGuide.Token = "abc"
	_, _ = app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyBackspace})
	if app.modelScopeGuide.Token != "ab" {
		t.Fatalf("expected backspace to trim token, got %q", app.modelScopeGuide.Token)
	}
	_, _ = app.handleModelScopeGuideInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" d ")})
	if app.modelScopeGuide.Token != "ab d " {
		t.Fatalf("expected runes sanitized into token, got %q", app.modelScopeGuide.Token)
	}
}

func TestRunModelScopeGuideOpenGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if cmd := app.runModelScopeGuideOpen(" "); cmd != nil {
		t.Fatalf("expected empty open target to return nil cmd")
	}

	prevOpen := openExternalResource
	openExternalResource = func(target string) error {
		return errors.New("open failed")
	}
	t.Cleanup(func() {
		openExternalResource = prevOpen
	})

	cmd := app.runModelScopeGuideOpen("https://www.modelscope.cn/")
	msg, ok := cmd().(modelScopeGuideOpenResultMsg)
	if !ok {
		t.Fatalf("expected open result msg, got %T", cmd())
	}
	if !strings.Contains(msg.Error, "open failed") {
		t.Fatalf("expected open failure in message, got %q", msg.Error)
	}
}

func TestHandleModelScopeGuideOpenResultMsgTransitionsAndErrors(t *testing.T) {
	app, _ := newTestApp(t)
	app.handleModelScopeGuideOpenResultMsg(modelScopeGuideOpenResultMsg{Target: modelScopeLoginURL})

	cases := []struct {
		name     string
		step     modelScopeGuideStep
		target   string
		expected modelScopeGuideStep
	}{
		{name: "guide advances to login", step: modelScopeGuideStepGuide, target: modelScopeLoginURL, expected: modelScopeGuideStepLogin},
		{name: "login advances to token", step: modelScopeGuideStepLogin, target: modelScopeTokenURL, expected: modelScopeGuideStepToken},
		{name: "token advances after token page", step: modelScopeGuideStepToken, target: modelScopeTokenURL, expected: modelScopeGuideStepPasteToken},
		{name: "token stays on auth page", step: modelScopeGuideStepToken, target: modelScopeAuthURL, expected: modelScopeGuideStepToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app.modelScopeGuide = &modelScopeGuideState{Step: tc.step}
			app.handleModelScopeGuideOpenResultMsg(modelScopeGuideOpenResultMsg{Target: tc.target})
			if app.modelScopeGuide.Step != tc.expected {
				t.Fatalf("expected step %v, got %v", tc.expected, app.modelScopeGuide.Step)
			}
		})
	}

	app.handleModelScopeGuideOpenResultMsg(modelScopeGuideOpenResultMsg{Error: "boom"})
	if app.state.StatusText != "ModelScope guide open failed" {
		t.Fatalf("expected status text set on open error, got %q", app.state.StatusText)
	}
	if app.state.ExecutionError != "boom" {
		t.Fatalf("expected execution error set on open error, got %q", app.state.ExecutionError)
	}
}

func TestModelScopeGuideOpenTarget(t *testing.T) {
	cases := []struct {
		name      string
		step      modelScopeGuideStep
		guidePath string
		target    string
		ok        bool
	}{
		{name: "guide uses local html", step: modelScopeGuideStepGuide, guidePath: "/tmp/guide.html", target: "/tmp/guide.html", ok: true},
		{name: "guide falls back to login", step: modelScopeGuideStepGuide, target: modelScopeLoginURL, ok: true},
		{name: "login uses login page", step: modelScopeGuideStepLogin, target: modelScopeLoginURL, ok: true},
		{name: "token uses token page", step: modelScopeGuideStepToken, target: modelScopeTokenURL, ok: true},
		{name: "paste token has no open target", step: modelScopeGuideStepPasteToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, ok := modelScopeGuideOpenTarget(tc.step, tc.guidePath)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v", tc.ok, ok)
			}
			if target != tc.target {
				t.Fatalf("expected target %q, got %q", tc.target, target)
			}
		})
	}
}

func TestAdvanceModelScopeGuideStep(t *testing.T) {
	cases := []struct {
		name     string
		current  modelScopeGuideStep
		target   string
		next     modelScopeGuideStep
		advanced bool
	}{
		{name: "guide advances", current: modelScopeGuideStepGuide, target: modelScopeLoginURL, next: modelScopeGuideStepLogin, advanced: true},
		{name: "login advances", current: modelScopeGuideStepLogin, target: modelScopeLoginURL, next: modelScopeGuideStepToken, advanced: true},
		{name: "token advances only for token page", current: modelScopeGuideStepToken, target: modelScopeTokenURL, next: modelScopeGuideStepPasteToken, advanced: true},
		{name: "token ignores auth page", current: modelScopeGuideStepToken, target: modelScopeAuthURL, next: modelScopeGuideStepToken, advanced: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, advanced := advanceModelScopeGuideStep(tc.current, tc.target)
			if advanced != tc.advanced {
				t.Fatalf("expected advanced=%v, got %v", tc.advanced, advanced)
			}
			if next != tc.next {
				t.Fatalf("expected next %v, got %v", tc.next, next)
			}
		})
	}
}

func TestRunModelScopeGuideSubmitValidationAndRollbackErrorAggregation(t *testing.T) {
	app, _ := newTestApp(t)

	msg := app.runModelScopeGuideSubmit(config.ModelScopeName, "MODELSCOPE_TEST", " ")()
	result, ok := msg.(modelScopeGuideSubmitResultMsg)
	if !ok || !strings.Contains(result.Error, "token is empty") {
		t.Fatalf("expected empty token error, got %#v", msg)
	}

	msg = app.runModelScopeGuideSubmit(config.ModelScopeName, "bad env", "token")()
	result, ok = msg.(modelScopeGuideSubmitResultMsg)
	if !ok || !strings.Contains(strings.ToLower(result.Error), "env") {
		t.Fatalf("expected env validation error, got %#v", msg)
	}

	msg = app.runModelScopeGuideSubmit(config.ModelScopeName, "PATH", "token")()
	result, ok = msg.(modelScopeGuideSubmitResultMsg)
	if !ok || !strings.Contains(result.Error, "protected") {
		t.Fatalf("expected protected env error, got %#v", msg)
	}

	app.providerSvc = stubProviderService{selectErr: errors.New("select failed")}
	msg = app.runModelScopeGuideSubmit(config.ModelScopeName, "MODELSCOPE_TEST_AGG", "token")()
	result, ok = msg.(modelScopeGuideSubmitResultMsg)
	if !ok || !strings.Contains(result.Error, "rollback selection") {
		t.Fatalf("expected rollback aggregation in error, got %#v", msg)
	}
}

func TestRollbackModelScopeGuideSelectionBranches(t *testing.T) {
	if err := rollbackModelScopeGuideSelection(nil, "openai", "model"); err != nil {
		t.Fatalf("expected nil provider controller to skip rollback, got %v", err)
	}
	if err := rollbackModelScopeGuideSelection(stubProviderService{}, " ", "model"); err != nil {
		t.Fatalf("expected empty provider id to skip rollback, got %v", err)
	}

	if err := rollbackModelScopeGuideSelection(stubProviderService{}, "openai", ""); err != nil {
		t.Fatalf("expected no model rollback to pass, got %v", err)
	}

	if err := rollbackModelScopeGuideSelection(stubProviderService{selectErr: errors.New("nope")}, "openai", "m1"); err == nil ||
		!strings.Contains(err.Error(), "rollback provider selection") {
		t.Fatalf("expected rollback provider selection error, got %v", err)
	}

	if err := rollbackModelScopeGuideSelection(stubProviderService{setModelErr: errors.New("set-model")}, "openai", "m1"); err == nil ||
		!strings.Contains(err.Error(), "rollback model selection") {
		t.Fatalf("expected rollback model selection error, got %v", err)
	}
}

func TestHandleModelScopeGuideSubmitResultMsgBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if cmd := app.handleModelScopeGuideSubmitResultMsg(modelScopeGuideSubmitResultMsg{}); cmd != nil {
		t.Fatalf("expected nil guide to return nil command")
	}

	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  config.ModelScopeDefaultAPIKeyEnv,
		Step:       modelScopeGuideStepPasteToken,
		Token:      "token",
		Submitting: true,
	}
	if cmd := app.handleModelScopeGuideSubmitResultMsg(modelScopeGuideSubmitResultMsg{Error: "network timeout"}); cmd != nil {
		t.Fatalf("expected non-auth error not to open fallback page")
	}
	if app.modelScopeGuide.Step != modelScopeGuideStepPasteToken || app.modelScopeGuide.Submitting {
		t.Fatalf("expected error to keep paste step and clear submitting")
	}

	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  config.ModelScopeDefaultAPIKeyEnv,
		Step:       modelScopeGuideStepPasteToken,
		Token:      "token",
		Submitting: true,
	}
	app.providerSvc = stubProviderService{
		listErr:       errors.New("list providers failed"),
		listModelsErr: errors.New("list models failed"),
	}
	cmd := app.handleModelScopeGuideSubmitResultMsg(modelScopeGuideSubmitResultMsg{
		Selection: configstate.Selection{ProviderID: "", ModelID: "m-1"},
	})
	if cmd == nil {
		t.Fatalf("expected catalog refresh command even when picker refresh fails")
	}
	if !strings.EqualFold(app.state.CurrentProvider, config.ModelScopeName) || app.state.CurrentModel != "m-1" {
		t.Fatalf("expected provider/model set from guide+selection, got provider=%q model=%q",
			app.state.CurrentProvider, app.state.CurrentModel)
	}
	var providerRefreshErr, modelRefreshErr bool
	for _, entry := range app.activities {
		if entry.Title == "Failed to refresh providers" {
			providerRefreshErr = true
		}
		if entry.Title == "Failed to refresh models" {
			modelRefreshErr = true
		}
	}
	if !providerRefreshErr || !modelRefreshErr {
		t.Fatalf("expected refresh failure activities, got %+v", app.activities)
	}
}

func TestIsModelScopeAuthOrPermissionError(t *testing.T) {
	if isModelScopeAuthOrPermissionError("") {
		t.Fatalf("expected empty error not to be auth/permission error")
	}
	if isModelScopeAuthOrPermissionError("network timeout") {
		t.Fatalf("expected generic timeout not to match auth/permission keywords")
	}
	for _, msg := range []string{
		"401 unauthorized",
		"forbidden by policy",
		"permission is required",
		"aliyun binding required",
	} {
		if !isModelScopeAuthOrPermissionError(msg) {
			t.Fatalf("expected auth/permission match for %q", msg)
		}
	}
}

func TestUpdatePickerRoutesToProviderAddFormHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.state.ActivePicker = pickerProviderAdd

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		t.Fatalf("expected nil cmd when editing provider add form")
	}
	ptr, ok := model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	if ptr.providerAddForm == nil || ptr.providerAddForm.Name != "n" {
		t.Fatalf("expected provider add form input to be routed")
	}
}

func TestUpdateModelCatalogRefreshBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelRefreshID = app.state.CurrentProvider

	model, cmd := app.Update(modelCatalogRefreshMsg{
		ProviderID: app.state.CurrentProvider,
		Models: []providertypes.ModelDescriptor{
			{ID: "m-new", Name: "m-new"},
		},
	})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.modelRefreshID != "" {
		t.Fatalf("expected refresh id to be cleared")
	}
	if len(app.modelPicker.Items()) == 0 {
		t.Fatalf("expected model picker items to be replaced")
	}

	prevActivities := len(app.activities)
	model, _ = app.Update(modelCatalogRefreshMsg{
		ProviderID: app.state.CurrentProvider,
		Err:        errors.New("refresh failed"),
	})
	app = model.(App)
	if len(app.activities) <= prevActivities {
		t.Fatalf("expected refresh error activity to be appended")
	}

	prevActivities = len(app.activities)
	model, _ = app.Update(modelCatalogRefreshMsg{
		ProviderID: "another-provider",
		Err:        errors.New("ignored"),
	})
	app = model.(App)
	if len(app.activities) != prevActivities {
		t.Fatalf("expected mismatch provider refresh to be ignored")
	}
}

func TestUpdateLocalAndWorkspaceCommandResultBranches(t *testing.T) {
	app, _ := newTestApp(t)

	model, _ := app.Update(localCommandResultMsg{Err: errors.New("local failed")})
	app = model.(App)
	if app.state.StatusText != "local failed" {
		t.Fatalf("expected local command error status, got %q", app.state.StatusText)
	}

	model, _ = app.Update(localCommandResultMsg{Notice: "ok", ModelChanged: true})
	app = model.(App)
	if app.state.StatusText != "ok" {
		t.Fatalf("expected local command success notice")
	}
}

func TestUpdateLocalCommandProviderChangedRefreshErrors(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = errorProviderService{err: errors.New("refresh providers failed")}

	model, _ := app.Update(localCommandResultMsg{
		Notice:          "ok",
		ProviderChanged: true,
	})
	app = model.(App)
	if app.state.ExecutionError != "refresh providers failed" {
		t.Fatalf("expected provider refresh error, got %q", app.state.ExecutionError)
	}
	if len(app.activities) == 0 {
		t.Fatalf("expected failure activity")
	}
}

func TestUpdateKeyToggleQuitCancelAndPickerClose(t *testing.T) {
	app, runtime := newTestApp(t)

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	app = model.(App)
	if app.state.ActivePicker != pickerHelp {
		t.Fatalf("expected ctrl+q to open help picker")
	}
	if app.state.StatusText != statusChooseHelp {
		t.Fatalf("expected help status text %q, got %q", statusChooseHelp, app.state.StatusText)
	}

	app.state.IsAgentRunning = true
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	app = model.(App)
	if !runtime.cancelInvoked {
		t.Fatalf("expected cancel to be invoked")
	}
	if app.state.StatusText != statusCanceling {
		t.Fatalf("expected canceling status, got %q", app.state.StatusText)
	}

	app.openHelpPicker()
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd when closing active picker")
	}
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close on focus input key")
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if model == nil || cmd == nil {
		t.Fatalf("expected quit command")
	}
}

func TestUpdatePickerEnterInvalidSelectionsAndSessionActivationError(t *testing.T) {
	app, runtime := newTestApp(t)

	app.providerPicker.SetItems([]list.Item{sessionItem{Summary: agentsession.Summary{ID: "s1"}}})
	app.openPicker(pickerProvider, statusChooseProvider, &app.providerPicker, "")
	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd when provider picker item type is invalid")
	}

	app.modelPicker.SetItems([]list.Item{sessionItem{Summary: agentsession.Summary{ID: "s1"}}})
	app.openPicker(pickerModel, statusChooseModel, &app.modelPicker, "")
	model, cmd = app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd when model picker item type is invalid")
	}

	app.sessionPicker.SetItems([]list.Item{sessionItem{Summary: agentsession.Summary{ID: "missing", Title: "missing"}}})
	runtime.loadSessionErr = errors.New("load failed")
	app.openPicker(pickerSession, statusChooseSession, &app.sessionPicker, "")
	model, cmd = app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd for session picker enter")
	}
	if app.state.ExecutionError == "" {
		t.Fatalf("expected session activation error to be recorded")
	}
}

func TestUpdateInputPanelSlashAndWorkspaceBranches(t *testing.T) {
	app, _ := newTestApp(t)

	app.input.SetValue("/provider")
	app.state.InputText = "/provider"
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ActivePicker != pickerProvider {
		t.Fatalf("expected /provider to open provider picker")
	}

	app.closePicker()
	app.input.SetValue("/model")
	app.state.InputText = "/model"
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ActivePicker != pickerModel {
		t.Fatalf("expected /model to open model picker")
	}
	_ = cmd

	app.closePicker()
	app.input.SetValue("/provider add")
	app.state.InputText = "/provider add"
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ActivePicker != pickerProviderAdd || app.providerAddForm == nil {
		t.Fatalf("expected /provider add to open provider add form")
	}

}

func TestNewWithMemoAndNewAppErrorBranches(t *testing.T) {
	baseCfg := newDefaultAppConfig()
	baseCfg.Workdir = t.TempDir()

	manager := config.NewManager(config.NewLoader(baseCfg.Workdir, baseCfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	runtime := newStubRuntime()
	providerSvc := stubProviderService{
		providers: []configstate.ProviderOption{{ID: "openai", Name: "openai"}},
		models:    []providertypes.ModelDescriptor{{ID: "gpt-5", Name: "gpt-5"}},
	}

	app, err := NewWithMemo(baseCfg, manager, runtime, providerSvc, nil)
	if err != nil {
		t.Fatalf("NewWithMemo() error = %v", err)
	}
	if app.memoSvc != nil {
		t.Fatalf("expected nil memo service")
	}

	errorCases := []struct {
		name        string
		cfg         func() config.Config
		providerSvc ProviderController
	}{
		{
			name: "provider list error",
			cfg:  func() config.Config { return *baseCfg },
			providerSvc: stubProviderService{
				listErr: errors.New("list providers failed"),
			},
		},
		{
			name: "model list error",
			cfg:  func() config.Config { return *baseCfg },
			providerSvc: stubProviderService{
				providers:     []configstate.ProviderOption{{ID: "openai", Name: "openai"}},
				listModelsErr: errors.New("list models failed"),
			},
		},
		{
			name: "workspace scan error",
			cfg: func() config.Config {
				cfg := *baseCfg
				cfg.Workdir = filepath.Join(baseCfg.Workdir, "missing", "workspace")
				return cfg
			},
			providerSvc: stubProviderService{
				providers: []configstate.ProviderOption{{ID: "openai", Name: "openai"}},
				models:    []providertypes.ModelDescriptor{{ID: "gpt-5", Name: "gpt-5"}},
			},
		},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			_, err := newApp(tuibootstrap.Container{
				Config:          cfg,
				ConfigManager:   manager,
				Runtime:         runtime,
				ProviderService: tc.providerSvc,
			})
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestNowFallbackToSystemClock(t *testing.T) {
	app, _ := newTestApp(t)
	app.nowFn = nil
	if got := app.now(); got.IsZero() {
		t.Fatalf("expected non-zero time")
	}
}

func TestSyncTodosFromRunAndActivateSessionByIDFound(t *testing.T) {
	app, runtime := newTestApp(t)
	now := time.Now()
	runtime.loadSessions = map[string]agentsession.Session{
		"s1": {
			ID:    "s1",
			Title: "Session One",
			Todos: nil,
		},
		"s2": {
			ID:    "s2",
			Title: "Session Two",
			Todos: []agentsession.TodoItem{
				{
					ID:        "todo-1",
					Content:   "task",
					Status:    agentsession.TodoStatusPending,
					Priority:  1,
					CreatedAt: now,
					UpdatedAt: now,
				},
			},
		},
	}

	app.state.ActiveSessionID = "s1"
	app.todoItems = []todoViewItem{{ID: "legacy"}}
	app.todoPanelVisible = true
	app.syncTodosFromRun()
	if len(app.todoItems) != 0 {
		t.Fatalf("expected todo items cleared when session has no todos")
	}
	if app.todoPanelVisible {
		t.Fatalf("expected todo panel hidden when session has no todos")
	}

	app.state.Sessions = []agentsession.Summary{
		{ID: "s2", Title: "Session Two"},
	}
	if err := app.activateSessionByID("s2"); err != nil {
		t.Fatalf("activateSessionByID() error = %v", err)
	}
	if app.state.ActiveSessionID != "s2" {
		t.Fatalf("expected active session switched to s2, got %q", app.state.ActiveSessionID)
	}
}

func TestUpdateInputPanelTypingPathAndProviderAddFormExtraBranches(t *testing.T) {
	app, _ := newTestApp(t)
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	app = model.(App)
	if app.input.Value() != "x" {
		t.Fatalf("expected composer value to be updated, got %q", app.input.Value())
	}

	app.startProviderAddForm()
	app.providerAddForm.Step = 1
	app.providerAddForm.Driver = "unknown-driver"
	modelPtr, cmd := app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyUp})
	if cmd != nil {
		t.Fatalf("expected nil cmd for key up")
	}
	ptr, ok := modelPtr.(*App)
	if !ok {
		t.Fatalf("expected *App, got %T", modelPtr)
	}
	app = *ptr
	if app.providerAddForm.Driver != "unknown-driver" {
		t.Fatalf("expected driver unchanged when current driver not in options")
	}

	app.startProviderAddForm()
	app.providerAddForm.Driver = "unknown-driver"
	app.providerAddForm.Step = 5 // chat endpoint
	modelPtr, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2024-10-01")})
	app = *modelPtr.(*App)
	if app.providerAddForm.ChatEndpointPath == "" {
		t.Fatalf("expected chat endpoint to accept rune input")
	}
}

func TestHandleImmediateSlashCommandSessionRefreshError(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.IsAgentRunning = false
	app.state.ActiveRunID = ""
	runtime.listSessionsErr = errors.New("list sessions failed")

	handled, cmd := app.handleImmediateSlashCommand("/session")
	if !handled {
		t.Fatalf("expected /session to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd for failed /session handling")
	}
	if !strings.Contains(app.state.ExecutionError, "list sessions failed") {
		t.Fatalf("expected execution error to capture refresh failure, got %q", app.state.ExecutionError)
	}
}

func TestHandleProviderAddResultMsgRefreshPickerErrors(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:        *cfg,
		ConfigManager: manager,
		Runtime:       runtime,
		ProviderService: stubProviderService{
			providers: []configstate.ProviderOption{{ID: "p0", Name: "p0"}},
			models:    []providertypes.ModelDescriptor{{ID: "m0", Name: "m0"}},
		},
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	app.providerSvc = stubProviderService{
		listErr:       errors.New("refresh providers failed"),
		listModelsErr: errors.New("refresh models failed"),
	}
	app.startProviderAddForm()
	app.handleProviderAddResultMsg(providerAddResultMsg{Name: "new-provider", Model: "new-model"})

	if !strings.Contains(app.state.StatusText, "Provider added") {
		t.Fatalf("expected success status even when picker refresh fails, got %q", app.state.StatusText)
	}
	if len(app.activities) < 3 {
		t.Fatalf("expected activity entries for add success and refresh failures")
	}
}

func TestUpdateInputPanelSlashAndInlineImageErrorBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = stubProviderService{
		listErr:       errors.New("providers unavailable"),
		listModelsErr: errors.New("models unavailable"),
	}

	app.input.SetValue("/provider")
	app.state.InputText = "/provider"
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if !strings.Contains(app.state.ExecutionError, "providers unavailable") {
		t.Fatalf("expected provider refresh error, got %q", app.state.ExecutionError)
	}

	app.input.SetValue("/model")
	app.state.InputText = "/model"
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if !strings.Contains(app.state.ExecutionError, "models unavailable") {
		t.Fatalf("expected model refresh error, got %q", app.state.ExecutionError)
	}

	app.pendingImageAttachments = make([]pendingImageAttachment, maxImageAttachments)
	app.input.SetValue("please inspect @image:/tmp/neo-code-inline.png")
	app.state.InputText = "please inspect @image:/tmp/neo-code-inline.png"
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if !strings.Contains(strings.ToLower(app.state.ExecutionError), "maximum") {
		t.Fatalf("expected inline image absorb error, got %q", app.state.ExecutionError)
	}
}

func TestUpdatePanelRoutingAndSessionRefreshBranches(t *testing.T) {
	app, runtime := newTestApp(t)

	app.focus = panelTranscript
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)

	app.focus = panelActivity
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app = model.(App)

	now := time.Now()
	app.syncTodos([]agentsession.TodoItem{
		{
			ID:        "todo-1",
			Content:   "first",
			Status:    agentsession.TodoStatusPending,
			Priority:  1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "todo-2",
			Content:   "second",
			Status:    agentsession.TodoStatusInProgress,
			Priority:  2,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	app.todoPanelVisible = true
	app.focus = panelTodo
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	app = model.(App)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyHome})
	app = model.(App)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnd})
	app = model.(App)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	app = model.(App)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)

	app.focus = panelInput
	app.input.SetValue("abc")
	app.state.InputText = "abc"
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = model.(App)

	app.state.ActiveSessionID = ""
	app.activities = []tuistate.ActivityEntry{{Title: "legacy"}}
	app.todoItems = []todoViewItem{{ID: "legacy"}}
	if err := app.refreshMessages(); err != nil {
		t.Fatalf("refreshMessages() with draft session error = %v", err)
	}
	if len(app.activities) != 0 || len(app.todoItems) != 0 {
		t.Fatalf("expected refreshMessages to clear draft runtime state")
	}

	app.state.ActiveSessionID = "s1"
	runtime.loadSessionErr = errors.New("load failed")
	if err := app.refreshMessages(); err == nil {
		t.Fatalf("expected refreshMessages load error")
	}
}

func TestMouseHandlersAdditionalBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 40
	app.todoPanelVisible = true
	now := time.Now()
	app.syncTodos([]agentsession.TodoItem{
		{
			ID:        "todo-1",
			Content:   "first",
			Status:    agentsession.TodoStatusPending,
			Priority:  1,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	app.applyComponentLayout(true)

	todoLeft, todoTop, _, _ := app.todoBounds()
	collapsedClick := tea.MouseMsg{
		X:      todoLeft + 1,
		Y:      todoTop + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	app.todoCollapsed = true
	if !app.handleTodoMouse(collapsedClick) {
		t.Fatalf("expected collapsed todo body click handled")
	}
	if app.todoCollapsed {
		t.Fatalf("expected collapsed todo body click to expand panel")
	}

	app.todoCollapsed = true
	wheelDown := tea.MouseMsg{
		X:      todoLeft + 1,
		Y:      todoTop + 1,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Type:   tea.MouseWheelDown,
	}
	if !app.handleTodoMouse(wheelDown) {
		t.Fatalf("expected todo wheel down handled when collapsed")
	}

	inputLeft, inputTop, _, _ := app.inputBounds()
	if !app.handleInputMouse(tea.MouseMsg{
		X:      inputLeft + 1,
		Y:      inputTop + 1,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Type:   tea.MouseWheelDown,
	}) {
		t.Fatalf("expected input wheel down handled")
	}

	transcriptLeft, transcriptTop, _, _ := app.transcriptBounds()
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      transcriptLeft + 1,
		Y:      transcriptTop + 1,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Type:   tea.MouseWheelDown,
	}) {
		t.Fatalf("expected transcript wheel down handled")
	}
}

func TestSlashSelectionAndProviderAddUtilityBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerHelp

	app.runSlashCommandSelection("/help")
	if app.state.ActivePicker != pickerHelp {
		t.Fatalf("expected /help to keep help picker active")
	}

	app.runSlashCommandSelection("/clear")
	if !strings.Contains(app.state.StatusText, "Cleared") {
		t.Fatalf("expected /clear branch to update status")
	}

	fields := providerAddVisibleFields(provider.DriverOpenAICompat, config.ModelSourceDiscover)
	if len(fields) == 0 || fields[0] != providerAddFieldName {
		t.Fatalf("expected provider add visible fields to start from name field")
	}
	if !slices.Contains(fields, providerAddFieldDiscoveryEndpointPath) ||
		!slices.Contains(fields, providerAddFieldChatEndpointPath) ||
		!slices.Contains(fields, providerAddFieldChatAPIMode) {
		t.Fatalf("expected discover source to include discovery fields")
	}

	manualFields := providerAddVisibleFields(provider.DriverOpenAICompat, config.ModelSourceManual)
	if slices.Contains(manualFields, providerAddFieldDiscoveryEndpointPath) {
		t.Fatalf("expected manual source to exclude discovery fields")
	}
	geminiFields := providerAddVisibleFields(provider.DriverGemini, config.ModelSourceDiscover)
	if slices.Contains(geminiFields, providerAddFieldChatAPIMode) {
		t.Fatalf("expected non-openai driver to exclude chat api mode field")
	}
	clampProviderAddStep(nil)

	if _, err := buildProviderAddRequest(providerAddFormState{
		Name:                  "custom-provider",
		Driver:                "custom-driver",
		ModelSource:           config.ModelSourceDiscover,
		BaseURL:               "https://example.com",
		DiscoveryEndpointPath: "/models",
		APIKeyEnv:             "CUSTOM_PROVIDER_API_KEY",
		APIKey:                "test-key",
	}); err != "" {
		t.Fatalf("expected custom driver request to pass with base url, got %q", err)
	}

	prevSupports := supportsUserEnvPersistence
	supportsUserEnvPersistence = func() bool { return true }
	if got := providerAddPersistenceWarning(); got != "" {
		t.Fatalf("expected empty persistence warning when env persistence is supported")
	}
	supportsUserEnvPersistence = prevSupports

	app.providerAddForm = nil
	app.handleProviderAddResultMsg(providerAddResultMsg{Name: "unused"})
}

func TestSyncProviderAddOpenAICompatModeDefaults(t *testing.T) {
	form := &providerAddFormState{
		Driver:           provider.DriverOpenAICompat,
		ChatAPIMode:      provider.ChatAPIModeResponses,
		ChatEndpointPath: "/chat/completions",
	}
	syncProviderAddOpenAICompatModeDefaults(form, provider.ChatAPIModeChatCompletions)
	if form.ChatEndpointPath != "/responses" {
		t.Fatalf("expected default endpoint to follow responses mode, got %q", form.ChatEndpointPath)
	}

	form.ChatAPIMode = provider.ChatAPIModeChatCompletions
	form.ChatEndpointPath = "/custom/chat"
	syncProviderAddOpenAICompatModeDefaults(form, provider.ChatAPIModeResponses)
	if form.ChatEndpointPath != "/custom/chat" {
		t.Fatalf("expected custom endpoint unchanged, got %q", form.ChatEndpointPath)
	}
}

func TestRunProviderAddFlowDeadlineExceededBranch(t *testing.T) {
	service := stubProviderService{
		createErr: context.DeadlineExceeded,
	}
	app, _ := newTestAppWithProviderService(t, service)
	cmd := app.runProviderAddFlow(providerAddRequest{
		Name:                  "demo",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://example.com",
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		APIKeyEnv:             "DEMO_API_KEY",
		APIKey:                "secret",
	})
	msg := cmd()
	result, ok := msg.(providerAddResultMsg)
	if !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if !strings.Contains(strings.ToLower(result.Error), "timed out") {
		t.Fatalf("expected timeout error message, got %q", result.Error)
	}
}

func TestUpdateLogViewerModalKeyboardAndMouse(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 14
	app.applyComponentLayout(true)
	for i := 0; i < 24; i++ {
		app.logEntries = append(app.logEntries, logEntry{
			Timestamp: time.Unix(int64(i), 0),
			Level:     "info",
			Source:    "test",
			Message:   "entry-" + string(rune('A'+i)),
		})
	}

	app.logViewerOffset = 3
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	app = model.(App)
	if !app.logViewerVisible {
		t.Fatalf("expected ctrl+l to open log viewer")
	}
	if app.logViewerOffset != 0 {
		t.Fatalf("expected opening log viewer to reset offset, got %d", app.logViewerOffset)
	}

	app.setTranscriptContent(strings.Repeat("line\n", 80))
	app.transcript.SetYOffset(7)
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)
	if app.logViewerOffset != 1 {
		t.Fatalf("expected log viewer down key to scroll entries, got offset %d", app.logViewerOffset)
	}
	if app.transcript.YOffset != 7 {
		t.Fatalf("expected transcript offset unchanged while log viewer visible, got %d", app.transcript.YOffset)
	}

	x, y, _, _ := app.logViewerBounds()
	model, _ = app.Update(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	app = model.(App)
	if app.logViewerOffset != 2 {
		t.Fatalf("expected log viewer mouse wheel to scroll entries, got offset %d", app.logViewerOffset)
	}
	if app.transcript.YOffset != 7 {
		t.Fatalf("expected transcript y-offset to stay unchanged after log viewer wheel, got %d", app.transcript.YOffset)
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = model.(App)
	if app.logViewerVisible {
		t.Fatalf("expected esc to close log viewer")
	}
}

func TestSetTranscriptContentNormalizesTabStops(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 40
	app.applyComponentLayout(true)
	app.setTranscriptContent("a\tb")
	if app.transcriptContent != "a    b" {
		t.Fatalf("expected tabs normalized in transcript content, got %q", app.transcriptContent)
	}
	if got := app.transcript.View(); !strings.Contains(got, "a    b") {
		t.Fatalf("expected normalized tabs in viewport content, got %q", got)
	}
}

func TestStartupScreenUnlocksAfterSend(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.startupScreenLocked = true
	app.input.SetValue("hello")
	app.state.InputText = "hello"

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.startupScreenLocked {
		t.Fatalf("expected startup screen to unlock after sending first message")
	}
}

func TestSetActiveSessionIDTogglesStartupScreenLock(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true

	app.setActiveSessionID("session-1")
	if app.startupScreenLocked {
		t.Fatalf("expected switching to session to unlock startup screen")
	}

	app.startupScreenLocked = false
	app.setActiveSessionID("")
	if app.startupScreenLocked {
		t.Fatalf("expected returning to draft to keep startup unlocked")
	}
}

func TestRebuildTranscriptCollapsesConsecutiveAssistantTags(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 32
	app.applyComponentLayout(true)
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first chunk")}},
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("second chunk")}},
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("third chunk")}},
	}

	app.rebuildTranscript()
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if count := strings.Count(plain, messageTagAgent); count != 1 {
		t.Fatalf("expected one agent tag for consecutive assistant chunks, got %d in %q", count, plain)
	}
	if !strings.Contains(plain, "first chunk") || !strings.Contains(plain, "second chunk") || !strings.Contains(plain, "third chunk") {
		t.Fatalf("expected all assistant chunks to be present, got %q", plain)
	}
}

func TestTranscriptManualScrollPersistsWhileBusy(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 32
	app.applyComponentLayout(true)
	app.activeMessages = make([]providertypes.Message, 0, 120)
	for i := 0; i < 120; i++ {
		app.activeMessages = append(app.activeMessages, providertypes.Message{
			Role:  roleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(fmt.Sprintf("assistant-line-%03d", i))},
		})
	}
	app.rebuildTranscript()
	if app.transcriptMaxOffset() <= 6 {
		t.Fatalf("expected transcript to be scrollable, max offset=%d", app.transcriptMaxOffset())
	}
	app.transcript.SetYOffset(6)
	app.state.IsAgentRunning = true

	app.rebuildTranscript()
	if app.transcript.YOffset != 6 {
		t.Fatalf("expected rebuildTranscript to keep manual offset while busy, got %d", app.transcript.YOffset)
	}

	app.layoutCached = false
	app.applyComponentLayout(false)
	if app.transcript.YOffset != 6 {
		t.Fatalf("expected applyComponentLayout to keep manual offset while busy, got %d", app.transcript.YOffset)
	}
}

func TestAppendActivityAppendsInlineLogMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 30
	app.applyComponentLayout(true)

	app.appendActivity("tool", "success", "", false)
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if !strings.Contains(plain, "tool: success") {
		t.Fatalf("expected inline log message in transcript, got %q", plain)
	}
}

func TestInlineLogDoesNotBreakAssistantContinuationTag(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 30
	app.applyComponentLayout(true)
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("第一段")}},
	}

	app.appendActivity("tool", "success", "", false)
	app.activeMessages = append(app.activeMessages, providertypes.Message{
		Role:  roleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("第二段")},
	})
	app.rebuildTranscript()

	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if count := strings.Count(plain, messageTagAgent); count != 1 {
		t.Fatalf("expected assistant tag rendered once across inline log continuation, got %d in %q", count, plain)
	}
}

func TestSessionLogViewerPersistenceAndCap(t *testing.T) {
	app, runtime := newTestApp(t)

	app.setActiveSessionID("session-one")
	for i := 0; i < 520; i++ {
		app.addLogEntry("test", fmt.Sprintf("entry-%03d", i), "")
	}
	if len(app.logEntries) != logViewerEntryLimit {
		t.Fatalf("expected %d capped entries, got %d", logViewerEntryLimit, len(app.logEntries))
	}
	if !strings.Contains(app.logEntries[0].Message, "entry-020") {
		t.Fatalf("expected oldest in-memory entry to be entry-020, got %q", app.logEntries[0].Message)
	}
	if app.deferredLogPersistCmd == nil {
		t.Fatalf("expected deferred log persistence command to be queued")
	}
	model, _ := app.Update(logPersistFlushMsg{Version: app.logPersistVersion})
	app = model.(App)
	if got := runtime.logEntriesBySID["session-one"]; len(got) != logViewerEntryLimit {
		t.Fatalf("expected runtime persisted %d entries, got %d", logViewerEntryLimit, len(got))
	}

	app.setActiveSessionID("session-two")
	app.addLogEntry("tool", "other", "detail")
	if len(app.logEntries) != 1 {
		t.Fatalf("expected session-two to start with its own log list, got %d entries", len(app.logEntries))
	}

	app.setActiveSessionID("session-one")
	if len(app.logEntries) != logViewerEntryLimit {
		t.Fatalf("expected loading session-one logs to restore %d entries, got %d", logViewerEntryLimit, len(app.logEntries))
	}
	if !strings.Contains(app.logEntries[0].Message, "entry-020") {
		t.Fatalf("expected restored oldest entry entry-020, got %q", app.logEntries[0].Message)
	}
	if !strings.Contains(app.logEntries[len(app.logEntries)-1].Message, "entry-519") {
		t.Fatalf("expected restored newest entry entry-519, got %q", app.logEntries[len(app.logEntries)-1].Message)
	}
}

func TestSanitizeProviderAddJSONInputRunes(t *testing.T) {
	input := []rune{'a', '\u200b', '\n', '\t', '\r', 0x01, 'b'}
	got := sanitizeProviderAddJSONInputRunes(input)
	if got != "a\n\tb" {
		t.Fatalf("sanitizeProviderAddJSONInputRunes() = %q, want %q", got, "a\n\tb")
	}
}

func TestFooterErrorToastSyncBranches(t *testing.T) {
	app, _ := newTestApp(t)
	base := time.Unix(1_700_000_100, 0)
	app.nowFn = func() time.Time { return base }

	app.showFooterError(" permission denied ")
	if app.footerErrorText != "Error: permission denied" {
		t.Fatalf("expected error prefix applied, got %q", app.footerErrorText)
	}
	if !app.footerErrorUntil.Equal(base.Add(footerErrorFlashDuration)) {
		t.Fatalf("unexpected footer toast expiration: %v", app.footerErrorUntil)
	}
	if app.deferredFooterTick == nil {
		t.Fatalf("expected footer toast to schedule tick command")
	}

	app.state.ExecutionError = "Runtime failed"
	app.syncFooterErrorToast()
	firstUntil := app.footerErrorUntil
	if app.footerErrorLast != "Runtime failed" {
		t.Fatalf("expected footerErrorLast to track latest execution error, got %q", app.footerErrorLast)
	}

	app.nowFn = func() time.Time { return base.Add(5 * time.Second) }
	app.state.ExecutionError = "runtime FAILED"
	app.syncFooterErrorToast()
	if !app.footerErrorUntil.Equal(firstUntil) {
		t.Fatalf("expected equal-fold duplicate error to avoid refreshing toast timeout")
	}

	app.state.ExecutionError = ""
	app.syncFooterErrorToast()
	if app.footerErrorLast != "" {
		t.Fatalf("expected empty execution error to clear footerErrorLast")
	}
}

func TestDeferredFooterTickDispatchedOnce(t *testing.T) {
	app, _ := newTestApp(t)
	app.showFooterError("permission denied")
	if app.deferredFooterTick == nil {
		t.Fatal("expected deferred footer tick to be prepared")
	}

	model, cmd := app.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	app = model.(App)
	if app.deferredFooterTick != nil {
		t.Fatal("expected deferred footer tick to be cleared after dispatch")
	}
	if cmd == nil {
		t.Fatal("expected update to include deferred footer tick command")
	}
	if _, ok := cmd().(tickMsg); !ok {
		t.Fatalf("expected tick command, got %T", cmd())
	}
}

func TestHandleLogViewerKeyAndScrollBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.logViewerVisible = true
	for i := 0; i < 30; i++ {
		app.logEntries = append(app.logEntries, logEntry{Timestamp: time.Unix(int64(i), 0), Level: "info", Source: "test", Message: "m"})
	}

	_, _, _, height := app.logViewerBounds()
	app.logViewerOffset = app.logViewerMaxOffset(height)
	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyHome})
	if app.logViewerOffset != 0 {
		t.Fatalf("expected Home to jump to newest offset 0, got %d", app.logViewerOffset)
	}

	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyEnd})
	if app.logViewerOffset != app.logViewerMaxOffset(height) {
		t.Fatalf("expected End to jump to oldest offset, got %d", app.logViewerOffset)
	}

	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if app.logViewerOffset > app.logViewerMaxOffset(height) {
		t.Fatalf("expected PgUp offset to stay clamped, got %d", app.logViewerOffset)
	}
	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyPgDown})
	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyUp})
	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyDown})

	app.handleLogViewerKey(tea.KeyMsg{Type: tea.KeyEsc})
	if app.logViewerVisible {
		t.Fatalf("expected Esc to close log viewer")
	}
	if app.state.StatusText != statusReady {
		t.Fatalf("expected status reset when closing log viewer, got %q", app.state.StatusText)
	}
}

func TestRestoreStatusAfterLogViewerUsesRuntimeState(t *testing.T) {
	app, _ := newTestApp(t)

	app.logViewerPrevStatus = "Manual status"
	app.state.ExecutionError = "runtime failed"
	app.restoreStatusAfterLogViewer()
	if app.state.StatusText != "runtime failed" {
		t.Fatalf("expected execution error to win, got %q", app.state.StatusText)
	}

	app.logViewerPrevStatus = "Manual status"
	app.state.ExecutionError = ""
	app.state.IsCompacting = true
	app.restoreStatusAfterLogViewer()
	if app.state.StatusText != statusCompacting {
		t.Fatalf("expected compacting status, got %q", app.state.StatusText)
	}

	app.logViewerPrevStatus = "Manual status"
	app.state.IsCompacting = false
	app.state.IsAgentRunning = true
	app.state.CurrentTool = "bash"
	app.restoreStatusAfterLogViewer()
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected running tool status, got %q", app.state.StatusText)
	}
}

func TestHandleLogViewerMouseAndClampOffset(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.logViewerVisible = true
	for i := 0; i < 60; i++ {
		app.logEntries = append(app.logEntries, logEntry{Timestamp: time.Unix(int64(i), 0), Level: "info", Source: "test", Message: "m"})
	}

	if !app.handleLogViewerMouse(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}) {
		t.Fatalf("expected outside click to be treated as handled while log viewer is visible")
	}

	x, y, w, h := app.logViewerBounds()
	if w <= 2 || h <= 2 {
		t.Fatalf("expected log viewer bounds to be drawable, got w=%d h=%d", w, h)
	}
	app.handleLogViewerMouse(tea.MouseMsg{
		X:      x + w/2,
		Y:      y + h/2,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	if app.logViewerOffset != 1 {
		t.Fatalf("expected wheel down to increase offset, got %d", app.logViewerOffset)
	}

	app.logViewerOffset = 999
	app.clampLogViewerOffset()
	_, _, _, height := app.logViewerBounds()
	if app.logViewerOffset != app.logViewerMaxOffset(height) {
		t.Fatalf("expected clampLogViewerOffset to constrain offset, got %d", app.logViewerOffset)
	}
}

func TestSetTranscriptOffsetFromScrollbarY(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 28
	app.applyComponentLayout(true)
	app.setTranscriptContent(strings.Repeat("line\n", 200))
	app.transcript.SetYOffset(0)

	_, y, _, h := app.transcriptScrollbarBounds()
	app.setTranscriptOffsetFromScrollbarY(y - 5)
	if app.transcript.YOffset != 0 {
		t.Fatalf("expected dragging above track to clamp to top, got %d", app.transcript.YOffset)
	}

	app.setTranscriptOffsetFromScrollbarY(y + h + 10)
	if app.transcript.YOffset != app.transcriptMaxOffset() {
		t.Fatalf("expected dragging below track to clamp to bottom, got %d want %d", app.transcript.YOffset, app.transcriptMaxOffset())
	}
}

func TestHandleTranscriptMouseDragSupportsMotionType(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 28
	app.applyComponentLayout(true)
	app.setTranscriptContent(strings.Repeat("line\n", 200))

	_, y, _, h := app.transcriptScrollbarBounds()
	app.transcriptScrollbarDrag = true
	before := app.transcript.YOffset
	handled := app.handleTranscriptMouse(tea.MouseMsg{Y: y + h - 1, Type: tea.MouseMotion})
	if !handled {
		t.Fatal("expected mouse motion type during drag to be handled")
	}
	if app.transcript.YOffset == before {
		t.Fatalf("expected drag motion to update offset, still %d", app.transcript.YOffset)
	}
}

func TestSessionLogHelpersAndSwitchBootstrap(t *testing.T) {
	app, _ := newTestApp(t)

	now := time.Unix(1_700_001_000, 0)
	app.logEntries = []logEntry{{Timestamp: now, Level: "info", Source: "bootstrap", Message: "in-memory"}}
	runtime := app.runtime.(*stubRuntime)
	runtime.logEntriesBySID["session-A"] = []agentruntime.SessionLogEntry{
		{Timestamp: now, Level: "info", Source: "file", Message: "persisted"},
	}

	app.setActiveSessionID("session-A")
	if len(app.logEntries) != 2 {
		t.Fatalf("expected bootstrap switch to merge file + in-memory entries, got %d", len(app.logEntries))
	}

	app.setActiveSessionID("")
	if len(app.logEntries) != 0 || app.logViewerOffset != 0 {
		t.Fatalf("expected clearing active session to reset log state")
	}

	runtime.loadLogErr = errors.New("decode failed")
	if got := app.readLogEntriesForSession("session-invalid"); got != nil {
		t.Fatalf("expected load error branch to return nil entries, got %+v", got)
	}
}

func TestAppendActivityCapsAndFooterError(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)

	for i := 0; i < maxActivityEntries+3; i++ {
		app.appendActivity("tool", fmt.Sprintf("warn-%03d", i), "detail", false)
	}
	if len(app.activities) != maxActivityEntries {
		t.Fatalf("expected activities capped at %d, got %d", maxActivityEntries, len(app.activities))
	}

	app.showFooterError("  ")
	before := app.footerErrorText
	app.appendActivity("tool", "failed-run", "", true)
	if app.footerErrorText == before || !strings.Contains(app.footerErrorText, "Error:") {
		t.Fatalf("expected error activity to refresh footer toast, got %q", app.footerErrorText)
	}
}

func TestAddLogEntryWarnAndOffsetClamp(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.logViewerOffset = 999

	app.addLogEntry("tool", "Warn threshold", "almost full")
	if len(app.logEntries) == 0 || app.logEntries[len(app.logEntries)-1].Level != "warn" {
		t.Fatalf("expected warn title to map to warn log level")
	}
	if app.logViewerOffset > app.logViewerMaxOffset(app.logViewerRows(app.height)) {
		t.Fatalf("expected log viewer offset to be clamped, got %d", app.logViewerOffset)
	}
}

func TestHandleLogViewerMouseWheelUpAndScrollClamp(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.logViewerVisible = true
	for i := 0; i < 60; i++ {
		app.logEntries = append(app.logEntries, logEntry{Timestamp: time.Unix(int64(i), 0), Level: "info", Source: "test", Message: "m"})
	}
	_, _, _, h := app.logViewerBounds()
	app.logViewerOffset = 5

	x, y, w, height := app.logViewerBounds()
	app.handleLogViewerMouse(tea.MouseMsg{
		X:      x + w/2,
		Y:      y + height/2,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	if app.logViewerOffset != 4 {
		t.Fatalf("expected wheel up to decrease offset, got %d", app.logViewerOffset)
	}

	app.scrollLogViewer(0, h)
	if app.logViewerOffset != 4 {
		t.Fatalf("expected zero-delta scroll to keep offset unchanged, got %d", app.logViewerOffset)
	}
	app.scrollLogViewer(-1000, h)
	if app.logViewerOffset != 0 {
		t.Fatalf("expected large negative scroll to clamp to 0, got %d", app.logViewerOffset)
	}
	app.scrollLogViewer(1000, h)
	if app.logViewerOffset != app.logViewerMaxOffset(h) {
		t.Fatalf("expected large positive scroll to clamp to max offset, got %d", app.logViewerOffset)
	}
}

func TestMouseHitHelpersGuardWhenBoundsZero(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 0
	app.height = 0
	app.transcript.Width = 0
	app.transcript.Height = 0

	msg := tea.MouseMsg{X: 0, Y: 0}
	if app.isMouseWithinTranscriptScrollbar(msg) {
		t.Fatalf("expected transcript scrollbar hit test to fail when bounds are zero")
	}
	if app.isMouseWithinLogViewer(msg) {
		t.Fatalf("expected log viewer hit test to fail when bounds are zero")
	}
}

func TestReadAndPersistLogEntriesGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if got := app.readLogEntriesForSession("   "); got != nil {
		t.Fatalf("expected blank session id to return nil log entries, got %+v", got)
	}

	app.state.ActiveSessionID = ""
	app.persistLogEntriesForActiveSession()
}

func TestPersistLogEntriesRetryOnSaveFailure(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-save-error"
	app.logEntries = []logEntry{{Timestamp: time.Now(), Level: "info", Source: "test", Message: "m"}}
	app.logPersistDirty = true
	app.logPersistVersion = 1
	runtime.saveLogErr = errors.New("disk full")

	app.persistLogEntriesForActiveSession()
	if !app.logPersistDirty {
		t.Fatal("expected dirty flag to remain true after save failure")
	}
	if app.deferredLogPersistCmd == nil {
		t.Fatal("expected deferred persist command to be rescheduled on save failure")
	}
}

func TestUpdateFocusInputNewSessionAndTodoScroll(t *testing.T) {
	app, runtime := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.startupScreenLocked = false
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("existing message")}},
	}
	app.rebuildTranscript()

	app.focus = panelTranscript
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = model.(App)
	if app.focus != panelInput {
		t.Fatalf("expected Esc to focus input panel")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	app = model.(App)
	if len(runtime.listSessions) != 0 && strings.TrimSpace(app.state.ActiveSessionID) == "" {
		t.Fatalf("expected Ctrl+N to create or activate draft session")
	}
	if !app.startupScreenLocked {
		t.Fatalf("expected Ctrl+N to relock startup screen")
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected Ctrl+N to clear transcript messages")
	}
	if view := app.renderWaterfall(100, 24); !strings.Contains(view, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup content after Ctrl+N, got %q", view)
	}

	app.focus = panelTodo
	app.todoItems = []todoViewItem{
		{ID: "1", Title: "a", Status: "pending"},
		{ID: "2", Title: "b", Status: "pending"},
	}
	app.todoSelectedIndex = 1
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	app = model.(App)
	if app.todoSelectedIndex != 0 {
		t.Fatalf("expected todo selection to move up, got %d", app.todoSelectedIndex)
	}
}

func TestActivateSessionByIDAndCompactDoneInvalidPayload(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.Sessions = []agentsession.Summary{{ID: "s1", Title: "Session 1"}}
	if err := app.activateSessionByID("s1"); err != nil {
		t.Fatalf("activateSessionByID() error = %v", err)
	}

	if handled := runtimeEventCompactDoneHandler(&app, agentruntime.RuntimeEvent{Payload: "invalid"}); handled {
		t.Fatalf("expected compact done handler to ignore invalid payload")
	}
}

func TestHandleTranscriptMouseWheelAndClickFallback(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent(strings.Repeat("line\n", 120))
	app.transcript.SetYOffset(20)

	x, y, w, h := app.transcriptBounds()
	if w <= 2 || h <= 2 {
		t.Fatalf("expected transcript bounds to be drawable, got w=%d h=%d", w, h)
	}

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + w/2,
		Y:      y + h/2,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript wheel up to be handled")
	}

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + w/2,
		Y:      y + h/2,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript wheel down to be handled")
	}

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected left click in transcript to begin selection")
	}
	if !app.textSelection.dragging {
		t.Fatalf("expected left click to enter selection dragging mode")
	}
}

func TestMouseSelectionUsesYOffsetAndCopiesExactRange(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	lines := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("row-%02d-abcdef", i))
	}
	app.setTranscriptContent(strings.Join(lines, "\n"))
	app.transcript.SetYOffset(10)

	x, y, _, _ := app.transcriptBounds()
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 5,
		Y:      y + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected left press to begin selection")
	}
	if got := app.textSelection.startLine; got != 12 {
		t.Fatalf("expected selection start line to include y-offset, got %d", got)
	}

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 9,
		Y:      y + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
		Type:   tea.MouseMotion,
	}) {
		t.Fatalf("expected mouse drag motion to update selection")
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 9,
		Y:      y + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
		Type:   tea.MouseRelease,
	}) {
		t.Fatalf("expected release to finish selection")
	}

	originalClipboard := clipboardWriteAll
	var copied string
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 9,
		Y:      y + 3,
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected right click to copy selected text")
	}

	want := "2-abcdef\nrow-13-ab"
	if copied != want {
		t.Fatalf("expected copied selection %q, got %q", want, copied)
	}
	if app.textSelection.active {
		t.Fatalf("expected selection to be cleared after copy")
	}
}

func TestHighlightTranscriptContentUsesColumnRange(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 6
	app.textSelection.endLine = 0
	app.textSelection.endCol = 11
	app.setTranscriptContent("\x1b[31mhello world\x1b[0m")

	highlighted := app.highlightTranscriptContent(app.transcriptContent)
	plain := copyCodeANSIPattern.ReplaceAllString(highlighted, "")
	if plain != "hello world" {
		t.Fatalf("expected highlighted output to preserve visible text, got %q", plain)
	}
	if app.transcriptContent != "\x1b[31mhello world\x1b[0m" {
		t.Fatalf("expected transcriptContent to keep raw normalized content")
	}
}

func TestInputBoundsAndTranscriptOffsetGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 0
	app.height = 0
	if app.isMouseWithinInput(tea.MouseMsg{X: 0, Y: 0}) {
		t.Fatalf("expected input hit test to fail when layout is zero-sized")
	}

	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.transcript.Height = 0
	app.setTranscriptOffsetFromScrollbarY(10)

	app.transcript.Height = 8
	app.setTranscriptContent("short\n")
	app.transcript.SetYOffset(5)
	_, y, _, _ := app.transcriptScrollbarBounds()
	app.setTranscriptOffsetFromScrollbarY(y + 1)
	if app.transcript.YOffset != 0 {
		t.Fatalf("expected maxOffset<=0 branch to reset transcript y-offset, got %d", app.transcript.YOffset)
	}
}

func TestRebuildActivityWithHeightAndPersistPathGuard(t *testing.T) {
	app, _ := newTestApp(t)
	app.activity.Width = 30
	app.activity.Height = 5
	app.activities = []tuistate.ActivityEntry{
		{Kind: "tool", Title: "run", Detail: "ok"},
	}
	app.rebuildActivity()
	if strings.TrimSpace(app.activity.View()) == "" {
		t.Fatalf("expected rebuildActivity to render entries when viewport height is available")
	}

	app.state.ActiveSessionID = "___"
	app.persistLogEntriesForActiveSession()
}

func updateWithSkillCommandResult(t *testing.T, app App, result skillCommandResultMsg) App {
	t.Helper()

	model, _ := app.Update(result)
	return model.(App)
}

func assertIgnoredStaleSkillResultActivity(t *testing.T, app App, beforeActivities int, wantError bool) tuistate.ActivityEntry {
	t.Helper()

	if len(app.activities) != beforeActivities+1 {
		t.Fatalf("expected stale skill result to be logged, got %d activities", len(app.activities))
	}
	last := app.activities[len(app.activities)-1]
	if last.Title != "Ignored stale skill command result" {
		t.Fatalf("expected stale result activity title, got %q", last.Title)
	}
	if last.IsError != wantError {
		t.Fatalf("expected stale result error flag=%v, got %v", wantError, last.IsError)
	}
	return last
}

func TestUpdateIgnoresStaleSkillCommandResultBySession(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-current"
	app.state.StatusText = "before"
	beforeActivities := len(app.activities)

	app = updateWithSkillCommandResult(t, app, skillCommandResultMsg{
		Notice:           "should be ignored",
		RequestSessionID: "session-old",
	})

	if app.state.StatusText != "before" {
		t.Fatalf("expected stale skill result to be ignored, got status %q", app.state.StatusText)
	}
	assertIgnoredStaleSkillResultActivity(t, app, beforeActivities, false)
}

func TestUpdateAcceptsSkillCommandResultForCurrentSession(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-current"

	app = updateWithSkillCommandResult(t, app, skillCommandResultMsg{
		Notice:           "Skill command completed.",
		RequestSessionID: "session-current",
	})

	if app.state.StatusText != "Skill command completed." {
		t.Fatalf("expected status to be updated, got %q", app.state.StatusText)
	}
}

func TestUpdateLogsStaleSkillCommandErrorBySession(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-current"
	app.state.StatusText = "before"
	beforeActivities := len(app.activities)

	app = updateWithSkillCommandResult(t, app, skillCommandResultMsg{
		Err:              errors.New("activate failed"),
		RequestSessionID: "session-old",
	})

	if app.state.StatusText != "before" {
		t.Fatalf("expected stale skill error to keep current status, got %q", app.state.StatusText)
	}
	last := assertIgnoredStaleSkillResultActivity(t, app, beforeActivities, true)
	if !strings.Contains(last.Detail, "activate failed") {
		t.Fatalf("expected stale error detail to include original error, got %q", last.Detail)
	}
}
