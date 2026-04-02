package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/config"
)

func TestJSONModelCatalogStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewJSONModelCatalogStore(t.TempDir())
	identity, err := config.NewProviderIdentity("OPENAI", "https://API.OPENAI.COM/v1/")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	expected := ModelCatalog{
		SchemaVersion: modelCatalogSchemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC),
		Models: []ModelDescriptor{{
			ID:   "gpt-test",
			Name: "GPT Test",
			Metadata: map[string]any{
				"id":           "gpt-test",
				"experimental": map[string]any{"tier": "beta"},
			},
		}},
	}

	if err := store.Save(context.Background(), expected); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Identity.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected normalized base url, got %q", got.Identity.BaseURL)
	}
	if len(got.Models) != 1 {
		t.Fatalf("expected 1 model, got %+v", got.Models)
	}
	if _, ok := got.Models[0].Metadata["experimental"]; !ok {
		t.Fatalf("expected nested metadata to survive round-trip, got %+v", got.Models[0].Metadata)
	}
}

func TestJSONModelCatalogStoreMissingCatalog(t *testing.T) {
	t.Parallel()

	store := NewJSONModelCatalogStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	_, err = store.Load(context.Background(), identity)
	if !errors.Is(err, ErrModelCatalogNotFound) {
		t.Fatalf("expected ErrModelCatalogNotFound, got %v", err)
	}
}
