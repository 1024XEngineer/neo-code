package repository

import (
	"context"
	"sync"

	"go-llm-demo/internal/server/domain"
)

// WorkingMemoryStore 在当前进程内保存会话级工作记忆。
// 第一阶段先使用内存实现，后续如需跨进程恢复再替换为持久化版本。
type WorkingMemoryStore struct {
	mu    sync.RWMutex
	state *domain.WorkingMemoryState
}

// NewWorkingMemoryStore 创建一个进程内工作记忆存储。
func NewWorkingMemoryStore() *WorkingMemoryStore {
	return &WorkingMemoryStore{}
}

// Get 返回当前工作记忆快照的拷贝。
func (s *WorkingMemoryStore) Get(ctx context.Context) (*domain.WorkingMemoryState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil {
		return &domain.WorkingMemoryState{}, nil
	}
	return cloneWorkingMemoryState(s.state), nil
}

// Save 替换当前保存的工作记忆快照。
func (s *WorkingMemoryStore) Save(ctx context.Context, state *domain.WorkingMemoryState) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = cloneWorkingMemoryState(state)
	return nil
}

// Clear 清空已保存的工作记忆快照。
func (s *WorkingMemoryStore) Clear(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = nil
	return nil
}

func cloneWorkingMemoryState(state *domain.WorkingMemoryState) *domain.WorkingMemoryState {
	if state == nil {
		return &domain.WorkingMemoryState{}
	}
	cloned := &domain.WorkingMemoryState{
		CurrentTask: state.CurrentTask,
		TaskSummary: state.TaskSummary,
	}
	if len(state.RecentTurns) > 0 {
		cloned.RecentTurns = make([]domain.WorkingMemoryTurn, len(state.RecentTurns))
		copy(cloned.RecentTurns, state.RecentTurns)
	}
	if len(state.OpenQuestions) > 0 {
		cloned.OpenQuestions = make([]string, len(state.OpenQuestions))
		copy(cloned.OpenQuestions, state.OpenQuestions)
	}
	if len(state.RecentFiles) > 0 {
		cloned.RecentFiles = make([]string, len(state.RecentFiles))
		copy(cloned.RecentFiles, state.RecentFiles)
	}
	return cloned
}
