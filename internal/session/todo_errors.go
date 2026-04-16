package session

import "errors"

var (
	// ErrTodoNotFound 表示按 ID 查询不到 Todo。
	ErrTodoNotFound = errors.New("session: todo not found")
	// ErrInvalidTransition 表示 Todo 状态机迁移非法。
	ErrInvalidTransition = errors.New("session: invalid status transition")
	// ErrRevisionConflict 表示 expected revision 与当前版本不一致。
	ErrRevisionConflict = errors.New("session: revision conflict")
	// ErrCyclicDependency 表示 Todo 依赖图出现环。
	ErrCyclicDependency = errors.New("session: cyclic dependency detected")
	// ErrDependencyViolation 表示依赖约束不满足。
	ErrDependencyViolation = errors.New("session: dependency violation")
)
