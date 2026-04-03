package catalog

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
	"neo-code/internal/provider"
)

const SchemaVersion = 1

var ErrCatalogNotFound = errors.New("provider: model catalog not found")

// ModelCatalog stores discovered models for a concrete provider endpoint.
type ModelCatalog struct {
	SchemaVersion int                        `json:"schema_version"`
	Identity      config.ProviderIdentity    `json:"identity"`
	FetchedAt     time.Time                  `json:"fetched_at"`
	ExpiresAt     time.Time                  `json:"expires_at"`
	Models        []provider.ModelDescriptor `json:"models"`
}

func (c ModelCatalog) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

// Store persists model catalogs keyed by normalized provider identity.
type Store interface {
	Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error)
	Save(ctx context.Context, catalog ModelCatalog) error
}

type JSONStore struct {
	dir string
}

func NewJSONStore(baseDir string) *JSONStore {
	return &JSONStore{
		dir: filepath.Join(strings.TrimSpace(baseDir), "cache", "models"),
	}
}

func (s *JSONStore) Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	normalized, err := config.NewProviderIdentity(identity.Driver, identity.BaseURL)
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("provider: normalize model catalog key: %w", err)
	}

	path := s.catalogPath(normalized)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ModelCatalog{}, ErrCatalogNotFound
		}
		return ModelCatalog{}, fmt.Errorf("provider: read model catalog: %w", err)
	}

	var modelCatalog ModelCatalog
	if err := json.Unmarshal(data, &modelCatalog); err != nil {
		return ModelCatalog{}, fmt.Errorf("provider: decode model catalog: %w", err)
	}

	modelCatalog.Identity = normalized
	return normalizeCatalog(modelCatalog), nil
}

func (s *JSONStore) Save(ctx context.Context, catalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normalized := normalizeCatalog(catalog)
	identity, err := config.NewProviderIdentity(normalized.Identity.Driver, normalized.Identity.BaseURL)
	if err != nil {
		return fmt.Errorf("provider: normalize model catalog key: %w", err)
	}
	normalized.Identity = identity

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

	if err := os.WriteFile(s.catalogPath(identity), data, 0o644); err != nil {
		return fmt.Errorf("provider: write model catalog: %w", err)
	}
	return nil
}

func normalizeCatalog(modelCatalog ModelCatalog) ModelCatalog {
	if modelCatalog.SchemaVersion == 0 {
		modelCatalog.SchemaVersion = SchemaVersion
	}
	modelCatalog.Models = provider.MergeModelDescriptors(modelCatalog.Models)
	return modelCatalog
}

func (s *JSONStore) catalogPath(identity config.ProviderIdentity) string {
	sum := sha256.Sum256([]byte(identity.Key()))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}
