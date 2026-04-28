package hooks

import (
	"context"
	"time"
)

// HookEventType 标识 hook 事件类型。
type HookEventType string

const (
	// HookEventStarted 表示 hook 执行开始事件。
	HookEventStarted HookEventType = "hook_started"
	// HookEventFinished 表示 hook 执行结束事件（pass/block）。
	HookEventFinished HookEventType = "hook_finished"
	// HookEventFailed 表示 hook 执行失败事件。
	HookEventFailed HookEventType = "hook_failed"
)

// HookEvent 描述 hook 执行过程中的结构化事件。
type HookEvent struct {
	Type       HookEventType
	HookID     string
	Point      HookPoint
	Scope      HookScope
	Kind       HookKind
	Mode       HookMode
	Status     HookResultStatus
	StartedAt  time.Time
	DurationMS int64
	Error      string
}

// EventEmitter 抽象 hook 事件发射器，避免依赖 runtime.Service。
type EventEmitter interface {
	EmitHookEvent(ctx context.Context, event HookEvent) error
}
