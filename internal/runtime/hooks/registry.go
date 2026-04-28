package hooks

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type registryEntry struct {
	spec HookSpec
	seq  int64
}

type resolvedEntry struct {
	spec HookSpec
	seq  int64
}

// Registry 管理 hook 的注册、移除与按点位解析。
type Registry struct {
	mu    sync.RWMutex
	hooks map[string]registryEntry
	seq   int64
}

// NewRegistry 创建一个空的 hook 注册表。
func NewRegistry() *Registry {
	return &Registry{
		hooks: make(map[string]registryEntry),
	}
}

// Register 注册一个 hook；若 ID 重复则返回 ErrHookAlreadyExists。
func (r *Registry) Register(spec HookSpec) error {
	if r == nil {
		return wrapInvalidSpec("registry is nil")
	}
	normalized, err := spec.normalizeAndValidate()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.hooks[normalized.ID]; exists {
		return fmt.Errorf("%w: %s", ErrHookAlreadyExists, normalized.ID)
	}

	r.seq++
	r.hooks[normalized.ID] = registryEntry{
		spec: normalized,
		seq:  r.seq,
	}
	return nil
}

// Remove 按 ID 删除 hook；若不存在则返回 ErrHookNotFound。
func (r *Registry) Remove(id string) error {
	if r == nil {
		return fmt.Errorf("%w: registry is nil", ErrHookNotFound)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrHookNotFound)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.hooks[id]; !exists {
		return fmt.Errorf("%w: %s", ErrHookNotFound, id)
	}
	delete(r.hooks, id)
	return nil
}

// Resolve 返回指定点位的 hook 快照，并按优先级稳定排序。
func (r *Registry) Resolve(point HookPoint) []HookSpec {
	if r == nil {
		return nil
	}
	point = HookPoint(strings.TrimSpace(string(point)))
	if point == "" {
		return nil
	}

	r.mu.RLock()
	entries := make([]resolvedEntry, 0, len(r.hooks))
	for _, entry := range r.hooks {
		if entry.spec.Point != point {
			continue
		}
		entries = append(entries, resolvedEntry{
			spec: entry.spec,
			seq:  entry.seq,
		})
	}
	r.mu.RUnlock()

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].spec.Priority != entries[j].spec.Priority {
			return entries[i].spec.Priority > entries[j].spec.Priority
		}
		return entries[i].seq < entries[j].seq
	})

	out := make([]HookSpec, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.spec)
	}
	return out
}
