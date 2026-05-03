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
	// HookPointBeforePermissionDecision 表示权限决策前挂点。
	HookPointBeforePermissionDecision HookPoint = "before_permission_decision"
	// HookPointAfterToolFailure 表示工具失败后挂点。
	HookPointAfterToolFailure HookPoint = "after_tool_failure"
	// HookPointSessionStart 表示会话启动挂点。
	HookPointSessionStart HookPoint = "session_start"
	// HookPointSessionEnd 表示会话结束挂点。
	HookPointSessionEnd HookPoint = "session_end"
	// HookPointUserPromptSubmit 表示用户输入提交前挂点。
	HookPointUserPromptSubmit HookPoint = "user_prompt_submit"
	// HookPointPreCompact 表示 compact 前挂点。
	HookPointPreCompact HookPoint = "pre_compact"
	// HookPointPostCompact 表示 compact 后挂点。
	HookPointPostCompact HookPoint = "post_compact"
	// HookPointSubAgentStart 表示子代理启动前挂点。
	HookPointSubAgentStart HookPoint = "subagent_start"
	// HookPointSubAgentStop 表示子代理结束后挂点。
	HookPointSubAgentStop HookPoint = "subagent_stop"
)

// HookPointCapability 描述每个 hook 点位允许的能力。
type HookPointCapability struct {
	CanBlock       bool
	CanAnnotate    bool
	CanUpdateInput bool
	UserAllowed    bool
}

var hookPointCapabilities = map[HookPoint]HookPointCapability{
	HookPointBeforeToolCall:           {CanBlock: true, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointAfterToolResult:          {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointBeforeCompletionDecision: {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointBeforePermissionDecision: {CanBlock: true, CanAnnotate: true, CanUpdateInput: false, UserAllowed: false},
	HookPointAfterToolFailure:         {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointSessionStart:             {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointSessionEnd:               {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointUserPromptSubmit:         {CanBlock: true, CanAnnotate: true, CanUpdateInput: true, UserAllowed: true},
	HookPointPreCompact:               {CanBlock: true, CanAnnotate: true, CanUpdateInput: false, UserAllowed: false},
	HookPointPostCompact:              {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
	HookPointSubAgentStart:            {CanBlock: true, CanAnnotate: true, CanUpdateInput: false, UserAllowed: false},
	HookPointSubAgentStop:             {CanBlock: false, CanAnnotate: true, CanUpdateInput: false, UserAllowed: true},
}

// HookScope 描述 hook 的权限/上下文裁剪等级。
type HookScope string

const (
	// HookScopeInternal 表示 runtime 内部 hook。
	HookScopeInternal HookScope = "internal"
	// HookScopeUser 表示用户配置 hook（P2 预留）。
	HookScopeUser HookScope = "user"
	// HookScopeRepo 表示仓库配置 hook（P3 预留）。
	HookScopeRepo HookScope = "repo"
)

// HookSource 描述 hook 的配置来源。
type HookSource string

const (
	// HookSourceInternal 表示运行时内部注册来源。
	HookSourceInternal HookSource = "internal"
	// HookSourceUser 表示全局用户配置来源。
	HookSourceUser HookSource = "user"
	// HookSourceRepo 表示仓库级配置来源。
	HookSourceRepo HookSource = "repo"
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
	Source        HookSource
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
		return HookSpec{}, wrapInvalidSpec("point %q is not supported", s.Point)
	}
	if s.Handler == nil {
		return HookSpec{}, wrapInvalidSpec("handler is required")
	}
	if s.Scope == "" {
		s.Scope = HookScopeInternal
	}
	switch s.Scope {
	case HookScopeInternal, HookScopeUser, HookScopeRepo:
	default:
		return HookSpec{}, wrapInvalidSpec("scope %q is not supported", s.Scope)
	}
	if s.Source == "" {
		s.Source = HookSource(strings.TrimSpace(string(s.Scope)))
	}
	switch s.Source {
	case HookSourceInternal, HookSourceUser, HookSourceRepo:
	default:
		return HookSpec{}, wrapInvalidSpec("source %q is not supported", s.Source)
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
	switch s.Mode {
	case HookModeSync, HookModeAsync, HookModeAsyncRewake:
	default:
		return HookSpec{}, wrapInvalidSpec("mode %q is not supported in current stage", s.Mode)
	}
	if (s.Scope == HookScopeUser || s.Scope == HookScopeRepo) && s.Mode != HookModeSync {
		return HookSpec{}, wrapInvalidSpec("scope %q only supports sync mode", s.Scope)
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
	_, ok := hookPointCapabilities[point]
	return ok
}

// HookPointCapabilities 返回指定点位能力描述和存在标记。
func HookPointCapabilities(point HookPoint) (HookPointCapability, bool) {
	capability, ok := hookPointCapabilities[point]
	return capability, ok
}
