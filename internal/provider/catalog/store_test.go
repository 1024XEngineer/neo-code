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
	providertypes "neo-code/internal/provider/types"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	expected := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC),
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
	if !got.FetchedAt.Equal(expected.FetchedAt) || !got.ExpiresAt.Equal(expected.ExpiresAt) {
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

func TestJSONStoreSeparatesDriverSpecificIdentityKeys(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	responsesIdentity := config.ProviderIdentity{
		Driver:   "openaicompat",
		BaseURL:  "https://API.EXAMPLE.COM/v1/",
		APIStyle: " Responses ",
	}
	chatIdentity := config.ProviderIdentity{
		Driver:   "openaicompat",
		BaseURL:  "https://api.example.com/v1",
		APIStyle: "chat_completions",
	}

	if err := store.Save(context.Background(), ModelCatalog{
		Identity: responsesIdentity,
		Models: []providertypes.ModelDescriptor{
			{ID: "responses-model", Name: "Responses Model"},
		},
	}); err != nil {
		t.Fatalf("save responses catalog: %v", err)
	}
	if err := store.Save(context.Background(), ModelCatalog{
		Identity: chatIdentity,
		Models: []providertypes.ModelDescriptor{
			{ID: "chat-model", Name: "Chat Model"},
		},
	}); err != nil {
		t.Fatalf("save chat catalog: %v", err)
	}

	responsesCatalog, err := store.Load(context.Background(), responsesIdentity)
	if err != nil {
		t.Fatalf("load responses catalog: %v", err)
	}
	if len(responsesCatalog.Models) != 1 || responsesCatalog.Models[0].ID != "responses-model" {
		t.Fatalf("expected responses catalog to stay isolated, got %+v", responsesCatalog.Models)
	}
	if responsesCatalog.Identity.APIStyle != "responses" {
		t.Fatalf("expected normalized api_style=responses, got %+v", responsesCatalog.Identity)
	}

	chatCatalog, err := store.Load(context.Background(), chatIdentity)
	if err != nil {
		t.Fatalf("load chat catalog: %v", err)
	}
	if len(chatCatalog.Models) != 1 || chatCatalog.Models[0].ID != "chat-model" {
		t.Fatalf("expected chat catalog to stay isolated, got %+v", chatCatalog.Models)
	}
	if chatCatalog.Identity.APIStyle != "chat_completions" {
		t.Fatalf("expected normalized api_style=chat_completions, got %+v", chatCatalog.Identity)
	}
}

func TestJSONStoreMissingCatalog(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
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
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	first := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC),
		Models: []providertypes.ModelDescriptor{
			{ID: "gpt-old", Name: "GPT Old"},
		},
	}
	second := ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC),
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

	data, err := os.ReadFile(store.catalogPath(identity))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected persisted catalog to end with newline, got %q", string(data))
	}
}

func TestModelCatalogExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	if (ModelCatalog{}).Expired(now) {
		t.Fatal("expected zero-value catalog to be treated as not expired")
	}
	if !(ModelCatalog{ExpiresAt: now}).Expired(now) {
		t.Fatal("expected catalog expiring at now to be expired")
	}
	if (ModelCatalog{ExpiresAt: now.Add(time.Minute)}).Expired(now) {
		t.Fatal("expected future expiry to be treated as fresh")
	}
}

func TestJSONStoreLoadHonorsContextError(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
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
	_, err := store.Load(context.Background(), config.ProviderIdentity{
		Driver:  "openai",
		BaseURL: "://bad",
	})
	if err == nil || !strings.Contains(err.Error(), "normalize model catalog key") {
		t.Fatalf("expected identity normalization error, got %v", err)
	}
}

func TestJSONStoreLoadRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	store := newJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(store.catalogPath(identity)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(store.catalogPath(identity), []byte("{not-json"), 0o644); err != nil {
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
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
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
		Identity: config.ProviderIdentity{
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
