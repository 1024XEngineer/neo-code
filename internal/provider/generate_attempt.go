package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"time"

	providertypes "neo-code/internal/provider/types"
)

var (
	// ErrGenerateStartTimeout 标记生成请求在首包前超时。
	ErrGenerateStartTimeout = errors.New("provider: generate start timeout")
	// ErrGenerateIdleTimeout 标记生成请求在首包后空闲超时。
	ErrGenerateIdleTimeout = errors.New("provider: generate idle timeout")
)

type generateAttemptPhase uint32

const (
	generateAttemptPhaseWaitingFirstPayload generateAttemptPhase = iota
	generateAttemptPhaseStreaming
	generateAttemptPhaseCompleted
)

type generateAttemptRunner struct {
	cfg          RuntimeConfig
	retryBackoff func(attempt int) time.Duration
	retryWait    func(ctx context.Context, wait time.Duration) error
}

type generateAttemptRunFunc func(ctx context.Context, events chan<- providertypes.StreamEvent) error

type generateAttemptResult struct {
	payloadStarted bool
	err            error
	retryable      bool
}

// RunGenerateWithRetry 以统一的首包/空闲超时语义执行 provider 生成请求。
func RunGenerateWithRetry(
	ctx context.Context,
	cfg RuntimeConfig,
	events chan<- providertypes.StreamEvent,
	run generateAttemptRunFunc,
) error {
	runner := generateAttemptRunner{cfg: cfg}
	return runner.run(ctx, events, run)
}

// RunGenerateWithRetryUsing 使用可注入的等待策略执行统一生成 runner，供测试场景稳定控制重试节奏。
func RunGenerateWithRetryUsing(
	ctx context.Context,
	cfg RuntimeConfig,
	events chan<- providertypes.StreamEvent,
	retryBackoff func(attempt int) time.Duration,
	retryWait func(ctx context.Context, wait time.Duration) error,
	run generateAttemptRunFunc,
) error {
	runner := generateAttemptRunner{
		cfg:          cfg,
		retryBackoff: retryBackoff,
		retryWait:    retryWait,
	}
	return runner.run(ctx, events, run)
}

// run 执行统一生成 runner 的外层重试循环。
func (r generateAttemptRunner) run(
	ctx context.Context,
	events chan<- providertypes.StreamEvent,
	run generateAttemptRunFunc,
) error {
	maxRetries := r.cfg.ResolvedGenerateMaxRetries()
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := generateRetryBackoff(attempt)
			if r.retryBackoff != nil {
				wait = r.retryBackoff(attempt)
			}
			waitFn := waitForRetry
			if r.retryWait != nil {
				waitFn = r.retryWait
			}
			if err := waitFn(ctx, wait); err != nil {
				return err
			}
		}

		result := r.runOnce(ctx, events, run)
		if result.err == nil {
			return nil
		}
		lastErr = result.err
		if result.payloadStarted || !result.retryable {
			return result.err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return lastErr
}

// runOnce 执行一次生成尝试，并基于统一观察器产出重试决策。
func (r generateAttemptRunner) runOnce(
	ctx context.Context,
	events chan<- providertypes.StreamEvent,
	run generateAttemptRunFunc,
) generateAttemptResult {
	attemptCtx, cancelAttempt := context.WithCancelCause(ctx)
	defer cancelAttempt(nil)

	proxyEvents := make(chan providertypes.StreamEvent, 32)
	phase := &atomic.Uint32{}
	forwardDone := make(chan error, 1)
	go func() {
		forwardDone <- forwardAttemptEvents(
			attemptCtx,
			proxyEvents,
			events,
			phase,
			r.cfg.ResolvedGenerateStartTimeout(),
			r.cfg.ResolvedGenerateIdleTimeout(),
			cancelAttempt,
		)
	}()

	runErr := run(attemptCtx, proxyEvents)
	close(proxyEvents)
	forwardErr := <-forwardDone

	phaseValue := generateAttemptPhase(phase.Load())
	if phaseValue == generateAttemptPhaseCompleted {
		return generateAttemptResult{}
	}

	if ctx.Err() != nil {
		return generateAttemptResult{
			payloadStarted: phaseValue == generateAttemptPhaseStreaming,
			err:            ctx.Err(),
			retryable:      false,
		}
	}
	if forwardErr != nil && runErr == nil {
		runErr = forwardErr
	}

	if cause := context.Cause(attemptCtx); cause != nil && !errors.Is(cause, ctx.Err()) {
		if runErr == nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			runErr = cause
		}
	}

	if runErr == nil {
		return generateAttemptResult{payloadStarted: phaseValue == generateAttemptPhaseStreaming}
	}
	payloadStarted := phaseValue == generateAttemptPhaseStreaming
	return generateAttemptResult{
		payloadStarted: payloadStarted,
		err:            runErr,
		retryable:      !payloadStarted && isRetryableAttemptError(runErr),
	}
}

