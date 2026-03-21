package repository

import (
	"context"
	"strings"
	"sync"

	"go-llm-demo/internal/server/domain"
)

type SessionMemoryStore struct {
	maxItems int
	mu       sync.Mutex
	items    []domain.MemoryItem
}

// NewSessionMemoryStore 创建一个用于会话记忆的内存存储。
func NewSessionMemoryStore(maxItems int) *SessionMemoryStore {
	return &SessionMemoryStore{maxItems: maxItems}
}

// List 返回当前会话中的记忆项。
func (s *SessionMemoryStore) List(ctx context.Context) ([]domain.MemoryItem, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	cloned := make([]domain.MemoryItem, len(s.items))
	copy(cloned, s.items)
	return cloned, nil
}

// Add 新增或更新一条会话记忆项。
func (s *SessionMemoryStore) Add(ctx context.Context, item domain.MemoryItem) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := item.Normalized()
	key := sessionKey(normalized)
	for idx, existing := range s.items {
		if sessionKey(existing.Normalized()) != key {
			continue
		}
		updated := existing.Normalized()
		updated.Summary = normalized.Summary
		updated.Details = normalized.Details
		updated.Tags = normalized.Tags
		updated.Text = normalized.Text
		updated.Source = normalized.Source
		updated.Scope = normalized.Scope
		updated.Confidence = normalized.Confidence
		updated.UpdatedAt = normalized.UpdatedAt
		s.items[idx] = updated
		return nil
	}

	s.items = append(s.items, normalized)
	if s.maxItems > 0 && len(s.items) > s.maxItems {
		s.items = s.items[len(s.items)-s.maxItems:]
	}
	return nil
}

// Clear 清空全部会话记忆项。
func (s *SessionMemoryStore) Clear(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
	return nil
}

func sessionKey(item domain.MemoryItem) string {
	normalized := item.Normalized()
	return normalized.Type + "::" + normalized.Scope + "::" + strings.ToLower(strings.TrimSpace(normalized.Summary))
}
