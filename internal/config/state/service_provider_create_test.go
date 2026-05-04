package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	configpkg "neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestCreateCustomProviderSuccess(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "custom-model", Name: "custom-model"},
		},
	})

	input := CreateCustomProviderInput{
		Name:                  "company-gateway",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		APIKey:                "test-key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}

	restore := captureEnvForCreateProvider(t, input.APIKeyEnv)
	defer restore()
	_ = os.Unsetenv(input.APIKeyEnv)

	selection, err := service.CreateCustomProvider(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateCustomProvider() error = %v", err)
	}
	if selection.ProviderID != input.Name {
		t.Fatalf("expected provider %q, got %+v", input.Name, selection)
	}
	if strings.TrimSpace(os.Getenv(input.APIKeyEnv)) != input.APIKey {
		t.Fatalf("expected process env %q to be set", input.APIKeyEnv)
	}

	providerPath := filepath.Join(manager.BaseDir(), "providers", input.Name, "provider.yaml")
	data, readErr := os.ReadFile(providerPath)
	if readErr != nil {
		t.Fatalf("read provider config: %v", readErr)
	}
	providerText := string(data)
	if !strings.Contains(providerText, "api_key_env: "+input.APIKeyEnv) {
		t.Fatalf("expected provider config to persist env name, got %q", providerText)
	}
	if strings.Contains(providerText, input.APIKey) {
		t.Fatalf("provider config should not persist api key, got %q", providerText)
	}
}

func TestRemoveCustomProviderSelectedProvider(t *testing.T) {
	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	const providerName = "removable-provider"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "REMOVABLE_PROVIDER_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = providerName
		cfg.CurrentModel = "custom-model"
		return nil
	}); err != nil {
		t.Fatalf("seed selected custom provider: %v", err)
	}

	if err := service.RemoveCustomProvider(context.Background(), providerName); err != nil {
		t.Fatalf("RemoveCustomProvider() error = %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(manager.BaseDir(), "providers", providerName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected provider dir removed, stat err = %v", statErr)
	}
	cfg := manager.Get()
	if strings.EqualFold(strings.TrimSpace(cfg.SelectedProvider), providerName) {
		t.Fatalf("expected selected provider repaired after removal, got %q", cfg.SelectedProvider)
	}
}

func TestRemoveCustomProviderNoopCases(t *testing.T) {
	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	if err := service.RemoveCustomProvider(context.Background(), "missing-provider"); err != nil {
		t.Fatalf("expected missing provider remove to be noop, got %v", err)
	}
	if err := service.RemoveCustomProvider(context.Background(), configpkg.OpenAIName); err == nil {
		t.Fatal("expected builtin provider remove to fail with error")
	}
}

func TestCreateCustomProviderPreservesExplicitCustomChatEndpoint(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "custom-model", Name: "custom-model"},
		},
	})

	input := CreateCustomProviderInput{
		Name:                  "custom-chat-endpoint-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/v1/text/chatcompletion_v2",
		APIKeyEnv:             "CUSTOM_CHAT_ENDPOINT_PROVIDER_API_KEY",
		APIKey:                "test-key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}

	if _, err := service.CreateCustomProvider(context.Background(), input); err != nil {
		t.Fatalf("CreateCustomProvider() error = %v", err)
	}

	cfg := manager.Get()
	providerCfg, err := cfg.ProviderByName(input.Name)
	if err != nil {
		t.Fatalf("expected provider %q in config, got %v", input.Name, err)
	}
	if providerCfg.ChatEndpointPath != input.ChatEndpointPath {
		t.Fatalf("expected chat endpoint path %q, got %q", input.ChatEndpointPath, providerCfg.ChatEndpointPath)
	}
	if providerCfg.ChatAPIMode != input.ChatAPIMode {
		t.Fatalf("expected chat api mode %q, got %q", input.ChatAPIMode, providerCfg.ChatAPIMode)
	}
}

