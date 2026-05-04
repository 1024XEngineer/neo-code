package state

import (
	"context"
	"errors"
	"testing"

	configpkg "neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestStateCoverageGapSelectAndSetBranches(t *testing.T) {
	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	if _, err := service.SelectProvider(context.Background(), OpenAIName); err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if _, err := service.SelectProviderWithModel(context.Background(), OpenAIName, OpenAIDefaultModel); err != nil {
		t.Fatalf("SelectProviderWithModel() error = %v", err)
	}
	if _, err := service.selectProviderUnlocked(context.Background(), OpenAIName); err != nil {
		t.Fatalf("selectProviderUnlocked() error = %v", err)
	}
	if _, err := service.setCurrentModelUnlocked(context.Background(), OpenAIDefaultModel); err != nil {
		t.Fatalf("setCurrentModelUnlocked() error = %v", err)
	}
}

func TestStateCoverageGapRemoveCustomProviderContextAndEnvBranches(t *testing.T) {
	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{{ID: OpenAIDefaultModel, Name: OpenAIDefaultModel}},
	})

	const providerName = "coverage-removable-provider"
	const apiKeyEnv = "COVERAGE_REMOVABLE_KEY"
	if err := configpkg.SaveCustomProviderWithModels(manager.BaseDir(), configpkg.SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             apiKeyEnv,
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	}); err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.RemoveCustomProvider(ctx, providerName); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveCustomProvider(canceled) err = %v, want context.Canceled", err)
	}

	originalDelete := deleteUserEnvVarForCreate
	t.Cleanup(func() { deleteUserEnvVarForCreate = originalDelete })
	called := false
	deleteUserEnvVarForCreate = func(name string) error {
		called = true
		if name != apiKeyEnv {
			t.Fatalf("delete env name = %q, want %q", name, apiKeyEnv)
		}
		return nil
	}

	if err := service.RemoveCustomProvider(context.Background(), providerName); err != nil {
		t.Fatalf("RemoveCustomProvider() error = %v", err)
	}
	if !called {
		t.Fatal("expected deleteUserEnvVarForCreate to be called")
	}
}

func TestStateCoverageGapNormalizeCreateInputEntry(t *testing.T) {
	_, err := normalizeCreateCustomProviderInput(CreateCustomProviderInput{
		Name:                  "cov-provider",
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COV_PROVIDER_KEY",
		APIKey:                "sk-test",
		ModelSource:           configpkg.ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err != nil {
		t.Fatalf("normalizeCreateCustomProviderInput() error = %v", err)
	}
}
