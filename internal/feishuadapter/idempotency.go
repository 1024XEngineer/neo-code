package feishuadapter

import (
	"strings"
	"sync"
	"time"
)

type idempotencyStore struct {
	ttl   time.Duration
	mu    sync.Mutex
	items map[string]idempotencyItem
}

type idempotencyState string

const (
	idempotencyStatePending idempotencyState = "pending"
	idempotencyStateDone    idempotencyState = "done"
)

type idempotencyItem struct {
	ExpireAt time.Time
	State    idempotencyState
}

// newIdempotencyStore 创建带 TTL 的内存去重存储。
func newIdempotencyStore(ttl time.Duration) *idempotencyStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &idempotencyStore{
		ttl:   ttl,
		items: make(map[string]idempotencyItem),
	}
}

// TryStart 尝试开始处理一个去重键，若键仍在有效窗口内则返回 false。
func (s *idempotencyStore) TryStart(key string, now time.Time) bool {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	item, exists := s.items[trimmed]
	if exists && item.ExpireAt.After(now) {
		return false
	}
	s.items[trimmed] = idempotencyItem{
		ExpireAt: now.Add(s.ttl),
		State:    idempotencyStatePending,
	}
	return true
}

// MarkDone 在请求成功受理后标记去重键为完成态。
func (s *idempotencyStore) MarkDone(key string, now time.Time) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	s.items[trimmed] = idempotencyItem{
		ExpireAt: now.Add(s.ttl),
		State:    idempotencyStateDone,
	}
}

// MarkFailed 在请求失败后释放去重键，允许后续重试。
func (s *idempotencyStore) MarkFailed(key string) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, trimmed)
}

// cleanupLocked 清理已经过期的去重键。
func (s *idempotencyStore) cleanupLocked(now time.Time) {
	if len(s.items) == 0 {
		return
	}
	for key, item := range s.items {
		if item.ExpireAt.After(now) {
			continue
		}
		delete(s.items, key)
	}
}