func TestCreateCustomProviderManualSourceRequiresModelJSON(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "manual-model", Name: "manual-model"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:        "manual-source-no-models",
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "MANUAL_SOURCE_NO_MODELS_API_KEY",
		APIKey:      "test-key",
		ModelSource: configpkg.ModelSourceManual,
	})
	if err == nil || !strings.Contains(err.Error(), "manual model json is empty") {
		t.Fatalf("expected missing manual model json error, got %v", err)
	}
}

func TestCreateCustomProviderManualSourcePersistsModels(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "manual-model-1", Name: "Manual Model 1"}},
	})

	input := CreateCustomProviderInput{
		Name:                  "manual-source-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "MANUAL_SOURCE_PROVIDER_API_KEY",
		APIKey:                "test-key",
		ModelSource:           configpkg.ModelSourceManual,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		ManualModelsJSON: `[
			{
				"id": "manual-model-1",
				"name": "Manual Model 1"
			},
			{
				"id": "manual-model-2",
				"name": "Manual Model 2"
			}
		]`,
	}
	if _, err := service.CreateCustomProvider(context.Background(), input); err != nil {
		t.Fatalf("CreateCustomProvider() error = %v", err)
	}

	providerPath := filepath.Join(manager.BaseDir(), "providers", input.Name, "provider.yaml")
	data, readErr := os.ReadFile(providerPath)
	if readErr != nil {
		t.Fatalf("read provider config: %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, "model_source: manual") {
		t.Fatalf("expected provider config to persist manual model source, got %q", text)
	}
	if strings.Contains(text, "discovery_endpoint_path:") {
		t.Fatalf("expected provider config to omit discovery endpoint in manual mode, got %q", text)
	}
	if !strings.Contains(text, "id: manual-model-1") || !strings.Contains(text, "id: manual-model-2") {
		t.Fatalf("expected provider config to persist manual model entries, got %q", text)
	}

	cfg := manager.Get()
	providerCfg, err := cfg.ProviderByName(input.Name)
	if err != nil {
		t.Fatalf("expected provider %q in config, got %v", input.Name, err)
	}
	if configpkg.NormalizeModelSource(providerCfg.ModelSource) != configpkg.ModelSourceManual {
		t.Fatalf("expected provider model source manual, got %q", providerCfg.ModelSource)
	}
	if len(providerCfg.Models) != 2 {
		t.Fatalf("expected 2 manual models after reload, got %d", len(providerCfg.Models))
	}
}

func TestNormalizeCreateCustomProviderInputDefaultsToDiscoverWhenModelSourceEmpty(t *testing.T) {
	_, err := normalizeCreateCustomProviderInput(CreateCustomProviderInput{
		Name:      "default-discover-provider",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "DEFAULT_DISCOVER_PROVIDER_API_KEY",
		APIKey:    "test-key",
	})
	if err == nil || !strings.Contains(err.Error(), "model_source discover requires discovery_endpoint_path") {
		t.Fatalf("expected missing discovery endpoint error, got %v", err)
	}
}

