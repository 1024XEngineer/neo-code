package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
)

// setBaseRunState 更新主链生命周期状态，并触发有效运行态重计算。
func (s *Service) setBaseRunState(ctx context.Context, state *runState, next controlplane.RunState) error {
	if !isBaseLifecycleState(next) {
		return errors.New("runtime: invalid base lifecycle state")
	}
	state.mu.Lock()
	state.baseLifecycle = next
	state.mu.Unlock()
	return s.refreshEffectiveRunState(ctx, state)
}

// enterTemporaryRunState 增加临时治理态计数，并触发有效运行态重计算。
func (s *Service) enterTemporaryRunState(ctx context.Context, state *runState, temporary controlplane.RunState) error {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	switch temporary {
	case controlplane.RunStateWaitingPermission:
		state.waitingPermissionCount++
	case controlplane.RunStateCompacting:
		state.compactingCount++
	default:
		state.mu.Unlock()
		return errors.New("runtime: unsupported temporary lifecycle state")
	}
	state.mu.Unlock()
	return s.refreshEffectiveRunState(ctx, state)
}

// leaveTemporaryRunState 释放临时治理态计数，并触发有效运行态重计算。
func (s *Service) leaveTemporaryRunState(ctx context.Context, state *runState, temporary controlplane.RunState) error {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	switch temporary {
	case controlplane.RunStateWaitingPermission:
		if state.waitingPermissionCount > 0 {
			state.waitingPermissionCount--
		}
	case controlplane.RunStateCompacting:
		if state.compactingCount > 0 {
			state.compactingCount--
		}
	default:
		state.mu.Unlock()
		return errors.New("runtime: unsupported temporary lifecycle state")
	}
	state.mu.Unlock()
	return s.refreshEffectiveRunState(ctx, state)
}

// refreshEffectiveRunState 根据 base + 临时态覆盖层计算并发出统一 phase_changed 事件。
func (s *Service) refreshEffectiveRunState(ctx context.Context, state *runState) error {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	next := deriveEffectiveRunState(state)
	from := state.lifecycle
	if next == from {
		state.mu.Unlock()
		return nil
	}
	if err := controlplane.ValidateRunStateTransition(from, next); err != nil {
		state.mu.Unlock()
		return err
	}
	state.lifecycle = next
	state.mu.Unlock()

	s.emitRunScoped(ctx, EventPhaseChanged, state, PhaseChangedPayload{
		From: string(from),
		To:   string(next),
	})
	return nil
}

// deriveEffectiveRunState 统一推导当前有效运行态，临时治理态优先级高于 base 主链态。
func deriveEffectiveRunState(state *runState) controlplane.RunState {
	if state.waitingPermissionCount > 0 {
		return controlplane.RunStateWaitingPermission
	}
	if state.compactingCount > 0 {
		return controlplane.RunStateCompacting
	}
	if state.baseLifecycle != "" {
		return state.baseLifecycle
	}
	return state.lifecycle
}

// isBaseLifecycleState 判断状态是否属于主链 base lifecycle 集合。
func isBaseLifecycleState(state controlplane.RunState) bool {
	switch state {
	case controlplane.RunStatePlan, controlplane.RunStateExecute, controlplane.RunStateVerify, controlplane.RunStateStopped:
		return true
	default:
		return false
	}
}

// emitRunTermination 在 Run 退出时决议并发出唯一的 stop_reason_decided 事件。
func (s *Service) emitRunTermination(ctx context.Context, input UserInput, state *runState, err error) {
	runID := strings.TrimSpace(input.RunID)
	sessionID := strings.TrimSpace(input.SessionID)
	if state != nil {
		state.mu.Lock()
		if strings.TrimSpace(state.runID) != "" {
			runID = state.runID
		}
		if strings.TrimSpace(state.session.ID) != "" {
			sessionID = state.session.ID
		}
		if state.stopEmitted {
			state.mu.Unlock()
			return
		}
		state.stopEmitted = true
		state.baseLifecycle = controlplane.RunStateStopped
		state.lifecycle = controlplane.RunStateStopped
		state.waitingPermissionCount = 0
		state.compactingCount = 0
		state.mu.Unlock()
	}

	in := controlplane.StopInput{}
	if state != nil && state.terminalSet {
		in.PreDecidedReason = state.terminalStopReason
		in.PreDecidedDetail = state.terminalStopDetail
	} else if state != nil && state.maxTurnsReached {
		in.MaxTurnsReached = true
		in.MaxTurnsLimit = state.maxTurnsLimit
	} else if state != nil && state.budgetExceeded {
		in.BudgetExceeded = true
	} else if err != nil {
		var maxTurnErr maxTurnLimitError
		if errors.As(err, &maxTurnErr) {
			in.MaxTurnsReached = true
			in.MaxTurnsLimit = maxTurnErr.Limit()
		} else {
			switch {
			case errors.Is(err, context.Canceled):
				in.UserInterrupted = true
			default:
				in.FatalError = err
			}
		}
	} else {
		in.Completed = true
	}

	reason, detail := controlplane.DecideStopReason(in)
	turn := turnUnspecified
	phase := ""
	if state != nil {
		_ = s.runHookPoint(ctx, state, runtimehooks.HookPointSessionEnd, runtimehooks.HookContext{
			RunID:     strings.TrimSpace(runID),
			SessionID: strings.TrimSpace(sessionID),
			Metadata: map[string]any{
				"stop_reason": string(reason),
				"detail":      strings.TrimSpace(detail),
			},
		})
	}
	if state != nil {
		turn = state.turn
		if state.lifecycle != "" {
			phase = string(state.lifecycle)
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

// stopReasonEmitContext 为终止事件提供可用发送窗口，避免继承已取消上下文导致事件丢失。
func stopReasonEmitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() == nil {
		return context.WithTimeout(ctx, terminationEventEmitTimeout)
	}
	return context.WithTimeout(context.Background(), terminationEventEmitTimeout)
}

// handleRunError 统一转换 runtime 终止错误，保证取消语义收敛到同一路径。
func (s *Service) handleRunError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return err
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
