package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-llm-demo/internal/server/domain"
)

type FileMemoryStore struct {
	path     string
	maxItems int
	mu       sync.Mutex
}

func NewFileMemoryStore(path string, maxItems int) *FileMemoryStore {
	return &FileMemoryStore{path: path, maxItems: maxItems}
}

func (s *FileMemoryStore) List(ctx context.Context) ([]domain.MemoryItem, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}
	cloned := make([]domain.MemoryItem, len(items))
	copy(cloned, items)
	return cloned, nil
}

func (s *FileMemoryStore) Add(ctx context.Context, item domain.MemoryItem) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	item = item.Normalized()
	if !domain.IsPersistentType(item.Type) {
		return nil
	}

	items, err := s.readAllLocked()
	if err != nil {
		return err
	}
	items = upsertPersistentItem(items, item)
	if s.maxItems > 0 && len(items) > s.maxItems {
		items = items[len(items)-s.maxItems:]
	}
	return s.writeAllLocked(items)
}

func (s *FileMemoryStore) Clear(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeAllLocked(nil)
}

func (s *FileMemoryStore) readAllLocked() ([]domain.MemoryItem, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var items []domain.MemoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		backupPath := fmt.Sprintf("%s.corrupt.%d", s.path, time.Now().UnixNano())
		if renameErr := os.Rename(s.path, backupPath); renameErr != nil {
			return nil, fmt.Errorf("persistent memory decode failed: %w", err)
		}
		return nil, fmt.Errorf("persistent memory decode failed, corrupt file moved to %s: %w", backupPath, err)
	}

	filtered := make([]domain.MemoryItem, 0, len(items))
	for _, item := range items {
		normalized := item.Normalized()
		if domain.IsPersistentType(normalized.Type) {
			filtered = append(filtered, normalized)
		}
	}
	return filtered, nil
}

func (s *FileMemoryStore) writeAllLocked(items []domain.MemoryItem) error {
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("persistent memory path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "memory-rules-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err == nil {
		return nil
	}
	if removeErr := os.Remove(s.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func upsertPersistentItem(items []domain.MemoryItem, item domain.MemoryItem) []domain.MemoryItem {
	key := persistentKey(item)
	for idx, existing := range items {
		if persistentKey(existing.Normalized()) != key {
			continue
		}
		updated := existing.Normalized()
		updated.Type = item.Type
		updated.Summary = item.Summary
		updated.Details = item.Details
		updated.Scope = item.Scope
		updated.Tags = item.Tags
		updated.Source = item.Source
		updated.UserInput = item.UserInput
		updated.AssistantReply = item.AssistantReply
		updated.Text = item.Text
		if item.Confidence > updated.Confidence {
			updated.Confidence = item.Confidence
		}
		if updated.CreatedAt.IsZero() {
			updated.CreatedAt = item.CreatedAt
		}
		updated.UpdatedAt = item.UpdatedAt
		items[idx] = updated
		return items
	}
	return append(items, item)
}

func persistentKey(item domain.MemoryItem) string {
	normalized := item.Normalized()
	return normalized.Type + "::" + normalized.Scope + "::" + compactKey(normalized.Summary)
}

func compactKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer(" ", "", "\n", "", "\t", "", ",", "", ".", "", ":", "", ";", "", "-", "", "_", "", "/", "", "\\", "").Replace(text)
	return text
}
