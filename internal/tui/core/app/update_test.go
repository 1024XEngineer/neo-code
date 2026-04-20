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
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	approvalflow "neo-code/internal/runtime/approval"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiservices "neo-code/internal/tui/services"
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
	return s.models, nil
}

func (s stubProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if s.listModelsErr != nil {
		return nil, s.listModelsErr
	}
	return s.models, nil
}

func (s stubProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
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
	events          chan agentruntime.RuntimeEvent
	prepareInputs   []agentruntime.PrepareInput
	prepareErr      error
	preparedOutput  agentruntime.UserInput
	runInputs       []agentruntime.UserInput
	systemToolCalls []agentruntime.SystemToolInput
	systemToolRes   tools.ToolResult
	systemToolErr   error
	resolveCalls    []agentruntime.PermissionResolutionInput
	resolveErr      error
	cancelInvoked   bool
	listSessions    []agentsession.Summary
	listSessionsErr error
	loadSessions    map[string]agentsession.Session
	loadSessionErr  error
	logEntriesBySID map[string][]agentruntime.SessionLogEntry
	loadLogErr      error
	saveLogErr      error
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
	return nil
}

func (s *stubRuntime) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (s *stubRuntime) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return nil, nil
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
	app.layoutCached = true
	app.cachedWidth = app.width
	app.cachedHeight = app.height
	return app, runtime
}