func TestNormalizeCreateCustomProviderInputRejectsInvalidModelSource(t *testing.T) {
	_, err := normalizeCreateCustomProviderInput(CreateCustomProviderInput{
		Name:        "invalid-model-source-provider",
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "INVALID_MODEL_SOURCE_PROVIDER_API_KEY",
		APIKey:      "test-key",
		ModelSource: "manul",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported model source") {
		t.Fatalf("expected invalid model source error, got %v", err)
	}
}

func TestNormalizeCreateCustomProviderInputAllowsAnthropicDriver(t *testing.T) {
	normalized, err := normalizeCreateCustomProviderInput(CreateCustomProviderInput{
		Name:                  "anthropic-provider",
		Driver:                provider.DriverAnthropic,
		BaseURL:               "https://api.anthropic.com/v1",
		APIKeyEnv:             "ANTHROPIC_PROVIDER_API_KEY",
		APIKey:                "test-key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err != nil {
		t.Fatalf("expected anthropic driver to be allowed, got %v", err)
	}
	if normalized.Driver != provider.DriverAnthropic {
		t.Fatalf("expected normalized driver %q, got %q", provider.DriverAnthropic, normalized.Driver)
	}
}

func TestNormalizeCreateCustomProviderInputManualSkipsDiscoveryFieldValidation(t *testing.T) {
	normalized, err := normalizeCreateCustomProviderInput(CreateCustomProviderInput{
		Name:             "manual-no-discovery-validation",
		Driver:           provider.DriverOpenAICompat,
		BaseURL:          "https://llm.example.com/v1",
		APIKeyEnv:        "MANUAL_NO_DISCOVERY_VALIDATION_API_KEY",
		APIKey:           "test-key",
		ModelSource:      configpkg.ModelSourceManual,
		ManualModelsJSON: `[{"id":"manual-model","name":"Manual Model"}]`,
	})
	if err != nil {
		t.Fatalf("normalizeCreateCustomProviderInput() error = %v", err)
	}
	if normalized.DiscoveryEndpointPath != "" {
		t.Fatalf("expected manual mode to clear discovery endpoint path, got %q", normalized.DiscoveryEndpointPath)
	}
	if len(normalized.ManualModels) != 1 {
		t.Fatalf("expected one manual model, got %d", len(normalized.ManualModels))
	}
}

func TestCreateCustomProviderRollbackOnSelectFailure(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), errorCatalogStub{err: context.DeadlineExceeded})

	input := CreateCustomProviderInput{
		Name:                  "rollback-gateway",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "ROLLBACK_GATEWAY_API_KEY",
		APIKey:                "new-key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}

	restore := captureEnvForCreateProvider(t, input.APIKeyEnv)
	defer restore()
	if err := os.Setenv(input.APIKeyEnv, "old-key"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}

	if _, err := service.CreateCustomProvider(context.Background(), input); err == nil {
		t.Fatal("expected CreateCustomProvider() to fail")
	}

	if got := os.Getenv(input.APIKeyEnv); got != "old-key" {
		t.Fatalf("expected process env rollback, got %q", got)
	}
	providerDir := filepath.Join(manager.BaseDir(), "providers", input.Name)
	if _, statErr := os.Stat(providerDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected provider dir rollback, stat err = %v", statErr)
	}
	cfgAfterRollback := manager.Get()
	if _, findErr := cfgAfterRollback.ProviderByName(input.Name); findErr == nil {
		t.Fatalf("expected provider %q to be absent from manager snapshot after rollback", input.Name)
	}
}

func TestCreateCustomProviderRejectsEnvConflicts(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:                  "conflict-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             configpkg.OpenAIDefaultAPIKeyEnv,
		APIKey:                "key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates provider") {
		t.Fatalf("expected duplicate env error, got %v", err)
	}
}

func TestCreateCustomProviderRejectsDuplicateCustomProviderName(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	firstInput := CreateCustomProviderInput{
		Name:                  "duplicate-custom-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "DUPLICATE_CUSTOM_PROVIDER_A_API_KEY",
		APIKey:                "key-a",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}
	restoreA := captureEnvForCreateProvider(t, firstInput.APIKeyEnv)
	defer restoreA()
	if _, err := service.CreateCustomProvider(context.Background(), firstInput); err != nil {
		t.Fatalf("first CreateCustomProvider() error = %v", err)
	}

	secondInput := firstInput
	secondInput.APIKeyEnv = "DUPLICATE_CUSTOM_PROVIDER_B_API_KEY"
	secondInput.APIKey = "key-b"
	restoreB := captureEnvForCreateProvider(t, secondInput.APIKeyEnv)
	defer restoreB()

	_, err := service.CreateCustomProvider(context.Background(), secondInput)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate provider name error, got %v", err)
	}
}

func TestCreateCustomProviderRejectsProtectedEnvName(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:                  "protected-env-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "PATH",
		APIKey:                "key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("expected protected env error, got %v", err)
	}
}

