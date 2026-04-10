package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	identity, err := provider.NewProviderIdentity("openai", "https://api.openai.com/v1")
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

func TestQueueRefreshSkipsIncompleteIdentity(t *testing.T) {
	t.Parallel()

	var discoverCalls int32
	registry := newRegistry(t, openaicompat.DriverName, func(context.Context, provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		atomic.AddInt32(&discoverCalls, 1)
		return nil, nil
	})
	service := NewService("", registry, newMemoryStore())
	service.backgroundTimeout = 50 * time.Millisecond

	service.queueRefresh(provider.CatalogInput{
		Identity: provider.ProviderIdentity{
			Driver: openaicompat.DriverName,
		},
	})

	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&discoverCalls) != 0 {
		t.Fatalf("expected no background discovery, got %d calls", discoverCalls)
	}
	if len(service.inFlightByID) != 0 {
		t.Fatalf("expected no in-flight markers, got %+v", service.inFlightByID)
	}
}

func TestJSONStoreAdditionalFilesystemErrors(t *testing.T) {
	t.Parallel()

	t.Run("load read failure", func(t *testing.T) {
		t.Parallel()

		store := newJSONStore(t.TempDir())
		identity, err := provider.NewProviderIdentity("openai", "https://api.openai.com/v1")
		if err != nil {
			t.Fatalf("NewProviderIdentity() error = %v", err)
		}
		if err := os.MkdirAll(store.catalogPath(identity), 0o755); err != nil {
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
		identity, err := provider.NewProviderIdentity("openai", "https://api.openai.com/v1")
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
	input, err := config.NewProviderCatalogInput(config.OpenAIProvider())
	if err != nil {
		t.Fatalf("NewProviderCatalogInput() error = %v", err)
	}
	snapshot := service.catalogSnapshot(context.Background(), input)
	if snapshot.ok || snapshot.expired || len(snapshot.models) != 0 {
		t.Fatalf("expected empty snapshot on cache miss, got %+v", snapshot)
	}
}
