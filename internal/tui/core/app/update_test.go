package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/memo"
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
	selectErr      error
	selectDelay    time.Duration
	selectResponse configstate.Selection
}

func (s stubProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
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
	return s.models, nil
}

func (s stubProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	providerID := ""
	if len(s.providers) > 0 {
		providerID = s.providers[0].ID
	}
	return configstate.Selection{ProviderID: providerID, ModelID: modelID}, nil
}

type stubRuntime struct {
	events          chan agentruntime.RuntimeEvent
	runInputs       []agentruntime.UserInput
	resolveCalls    []agentruntime.PermissionResolutionInput
	resolveErr      error
	cancelInvoked   bool
	listSessions    []agentsession.Summary
	listSessionsErr error
	loadSessions    map[string]agentsession.Session
	loadSessionErr  error
}

type snapshotRuntime struct {
	*stubRuntime
	sessionContext any
	sessionUsage   any
	runSnapshot    any
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{events: make(chan agentruntime.RuntimeEvent)}
}

func (s *stubRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	s.runInputs = append(s.runInputs, input)
	return nil
}

func (s *stubRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
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

	return newTestAppWithProviderService(t, stubProviderService{providers: providers, models: models})
}

func TestSubmitProviderAddFormRequiresAnthropicBaseURL(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()

	app.providerAddForm.Name = "anthropic-gateway"
	app.providerAddForm.Driver = provider.DriverAnthropic
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
	restorePersistUserEnv := persistProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error { return nil }
	t.Cleanup(func() { persistProviderUserEnvVar = restorePersistUserEnv })

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
		selectResponse: configstate.Selection{
			ProviderID: providerName,
			ModelID:    modelID,
		},
	}
	app, _ := newTestAppWithProviderService(t, service)
	app.startProviderAddForm()

	app.providerAddForm.Name = providerName
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://team-gateway.example.com/v1"
	app.providerAddForm.APIKey = "sk-test-123"
	app.providerAddForm.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command for provider add")
	}
	if !app.providerAddForm.Submitting {
		t.Fatalf("expected form to enter submitting state")
	}

	envName := providerAddAPIKeyEnv(providerName)
	defer os.Unsetenv(envName)

	msg := cmd()
	if _, ok := msg.(providerAddResultMsg); !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if got := strings.TrimSpace(os.Getenv(envName)); got != "sk-test-123" {
		t.Fatalf("expected API key in env %s, got %q", envName, got)
	}
	envData, readErr := os.ReadFile(config.EnvFilePath(app.configManager.BaseDir()))
	if readErr != nil {
		t.Fatalf("expected persisted env file, read error = %v", readErr)
	}
	if !strings.Contains(string(envData), envName+"=sk-test-123") {
		t.Fatalf("expected env file to persist API key, got %q", string(envData))
	}

	next, _ := app.Update(msg)
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
}

