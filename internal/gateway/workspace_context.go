package gateway

import (
	"context"
	"strings"
	"sync"
)

type workspaceHashContextKey struct{}
type connectionWorkspaceStateContextKey struct{}

// ConnectionWorkspaceState 维护连接当前活跃的工作区哈希。
type ConnectionWorkspaceState struct {
	mu          sync.RWMutex
	workspaceHash string
}

// NewConnectionWorkspaceState 创建连接工作区状态对象。
func NewConnectionWorkspaceState() *ConnectionWorkspaceState {
	return &ConnectionWorkspaceState{}
}

// SetWorkspaceHash 设置当前连接的工作区哈希。
func (s *ConnectionWorkspaceState) SetWorkspaceHash(hash string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.workspaceHash = strings.TrimSpace(hash)
	s.mu.Unlock()
}

// GetWorkspaceHash 返回当前连接的工作区哈希。
func (s *ConnectionWorkspaceState) GetWorkspaceHash() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspaceHash
}

// WithConnectionWorkspaceState 将连接工作区状态注入上下文。
func WithConnectionWorkspaceState(ctx context.Context, state *ConnectionWorkspaceState) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, connectionWorkspaceStateContextKey{}, state)
}

// ConnectionWorkspaceStateFromContext 从上下文读取连接工作区状态。
func ConnectionWorkspaceStateFromContext(ctx context.Context) (*ConnectionWorkspaceState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(connectionWorkspaceStateContextKey{}).(*ConnectionWorkspaceState)
	if !ok || state == nil {
		return nil, false
	}
	return state, true
}

// WorkspaceHashFromContext 从上下文读取当前工作区哈希（快捷方法）。
func WorkspaceHashFromContext(ctx context.Context) string {
	if state, ok := ConnectionWorkspaceStateFromContext(ctx); ok {
		return state.GetWorkspaceHash()
	}
	return ""
}