func TestCreateCustomProviderRejectsInvalidProviderName(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:                  "../invalid-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "INVALID_PROVIDER_NAME_API_KEY",
		APIKey:                "key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err == nil || !strings.Contains(err.Error(), "provider name") {
		t.Fatalf("expected invalid provider name error, got %v", err)
	}
}

func TestCreateCustomProviderSerializesAcrossServicesSharingManager(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	failingService := NewService(manager, newDriverSupporterStub(), errorCatalogStub{err: context.DeadlineExceeded})
	successService := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "shared-model", Name: "shared-model"},
		},
	})

	reachedPersist := make(chan struct{})
	releasePersist := make(chan struct{})
	var notifyOnce sync.Once
	persistUserEnvVarForCreate = func(key string, value string) error {
		if value == "key-a" {
			notifyOnce.Do(func() { close(reachedPersist) })
			<-releasePersist
		}
		return nil
	}

	inputA := CreateCustomProviderInput{
		Name:                  "shared-gateway",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://shared.example.com/v1",
		APIKeyEnv:             "SHARED_GATEWAY_API_KEY",
		APIKey:                "key-a",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}
	inputB := inputA
	inputB.APIKey = "key-b"

	restore := captureEnvForCreateProvider(t, inputA.APIKeyEnv)
	defer restore()
	_ = os.Unsetenv(inputA.APIKeyEnv)

	errACh := make(chan error, 1)
	errBCh := make(chan error, 1)

	go func() {
		_, err := failingService.CreateCustomProvider(context.Background(), inputA)
		errACh <- err
	}()

	select {
	case <-reachedPersist:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting first create flow to reach persist stage")
	}

	go func() {
		_, err := successService.CreateCustomProvider(context.Background(), inputB)
		errBCh <- err
	}()

	select {
	case err := <-errBCh:
		t.Fatalf("expected second create to wait for manager lock, got early result err=%v", err)
	case <-time.After(120 * time.Millisecond):
	}

	close(releasePersist)

	if err := <-errACh; err == nil {
		t.Fatal("expected first create to fail on model selection")
	}
	if err := <-errBCh; err != nil {
		t.Fatalf("expected second create to succeed, got %v", err)
	}

	cfg := manager.Get()
	providerCfg, err := cfg.ProviderByName(inputA.Name)
	if err != nil {
		t.Fatalf("expected provider %q to exist after serialized create, got %v", inputA.Name, err)
	}
	if strings.TrimSpace(providerCfg.APIKeyEnv) != inputA.APIKeyEnv {
		t.Fatalf("expected provider api_key_env %q, got %q", inputA.APIKeyEnv, providerCfg.APIKeyEnv)
	}

	providerPath := filepath.Join(manager.BaseDir(), "providers", inputA.Name, "provider.yaml")
	data, readErr := os.ReadFile(providerPath)
	if readErr != nil {
		t.Fatalf("read provider config: %v", readErr)
	}
	if !strings.Contains(string(data), "api_key_env: "+inputA.APIKeyEnv) {
		t.Fatalf("expected provider config to remain after concurrent create flow, got %q", string(data))
	}
}

func TestCreateCustomProviderRollbackOnSaveProviderFailure(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})
	input := CreateCustomProviderInput{
		Name:                  "save-failed-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "SAVE_FAILED_PROVIDER_API_KEY",
		APIKey:                "key",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}

	saveCustomProviderWithModelsForCreate = func(baseDir string, input configpkg.SaveCustomProviderInput) error {
		providerDir := filepath.Join(baseDir, "providers", input.Name)
		if err := os.MkdirAll(providerDir, 0o755); err != nil {
			return err
		}
		return context.DeadlineExceeded
	}

	if _, err := service.CreateCustomProvider(context.Background(), input); err == nil {
		t.Fatal("expected CreateCustomProvider() to fail when saving provider config")
	}

	providerDir := filepath.Join(manager.BaseDir(), "providers", input.Name)
	if _, statErr := os.Stat(providerDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected rollback to remove provider dir, stat err = %v", statErr)
	}
	if got := strings.TrimSpace(os.Getenv(input.APIKeyEnv)); got != "" {
		t.Fatalf("expected process env to stay untouched, got %q", got)
	}
	cfg := manager.Get()
	if _, err := cfg.ProviderByName(input.Name); err == nil {
		t.Fatalf("expected provider %q absent after save failure rollback", input.Name)
	}
}

