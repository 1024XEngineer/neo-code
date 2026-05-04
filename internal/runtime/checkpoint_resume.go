package runtime

import (
	"context"
	"log"
	"time"

	agentsession "neo-code/internal/session"
)

// updateResumeCheckpoint 在 phase 转换时写入或更新 ResumeCheckpoint。
// 失败仅 log，不阻塞主流程。
func (s *Service) updateResumeCheckpoint(ctx context.Context, state *runState, phase string, completionState string) {
	if s.checkpointStore == nil {
		return
	}

	state.mu.Lock()
	session := state.session
	runID := state.runID
	turn := state.turn
	state.mu.Unlock()

	rc := agentsession.ResumeCheckpoint{
		ID:              agentsession.NewID("rc"),
		WorkspaceKey:    agentsession.WorkspacePathKey(session.Workdir),
		RunID:           runID,
		SessionID:       session.ID,
		Turn:            turn,
		Phase:           phase,
		CompletionState: completionState,
		UpdatedAt:       time.Now(),
	}

	if err := s.checkpointStore.SetResumeCheckpoint(ctx, rc); err != nil {
		log.Printf("checkpoint: set resume checkpoint for %s: %v", session.ID, err)
	}
}