// forwardAttemptEvents 在转发事件时统一维护首包与空闲超时观察器。
func forwardAttemptEvents(
	ctx context.Context,
	source <-chan providertypes.StreamEvent,
	target chan<- providertypes.StreamEvent,
	phase *atomic.Uint32,
	startTimeout time.Duration,
	idleTimeout time.Duration,
	cancel context.CancelCauseFunc,
) error {
	startTimer := time.NewTimer(startTimeout)
	defer startTimer.Stop()
	ctxDone := ctx.Done()

	var idleTimer *time.Timer
	var idleTimerC <-chan time.Time
	defer func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
	}()

	draining := false

	for {
		select {
		case <-ctxDone:
			draining = true
			ctxDone = nil
			stopTimer(startTimer)
			if idleTimer != nil {
				stopTimer(idleTimer)
				idleTimerC = nil
			}
		case <-startTimer.C:
			if phase.Load() == uint32(generateAttemptPhaseWaitingFirstPayload) {
				cancel(newGenerateStartTimeoutError(startTimeout))
			}
		case <-idleTimerC:
			if phase.Load() == uint32(generateAttemptPhaseStreaming) {
				cancel(newGenerateIdleTimeoutError(idleTimeout))
			}
		case event, ok := <-source:
			if !ok {
				return nil
			}
			phaseValue := updateGenerateAttemptPhase(event, phase)
			if phaseValue == generateAttemptPhaseStreaming {
				stopTimer(startTimer)
				if idleTimer == nil {
					idleTimer = time.NewTimer(idleTimeout)
					idleTimerC = idleTimer.C
				} else {
					resetTimer(idleTimer, idleTimeout)
				}
			}
			if phaseValue == generateAttemptPhaseCompleted {
				stopTimer(startTimer)
				if idleTimer != nil {
					stopTimer(idleTimer)
					idleTimerC = nil
				}
			}
			if draining {
				continue
			}
			if err := emitStreamEvent(ctx, target, event); err != nil {
				draining = true
				ctxDone = nil
				stopTimer(startTimer)
				if idleTimer != nil {
					stopTimer(idleTimer)
					idleTimerC = nil
				}
				continue
			}
			if phaseValue == generateAttemptPhaseCompleted {
				draining = true
				ctxDone = nil
				cancel(nil)
			}
		}
	}
}

// updateGenerateAttemptPhase 统一维护生成尝试的阶段流转，确保首包、完成态和重试边界只在公共层定义一次。
func updateGenerateAttemptPhase(
	event providertypes.StreamEvent,
	phase *atomic.Uint32,
) generateAttemptPhase {
	current := generateAttemptPhase(phase.Load())
	if current == generateAttemptPhaseCompleted {
		return current
	}
	if event.Type == providertypes.StreamEventMessageDone {
		phase.Store(uint32(generateAttemptPhaseCompleted))
		return generateAttemptPhaseCompleted
	}
	if IsEffectiveGeneratePayloadEvent(event) {
		if phase.CompareAndSwap(
			uint32(generateAttemptPhaseWaitingFirstPayload),
			uint32(generateAttemptPhaseStreaming),
		) {
			return generateAttemptPhaseStreaming
		}
		return generateAttemptPhase(phase.Load())
	}
	return current
}

// IsEffectiveGeneratePayloadEvent 判断事件是否属于“流已开始”的有效 payload。
func IsEffectiveGeneratePayloadEvent(event providertypes.StreamEvent) bool {
	switch event.Type {
	case providertypes.StreamEventTextDelta, providertypes.StreamEventToolCallStart, providertypes.StreamEventToolCallDelta:
		return true
	default:
		return false
	}
}

// isRetryableAttemptError 统一判断一次尝试失败后是否仍可在首包前继续重试。
func isRetryableAttemptError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStreamInterrupted) || errors.Is(err, ErrGenerateStartTimeout) {
		return true
	}
	var providerErr *ProviderError
	return errors.As(err, &providerErr) && providerErr.Retryable
}

// generateRetryBackoff 计算生成链路统一退避时间，避免三家 provider 各自维护一套重试等待规则。
func generateRetryBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	wait := DefaultGenerateRetryBaseWait << (attempt - 1)
	jitter := float64(wait) * (0.5 + rand.Float64())
	wait = time.Duration(jitter)
	if wait > DefaultGenerateRetryMaxWait {
		wait = DefaultGenerateRetryMaxWait
	}
	return wait
}

// waitForRetry 在统一重试窗口内等待，同时尊重上层上下文取消。
func waitForRetry(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// newGenerateStartTimeoutError 构造统一的首包超时错误，供 attempt runner 与上层错误判断复用。
func newGenerateStartTimeoutError(timeout time.Duration) error {
	return fmt.Errorf(
		"%w: %w",
		ErrGenerateStartTimeout,
		NewTimeoutProviderError(fmt.Sprintf("generate start timeout after %s", timeout)),
	)
}

// newGenerateIdleTimeoutError 构造统一的流空闲超时错误，供 attempt runner 与上层错误判断复用。
func newGenerateIdleTimeoutError(timeout time.Duration) error {
	return fmt.Errorf(
		"%w: %w",
		ErrGenerateIdleTimeout,
		NewTimeoutProviderError(fmt.Sprintf("generate idle timeout after %s", timeout)),
	)
}

// stopTimer 安全停止计时器，并清理可能残留的触发信号。
func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

// resetTimer 在统一观察器中安全重置计时器，避免复用时遗留旧信号。
func resetTimer(timer *time.Timer, wait time.Duration) {
	if timer == nil {
		return
	}
	stopTimer(timer)
	timer.Reset(wait)
}
