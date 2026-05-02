package runtime

import (
	"context"
	"strings"
)

// ListTodos 返回指定会话的 Todo 快照，供网关与桌面端做初始化/重连同步。
func (s *Service) ListTodos(ctx context.Context, sessionID string) (TodoSnapshot, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return TodoSnapshot{}, nil
	}
	session, err := s.sessionStore.LoadSession(ctx, normalizedSessionID)
	if err != nil {
		return TodoSnapshot{}, err
	}
	return buildTodoSnapshotFromItems(session.ListTodos()), nil
}