func TestSubmitProviderAddFormRequiresAnthropicBaseURL(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()

	app.providerAddForm.Name = "anthropic-gateway"
	app.providerAddForm.Driver = provider.DriverAnthropic
	app.providerAddForm.APIKeyEnv = "ANTHROPIC_GATEWAY_API_KEY"
	app.providerAddForm.APIKey = "test-key"
	app.providerAddForm.BaseURL = ""

	cmd := app.submitProviderAddForm()
	if cmd != nil {
		t.Fatalf("expected nil command for invalid anthropic form")
	}
	if !strings.Contains(app.providerAddForm.Error, "Base URL is required") {
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
	app.providerAddForm.ModelSource = provider.ModelSourceManual
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
		t.Error("Update returned non-nil cmd for runFinishedMsg with error")
	}

	canceledMsg := runFinishedMsg{Err: context.Canceled}
	model, cmd = app.Update(canceledMsg)
	if model == nil {
		t.Error("Update returned nil model for runFinishedMsg with canceled error")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for runFinishedMsg with canceled error")
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
	if decision, ok := parsePermissionShortcut("y"); !ok || decision != approvalflow.DecisionAllowOnce {
		t.Fatalf("expected allow_once, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("a"); !ok || decision != approvalflow.DecisionAllowSession {
		t.Fatalf("expected allow_session, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("n"); !ok || decision != approvalflow.DecisionReject {
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
	if runtime.resolveCalls[0].Decision != approvalflow.DecisionAllowOnce {
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
		Decision:  approvalflow.DecisionAllowOnce,
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
	cmd := runResolvePermission(runtime, "perm-5", approvalflow.DecisionAllowSession)
	if cmd == nil {
		t.Fatalf("expected command")
	}
	msg := cmd()
	resolved, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if resolved.RequestID != "perm-5" || resolved.Decision != approvalflow.DecisionAllowSession {
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
		Decision:  approvalflow.DecisionAllowOnce,
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
	if len(runtime.resolveCalls) != 1 || runtime.resolveCalls[0].Decision != approvalflow.DecisionReject {
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
	last := app.activeMessages[len(app.activeMessages)-1]
	if last.Role != roleTool || messageText(last) != "ok" {
		t.Fatalf("unexpected tool message: %#v", last)
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

func TestIsWorkspaceCommandInput(t *testing.T) {
	if !isWorkspaceCommandInput("& ls -la") {
		t.Fatalf("expected workspace command prefix to be detected")
	}
	if isWorkspaceCommandInput("ls -la") {
		t.Fatalf("expected non-workspace command to be false")
	}
}

func TestExtractWorkspaceCommand(t *testing.T) {
	command, err := extractWorkspaceCommand("& git status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if command != "git status" {
		t.Fatalf("expected command to be extracted, got %q", command)
	}

	if _, err := extractWorkspaceCommand("&"); err == nil {
		t.Fatalf("expected error for empty command")
	}
	if _, err := extractWorkspaceCommand("git status"); err == nil {
		t.Fatalf("expected error for missing prefix")
	}
}

func TestFormatWorkspaceCommandResult(t *testing.T) {
	output := "clean\n"
	got := formatWorkspaceCommandResult("git status", output, nil)
	if !strings.Contains(got, "Command: & git status") {
		t.Fatalf("expected success header, got %q", got)
	}
	if !strings.Contains(got, "clean") {
		t.Fatalf("expected output to be included")
	}

	errResult := formatWorkspaceCommandResult("git status", "", errors.New("boom"))
	if !strings.Contains(errResult, "Command Failed: & git status") {
		t.Fatalf("expected failure header, got %q", errResult)
	}
	if !strings.Contains(errResult, "boom") {
		t.Fatalf("expected error message in result")
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

func TestShellArgsAndPowerShellUTF8(t *testing.T) {
	args := shellArgs("bash", "echo hi")
	if len(args) == 0 {
		t.Fatalf("expected shell args to be returned")
	}
	utf8 := powershellUTF8Command("echo hi")
	if utf8 == "" {
		t.Fatalf("expected powershell utf8 command")
	}
}

func TestSanitizeAndDecodeWorkspaceOutput(t *testing.T) {
	raw := []byte("hello\u0000world")
	sanitized := sanitizeWorkspaceOutput(raw)
	if sanitized == "" {
		t.Fatalf("expected sanitized output")
	}
	decoded := decodeWorkspaceOutput(raw)
	if decoded == "" {
		t.Fatalf("expected decoded output")
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
	payload := tuiservices.RuntimeRunContextPayload{
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

func TestUpdatePasteImageShortcutFailure(t *testing.T) {
	app, _ := newTestApp(t)
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if !strings.Contains(strings.ToLower(app.state.StatusText), "clipboard") {
		t.Fatalf("expected clipboard failure status, got %q", app.state.StatusText)
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
	payload := tuiservices.RuntimeToolStatusPayload{ToolCallID: "tool-1", ToolName: "bash", Status: string(tuistate.ToolLifecyclePlanned)}
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
	payload := tuiservices.RuntimeUsagePayload{Run: tuiservices.RuntimeUsageSnapshot{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}}
	handled := runtimeEventUsageHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.TokenUsage.RunTotalTokens != 3 {
		t.Fatalf("expected token usage to update")
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

func TestRuntimeEventProviderRetryHandler(t *testing.T) {
	app, _ := newTestApp(t)
	runtimeEventProviderRetryHandler(&app, agentruntime.RuntimeEvent{Payload: "retry"})
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected thinking status")
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
	app.refreshHelpPicker()
	app.openHelpPicker()
	selectPickerItemByID(&app.helpPicker, slashCommandStatus)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected help picker to close after selecting /status")
	}
	if cmd == nil {
		t.Fatalf("expected local slash command cmd")
	}
	msg := cmd()
	result, ok := msg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", msg)
	}
	if !strings.Contains(result.Notice, "Status:") {
		t.Fatalf("expected status output in slash result, got %q", result.Notice)
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

	// /cwd is not handled by handleImmediateSlashCommand and is not in the direct switch cases.
	// It should therefore execute through runLocalCommand and return a localCommandResultMsg.
	localCmd := app.runSlashCommandSelection("/cwd")
	if localCmd == nil {
		t.Fatalf("expected local slash cmd for /cwd")
	}

	statusCmd := app.runSlashCommandSelection(slashCommandStatus)
	if statusCmd == nil {
		t.Fatalf("expected local slash cmd for status")
	}
	statusMsg := statusCmd()
	statusResult, ok := statusMsg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", statusMsg)
	}
	if !strings.Contains(statusResult.Notice, "Status:") {
		t.Fatalf("expected status output in local command result")
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
		X: sx + 1, Y: sy + max(1, sh/2), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript scrollbar press to be handled")
	}
	if !app.transcriptScrollbarDrag {
		t.Fatalf("expected scrollbar drag mode to start after press")
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: sx + 1, Y: sy + sh - 1, Action: tea.MouseActionMotion,
	}) {
		t.Fatalf("expected transcript scrollbar drag motion to be handled")
	}
	if app.transcript.YOffset <= offsetBeforeDrag {
		t.Fatalf("expected drag motion to move transcript offset, got %d <= %d", app.transcript.YOffset, offsetBeforeDrag)
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: sx + 1, Y: sy + sh - 1, Action: tea.MouseActionRelease,
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

	app.providerAddForm.Step = 6 // api key env
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
	if runtimeMsg.Event.RunID != "run-listen" {
		t.Fatalf("expected forwarded runtime event")
	}

	close(eventCh)
	cmd = ListenForRuntimeEvent(eventCh)
	msg = cmd()
	if _, ok := msg.(RuntimeClosedMsg); !ok {
		t.Fatalf("expected RuntimeClosedMsg after channel close, got %T", msg)
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
			ModelSource: provider.ModelSourceManual,
		}); !strings.Contains(err, "API Key is required") {
			t.Fatalf("expected missing key error, got %q", err)
		}
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "demo",
			Driver:      provider.DriverGemini,
			ModelSource: provider.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "",
		}); !strings.Contains(err, "API Key Env is required") {
			t.Fatalf("expected missing env key error, got %q", err)
		}
	})

	t.Run("openai compat discover mode uses default discovery endpoint path", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:        "openai-compat",
			Driver:      provider.DriverOpenAICompat,
			ModelSource: provider.ModelSourceDiscover,
			APIKey:      "k",
			APIKeyEnv:   "OPENAI_COMPAT_API_KEY",
		})
		if err != "" {
			t.Fatalf("expected default discovery endpoint, got %q and req=%+v", err, req)
		}
		if req.DiscoveryEndpointPath != provider.DiscoveryEndpointPathModels {
			t.Fatalf("expected default discovery endpoint /models, got %+v", req)
		}
	})

	t.Run("openai compat discover mode normalizes discovery settings", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:                  "openai-compat-discover",
			Driver:                provider.DriverOpenAICompat,
			ModelSource:           provider.ModelSourceDiscover,
			ChatEndpointPath:      "/chat/completions",
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_DISCOVER_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.ModelSource != provider.ModelSourceDiscover {
			t.Fatalf("expected discover model source, got %q", req.ModelSource)
		}
		if req.DiscoveryEndpointPath != provider.DiscoveryEndpointPathModels {
			t.Fatalf("expected default discovery endpoint, got %q", req.DiscoveryEndpointPath)
		}
		if req.ChatEndpointPath != "/chat/completions" {
			t.Fatalf("expected default chat endpoint, got %q", req.ChatEndpointPath)
		}
	})

	t.Run("strips control chars from env key before validation", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:        "openai-compat",
			Driver:      provider.DriverOpenAICompat,
			ModelSource: provider.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "\x00OPENAI_COMPAT_API_KEY",
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
			Name:        "openai-compat",
			Driver:      provider.DriverOpenAICompat,
			ModelSource: provider.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "PATH",
		}); !strings.Contains(err, "protected") {
			t.Fatalf("expected protected env key error, got %q", err)
		}
	})

	t.Run("gemini applies default base url", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:        "gemini",
			Driver:      provider.DriverGemini,
			ModelSource: provider.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "GEMINI_GATEWAY_API_KEY",
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
			ModelSource:           provider.ModelSourceDiscover,
			APIKey:                "k",
			APIKeyEnv:             "OPENAI_COMPAT_API_KEY",
			DiscoveryEndpointPath: "https://api.example.com/models",
		}); !strings.Contains(err, "relative path") {
			t.Fatalf("expected invalid discovery endpoint path error, got %q", err)
		}
	})

	t.Run("rejects invalid chat endpoint path", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:             "openai-compat",
			Driver:           provider.DriverOpenAICompat,
			ModelSource:      provider.ModelSourceDiscover,
			APIKey:           "k",
			APIKeyEnv:        "OPENAI_COMPAT_API_KEY",
			ChatEndpointPath: "https://api.example.com/chat/completions",
		}); !strings.Contains(err, "relative path") {
			t.Fatalf("expected invalid chat endpoint path error, got %q", err)
		}
	})

	t.Run("anthropic/custom require base url", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "anthropic",
			Driver:      provider.DriverAnthropic,
			ModelSource: provider.ModelSourceManual,
			APIKey:      "k",
			APIKeyEnv:   "ANTHROPIC_GATEWAY_API_KEY",
		}); !strings.Contains(err, "Base URL is required") {
			t.Fatalf("expected anthropic base url error, got %q", err)
		}

		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:        "custom",
			Driver:      "custom-driver",
			ModelSource: provider.ModelSourceManual,
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
			ModelSource:           provider.ModelSourceManual,
			APIKey:                "k",
			APIKeyEnv:             "MANUAL_GATEWAY_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.DiscoveryEndpointPath != "" {
			t.Fatalf("expected manual mode to clear discovery settings, got %+v", req)
		}
	})
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

	model, _ = app.Update(workspaceCommandResultMsg{Command: "", Err: errors.New("workspace failed")})
	app = model.(App)
	if app.state.StatusText != "workspace failed" {
		t.Fatalf("expected workspace empty command error status")
	}

	model, _ = app.Update(workspaceCommandResultMsg{Command: "git status", Err: errors.New("boom")})
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Command failed") {
		t.Fatalf("expected workspace command failed status")
	}

	model, _ = app.Update(workspaceCommandResultMsg{Command: "git status", Output: "clean"})
	app = model.(App)
	if app.state.StatusText != statusCommandDone {
		t.Fatalf("expected workspace success status, got %q", app.state.StatusText)
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
	if !app.state.ShowHelp {
		t.Fatalf("expected help to toggle on")
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

	app.providerAddForm = nil
	app.state.ActivePicker = pickerNone
	app.input.SetValue("& echo hi")
	app.state.InputText = "& echo hi"
	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.StatusText != statusRunningCommand {
		t.Fatalf("expected workspace command running status, got %q", app.state.StatusText)
	}
	if cmd == nil {
		t.Fatalf("expected workspace command to return async cmd")
	}

	app.input.SetValue("&")
	app.state.InputText = "&"
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if strings.TrimSpace(app.state.ExecutionError) == "" {
		t.Fatalf("expected invalid workspace command error")
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
	app.providerAddForm.Driver = provider.DriverAnthropic
	app.providerAddForm.Step = 4 // chat endpoint
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

	fields := providerAddVisibleFields(provider.DriverOpenAICompat, provider.ModelSourceDiscover)
	if len(fields) == 0 || fields[0] != providerAddFieldName {
		t.Fatalf("expected provider add visible fields to start from name field")
	}
	if !slices.Contains(fields, providerAddFieldDiscoveryEndpointPath) ||
		!slices.Contains(fields, providerAddFieldChatEndpointPath) {
		t.Fatalf("expected discover source to include discovery fields")
	}

	manualFields := providerAddVisibleFields(provider.DriverOpenAICompat, provider.ModelSourceManual)
	if slices.Contains(manualFields, providerAddFieldDiscoveryEndpointPath) ||
		slices.Contains(manualFields, providerAddFieldDiscoveryEndpointPath) {
		t.Fatalf("expected manual source to exclude discovery fields")
	}
	clampProviderAddStep(nil)

	if _, err := buildProviderAddRequest(providerAddFormState{
		Name:                  "custom-provider",
		Driver:                "custom-driver",
		ModelSource:           provider.ModelSourceDiscover,
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

func TestRebuildTranscriptDoesNotCollapseAssistantAcrossToolBoundary(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 32
	app.applyComponentLayout(true)
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before tool")}},
		{Role: roleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool output")}},
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after tool")}},
	}

	app.rebuildTranscript()
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if count := strings.Count(plain, messageTagAgent); count != 2 {
		t.Fatalf("expected two agent tags across tool boundary, got %d in %q", count, plain)
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

	if app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected plain left click without copy button hit to return false")
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
