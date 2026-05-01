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
	"sync"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const schemaVersion = 4

var ErrCatalogNotFound = errors.New("provider: model catalog not found")

// ModelCatalog stores discovered models for a concrete provider endpoint.
type ModelCatalog struct {
	SchemaVersion int                             `json:"schema_version"`
	Identity      provider.ProviderIdentity       `json:"identity"`
	FetchedAt     time.Time                       `json:"fetched_at"`
	Models        []providertypes.ModelDescriptor `json:"models"`
}

// Store persists model catalogs keyed by normalized provider identity.
type Store interface {
	Load(ctx context.Context, identity provider.ProviderIdentity) (ModelCatalog, error)
	Save(ctx context.Context, catalog ModelCatalog) error
}

type jsonStore struct {
	dir string
	mu  sync.RWMutex
}

func newJSONStore(baseDir string) *jsonStore {
	return &jsonStore{
		dir: filepath.Join(strings.TrimSpace(baseDir), "cache", "models"),
	}
}

func (s *jsonStore) Load(ctx context.Context, identity provider.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	normalized, err := provider.NormalizeProviderIdentity(identity)
	if err != nil {
		return ModelCatalog{}, fmt.Errorf("provider: normalize model catalog key: %w", err)
	}

	path := s.catalogPath(normalized)
	s.mu.RLock()
	data, err := os.ReadFile(path)
	s.mu.RUnlock()
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

func (s *jsonStore) Save(ctx context.Context, catalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normalized := normalizeCatalog(catalog)
	identity, err := provider.NormalizeProviderIdentity(normalized.Identity)
	if err != nil {
		return fmt.Errorf("provider: normalize model catalog key: %w", err)
	}
	normalized.Identity = identity

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("provider: encode model catalog: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeCatalogFile(s.catalogPath(identity), data); err != nil {
		return fmt.Errorf("provider: write model catalog: %w", err)
	}
	return nil
}

func normalizeCatalog(modelCatalog ModelCatalog) ModelCatalog {
	if modelCatalog.SchemaVersion == 0 {
		modelCatalog.SchemaVersion = schemaVersion
	}
	modelCatalog.Models = providertypes.MergeModelDescriptors(modelCatalog.Models)
	return modelCatalog
}

func (s *jsonStore) catalogPath(identity provider.ProviderIdentity) string {
	sum := sha256.Sum256([]byte(identity.Key()))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

func (s *jsonStore) writeCatalogFile(path string, data []byte) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("provider: create model catalog dir: %w", err)
	}

	tempFile, err := os.CreateTemp(s.dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("provider: create temp model catalog: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("provider: write temp model catalog: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("provider: sync temp model catalog: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("provider: close temp model catalog: %w", err)
	}

	if err := os.Rename(tempPath, path); err == nil {
		cleanupTemp = false
		return nil
	}

	// Windows 上覆盖已存在文件时 rename 可能失败，退回到同步保护下的直接写入。
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("provider: replace model catalog: %w", err)
	}

	return nil
}
