package hooks

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// DefaultHookTimeout 是未显式配置 timeout 时的默认执行超时。
	DefaultHookTimeout = 2 * time.Second
	// DefaultMaxInFlightHooks 是默认同时在执行中的 hook 上限，用于防止超时后执行堆积。
	DefaultMaxInFlightHooks = int32(128)
)

// Executor 负责按点位同步执行 hook 快照。
type Executor struct {
	registry       *Registry
	emitter        EventEmitter
	defaultTimeout time.Duration
	maxInFlight    int32
	inFlight       atomic.Int32
	now            func() time.Time
}

// NewExecutor 创建一个同步 hook 执行器。
func NewExecutor(registry *Registry, emitter EventEmitter, defaultTimeout time.Duration) *Executor {
	if registry == nil {
		registry = NewRegistry()
	}
	if defaultTimeout <= 0 {
		defaultTimeout = DefaultHookTimeout
	}
	return &Executor{
		registry:       registry,
		emitter:        emitter,
		defaultTimeout: defaultTimeout,
		maxInFlight:    DefaultMaxInFlightHooks,
		now:            time.Now,
	}
}

// Run 在指定挂载点执行 hook 快照并返回聚合结果。
func (e *Executor) Run(ctx context.Context, point HookPoint, input HookContext) RunOutput {
	if e == nil || e.registry == nil {
		return RunOutput{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	specs := e.registry.Resolve(point)
	if len(specs) == 0 {
		return RunOutput{}
	}

	output := RunOutput{
		Results: make([]HookResult, 0, len(specs)),
	}
	for _, spec := range specs {
		hookInput := input.Clone()
		if spec.Scope == HookScopeUser || spec.Scope == HookScopeRepo {
			hookInput = sanitizeUserHookContext(hookInput)
		}
		result := e.runOne(ctx, spec, hookInput)
		result = normalizeHookResultByCapability(spec.Point, result)
		output.Results = append(output.Results, result)

		if result.Status == HookResultBlock {
			output.Blocked = true
			output.BlockedBy = spec.ID
			output.BlockedSource = spec.Source
			break
		}
		if result.Status == HookResultFailed && spec.FailurePolicy == FailurePolicyFailClosed {
			output.Blocked = true
			output.BlockedBy = spec.ID
			output.BlockedSource = spec.Source
			break
		}
	}
	return output
}

// normalizeHookResultByCapability 根据 HookPoint 能力矩阵约束单条结果。
func normalizeHookResultByCapability(point HookPoint, result HookResult) HookResult {
	capability, ok := HookPointCapabilities(point)
	if !ok {
		return result
	}
	if result.Status == HookResultBlock && !capability.CanBlock {
		result.Status = HookResultPass
		if strings.TrimSpace(result.Message) == "" {
			result.Message = "hook block downgraded: point does not allow blocking"
		}
		if strings.TrimSpace(result.Error) == "" {
			result.Error = "hook block downgraded"
		}
	}
	return result
}

func (e *Executor) runOne(ctx context.Context, spec HookSpec, input HookContext) HookResult {
	startedAt := e.now()
	e.emitBestEffort(ctx, HookEvent{
		Type:      HookEventStarted,
		HookID:    spec.ID,
		Point:     spec.Point,
		Scope:     spec.Scope,
		Source:    spec.Source,
		Kind:      spec.Kind,
		Mode:      spec.Mode,
		StartedAt: startedAt,
	})

	hookCtx, cancel := e.withHookTimeout(ctx, spec.Timeout)
	defer cancel()

	result := e.callHandler(hookCtx, spec, input, startedAt)
	durationMS := e.now().Sub(startedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = startedAt
	}
	if result.Scope == "" {
		result.Scope = spec.Scope
	}
	if result.Source == "" {
		result.Source = spec.Source
	}
	if result.DurationMS <= 0 {
		result.DurationMS = durationMS
	}

	switch result.Status {
	case HookResultPass, HookResultBlock:
		e.emitBestEffort(ctx, HookEvent{
			Type:       HookEventFinished,
			HookID:     spec.ID,
			Point:      spec.Point,
			Scope:      spec.Scope,
			Source:     spec.Source,
			Kind:       spec.Kind,
			Mode:       spec.Mode,
			Status:     result.Status,
			StartedAt:  result.StartedAt,
			DurationMS: result.DurationMS,
			Message:    strings.TrimSpace(result.Message),
		})
	case HookResultFailed:
		e.emitBestEffort(ctx, HookEvent{
			Type:       HookEventFailed,
			HookID:     spec.ID,
			Point:      spec.Point,
			Scope:      spec.Scope,
			Source:     spec.Source,
			Kind:       spec.Kind,
			Mode:       spec.Mode,
			Status:     result.Status,
			StartedAt:  result.StartedAt,
			DurationMS: result.DurationMS,
			Message:    strings.TrimSpace(result.Message),
			Error:      result.Error,
		})
	}
	return result
}

func (e *Executor) withHookTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	effective := timeout
	if effective <= 0 {
		effective = e.defaultTimeout
	}
	if effective <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, effective)
}

