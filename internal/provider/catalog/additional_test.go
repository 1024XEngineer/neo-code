package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat"
	providertypes "neo-code/internal/provider/types"
)

func TestNewServiceCreatesJSONStoreWhenBaseDirProvided(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	service := NewService(baseDir, provider.NewRegistry(), nil)
	store, ok := service.store.(*jsonStore)
	if !ok {
		t.Fatalf("expected jsonStore, got %T", service.store)
	}
	if store.dir != filepath.Join(baseDir, "cache", "models") {
		t.Fatalf("unexpected store dir: %s", store.dir)
	}
}

func TestLoadCatalogWithoutStoreReturnsNotFound(t *testing.T) {
	t.Parallel()

	service := NewService("", provider.NewRegistry(), nil)
	identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}
	_, err = service.loadCatalog(context.Background(), identity)
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("expected ErrCatalogNotFound, got %v", err)
	}
}

func TestDiscoverAndPersistRejectsNilResolver(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")
	service := NewService("", newRegistry(t, openaicompat.DriverName, func(context.Context, provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return []providertypes.ModelDescriptor{{ID: "gpt-4.1", Name: "GPT-4.1"}}, nil
	}), newMemoryStore())

	input := openAIProviderSource()
	input.ResolveDiscoveryConfig = nil

	models, err := service.discoverAndPersist(context.Background(), input)
	if err == nil || models != nil || !strings.Contains(err.Error(), "discovery config resolver is nil") {
		t.Fatalf("expected nil resolver error, got models=%+v err=%v", models, err)
	}
}

func TestJSONStoreAdditionalFilesystemErrors(t *testing.T) {
	t.Parallel()

	t.Run("load read failure", func(t *testing.T) {
		t.Parallel()

		store := newJSONStore(t.TempDir())
		identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
		if err != nil {
			t.Fatalf("NewProviderIdentity() error = %v", err)
		}
		normalizedIdentity, err := provider.NormalizeProviderIdentity(identity)
		if err != nil {
			t.Fatalf("NormalizeProviderIdentity() error = %v", err)
		}
		if err := os.MkdirAll(store.catalogPath(normalizedIdentity), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		_, err = store.Load(context.Background(), identity)
		if err == nil || !strings.Contains(err.Error(), "read model catalog") {
			t.Fatalf("expected read model catalog error, got %v", err)
		}
	})

	t.Run("save write failure", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		blocker := filepath.Join(baseDir, "not-a-dir")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		store := &jsonStore{dir: blocker}
		identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
		if err != nil {
			t.Fatalf("NewProviderIdentity() error = %v", err)
		}
		err = store.Save(context.Background(), ModelCatalog{Identity: identity})
		if err == nil || !strings.Contains(err.Error(), "write model catalog") {
			t.Fatalf("expected write model catalog error, got %v", err)
		}
	})
}

func TestCatalogSnapshotOnMissingCatalog(t *testing.T) {
	t.Parallel()

	service := NewService("", provider.NewRegistry(), newMemoryStore())
	input := mustCatalogInput(t, config.OpenAIProvider())
	snapshot := service.catalogSnapshot(context.Background(), input)
	if snapshot.ok || len(snapshot.models) != 0 {
		t.Fatalf("expected empty snapshot on cache miss, got %+v", snapshot)
	}
}

func TestListProviderModelsAllowsUnsupportedOpenAICompatibleAPIStyleWhenCatalogIsWarm(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}

	store := newMemoryStore()
	service := NewService("", registry, store)
	cfg := customGatewayProvider()

	input := mustCatalogInput(t, cfg)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     time.Now(),
		Models:        []providertypes.ModelDescriptor{{ID: "cached-model", Name: "Cached Model"}},
	}); err != nil {
		t.Fatalf("Save() cached catalog error = %v", err)
	}

	models, err := service.ListProviderModels(context.Background(), input)
	if err != nil {
		t.Fatalf("expected warm catalog to bypass chat-only api_style validation, got %v", err)
	}
	if !containsModelDescriptorID(models, "cached-model") {
		t.Fatalf("expected cached model in result, got %+v", models)
	}
}

func TestListBuiltinProviderModelsAllowsUnsupportedOpenAICompatibleAPIStyleWhenCatalogIsWarm(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}

	store := newMemoryStore()
	service := NewService("", registry, store)
	cfg := config.OpenAIProvider()

	input := mustCatalogInput(t, cfg)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     time.Now(),
		Models:        []providertypes.ModelDescriptor{{ID: "cached-model", Name: "Cached Model"}},
	}); err != nil {
		t.Fatalf("Save() cached catalog error = %v", err)
	}

	models, err := service.ListProviderModels(context.Background(), input)
	if err != nil {
		t.Fatalf("expected warm catalog to bypass chat-only api_style validation, got %v", err)
	}
	if !containsModelDescriptorID(models, config.OpenAIDefaultModel) {
		t.Fatalf("expected builtin static models in result, got %+v", models)
	}
}

func TestListBuiltinProviderModelsSnapshotAllowsUnsupportedOpenAICompatibleAPIStyle(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}

	service := NewService("", registry, newMemoryStore())
	cfg := config.OpenAIProvider()

	input := mustCatalogInput(t, cfg)
	if err := service.store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     time.Now(),
		Models:        []providertypes.ModelDescriptor{{ID: "cached-model", Name: "Cached Model"}},
	}); err != nil {
		t.Fatalf("Save() cached catalog error = %v", err)
	}

	models, err := service.ListProviderModelsSnapshot(context.Background(), input)
	if err != nil {
		t.Fatalf("expected snapshot path to allow chat-only api_style drift, got %v", err)
	}
	if !containsModelDescriptorID(models, config.OpenAIDefaultModel) {
		t.Fatalf("expected builtin static models in snapshot result, got %+v", models)
	}
}

func TestListBuiltinProviderModelsSnapshotAndCachedAllowUnsupportedAPIStyleWithCachedCatalog(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}

	store := newMemoryStore()
	service := NewService("", registry, store)
	cfg := config.OpenAIProvider()

	input := mustCatalogInput(t, cfg)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     time.Now(),
		Models:        []providertypes.ModelDescriptor{{ID: "cached-model", Name: "Cached Model"}},
	}); err != nil {
		t.Fatalf("Save() cached catalog error = %v", err)
	}

	tests := []struct {
		name string
		call func(context.Context, provider.CatalogInput) ([]providertypes.ModelDescriptor, error)
	}{
		{name: "snapshot", call: service.ListProviderModelsSnapshot},
		{name: "cached", call: service.ListProviderModelsCached},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, err := tt.call(context.Background(), input)
			if err != nil {
				t.Fatalf("expected cached %s path to allow chat-only api_style drift, got %v", tt.name, err)
			}
			if !containsModelDescriptorID(models, config.OpenAIDefaultModel) {
				t.Fatalf("expected builtin static %s result, got %+v", tt.name, models)
			}
		})
	}
}
