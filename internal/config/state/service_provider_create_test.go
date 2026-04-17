package state

import (
	"context"
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
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "custom-model", Name: "custom-model"},
		},
	})

	input := CreateCustomProviderInput{
		Name:      "company-gateway",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		APIKey:    "test-key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
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

func TestCreateCustomProviderRollbackOnSelectFailure(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), errorCatalogStub{err: context.DeadlineExceeded})

	input := CreateCustomProviderInput{
		Name:      "rollback-gateway",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "ROLLBACK_GATEWAY_API_KEY",
		APIKey:    "new-key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
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
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:      "conflict-provider",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: configpkg.OpenAIDefaultAPIKeyEnv,
		APIKey:    "key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates provider") {
		t.Fatalf("expected duplicate env error, got %v", err)
	}
}

func TestCreateCustomProviderRejectsProtectedEnvName(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:      "protected-env-provider",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "PATH",
		APIKey:    "key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
	})
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("expected protected env error, got %v", err)
	}
}

func TestCreateCustomProviderRejectsInvalidProviderName(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})

	_, err := service.CreateCustomProvider(context.Background(), CreateCustomProviderInput{
		Name:      "../invalid-provider",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "INVALID_PROVIDER_NAME_API_KEY",
		APIKey:    "key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
	})
	if err == nil || !strings.Contains(err.Error(), "provider name") {
		t.Fatalf("expected invalid provider name error, got %v", err)
	}
}

func TestCreateCustomProviderSerializesAcrossServicesSharingManager(t *testing.T) {
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

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
		Name:      "shared-gateway",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://shared.example.com/v1",
		APIKeyEnv: "SHARED_GATEWAY_API_KEY",
		APIKey:    "key-a",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
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
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: "m1", Name: "m1"}},
	})
	input := CreateCustomProviderInput{
		Name:      "save-failed-provider",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "SAVE_FAILED_PROVIDER_API_KEY",
		APIKey:    "key",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
	}

	saveCustomProviderForCreate = func(
		baseDir string,
		name string,
		driver string,
		baseURL string,
		apiKeyEnv string,
		apiStyle string,
		deploymentMode string,
		apiVersion string,
	) error {
		providerDir := filepath.Join(baseDir, "providers", name)
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
	restorePersist, restoreDelete, restoreLookup, restoreSave := stubUserEnvOpsForCreateProvider(t)
	defer restorePersist()
	defer restoreDelete()
	defer restoreLookup()
	defer restoreSave()

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
		Name:      "shared-by-managers",
		Driver:    provider.DriverOpenAICompat,
		BaseURL:   "https://shared.example.com/v1",
		APIKeyEnv: "SHARED_BY_MANAGERS_API_KEY",
		APIKey:    "key-a",
		APIStyle:  provider.OpenAICompatibleAPIStyleChatCompletions,
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
	prevSave := saveCustomProviderForCreate

	persistUserEnvVarForCreate = func(key string, value string) error { return nil }
	deleteUserEnvVarForCreate = func(key string) error { return nil }
	lookupUserEnvVarForCreate = func(key string) (string, bool, error) { return "", false, nil }
	saveCustomProviderForCreate = configpkg.SaveCustomProvider

	return func() { persistUserEnvVarForCreate = prevPersist },
		func() { deleteUserEnvVarForCreate = prevDelete },
		func() { lookupUserEnvVarForCreate = prevLookup },
		func() { saveCustomProviderForCreate = prevSave }
}
