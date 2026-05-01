package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestJSONStoreRoundTrip(t *testing.T) {
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

	expected := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      normalizedIdentity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		Models: []providertypes.ModelDescriptor{
			{
				ID:              "gpt-4.1",
				Name:            "GPT-4.1",
				Description:     "Fast flagship",
				ContextWindow:   128000,
				MaxOutputTokens: 16384,
				CapabilityHints: providertypes.ModelCapabilityHints{
					ToolCalling: providertypes.ModelCapabilityStateSupported,
				},
			},
		},
	}

	if err := store.Save(context.Background(), expected); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.SchemaVersion != expected.SchemaVersion {
		t.Fatalf("expected schema version %d, got %d", expected.SchemaVersion, got.SchemaVersion)
	}
	if got.Identity != expected.Identity {
		t.Fatalf("expected identity %+v, got %+v", expected.Identity, got.Identity)
	}
	if !got.FetchedAt.Equal(expected.FetchedAt) {
		t.Fatalf("expected timestamps %+v, got %+v", expected, got)
	}
	if len(got.Models) != 1 {
		t.Fatalf("expected 1 model, got %+v", got.Models)
	}
	if got.Models[0].ID != expected.Models[0].ID || got.Models[0].Name != expected.Models[0].Name {
		t.Fatalf("expected model %+v, got %+v", expected.Models[0], got.Models[0])
	}
	if got.Models[0].CapabilityHints.ToolCalling != providertypes.ModelCapabilityStateSupported {
		t.Fatalf("expected capability hints to round-trip, got %+v", got.Models[0].CapabilityHints)
	}
}

func TestJSONStoreSeparatesEndpointSpecificIdentityKeys(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	firstIdentity := provider.ProviderIdentity{
		Driver:           "openaicompat",
		BaseURL:          "https://API.EXAMPLE.COM/v1/",
		ChatEndpointPath: "/chat/completions",
	}
	secondIdentity := provider.ProviderIdentity{
		Driver:           "openaicompat",
		BaseURL:          "https://api.example.com/v1",
		ChatEndpointPath: "/v2/chat/completions",
	}

	if err := store.Save(context.Background(), ModelCatalog{
		Identity: firstIdentity,
		Models: []providertypes.ModelDescriptor{
			{ID: "first-model", Name: "First Model"},
		},
	}); err != nil {
		t.Fatalf("save first catalog: %v", err)
	}
	if err := store.Save(context.Background(), ModelCatalog{
		Identity: secondIdentity,
		Models: []providertypes.ModelDescriptor{
			{ID: "second-model", Name: "Second Model"},
		},
	}); err != nil {
		t.Fatalf("save second catalog: %v", err)
	}

	firstCatalog, err := store.Load(context.Background(), firstIdentity)
	if err != nil {
		t.Fatalf("load first catalog: %v", err)
	}
	if len(firstCatalog.Models) != 1 || firstCatalog.Models[0].ID != "first-model" {
		t.Fatalf("expected first catalog to stay isolated, got %+v", firstCatalog.Models)
	}
	secondCatalog, err := store.Load(context.Background(), secondIdentity)
	if err != nil {
		t.Fatalf("load second catalog: %v", err)
	}
	if len(secondCatalog.Models) != 1 || secondCatalog.Models[0].ID != "second-model" {
		t.Fatalf("expected second catalog to stay isolated, got %+v", secondCatalog.Models)
	}
}

func TestJSONStoreMissingCatalog(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	_, err = store.Load(context.Background(), identity)
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("expected ErrCatalogNotFound, got %v", err)
	}
}

func TestJSONStoreSaveReplacesExistingCatalogWithoutTempLeak(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := newJSONStore(baseDir)
	identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}
	normalizedIdentity, err := provider.NormalizeProviderIdentity(identity)
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() error = %v", err)
	}

	first := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		Models: []providertypes.ModelDescriptor{
			{ID: "gpt-old", Name: "GPT Old"},
		},
	}
	second := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
		Models: []providertypes.ModelDescriptor{
			{ID: "gpt-new", Name: "GPT New"},
		},
	}

	if err := store.Save(context.Background(), first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := store.Save(context.Background(), second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	got, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "gpt-new" {
		t.Fatalf("expected replaced catalog contents, got %+v", got.Models)
	}

	matches, err := filepath.Glob(filepath.Join(baseDir, "cache", "models", "*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp files to remain, got %+v", matches)
	}

	data, err := os.ReadFile(store.catalogPath(normalizedIdentity))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected persisted catalog to end with newline, got %q", string(data))
	}
}

func TestJSONStoreLoadHonorsContextError(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = store.Load(ctx, identity)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestJSONStoreLoadRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	_, err := store.Load(context.Background(), provider.ProviderIdentity{
		Driver:  "openaicompat",
		BaseURL: "://bad",
	})
	if err == nil || !strings.Contains(err.Error(), "normalize model catalog key") {
		t.Fatalf("expected identity normalization error, got %v", err)
	}
}

func TestJSONStoreLoadRejectsInvalidJSON(t *testing.T) {
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

	if err := os.MkdirAll(filepath.Dir(store.catalogPath(normalizedIdentity)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(store.catalogPath(normalizedIdentity), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = store.Load(context.Background(), identity)
	if err == nil || !strings.Contains(err.Error(), "decode model catalog") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestJSONStoreSaveHonorsContextError(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := provider.NewProviderIdentity("openaicompat", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = store.Save(ctx, ModelCatalog{Identity: identity})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestJSONStoreSaveRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	err := store.Save(context.Background(), ModelCatalog{
		Identity: provider.ProviderIdentity{
			Driver:  "",
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "normalize model catalog key") {
		t.Fatalf("expected identity normalization error, got %v", err)
	}
}

func TestNormalizeCatalogDefaultsSchemaAndDedupesModels(t *testing.T) {
	t.Parallel()

	modelCatalog := normalizeCatalog(ModelCatalog{
		Models: []providertypes.ModelDescriptor{
			{ID: "gpt-4o", Name: "GPT-4o"},
			{ID: "gpt-4o", Name: "GPT-4o Duplicate"},
		},
	})
	if modelCatalog.SchemaVersion != schemaVersion {
		t.Fatalf("expected schema version %d, got %d", schemaVersion, modelCatalog.SchemaVersion)
	}
	if len(modelCatalog.Models) != 1 {
		t.Fatalf("expected duplicate models to be merged, got %+v", modelCatalog.Models)
	}
}
