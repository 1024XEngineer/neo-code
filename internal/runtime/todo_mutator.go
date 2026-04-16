package runtime

import (
	"context"
	"errors"
	"time"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// runtimeSessionMutator 把工具层 Todo 写操作安全落到当前运行中的会话状态。
type runtimeSessionMutator struct {
	ctx     context.Context
	service *Service
	state   *runState
}

// newRuntimeSessionMutator 创建绑定本次 run 的会话 Todo 变更适配器。
func newRuntimeSessionMutator(ctx context.Context, service *Service, state *runState) tools.SessionMutator {
	if ctx == nil || service == nil || state == nil {
		return nil
	}
	return &runtimeSessionMutator{
		ctx:     ctx,
		service: service,
		state:   state,
	}
}

// ListTodos 返回当前会话 Todo 的深拷贝快照。
func (m *runtimeSessionMutator) ListTodos() []agentsession.TodoItem {
	if m == nil || m.state == nil {
		return nil
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.session.ListTodos()
}

// FindTodo 返回指定 Todo 的深拷贝快照。
func (m *runtimeSessionMutator) FindTodo(id string) (agentsession.TodoItem, bool) {
	if m == nil || m.state == nil {
		return agentsession.TodoItem{}, false
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.session.FindTodo(id)
}

// ReplaceTodos 批量替换 Todos 并立即持久化。
func (m *runtimeSessionMutator) ReplaceTodos(items []agentsession.TodoItem) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.ReplaceTodos(items)
	})
}

// AddTodo 追加 Todo 并立即持久化。
func (m *runtimeSessionMutator) AddTodo(item agentsession.TodoItem) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.AddTodo(item)
	})
}

// UpdateTodo 按补丁更新 Todo 并立即持久化。
func (m *runtimeSessionMutator) UpdateTodo(id string, patch agentsession.TodoPatch, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.UpdateTodo(id, patch, expectedRevision)
	})
}

// SetTodoStatus 按状态迁移更新 Todo 并立即持久化。
func (m *runtimeSessionMutator) SetTodoStatus(id string, status agentsession.TodoStatus, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.SetTodoStatus(id, status, expectedRevision)
	})
}

// DeleteTodo 删除 Todo 并立即持久化。
func (m *runtimeSessionMutator) DeleteTodo(id string, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.DeleteTodo(id, expectedRevision)
	})
}

// ClaimTodo 领取 Todo 并立即持久化。
func (m *runtimeSessionMutator) ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.ClaimTodo(id, ownerType, ownerID, expectedRevision)
	})
}

// CompleteTodo 完成 Todo 并立即持久化。
func (m *runtimeSessionMutator) CompleteTodo(id string, artifacts []string, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.CompleteTodo(id, artifacts, expectedRevision)
	})
}

// FailTodo 标记 Todo 失败并立即持久化。
func (m *runtimeSessionMutator) FailTodo(id string, reason string, expectedRevision int64) error {
	return m.mutateAndSave(func(session *agentsession.Session) error {
		return session.FailTodo(id, reason, expectedRevision)
	})
}

// mutateAndSave 在同一临界区内完成“基于快照修改 + 落盘 + 内存替换”，保证会话更新原子性。
func (m *runtimeSessionMutator) mutateAndSave(mutate func(session *agentsession.Session) error) error {
	if m == nil || m.service == nil || m.state == nil {
		return errors.New("runtime: session mutator is unavailable")
	}
	if err := m.ctx.Err(); err != nil {
		return err
	}

	m.state.mu.Lock()
	sessionSnapshot := cloneSessionForPersistence(m.state.session)
	if err := mutate(&sessionSnapshot); err != nil {
		m.state.mu.Unlock()
		return err
	}
	sessionSnapshot.UpdatedAt = time.Now()
	if err := m.service.sessionStore.Save(m.ctx, &sessionSnapshot); err != nil {
		m.state.mu.Unlock()
		return err
	}
	m.state.session = sessionSnapshot
	m.state.mu.Unlock()
	return nil
}
