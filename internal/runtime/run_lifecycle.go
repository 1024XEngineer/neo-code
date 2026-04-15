package runtime

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
)

// ErrMaxLoopReached 表示达到配置的最大推理轮数，用于停止原因分类与测试断言。
var ErrMaxLoopReached = errors.New("runtime: max loop reached")

// ErrNoProgressStreakLimit 表示循环内连续多次未取得进展，触发死循环拦截。
var ErrNoProgressStreakLimit = errors.New("runtime: no progress streak limit reached")

// ErrRepeatCycleLimit 表示连续多次重复调用相同的工具且参数相同，触发死循环拦截。
var ErrRepeatCycleLimit = errors.New("runtime: repeat cycle limit reached")

// transitionRunPhase 在阶段变化时发出 phase_changed 并更新 runState。
func (s *Service) transitionRunPhase(ctx context.Context, state *runState, next controlplane.Phase) {
	if state == nil || state.phase == next {
		return
	}
	from := state.phase
	state.phase = next
	_ = s.emitRunScoped(ctx, EventPhaseChanged, state, PhaseChangedPayload{
		From: string(from),
		To:   string(next),
	})
}

// emitRunTermination 在 Run 退出时决议并发出唯一 stop_reason_decided 终止事实事件。
func (s *Service) emitRunTermination(ctx context.Context, input UserInput, state *runState, err error) {
	runID := strings.TrimSpace(input.RunID)
	sessionID := strings.TrimSpace(input.SessionID)
	if state != nil {
		if strings.TrimSpace(state.runID) != "" {
			runID = state.runID
		}
		if strings.TrimSpace(state.session.ID) != "" {
			sessionID = state.session.ID
		}
		if state.stopEmitted {
			return
		}
		state.stopEmitted = true
	}

	in := controlplane.StopInput{Success: err == nil}
	if err != nil {
		in.Success = false
		switch {
		case errors.Is(err, context.Canceled):
			in.ContextCanceled = true
		case errors.Is(err, ErrMaxLoopReached):
			in.MaxLoopsReached = true
			in.RunError = err
		default:
			in.RunError = err
		}
	}

	reason, detail := controlplane.DecideStopReason(in)
	turn := turnUnspecified
	phase := ""
	if state != nil {
		turn = state.turn
		if state.phase != "" {
			phase = string(state.phase)
		}
	}
	emitCtx, cancel := stopReasonEmitContext(ctx)
	defer cancel()
	_ = s.emitWithEnvelope(emitCtx, RuntimeEvent{
		Type:           EventStopReasonDecided,
		RunID:          runID,
		SessionID:      sessionID,
		Turn:           turn,
		Phase:          phase,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        StopReasonDecidedPayload{Reason: reason, Detail: detail},
	})
}

// stopReasonEmitContext 为终止事件提供可用发送窗口，避免继承已取消上下文导致事实事件丢失。
func stopReasonEmitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() == nil {
		return context.WithTimeout(ctx, terminationEventEmitTimeout)
	}
	return context.WithTimeout(context.Background(), terminationEventEmitTimeout)
}

// handleRunError 负责记录 provider 错误日志并原样返回错误；终止类事件由 Run 出口统一发出。
func (s *Service) handleRunError(ctx context.Context, runID string, sessionID string, err error) error {
	_ = ctx
	_ = runID
	_ = sessionID
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}

	return err
}

// isRetryableProviderError 判断 provider 错误是否允许 runtime 级重试。
func isRetryableProviderError(err error) bool {
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	return providerErr.Retryable
}

// providerRetryBackoff 计算 runtime 级 provider 重试等待时间。
func providerRetryBackoff(attempt int) time.Duration {
	wait := providerRetryBaseWait << (attempt - 1)
	jitter := float64(wait) * (0.5 + rand.Float64())
	wait = time.Duration(jitter)
	if wait > providerRetryMaxWait {
		wait = providerRetryMaxWait
	}
	return wait
}

// cloneMessages 深拷贝消息切片，避免后台调度读取到后续运行态修改。
func cloneMessages(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]providertypes.Message, 0, len(messages))
	for _, message := range messages {
		next := message
		if len(message.ToolCalls) > 0 {
			next.ToolCalls = append([]providertypes.ToolCall(nil), message.ToolCalls...)
		}
		if len(message.ToolMetadata) > 0 {
			next.ToolMetadata = make(map[string]string, len(message.ToolMetadata))
			for key, value := range message.ToolMetadata {
				next.ToolMetadata[key] = value
			}
		}
		cloned = append(cloned, next)
	}
	return cloned
}
