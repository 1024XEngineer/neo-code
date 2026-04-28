package hooks

import (
	"context"
	"fmt"
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
		result := e.runOne(ctx, spec, input.Clone())
		output.Results = append(output.Results, result)

		if result.Status == HookResultBlock {
			output.Blocked = true
			output.BlockedBy = spec.ID
			break
		}
		if result.Status == HookResultFailed && spec.FailurePolicy == FailurePolicyFailClosed {
			output.Blocked = true
			output.BlockedBy = spec.ID
			break
		}
	}
	return output
}

func (e *Executor) runOne(ctx context.Context, spec HookSpec, input HookContext) HookResult {
	startedAt := e.now()
	e.emitBestEffort(ctx, HookEvent{
		Type:      HookEventStarted,
		HookID:    spec.ID,
		Point:     spec.Point,
		Scope:     spec.Scope,
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
	if result.DurationMS <= 0 {
		result.DurationMS = durationMS
	}
	if result.HookID == "" {
		result.HookID = spec.ID
	}
	if result.Point == "" {
		result.Point = spec.Point
	}

	switch result.Status {
	case HookResultPass, HookResultBlock:
		e.emitBestEffort(ctx, HookEvent{
			Type:       HookEventFinished,
			HookID:     spec.ID,
			Point:      spec.Point,
			Scope:      spec.Scope,
			Kind:       spec.Kind,
			Mode:       spec.Mode,
			Status:     result.Status,
			StartedAt:  result.StartedAt,
			DurationMS: result.DurationMS,
		})
	case HookResultFailed:
		e.emitBestEffort(ctx, HookEvent{
			Type:       HookEventFailed,
			HookID:     spec.ID,
			Point:      spec.Point,
			Scope:      spec.Scope,
			Kind:       spec.Kind,
			Mode:       spec.Mode,
			Status:     result.Status,
			StartedAt:  result.StartedAt,
			DurationMS: result.DurationMS,
			Error:      result.Error,
		})
	default:
		invalidStatus := result.Status
		result.Status = HookResultFailed
		if result.Error == "" {
			result.Error = fmt.Sprintf("hook returned invalid status %q", invalidStatus)
		}
		if result.Message == "" {
			result.Message = result.Error
		}
		e.emitBestEffort(ctx, HookEvent{
			Type:       HookEventFailed,
			HookID:     spec.ID,
			Point:      spec.Point,
			Scope:      spec.Scope,
			Kind:       spec.Kind,
			Mode:       spec.Mode,
			Status:     result.Status,
			StartedAt:  result.StartedAt,
			DurationMS: result.DurationMS,
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
				Status:    HookResultFailed,
				Message:   err,
				Error:     err,
				StartedAt: startedAt,
			}
		}
		outcome.result.HookID = spec.ID
		outcome.result.Point = spec.Point
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
