package runtime

import (
	"context"
	"strings"
	"sync"
	"time"
)

// AskSessionStore 定义 Ask 轻量会话的最小存取契约。
type AskSessionStore interface {
	Load(ctx context.Context, sessionID string) (AskSession, bool, error)
	Save(ctx context.Context, session AskSession) error
	Delete(ctx context.Context, sessionID string) (bool, error)
}

type inMemoryAskSessionStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]AskSession
}

// newInMemoryAskSessionStore 创建内存版 AskSessionStore，默认按 TTL 回收闲置会话。
func newInMemoryAskSessionStore(ttl time.Duration) AskSessionStore {
	if ttl <= 0 {
		ttl = askSessionTTL
	}
	return &inMemoryAskSessionStore{
		ttl:      ttl,
		sessions: make(map[string]AskSession),
	}
}

// Load 读取 Ask 会话并返回副本；过期会话会在读取前被清理。
func (s *inMemoryAskSessionStore) Load(ctx context.Context, sessionID string) (AskSession, bool, error) {
	if err := ctx.Err(); err != nil {
		return AskSession{}, false, err
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return AskSession{}, false, nil
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	session, exists := s.sessions[normalizedSessionID]
	if !exists {
		return AskSession{}, false, nil
	}
	session.UpdatedAt = now
	s.sessions[normalizedSessionID] = session
	return session.Clone(), true, nil
}

// Save 写入 Ask 会话快照并刷新更新时间。
func (s *inMemoryAskSessionStore) Save(ctx context.Context, session AskSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	normalizedSessionID := strings.TrimSpace(session.ID)
	if normalizedSessionID == "" {
		return nil
	}

	now := time.Now().UTC()
	cloned := session.Clone()
	cloned.ID = normalizedSessionID
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = now
	}
	cloned.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	s.sessions[normalizedSessionID] = cloned
	return nil
}

// Delete 删除指定 Ask 会话，返回是否实际删除。
func (s *inMemoryAskSessionStore) Delete(ctx context.Context, sessionID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return false, nil
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
	if _, exists := s.sessions[normalizedSessionID]; !exists {
		return false, nil
	}
	delete(s.sessions, normalizedSessionID)
	return true, nil
}

// cleanupExpiredLocked 清理超出 TTL 的 Ask 会话（调用方需持有互斥锁）。
func (s *inMemoryAskSessionStore) cleanupExpiredLocked(now time.Time) {
	if s == nil || s.ttl <= 0 {
		return
	}
	for sessionID, session := range s.sessions {
		if session.UpdatedAt.IsZero() {
			session.UpdatedAt = now
			s.sessions[sessionID] = session
			continue
		}
		if now.Sub(session.UpdatedAt) <= s.ttl {
			continue
		}
		delete(s.sessions, sessionID)
	}
}