func (e *Executor) callHandler(
	ctx context.Context,
	spec HookSpec,
	input HookContext,
	startedAt time.Time,
) HookResult {
	if !e.tryAcquireSlot() {
		err := "hook executor is saturated by in-flight handlers"
		return HookResult{
			HookID:    spec.ID,
			Point:     spec.Point,
			Scope:     spec.Scope,
			Source:    spec.Source,
			Status:    HookResultFailed,
			Message:   err,
			Error:     err,
			StartedAt: startedAt,
		}
	}

	type invokeResult struct {
		result HookResult
		panicV any
	}
	resultCh := make(chan invokeResult, 1)

	go func() {
		defer e.releaseSlot()
		out := invokeResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				out.panicV = recovered
			}
			resultCh <- out
		}()
		out.result = spec.Handler(ctx, input)
	}()

	select {
	case <-ctx.Done():
		err := "hook execution canceled"
		if ctx.Err() == context.DeadlineExceeded {
			err = "hook execution timed out"
		}
		return HookResult{
			HookID:    spec.ID,
			Point:     spec.Point,
			Scope:     spec.Scope,
			Source:    spec.Source,
			Status:    HookResultFailed,
			Message:   err,
			Error:     err,
			StartedAt: startedAt,
		}
	case outcome := <-resultCh:
		if outcome.panicV != nil {
			err := fmt.Sprintf("hook panicked: %v", outcome.panicV)
			return HookResult{
				HookID:    spec.ID,
				Point:     spec.Point,
				Scope:     spec.Scope,
				Source:    spec.Source,
				Status:    HookResultFailed,
				Message:   err,
				Error:     err,
				StartedAt: startedAt,
			}
		}
		outcome.result.HookID = spec.ID
		outcome.result.Point = spec.Point
		outcome.result.Scope = spec.Scope
		outcome.result.Source = spec.Source
		if outcome.result.Status == "" {
			outcome.result.Status = HookResultPass
		}
		if outcome.result.Status != HookResultPass &&
			outcome.result.Status != HookResultBlock &&
			outcome.result.Status != HookResultFailed {
			err := fmt.Sprintf("hook returned invalid status %q", outcome.result.Status)
			return HookResult{
				HookID:    spec.ID,
				Point:     spec.Point,
				Scope:     spec.Scope,
				Source:    spec.Source,
				Status:    HookResultFailed,
				Message:   err,
				Error:     err,
				StartedAt: startedAt,
			}
		}
		if outcome.result.Status == HookResultFailed && outcome.result.Error == "" {
			if outcome.result.Message != "" {
				outcome.result.Error = outcome.result.Message
			} else {
				outcome.result.Error = "hook returned failed status"
				outcome.result.Message = "hook returned failed status"
			}
		}
		return outcome.result
	}
}

func sanitizeUserHookContext(input HookContext) HookContext {
	sanitized := HookContext{
		RunID:     strings.TrimSpace(input.RunID),
		SessionID: strings.TrimSpace(input.SessionID),
	}
	if len(input.Metadata) == 0 {
		return sanitized
	}
	allowedMetadataKeys := map[string]struct{}{
		"point":                   {},
		"tool_call_id":            {},
		"tool_name":               {},
		"is_error":                {},
		"error_class":             {},
		"result_content_preview":  {},
		"result_metadata_present": {},
		"execution_error":         {},
		"workdir":                 {},
		"session_id":              {},
		"run_id":                  {},
		"task_id":                 {},
		"role":                    {},
		"workspace":               {},
		"trigger":                 {},
		"state":                   {},
		"stop_reason":             {},
		"step_count":              {},
		"error":                   {},
		"trigger_mode":            {},
		"applied":                 {},
		"decision":                {},
		"reason":                  {},
		"rule_id":                 {},
		"completion_passed":       {},
		"has_tool_calls":          {},
		"assistant_role":          {},
		"detail":                  {},
	}
	for key, value := range input.Metadata {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, ok := allowedMetadataKeys[normalizedKey]; !ok {
			continue
		}
		if sanitized.Metadata == nil {
			sanitized.Metadata = make(map[string]any, len(input.Metadata))
		}
		sanitized.Metadata[normalizedKey] = cloneMetadataValue(value)
	}
	return sanitized
}

func (e *Executor) tryAcquireSlot() bool {
	limit := e.maxInFlight
	if limit <= 0 {
		return true
	}
	for {
		current := e.inFlight.Load()
		if current >= limit {
			return false
		}
		if e.inFlight.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (e *Executor) releaseSlot() {
	if e == nil || e.maxInFlight <= 0 {
		return
	}
	e.inFlight.Add(-1)
}

func (e *Executor) emitBestEffort(ctx context.Context, event HookEvent) {
	if e == nil || e.emitter == nil {
		return
	}
	_ = e.emitter.EmitHookEvent(ctx, event)
}