func TestCreateCustomProviderSerializesAcrossManagersSharingBaseDir(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	baseDir := t.TempDir()
	loaderA := configpkg.NewLoader(baseDir, testDefaultConfig())
	loaderB := configpkg.NewLoader(baseDir, testDefaultConfig())
	managerA := configpkg.NewManager(loaderA)
	managerB := configpkg.NewManager(loaderB)
	if _, err := managerA.Load(context.Background()); err != nil {
		t.Fatalf("managerA.Load() error = %v", err)
	}
	if _, err := managerB.Load(context.Background()); err != nil {
		t.Fatalf("managerB.Load() error = %v", err)
	}

	failingService := NewService(managerA, newDriverSupporterStub(), errorCatalogStub{err: context.DeadlineExceeded})
	successService := NewService(managerB, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	reachedPersist := make(chan struct{})
	releasePersist := make(chan struct{})
	var notifyOnce sync.Once
	persistUserEnvVarForCreate = func(key string, value string) error {
		if value == "key-a" {
			notifyOnce.Do(func() { close(reachedPersist) })
			<-releasePersist
		}
		return nil
	}

	inputA := CreateCustomProviderInput{
		Name:                  "shared-by-managers",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://shared.example.com/v1",
		APIKeyEnv:             "SHARED_BY_MANAGERS_API_KEY",
		APIKey:                "key-a",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}
	inputB := inputA
	inputB.APIKey = "key-b"

	errACh := make(chan error, 1)
	errBCh := make(chan error, 1)

	go func() {
		_, err := failingService.CreateCustomProvider(context.Background(), inputA)
		errACh <- err
	}()

	select {
	case <-reachedPersist:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting first create flow to reach persist stage")
	}

	go func() {
		_, err := successService.CreateCustomProvider(context.Background(), inputB)
		errBCh <- err
	}()

	select {
	case err := <-errBCh:
		t.Fatalf("expected second manager create to wait for cross-process lock, got early err=%v", err)
	case <-time.After(120 * time.Millisecond):
	}

	close(releasePersist)

	if err := <-errACh; err == nil {
		t.Fatal("expected first manager create to fail on model selection")
	}
	if err := <-errBCh; err != nil {
		t.Fatalf("expected second manager create to succeed, got %v", err)
	}
}

func TestLockProviderCreateCrossProcessReclaimsStaleLock(t *testing.T) {
	baseDir := t.TempDir()
	lockPath := filepath.Join(baseDir, providerCreateCrossProcessLockName)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	staleTime := time.Now().Add(-providerCreateCrossProcessLockStaleThreshold - time.Second)
	if err := touchProviderCreateLockLease(lockPath, staleTime); err != nil {
		t.Fatalf("touchProviderCreateLockLease() error = %v", err)
	}

	release, err := lockProviderCreateCrossProcess(context.Background(), baseDir)
	if err != nil {
		t.Fatalf("lockProviderCreateCrossProcess() error = %v", err)
	}
	defer release()

	leaseInfo, statErr := os.Stat(providerCreateLockLeasePath(lockPath))
	if statErr != nil {
		t.Fatalf("Stat() lease error = %v", statErr)
	}
	if !leaseInfo.ModTime().After(staleTime) {
		t.Fatalf("expected reclaimed lock lease modtime after stale time, got %v <= %v", leaseInfo.ModTime(), staleTime)
	}
}

func TestLockProviderCreateCrossProcessWaitsForFreshLockUntilContextDone(t *testing.T) {
	baseDir := t.TempDir()
	lockPath := filepath.Join(baseDir, providerCreateCrossProcessLockName)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := touchProviderCreateLockLease(lockPath, time.Now()); err != nil {
		t.Fatalf("touchProviderCreateLockLease() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	release, err := lockProviderCreateCrossProcess(ctx, baseDir)
	if err == nil {
		release()
		t.Fatal("expected lockProviderCreateCrossProcess() to fail on context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestCreateCustomProviderRejectsInvalidDiscoverySettings(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:                  "invalid-discovery-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "INVALID_DISCOVERY_PROVIDER_API_KEY",
		APIKey:                "key",
		DiscoveryEndpointPath: "https://llm.example.com/models",
	})
	if err == nil || !strings.Contains(err.Error(), "discovery endpoint path") {
		t.Fatalf("expected invalid discovery endpoint path error, got %v", err)
	}

	_, err = service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:                  "invalid-chat-endpoint-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "INVALID_CHAT_ENDPOINT_PROVIDER_API_KEY",
		APIKey:                "key",
		ChatEndpointPath:      "https://llm.example.com/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err == nil || !strings.Contains(err.Error(), "must be a relative path") {
		t.Fatalf("expected invalid chat endpoint path error, got %v", err)
	}
}

func TestLockProviderCreateCrossProcessCleansStaleLockBeforeAcquire(t *testing.T) {
	baseDir := t.TempDir()
	lockPath := filepath.Join(baseDir, providerCreateCrossProcessLockName)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	expiredAt := time.Now().Add(-providerCreateCrossProcessLockStaleThreshold - time.Second)
	if err := os.Chtimes(lockPath, expiredAt, expiredAt); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	release, err := lockProviderCreateCrossProcess(context.Background(), baseDir)
	if err != nil {
		t.Fatalf("lockProviderCreateCrossProcess() error = %v", err)
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock dir recreated and held, stat err = %v", err)
	}
	release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock dir released, stat err = %v", err)
	}
}

func TestTryReclaimStaleProviderCreateLockKeepsActiveLeaseWhenDirIsOld(t *testing.T) {
	baseDir := t.TempDir()
	lockPath := filepath.Join(baseDir, providerCreateCrossProcessLockName)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	staleDirTime := time.Now().Add(-providerCreateCrossProcessLockStaleThreshold - time.Second)
	if err := os.Chtimes(lockPath, staleDirTime, staleDirTime); err != nil {
		t.Fatalf("Chtimes() dir error = %v", err)
	}
	if err := touchProviderCreateLockLease(lockPath, time.Now()); err != nil {
		t.Fatalf("touchProviderCreateLockLease() error = %v", err)
	}

	reclaimed, err := tryReclaimStaleProviderCreateLock(lockPath, time.Now())
	if err != nil {
		t.Fatalf("tryReclaimStaleProviderCreateLock() error = %v", err)
	}
	if reclaimed {
		t.Fatal("expected active lease lock not to be reclaimed")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock dir to remain, stat err = %v", err)
	}
}

func TestRemoveCustomProviderValidationBranches(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	t.Run("context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := service.RemoveCustomProvider(ctx, "custom")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})

	t.Run("empty provider name", func(t *testing.T) {
		err := service.RemoveCustomProvider(context.Background(), "   ")
		if !errors.Is(err, ErrProviderNotFound) {
			t.Fatalf("err = %v, want ErrProviderNotFound", err)
		}
	})

	t.Run("invalid provider name", func(t *testing.T) {
		err := service.RemoveCustomProvider(context.Background(), "../bad")
		if err == nil {
			t.Fatal("expected invalid provider name error")
		}
	})
}

func TestRemoveCustomProviderEnvDeleteFailure(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	const providerName = "env-delete-failed-provider"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "ENV_DELETE_FAILED_PROVIDER_API_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	deleteUserEnvVarForCreate = func(string) error { return errors.New("delete env failed") }
	err := service.RemoveCustomProvider(context.Background(), providerName)
	if err == nil || !strings.Contains(err.Error(), "remove provider env") {
		t.Fatalf("err = %v, want remove provider env", err)
	}
}

func TestRemoveCustomProviderEnvDeleteFailureStillRepairsSelection(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	const providerName = "env-delete-failed-selected-provider"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "ENV_DELETE_FAILED_SELECTED_PROVIDER_API_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = providerName
		cfg.CurrentModel = "custom-model"
		return nil
	}); err != nil {
		t.Fatalf("seed selected provider: %v", err)
	}

	deleteUserEnvVarForCreate = func(string) error { return errors.New("delete env failed") }
	err := service.RemoveCustomProvider(context.Background(), providerName)
	if err == nil || !strings.Contains(err.Error(), "remove provider env") {
		t.Fatalf("err = %v, want remove provider env", err)
	}

	cfg := manager.Get()
	if strings.EqualFold(strings.TrimSpace(cfg.SelectedProvider), providerName) {
		t.Fatalf("selected provider still points to removed provider %q", providerName)
	}
	if strings.TrimSpace(cfg.SelectedProvider) == "" {
		t.Fatalf("selected provider should be repaired to a valid provider, got empty")
	}
	if _, providerErr := cfg.ProviderByName(cfg.SelectedProvider); providerErr != nil {
		t.Fatalf("selected provider %q is invalid: %v", cfg.SelectedProvider, providerErr)
	}
	removedConfigPath := filepath.Join(manager.BaseDir(), "providers", providerName, "provider.yaml")
	if _, statErr := os.Stat(removedConfigPath); !os.IsNotExist(statErr) {
		t.Fatalf("provider config should be removed, stat err = %v", statErr)
	}
}

