package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	expected := ModelCatalog{
		SchemaVersion: SchemaVersion,
		Identity:      identity,
		FetchedAt:     time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC),
		Models: []provider.ModelDescriptor{
			{
				ID:              "gpt-4.1",
				Name:            "GPT-4.1",
				Description:     "Fast flagship",
				ContextWindow:   128000,
				MaxOutputTokens: 16384,
				Capabilities: map[string]bool{
					"tool_call": true,
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
	if !got.Models[0].Capabilities["tool_call"] {
		t.Fatalf("expected capabilities to round-trip, got %+v", got.Models[0].Capabilities)
	}
}

func TestJSONStoreMissingCatalog(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir())
	identity, err := config.NewProviderIdentity("openai", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}

	_, err = store.Load(context.Background(), identity)
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("expected ErrCatalogNotFound, got %v", err)
	}
}
