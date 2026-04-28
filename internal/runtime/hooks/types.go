package hooks

import (
	"context"
	"strings"
	"time"
)

// HookPoint 表示 hook 的挂载点标识。
type HookPoint string

const (
	// HookPointBeforeToolCall 表示工具调用前挂点。
	HookPointBeforeToolCall HookPoint = "before_tool_call"
	// HookPointAfterToolResult 表示工具结果返回后挂点。
	HookPointAfterToolResult HookPoint = "after_tool_result"
	// HookPointBeforeCompletionDecision 表示完成决策前挂点。
	HookPointBeforeCompletionDecision HookPoint = "before_completion_decision"
)

// HookScope 描述 hook 的来源作用域。
type HookScope string

const (
	// HookScopeInternal 表示 runtime 内部 hook。
	HookScopeInternal HookScope = "internal"
	// HookScopeUser 表示用户配置 hook（P2 预留）。
	HookScopeUser HookScope = "user"
	// HookScopeRepo 表示仓库配置 hook（P3 预留）。
	HookScopeRepo HookScope = "repo"
)

// HookKind 描述 hook 处理器类型。
type HookKind string

const (
	// HookKindFunction 表示函数型 hook。
	HookKindFunction HookKind = "function"
	// HookKindCommand 表示命令型 hook（P6 预留）。
	HookKindCommand HookKind = "command"
	// HookKindHTTP 表示 HTTP 型 hook（P6 预留）。
	HookKindHTTP HookKind = "http"
	// HookKindPrompt 表示 prompt 型 hook（P6 预留）。
	HookKindPrompt HookKind = "prompt"
	// HookKindAgent 表示 agent 型 hook（P6 预留）。
	HookKindAgent HookKind = "agent"
)

// HookMode 描述 hook 的执行模式。
type HookMode string

const (
	// HookModeSync 表示同步执行。
	HookModeSync HookMode = "sync"
	// HookModeAsync 表示异步执行（P5 预留）。
	HookModeAsync HookMode = "async"
	// HookModeAsyncRewake 表示异步回灌执行（P5 预留）。
	HookModeAsyncRewake HookMode = "async_rewake"
)

// FailurePolicy 描述 hook 失败时的处理策略。
type FailurePolicy string

const (
	// FailurePolicyFailOpen 表示失败放行并继续后续 hook。
	FailurePolicyFailOpen FailurePolicy = "fail_open"
	// FailurePolicyFailClosed 表示失败即阻断执行。
	FailurePolicyFailClosed FailurePolicy = "fail_closed"
)

// HookHandler 定义 hook 的函数处理签名。
type HookHandler func(ctx context.Context, input HookContext) HookResult

// HookSpec 描述一个可注册的 hook 定义。
type HookSpec struct {
	ID            string
	Point         HookPoint
	Scope         HookScope
	Kind          HookKind
	Mode          HookMode
	Priority      int
	Timeout       time.Duration
	FailurePolicy FailurePolicy
	Handler       HookHandler
}

// normalizeAndValidate 将 HookSpec 归一化并校验当前阶段可用字段。
func (s HookSpec) normalizeAndValidate() (HookSpec, error) {
	s.ID = strings.TrimSpace(s.ID)
	s.Point = HookPoint(strings.TrimSpace(string(s.Point)))
	if s.ID == "" {
		return HookSpec{}, wrapInvalidSpec("id is required")
	}
	if s.Point == "" {
		return HookSpec{}, wrapInvalidSpec("point is required")
	}
	if !isSupportedHookPoint(s.Point) {
		return HookSpec{}, wrapInvalidSpec("point %q is not supported in P0", s.Point)
	}
	if s.Handler == nil {
		return HookSpec{}, wrapInvalidSpec("handler is required")
	}
	if s.Scope == "" {
		s.Scope = HookScopeInternal
	}
	switch s.Scope {
	case HookScopeInternal, HookScopeUser:
	default:
		return HookSpec{}, wrapInvalidSpec("scope %q is not supported", s.Scope)
	}
	if s.Kind == "" {
		s.Kind = HookKindFunction
	}
	if s.Kind != HookKindFunction {
		return HookSpec{}, wrapInvalidSpec("kind %q is not supported in P0", s.Kind)
	}
	if s.Mode == "" {
		s.Mode = HookModeSync
	}
	if s.Mode != HookModeSync {
		return HookSpec{}, wrapInvalidSpec("mode %q is not supported in P0", s.Mode)
	}
	if s.FailurePolicy == "" {
		s.FailurePolicy = FailurePolicyFailOpen
	}
	switch s.FailurePolicy {
	case FailurePolicyFailOpen, FailurePolicyFailClosed:
	default:
		return HookSpec{}, wrapInvalidSpec("failure_policy %q is invalid", s.FailurePolicy)
	}
	return s, nil
}

func isSupportedHookPoint(point HookPoint) bool {
	switch point {
	case HookPointBeforeToolCall, HookPointAfterToolResult, HookPointBeforeCompletionDecision:
		return true
	default:
		return false
	}
}