func TestRemoveCustomProviderSelectedEnsureSelectionFailure(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	defaults := testDefaultConfig()
	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"anthropic": true}}
	service := NewService(manager, supporters, newCatalogStub())

	const providerName = "selected-provider-for-failed-ensure"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "FAILED_ENSURE_SELECTION_API_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = providerName
		cfg.CurrentModel = "custom-model"
		return nil
	}); err != nil {
		t.Fatalf("seed selected provider: %v", err)
	}

	err := service.RemoveCustomProvider(context.Background(), providerName)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestRemoveCustomProviderContextCancellationDuringUpdate(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	const providerName = "cancel-during-update-provider"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "CANCEL_DURING_UPDATE_PROVIDER_API_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = providerName
		cfg.CurrentModel = "custom-model"
		return nil
	}); err != nil {
		t.Fatalf("seed selected provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	deleteUserEnvVarForCreate = func(string) error {
		cancel()
		return nil
	}
	err := service.RemoveCustomProvider(ctx, providerName)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRemoveCustomProviderContextCancellationBeforeReload(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSaveWithModels := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSaveWithModels()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	const providerName = "cancel-before-reload-provider"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "CANCEL_BEFORE_RELOAD_PROVIDER_API_KEY",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	deleteUserEnvVarForCreate = func(string) error {
		cancel()
		return nil
	}
	err := service.RemoveCustomProvider(ctx, providerName)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func captureEnvForCreateProvider(t *testing.T, key string) func() {
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

func stubUserEnvOpsForCreateProvider(t *testing.T) (func(), func(), func(), func()) {
	t.Helper()

	prevPersist := persistUserEnvVarForCreate
	prevDelete := deleteUserEnvVarForCreate
	prevLookup := lookupUserEnvVarForCreate
	prevSaveWithModels := saveCustomProviderWithModelsForCreate

	persistUserEnvVarForCreate = func(key string, value string) error { return nil }
	deleteUserEnvVarForCreate = func(key string) error { return nil }
	lookupUserEnvVarForCreate = func(key string) (string, bool, error) { return "", false, nil }
	saveCustomProviderWithModelsForCreate = configpkg.SaveCustomProviderWithModels

	return func() { persistUserEnvVarForCreate = prevPersist },
		func() { deleteUserEnvVarForCreate = prevDelete },
		func() { lookupUserEnvVarForCreate = prevLookup },
		func() { saveCustomProviderWithModelsForCreate = prevSaveWithModels }
}