func TestSubmitProviderAddFormRedactsSensitiveError(t *testing.T) {
	restorePersistUserEnv := persistProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error { return nil }
	t.Cleanup(func() { persistProviderUserEnvVar = restorePersistUserEnv })

	secretKey := "sk-secret-456"
	service := stubProviderService{
		selectErr: errors.New("authentication failed for key " + secretKey),
	}
	app, _ := newTestAppWithProviderService(t, service)
	app.startProviderAddForm()

	app.providerAddForm.Name = "redact-gateway"
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://redact-gateway.example.com/v1"
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

func TestSubmitProviderAddFormRollsBackPersistedStateOnSelectFailure(t *testing.T) {
	restorePersistUserEnv := persistProviderUserEnvVar
	restoreDeleteUserEnv := deleteProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error { return nil }
	deleteProviderUserEnvVar = func(key string) error { return nil }
	t.Cleanup(func() { persistProviderUserEnvVar = restorePersistUserEnv })
	t.Cleanup(func() { deleteProviderUserEnvVar = restoreDeleteUserEnv })

	providerName := "rollback-gateway"
	envName := providerAddAPIKeyEnv(providerName)
	restoreEnv := captureEnv(t, envName)
	defer restoreEnv()
	if err := os.Setenv(envName, "previous-value"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}

	service := stubProviderService{
		selectErr: errors.New("select failed"),
	}
	app, _ := newTestAppWithProviderService(t, service)
	app.startProviderAddForm()
	app.providerAddForm.Name = providerName
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://rollback.example.com/v1"
	app.providerAddForm.APIKey = "sk-failed-rollback"
	app.providerAddForm.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command")
	}
	msg := cmd()
	result, ok := msg.(providerAddResultMsg)
	if !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if strings.TrimSpace(result.Error) == "" {
		t.Fatalf("expected failure result")
	}

	if got := os.Getenv(envName); got != "previous-value" {
		t.Fatalf("expected process env restored, got %q", got)
	}

	envData, readErr := os.ReadFile(config.EnvFilePath(app.configManager.BaseDir()))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read env file: %v", readErr)
	}
	if strings.Contains(string(envData), envName+"=") {
		t.Fatalf("expected persisted env key rollback, got %q", string(envData))
	}

	providerPath := filepath.Join(app.configManager.BaseDir(), "providers", providerName)
	if _, err := os.Stat(providerPath); !os.IsNotExist(err) {
		t.Fatalf("expected provider dir rollback, stat err = %v", err)
	}
}

func TestSubmitProviderAddFormRollsBackOnUserEnvPersistFailure(t *testing.T) {
	restorePersistUserEnv := persistProviderUserEnvVar
	restoreDeleteUserEnv := deleteProviderUserEnvVar
	persistProviderUserEnvVar = func(key string, value string) error { return errors.New("user env failed") }
	deleteProviderUserEnvVar = func(key string) error { return nil }
	t.Cleanup(func() { persistProviderUserEnvVar = restorePersistUserEnv })
	t.Cleanup(func() { deleteProviderUserEnvVar = restoreDeleteUserEnv })

	providerName := "user-env-fail"
	envName := providerAddAPIKeyEnv(providerName)
	restoreEnv := captureEnv(t, envName)
	defer restoreEnv()
	_ = os.Unsetenv(envName)

	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Name = providerName
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://rollback.example.com/v1"
	app.providerAddForm.APIKey = "sk-failed"
	app.providerAddForm.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command")
	}
	msg := cmd()
	result, ok := msg.(providerAddResultMsg)
	if !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if strings.TrimSpace(result.Error) == "" {
		t.Fatalf("expected failure result")
	}

	if got := os.Getenv(envName); got != "" {
		t.Fatalf("expected process env to stay unset, got %q", got)
	}

	envData, readErr := os.ReadFile(config.EnvFilePath(app.configManager.BaseDir()))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read env file: %v", readErr)
	}
	if strings.Contains(string(envData), envName+"=") {
		t.Fatalf("expected persisted env key rollback, got %q", string(envData))
	}

	providerPath := filepath.Join(app.configManager.BaseDir(), "providers", providerName)
	if _, err := os.Stat(providerPath); !os.IsNotExist(err) {
		t.Fatalf("expected provider dir rollback, stat err = %v", err)
	}
}

