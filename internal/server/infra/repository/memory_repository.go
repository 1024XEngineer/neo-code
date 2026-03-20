package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

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
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAllLocked()
	if err != nil {
		return err
	}

	items = append(items, item)
	if s.maxItems > 0 && len(items) > s.maxItems {
		items = items[len(items)-s.maxItems:]
	}

	return s.writeAllLocked(items)
}

func (s *FileMemoryStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeAllLocked([]domain.MemoryItem{})
}

func (s *FileMemoryStore) readAllLocked() ([]domain.MemoryItem, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []domain.MemoryItem{}, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		return []domain.MemoryItem{}, nil
	}

	var items []domain.MemoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *FileMemoryStore) writeAllLocked(items []domain.MemoryItem) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}
