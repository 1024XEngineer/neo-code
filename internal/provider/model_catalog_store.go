package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"neo-code/internal/config"
)

const modelCatalogSchemaVersion = 1

var ErrModelCatalogNotFound = errors.New("provider: model catalog not found")

// ModelCatalog stores discovered provider models for a concrete endpoint.
type ModelCatalog struct {
	SchemaVersion int                     `json:"schema_version"`
	Identity      config.ProviderIdentity `json:"identity"`
	FetchedAt     time.Time               `json:"fetched_at"`
	ExpiresAt     time.Time               `json:"expires_at"`
	Models        []ModelDescriptor       `json:"models"`
}

func (c ModelCatalog) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

// ModelCatalogStore persists model catalogs keyed by normalized provider identity.
type ModelCatalogStore interface {
	Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error)
	Save(ctx context.Context, catalog ModelCatalog) error
}

type JSONModelCatalogStore struct {
	dir string
}

func NewJSONModelCatalogStore(baseDir string) *JSONModelCatalogStore {
	return &JSONModelCatalogStore{
		dir: filepath.Join(strings.TrimSpace(baseDir), "cache", "models"),
	}
}

func (s *JSONModelCatalogStore) Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	normalized, err := config.NewProviderIdentity(identity.Driver, identity.BaseURL)
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("provider: normalize model catalog key: %w", err)
	}

	path, err := s.catalogPathFromNormalized(normalized)
	if err != nil {
		return ModelCatalog{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ModelCatalog{}, ErrModelCatalogNotFound
		}
		return ModelCatalog{}, fmt.Errorf("provider: read model catalog: %w", err)
	}

	var catalog ModelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return ModelCatalog{}, fmt.Errorf("provider: decode model catalog: %w", err)
	}

	catalog.Identity = normalized
	return normalizeModelCatalog(catalog), nil
}

func (s *JSONModelCatalogStore) Save(ctx context.Context, catalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normalized := normalizeModelCatalog(catalog)

	normalizedIdentity, err := config.NewProviderIdentity(normalized.Identity.Driver, normalized.Identity.BaseURL)
	if err != nil {
		return fmt.Errorf("provider: normalize model catalog key: %w", err)
	}
	normalized.Identity = normalizedIdentity

	path, err := s.catalogPathFromNormalized(normalized.Identity)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("provider: create model catalog dir: %w", err)
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("provider: encode model catalog: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("provider: write model catalog: %w", err)
	}
	return nil
}

func normalizeModelCatalog(catalog ModelCatalog) ModelCatalog {
	if catalog.SchemaVersion == 0 {
		catalog.SchemaVersion = modelCatalogSchemaVersion
	}
	catalog.Models = MergeModelDescriptors(catalog.Models)
	return catalog
}

func (s *JSONModelCatalogStore) catalogPathFromNormalized(identity config.ProviderIdentity) (string, error) {
	sum := sha256.Sum256([]byte(identity.Key()))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json"), nil
}
