package catalog

import (
	"context"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	store := newMemoryStore()

	service := NewService("", registry, store)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.store != store {
		t.Fatal("expected explicit store to be used")
	}
}

func TestListProviderModelsFallsBackToDefaultModelWithoutDiscovery(t *testing.T) {
	t.Parallel()

	service := NewService("", provider.NewRegistry(), newMemoryStore())
	models, err := service.ListProviderModels(context.Background(), config.OpenAIProvider())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected default model fallback, got %+v", models)
	}
}

func TestListProviderModelsSnapshotReturnsDefaultAndRefreshesInBackgroundOnMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	refreshed := make(chan struct{}, 1)
	registry := newRegistry(t, config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []config.ModelDescriptor{{ID: "gpt-4o", Name: "GPT-4o"}}, nil
	})

	store := newMemoryStore()
	service := NewService("", registry, store)
	service.backgroundTimeout = time.Second

	models, err := service.ListProviderModelsSnapshot(context.Background(), config.OpenAIProvider())
	if err != nil {
		t.Fatalf("ListProviderModelsSnapshot() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected immediate default model fallback, got %+v", models)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background refresh to run")
	}

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		modelCatalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(modelCatalog.Models, "gpt-4o") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain gpt-4o, got %+v", modelCatalog.Models)
}

func TestListProviderModelsDiscoversAndCachesOnMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := newRegistry(t, config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
		return []config.ModelDescriptor{{
			ID:              "server-model",
			Name:            "Server Model",
			ContextWindow:   32000,
			MaxOutputTokens: 4096,
		}}, nil
	})

	store := newMemoryStore()
	service := NewService("", registry, store)

	models, err := service.ListProviderModels(context.Background(), config.OpenAIProvider())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "server-model") {
		t.Fatalf("expected discovered model in result, got %+v", models)
	}

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() cached catalog error = %v", err)
	}
	if !containsModelDescriptorID(modelCatalog.Models, "server-model") {
		t.Fatalf("expected cached discovered model, got %+v", modelCatalog.Models)
	}
}

func TestListProviderModelsReturnsStaleCacheAndRefreshesInBackground(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryStore()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: SchemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-48 * time.Hour),
		ExpiresAt:     now.Add(-24 * time.Hour),
		Models: []config.ModelDescriptor{
			{ID: "stale-model", Name: "Stale Model"},
		},
	}); err != nil {
		t.Fatalf("seed stale catalog: %v", err)
	}

	refreshed := make(chan struct{}, 1)
	registry := newRegistry(t, config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []config.ModelDescriptor{{ID: "fresh-model", Name: "Fresh Model"}}, nil
	})

	service := NewService("", registry, store)
	service.now = func() time.Time { return now }
	service.backgroundTimeout = time.Second

	models, err := service.ListProviderModels(context.Background(), config.OpenAIProvider())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "stale-model") {
		t.Fatalf("expected stale cached model to be returned immediately, got %+v", models)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background refresh to run")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		modelCatalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(modelCatalog.Models, "fresh-model") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain fresh-model, got %+v", modelCatalog.Models)
}

func TestDescriptorsFromIDsHelper(t *testing.T) {
	t.Parallel()

	models := config.DescriptorsFromIDs([]string{"gpt-4.1", "", "gpt-4o"})
	if len(models) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(models))
	}
	if models[0].ID != "gpt-4.1" || models[1].ID != "gpt-4o" {
		t.Fatalf("unexpected descriptors: %+v", models)
	}
}

func newRegistry(t *testing.T, name string, discover provider.DiscoveryFunc) *provider.Registry {
	t.Helper()

	registry := provider.NewRegistry()
	if err := registry.Register(provider.DriverDefinition{
		Name:     name,
		Discover: discover,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return catalogTestProvider{}, nil
		},
	}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	return registry
}

func containsModelDescriptorID(models []config.ModelDescriptor, modelID string) bool {
	target := config.NormalizeKey(modelID)
	if target == "" {
		return false
	}

	for _, model := range models {
		if config.NormalizeKey(model.ID) == target {
			return true
		}
	}
	return false
}

type catalogTestProvider struct{}

func (catalogTestProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, nil
}

type memoryStore struct {
	mu       sync.Mutex
	catalogs map[string]ModelCatalog
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		catalogs: map[string]ModelCatalog{},
	}
}

func (s *memoryStore) Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	modelCatalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return ModelCatalog{}, ErrCatalogNotFound
	}
	return modelCatalog, nil
}

func (s *memoryStore) Save(ctx context.Context, modelCatalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.catalogs[modelCatalog.Identity.Key()] = modelCatalog
	return nil
}

const testAPIKeyEnv = "OPENAI_API_KEY"