func TestSubmitProviderAddFormRestoresExistingStateOnSelectFailure(t *testing.T) {
	restorePersistUserEnv := persistProviderUserEnvVar
	restoreDeleteUserEnv := deleteProviderUserEnvVar
	restoreLookupUserEnv := lookupProviderUserEnvVar
	userEnvStore := map[string]string{}
	persistProviderUserEnvVar = func(key string, value string) error {
		userEnvStore[key] = value
		return nil
	}
	deleteProviderUserEnvVar = func(key string) error {
		delete(userEnvStore, key)
		return nil
	}
	lookupProviderUserEnvVar = func(key string) (string, bool, error) {
		value, ok := userEnvStore[key]
		return value, ok, nil
	}
	t.Cleanup(func() { persistProviderUserEnvVar = restorePersistUserEnv })
	t.Cleanup(func() { deleteProviderUserEnvVar = restoreDeleteUserEnv })
	t.Cleanup(func() { lookupProviderUserEnvVar = restoreLookupUserEnv })

	providerName := "restore-existing-provider"
	envName := providerAddAPIKeyEnv(providerName)
	restoreEnv := captureEnv(t, envName)
	defer restoreEnv()

	app, _ := newTestAppWithProviderService(t, stubProviderService{
		selectErr: errors.New("select failed"),
	})

	if err := config.SaveCustomProvider(
		app.configManager.BaseDir(),
		providerName,
		provider.DriverOpenAICompat,
		"https://old.example.com/v1",
		envName,
		provider.OpenAICompatibleAPIStyleChatCompletions,
		"",
		"",
	); err != nil {
		t.Fatalf("SaveCustomProvider() error = %v", err)
	}
	if err := config.PersistEnvVar(app.configManager.BaseDir(), envName, "old-file-key"); err != nil {
		t.Fatalf("PersistEnvVar() error = %v", err)
	}
	if err := os.Setenv(envName, "old-process-key"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	userEnvStore[envName] = "old-user-key"

	app.startProviderAddForm()
	app.providerAddForm.Name = providerName
	app.providerAddForm.Driver = provider.DriverOpenAICompat
	app.providerAddForm.BaseURL = "https://new.example.com/v1"
	app.providerAddForm.APIKey = "new-key"
	app.providerAddForm.APIStyle = provider.OpenAICompatibleAPIStyleChatCompletions

	cmd := app.submitProviderAddForm()
	if cmd == nil {
		t.Fatalf("expected async command")
	}
	msg := cmd()
	result, ok := msg.(providerAddResultMsg)
	if !ok {
		t.Fatalf("expected providerAddResultMsg, got %T", msg)
	}
	if strings.TrimSpace(result.Error) == "" {
		t.Fatalf("expected failure result")
	}

	if got := os.Getenv(envName); got != "old-process-key" {
		t.Fatalf("expected process env restored, got %q", got)
	}
	if got := userEnvStore[envName]; got != "old-user-key" {
		t.Fatalf("expected user env restored, got %q", got)
	}

	envData, readErr := os.ReadFile(config.EnvFilePath(app.configManager.BaseDir()))
	if readErr != nil {
		t.Fatalf("read env file: %v", readErr)
	}
	if !strings.Contains(string(envData), envName+"=old-file-key") {
		t.Fatalf("expected persisted env restored, got %q", string(envData))
	}

	providerPath := filepath.Join(app.configManager.BaseDir(), "providers", providerName, "provider.yaml")
	providerData, readProviderErr := os.ReadFile(providerPath)
	if readProviderErr != nil {
		t.Fatalf("read provider config: %v", readProviderErr)
	}
	providerText := string(providerData)
	if !strings.Contains(providerText, "https://old.example.com/v1") {
		t.Fatalf("expected provider config restored, got %q", providerText)
	}
	if strings.Contains(providerText, "https://new.example.com/v1") {
		t.Fatalf("expected new provider config to be rolled back, got %q", providerText)
	}
}

func TestTrimLastRune(t *testing.T) {
	if got := trimLastRune(""); got != "" {
		t.Fatalf("trimLastRune(empty) = %q, want empty", got)
	}
	if got := trimLastRune("ab"); got != "a" {
		t.Fatalf("trimLastRune(ascii) = %q, want a", got)
	}
	if got := trimLastRune("你好"); got != "你" {
		t.Fatalf("trimLastRune(utf8) = %q, want 你", got)
	}
}

func TestLoadAndRestoreEnvFileSnapshot(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")

	missingSnapshot, err := loadFileSnapshot(envPath)
	if err != nil {
		t.Fatalf("loadFileSnapshot(missing) error = %v", err)
	}
	if missingSnapshot.Exists {
		t.Fatalf("expected missing snapshot")
	}

	if err := os.WriteFile(envPath, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	existingSnapshot, err := loadFileSnapshot(envPath)
	if err != nil {
		t.Fatalf("loadFileSnapshot(existing) error = %v", err)
	}
	if !existingSnapshot.Exists {
		t.Fatalf("expected existing snapshot")
	}

	if err := os.WriteFile(envPath, []byte("A=2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := restoreEnvFileSnapshot(envPath, existingSnapshot); err != nil {
		t.Fatalf("restoreEnvFileSnapshot(existing) error = %v", err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "A=1\n" {
		t.Fatalf("expected snapshot content restored, got %q", string(data))
	}

	if err := restoreEnvFileSnapshot(envPath, missingSnapshot); err != nil {
		t.Fatalf("restoreEnvFileSnapshot(missing) error = %v", err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("expected env file removed for missing snapshot, stat err = %v", err)
	}
}

func TestRestoreProviderConfigSnapshot(t *testing.T) {
	baseDir := t.TempDir()
	providerName := "snapshot-provider"
	providerPath := filepath.Join(baseDir, "providers", providerName, "provider.yaml")
	if err := os.MkdirAll(filepath.Dir(providerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(providerPath, []byte("name: old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	snapshot, err := loadFileSnapshot(providerPath)
	if err != nil {
		t.Fatalf("loadFileSnapshot() error = %v", err)
	}
	if err := os.WriteFile(providerPath, []byte("name: new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := restoreProviderConfigSnapshot(baseDir, providerName, snapshot); err != nil {
		t.Fatalf("restoreProviderConfigSnapshot(existing) error = %v", err)
	}
	data, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "name: old\n" {
		t.Fatalf("expected provider snapshot restored, got %q", string(data))
	}

	missingSnapshot := fileSnapshot{}
	if err := restoreProviderConfigSnapshot(baseDir, providerName, missingSnapshot); err != nil {
		t.Fatalf("restoreProviderConfigSnapshot(missing) error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(providerPath)); !os.IsNotExist(err) {
		t.Fatalf("expected provider dir removed for missing snapshot, stat err = %v", err)
	}
}

func captureEnv(t *testing.T, key string) func() {
	t.Helper()
	value, exists := os.LookupEnv(key)
	return func() {
		if exists {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
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

func TestSplitIndentedCodeSegmentsDetectsCodeFeaturesInCodeMode(t *testing.T) {
	content := "func main() {\nreturn 1\n}\nplain text"
	segments := splitIndentedCodeSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected code and text segments, got %d", len(segments))
	}
	if segments[0].Kind != markdownSegmentCode {
		t.Fatalf("expected first segment to be code")
	}
	if !strings.Contains(segments[0].Code, "return 1") {
		t.Fatalf("expected code segment to include return statement, got %q", segments[0].Code)
	}
}

func TestExtractFencedCodeBlocks(t *testing.T) {
	content := "text\n```go\nfmt.Println(\"ok\")\n```\nend"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 1 || blocks[0] == "" {
		t.Fatalf("expected one code block")
	}
}

func TestParseCopyCodeButton(t *testing.T) {
	id, start, end, ok := parseCopyCodeButton("[Copy code #12]")
	if !ok || id != 12 || start >= end {
		t.Fatalf("unexpected parse result: id=%d start=%d end=%d ok=%v", id, start, end, ok)
	}
	if _, _, _, ok := parseCopyCodeButton("no button"); ok {
		t.Fatalf("expected no button parse")
	}
}

func TestCopyCodeBlockByIDSuccess(t *testing.T) {
	app, _ := newTestApp(t)

	var got string
	originalClipboard := clipboardWriteAll
	clipboardWriteAll = func(text string) error {
		got = text
		return nil
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	app.setCodeCopyBlocks([]copyCodeButtonBinding{{ID: 1, Code: "code"}})
	ok := app.copyCodeBlockByID(1)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if got != "code" {
		t.Fatalf("expected clipboard content, got %q", got)
	}
	if app.state.StatusText == "" {
		t.Fatalf("expected status text to be set")
	}
}

func TestCopyCodeBlockByIDMissing(t *testing.T) {
	app, _ := newTestApp(t)

	ok := app.copyCodeBlockByID(99)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if app.state.StatusText != statusCodeCopyError {
		t.Fatalf("expected error status, got %s", app.state.StatusText)
	}
}

func TestCopyCodeBlockByIDClipboardError(t *testing.T) {
	app, _ := newTestApp(t)

	originalClipboard := clipboardWriteAll
	clipboardWriteAll = func(text string) error {
		return errors.New("fail")
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	app.setCodeCopyBlocks([]copyCodeButtonBinding{{ID: 2, Code: "code"}})
	ok := app.copyCodeBlockByID(2)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if app.state.StatusText != statusCodeCopyError {
		t.Fatalf("expected error status, got %s", app.state.StatusText)
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

func TestRuntimeEventRunContextHandlerInvalidatesModelCapabilityCache(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentProvider = "provider-a"
	app.state.CurrentModel = "model-a"
	app.currentModelCapabilities = modelCapabilityState{
		checked:            true,
		supportsImageInput: true,
	}

	payload := tuiservices.RuntimeRunContextPayload{
		Provider: "provider-b",
		Model:    "model-b",
	}
	_ = runtimeEventRunContextHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if app.currentModelCapabilities.checked {
		t.Fatalf("expected capability cache to be invalidated when provider/model changes")
	}
}

func TestSyncConfigStateInvalidatesModelCapabilityCache(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentProvider = "provider-a"
	app.state.CurrentModel = "model-a"
	app.currentModelCapabilities = modelCapabilityState{
		checked:            true,
		supportsImageInput: true,
	}

	app.syncConfigState(config.Config{
		SelectedProvider: "provider-b",
		CurrentModel:     "model-b",
		Workdir:          app.state.CurrentWorkdir,
	})
	if app.currentModelCapabilities.checked {
		t.Fatalf("expected capability cache to be invalidated")
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

func TestUpdateSendWithUnsupportedImageInput(t *testing.T) {
	app, _ := newTestApp(t)
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
	if app.state.IsAgentRunning {
		t.Fatalf("expected send to be blocked for unsupported model image input")
	}
	if app.hasImageAttachments() {
		t.Fatalf("expected pending image attachments to be cleared on unsupported model")
	}
	if app.state.StatusText != "Model does not support images" {
		t.Fatalf("unexpected status text: %q", app.state.StatusText)
	}
	if app.input.Value() != "hello" {
		t.Fatalf("expected input to be preserved when send is blocked, got %q", app.input.Value())
	}
	if app.state.InputText != "hello" {
		t.Fatalf("expected state input text to be preserved, got %q", app.state.InputText)
	}
}

func TestUpdateSendWithImageAttachmentsWithoutSessionAssets(t *testing.T) {
	app, _ := newTestApp(t)
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
	if app.state.IsAgentRunning {
		t.Fatalf("expected send to be blocked when session assets are unavailable")
	}
	if app.hasImageAttachments() {
		t.Fatalf("expected pending image attachments to be cleared when storage is unavailable")
	}
	if app.state.StatusText != "Image attachments need session asset support" {
		t.Fatalf("unexpected status text: %q", app.state.StatusText)
	}
	if app.input.Value() != "hello" {
		t.Fatalf("expected input to be preserved when send is blocked, got %q", app.input.Value())
	}
	if app.state.InputText != "hello" {
		t.Fatalf("expected state input text to be preserved, got %q", app.state.InputText)
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

	// /cwd 不是 handleImmediateSlashCommand 处理的命令，也不是 switch 中的已知命令，
	// 所以走 default 分支返回 runLocalCommand -> localCommandResultMsg
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

func TestHandleImmediateSlashCommandDefault(t *testing.T) {
	app, _ := newTestApp(t)
	handled, cmd := app.handleImmediateSlashCommand("/unknown")
	if handled || cmd != nil {
		t.Fatalf("expected unknown slash command to be ignored")
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

// newTestAppWithMemo 创建一个注入了 memo 服务的测试 App。
func newTestAppWithMemo(t *testing.T) (App, *stubRuntime) {
	t.Helper()

	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	cfg.Memo.Enabled = true
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
			{ID: provider.Name, Name: provider.Name, Models: []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	// 创建真实的 memo 服务
	memoStore := memo.NewFileStore(t.TempDir(), cfg.Workdir)
	memoSvc := memo.NewService(memoStore, nil, cfg.Memo, nil)

	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:          *cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: stubProviderService{providers: providers, models: models},
		MemoSvc:         memoSvc,
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return app, runtime
}

func TestHandleMemoCommand(t *testing.T) {
	t.Parallel()

	t.Run("shows no memos message when empty", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		cmd := app.handleMemoCommand()
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		if len(msgs) == 0 {
			t.Fatal("expected at least one inline message")
		}
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "No memos stored yet") {
			t.Errorf("expected 'no memos' message, got: %s", messageText(last))
		}
	})

	t.Run("lists entries when memos exist", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeUser, Title: "test entry", Content: "test", Source: memo.SourceUserManual})

		app.handleMemoCommand()
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "1 memo(s)") {
			t.Errorf("expected memo count, got: %s", messageText(last))
		}
		if !strings.Contains(messageText(last), "test entry") {
			t.Errorf("expected entry title, got: %s", messageText(last))
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		cmd := app.handleMemoCommand()
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		if len(msgs) == 0 {
			t.Fatal("expected at least one inline message")
		}
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", messageText(last))
		}
	})
}

func TestHandleRememberCommand(t *testing.T) {
	t.Parallel()

	t.Run("saves memo and shows confirmation", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		cmd := app.handleRememberCommand("my preference")
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "Memo saved") {
			t.Errorf("expected saved confirmation, got: %s", messageText(last))
		}
		// Verify the entry was actually saved
		entries, _ := app.memoSvc.List(context.Background())
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Title != "my preference" {
			t.Errorf("Title = %q, want %q", entries[0].Title, "my preference")
		}
	})

	t.Run("empty text shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleRememberCommand("")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "Usage") {
			t.Errorf("expected usage message, got: %s", messageText(last))
		}
	})

	t.Run("whitespace only text shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleRememberCommand("   ")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "Usage") {
			t.Errorf("expected usage message, got: %s", messageText(last))
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		app.handleRememberCommand("something")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", messageText(last))
		}
	})
}

func TestHandleForgetCommand(t *testing.T) {
	t.Parallel()

	t.Run("removes matching memos", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeUser, Title: "remove me", Content: "test", Source: memo.SourceUserManual})
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeFeedback, Title: "keep this", Content: "test2", Source: memo.SourceUserManual})

		app.handleForgetCommand("remove")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "Removed 1 memo") {
			t.Errorf("expected removal confirmation, got: %s", messageText(last))
		}
		// Verify only one was removed
		entries, _ := app.memoSvc.List(context.Background())
		if len(entries) != 1 {
			t.Fatalf("expected 1 remaining entry, got %d", len(entries))
		}
		if entries[0].Title != "keep this" {
			t.Errorf("remaining entry Title = %q, want %q", entries[0].Title, "keep this")
		}
	})

	t.Run("no match shows message", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleForgetCommand("nonexistent")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "No memos matching") {
			t.Errorf("expected no match message, got: %s", messageText(last))
		}
	})

	t.Run("empty keyword shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleForgetCommand("")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "Usage") {
			t.Errorf("expected usage message, got: %s", messageText(last))
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		app.handleForgetCommand("something")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(messageText(last), "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", messageText(last))
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
	app.transcript.SetContent(strings.Repeat("line\n", 100))
	app.applyComponentLayout(true)

	tx, ty, _, _ := app.transcriptBounds()
	if !app.isMouseWithinTranscript(tea.MouseMsg{X: tx, Y: ty}) {
		t.Fatalf("expected transcript bounds hit")
	}
	if app.isMouseWithinTranscript(tea.MouseMsg{X: tx - 1, Y: ty - 1}) {
		t.Fatalf("expected transcript bounds miss")
	}

	app.pendingCopyID = 9
	if app.handleTranscriptMouse(tea.MouseMsg{X: tx - 1, Y: ty - 1, Action: tea.MouseActionRelease}) {
		t.Fatalf("expected outside transcript release to return false")
	}
	if app.pendingCopyID != 0 {
		t.Fatalf("expected pending copy id to reset after release outside transcript")
	}
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X: tx, Y: ty, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected transcript wheel down to be handled")
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
	if !app.isMouseWithinActivity(tea.MouseMsg{X: ax, Y: ay}) {
		t.Fatalf("expected activity bounds hit")
	}
	app.focus = panelTranscript
	if !app.handleActivityMouse(tea.MouseMsg{
		X: ax, Y: ay, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected activity wheel to be handled")
	}
	if app.focus != panelActivity {
		t.Fatalf("expected activity panel to gain focus")
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

	app.providerAddForm.Name = "你好"
	model, _ = app.handleProviderAddFormInput(tea.KeyMsg{Type: tea.KeyBackspace})
	ptr, ok = model.(*App)
	if !ok {
		t.Fatalf("expected *App model, got %T", model)
	}
	app = *ptr
	if app.providerAddForm.Name != "你" {
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
		if _, err := buildProviderAddRequest(providerAddFormState{Name: "demo", Driver: provider.DriverGemini}); !strings.Contains(err, "API Key is required") {
			t.Fatalf("expected missing key error, got %q", err)
		}
	})

	t.Run("openai compat applies defaults", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:   "openai-compat",
			Driver: provider.DriverOpenAICompat,
			APIKey: "k",
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.BaseURL != config.OpenAIDefaultBaseURL {
			t.Fatalf("expected openai default base url, got %q", req.BaseURL)
		}
		if req.APIStyle != provider.OpenAICompatibleAPIStyleChatCompletions {
			t.Fatalf("expected default api style")
		}
	})

	t.Run("gemini applies defaults and clears unrelated fields", func(t *testing.T) {
		req, err := buildProviderAddRequest(providerAddFormState{
			Name:           "gemini",
			Driver:         provider.DriverGemini,
			APIKey:         "k",
			APIStyle:       "x",
			APIVersion:     "v",
			DeploymentMode: "d",
		})
		if err != "" {
			t.Fatalf("unexpected error: %s", err)
		}
		if req.BaseURL != config.GeminiDefaultBaseURL || req.APIStyle != "" || req.APIVersion != "" {
			t.Fatalf("expected gemini normalization, got %+v", req)
		}
	})

	t.Run("anthropic/custom require base url", func(t *testing.T) {
		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:   "anthropic",
			Driver: provider.DriverAnthropic,
			APIKey: "k",
		}); !strings.Contains(err, "Base URL is required") {
			t.Fatalf("expected anthropic base url error, got %q", err)
		}

		if _, err := buildProviderAddRequest(providerAddFormState{
			Name:    "custom",
			Driver:  "custom-driver",
			APIKey:  "k",
			BaseURL: "",
		}); !strings.Contains(err, "Base URL is required for custom driver") {
			t.Fatalf("expected custom base url error, got %q", err)
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
