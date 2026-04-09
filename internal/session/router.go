package session

import (
	"context"
	"sync"
)

// WorkspaceStoreProvider 定义按工作区根目录路由会话存储的能力。
type WorkspaceStoreProvider interface {
	StoreForWorkspace(workspaceRoot string) Store
}

// ScopedStoreRouter 基于工作区根目录缓存并复用具体的会话存储实例。
type ScopedStoreRouter struct {
	mu          sync.Mutex
	baseDir     string
	defaultRoot string
	stores      map[string]Store
}

// NewScopedStoreRouter 创建一个按工作区根目录分桶的会话存储路由器。
func NewScopedStoreRouter(baseDir string, defaultRoot string) *ScopedStoreRouter {
	return &ScopedStoreRouter{
		baseDir:     baseDir,
		defaultRoot: normalizeWorkspaceRoot(defaultRoot),
		stores:      make(map[string]Store),
	}
}

// StoreForWorkspace 返回指定工作区根目录对应的会话存储实例。
func (r *ScopedStoreRouter) StoreForWorkspace(workspaceRoot string) Store {
	if r == nil {
		return nil
	}

	normalized := normalizeWorkspaceRoot(workspaceRoot)
	if normalized == "" {
		normalized = r.defaultRoot
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if store, ok := r.stores[normalized]; ok {
		return store
	}

	store := NewJSONStore(r.baseDir, normalized)
	r.stores[normalized] = store
	return store
}

// Save 使用默认工作区根目录对应的存储执行保存。
func (r *ScopedStoreRouter) Save(ctx context.Context, session *Session) error {
	if r == nil {
		return nil
	}
	store := r.StoreForWorkspace(r.defaultRoot)
	if store == nil {
		return nil
	}
	return store.Save(ctx, session)
}

// Load 使用默认工作区根目录对应的存储执行读取。
func (r *ScopedStoreRouter) Load(ctx context.Context, id string) (Session, error) {
	if r == nil {
		return Session{}, nil
	}
	store := r.StoreForWorkspace(r.defaultRoot)
	if store == nil {
		return Session{}, nil
	}
	return store.Load(ctx, id)
}

// ListSummaries 使用默认工作区根目录对应的存储列出会话摘要。
func (r *ScopedStoreRouter) ListSummaries(ctx context.Context) ([]Summary, error) {
	if r == nil {
		return nil, nil
	}
	store := r.StoreForWorkspace(r.defaultRoot)
	if store == nil {
		return nil, nil
	}
	return store.ListSummaries(ctx)
}
