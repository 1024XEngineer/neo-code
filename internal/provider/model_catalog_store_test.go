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
			ID:              "gpt-test",
			Name:            "GPT Test",
			Description:     "normalized model",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
			Capabilities: map[string]bool{
				"tool_call": true,
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
	if got.Models[0].ContextWindow != 128000 || got.Models[0].MaxOutputTokens != 8192 {
		t.Fatalf("expected normalized token fields to survive round-trip, got %+v", got.Models[0])
	}
	if !got.Models[0].Capabilities["tool_call"] {
		t.Fatalf("expected capabilities to survive round-trip, got %+v", got.Models[0].Capabilities)
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
