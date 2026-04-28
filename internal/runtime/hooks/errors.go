package hooks

import (
	"errors"
	"fmt"
)

var (
	// ErrHookAlreadyExists 表示注册了重复 hook ID。
	ErrHookAlreadyExists = errors.New("hook already exists")
	// ErrHookNotFound 表示待删除 hook 不存在。
	ErrHookNotFound = errors.New("hook not found")
	// ErrInvalidHookSpec 表示 HookSpec 未通过校验。
	ErrInvalidHookSpec = errors.New("invalid hook spec")
)

// wrapInvalidSpec 将参数化错误统一包装为 ErrInvalidHookSpec。
func wrapInvalidSpec(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidHookSpec, fmt.Sprintf(format, args...))
}
