package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type FileStore struct {
	path     string
	maxItems int
	mu       sync.Mutex
}

func NewFileStore(path string, maxItems int) *FileStore {
	return &FileStore{path: path, maxItems: maxItems}
}

func (s *FileStore) List(ctx context.Context) ([]Item, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	cloned := make([]Item, len(items))
	copy(cloned, items)
	return cloned, nil
}

func (s *FileStore) Add(ctx context.Context, item Item) error {
	_ = ctx

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

func (s *FileStore) Clear(ctx context.Context) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeAllLocked([]Item{})
}

func (s *FileStore) readAllLocked() ([]Item, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Item{}, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		return []Item{}, nil
	}

	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *FileStore) writeAllLocked(items []Item) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}
