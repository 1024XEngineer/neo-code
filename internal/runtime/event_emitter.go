package runtime

import (
	"context"
	"time"

	"neo-code/internal/runtime/controlplane"
)

const turnUnspecified = -1

// emit 将 runtime 事件投递到事件通道，并在通道阻塞且上下文取消时返回错误。
func (s *Service) emit(ctx context.Context, kind EventType, runID string, sessionID string, payload any) error {
	return s.emitWithEnvelope(ctx, RuntimeEvent{
		Type:           kind,
		RunID:          runID,
		SessionID:      sessionID,
		Turn:           turnUnspecified,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        payload,
	})
}

// emitRunScoped 携带当前 run 的 turn/phase 元数据发出事件。事件投递为 best-effort，不返回错误。
func (s *Service) emitRunScoped(ctx context.Context, kind EventType, state *runState, payload any) {
	if state == nil {
		_ = s.emit(ctx, kind, "", "", payload)
		return
	}
	phase := ""
	if state.lifecycle != "" {
		phase = string(state.lifecycle)
	}
	_ = s.emitWithEnvelope(ctx, RuntimeEvent{
		Type:           kind,
		RunID:          state.runID,
		SessionID:      state.session.ID,
		Turn:           state.turn,
		Phase:          phase,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        payload,
	})
}

func (s *Service) emitWithEnvelope(ctx context.Context, evt RuntimeEvent) error {
	if evt.PayloadVersion == 0 {
		evt.PayloadVersion = controlplane.PayloadVersion
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	if err := s.deliverEvent(ctx, evt); err != nil {
		return err
	}
	return nil
}

// emitRunScopedOptional 发送可观测增强事件；当事件通道拥堵时直接丢弃，避免阻塞主执行链路。
func (s *Service) emitRunScopedOptional(kind EventType, state *runState, payload any) {
	if s == nil || s.events == nil {
		return
	}
	runID := ""
	sessionID := ""
	turn := turnUnspecified
	phase := ""
	if state != nil {
		runID = state.runID
		sessionID = state.session.ID
		turn = state.turn
		if state.lifecycle != "" {
			phase = string(state.lifecycle)
		}
	}
	evt := RuntimeEvent{
		Type:           kind,
		RunID:          runID,
		SessionID:      sessionID,
		Turn:           turn,
		Phase:          phase,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        payload,
	}
	select {
	case s.events <- evt:
	default:
	}
}

// emitRunScopedPriority 发送关键状态事件；当队列满时淘汰一条旧事件后重试，尽量保证终态事件可见。
func (s *Service) emitRunScopedPriority(kind EventType, state *runState, payload any) {
	if s == nil || s.events == nil {
		return
	}
	runID := ""
	sessionID := ""
	turn := turnUnspecified
	phase := ""
	if state != nil {
		runID = state.runID
		sessionID = state.session.ID
		turn = state.turn
		if state.lifecycle != "" {
			phase = string(state.lifecycle)
		}
	}
	evt := RuntimeEvent{
		Type:           kind,
		RunID:          runID,
		SessionID:      sessionID,
		Turn:           turn,
		Phase:          phase,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        payload,
	}
	select {
	case s.events <- evt:
		return
	default:
	}
	select {
	case <-s.events:
	default:
	}
	select {
	case s.events <- evt:
	default:
	}
}

func (s *Service) deliverEvent(ctx context.Context, evt RuntimeEvent) error {
	select {
	case s.events <- evt:
		return nil
	default:
	}
	select {
	case s.events <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
