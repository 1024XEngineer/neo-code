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
