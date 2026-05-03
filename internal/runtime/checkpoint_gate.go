package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/checkpoint"
	agentsession "neo-code/internal/session"
)

// createPerTurnCheckpoint 在每轮 turn 开始时创建 checkpoint。
// shadowRepo 可用且有代码变更时做完整快照，否则仅做 session-only 快照。
// 失败时不阻塞执行，仅返回 error 由调用方发 warning event。
func (s *Service) createPerTurnCheckpoint(ctx context.Context, state *runState) error {
	if s.checkpointStore == nil {
		return nil
	}

	state.mu.Lock()
	session := state.session
	runID := state.runID
	state.mu.Unlock()

	// 降级模式：shadowRepo 不可用时创建 session-only checkpoint
	if s.shadowRepo == nil || !s.shadowRepo.IsAvailable() {
		return s.createDegradedCheckpoint(ctx, session, runID)
	}

	// 无代码变更时跳过代码快照，仅做 session-only checkpoint
	if !s.shadowRepo.HasCodeChanges(ctx) {
		return s.createDegradedCheckpoint(ctx, session, runID)
	}

	return s.createFullCheckpoint(ctx, session, runID, state)
}

// createFullCheckpoint 创建完整 checkpoint（代码快照 + 会话快照）。
func (s *Service) createFullCheckpoint(ctx context.Context, session agentsession.Session, runID string, state *runState) error {
	checkpointID := agentsession.NewID("checkpoint")
	ref := checkpoint.RefForCheckpoint(session.ID, checkpointID)
	commitMsg := fmt.Sprintf("per-turn checkpoint for session %s", session.ID)

	// Phase 1: shadow snapshot
	commitHash, err := s.shadowRepo.Snapshot(ctx, ref, commitMsg)
	if err != nil {
		return fmt.Errorf("checkpoint: shadow snapshot: %w", err)
	}

	// Phase 2: DB write
	head := session.HeadSnapshot()
	headJSON, err := json.Marshal(head)
	if err != nil {
		_ = s.shadowRepo.DeleteRef(ctx, ref)
		return fmt.Errorf("checkpoint: marshal head: %w", err)
	}
	messagesJSON, err := json.Marshal(session.Messages)
	if err != nil {
		_ = s.shadowRepo.DeleteRef(ctx, ref)
		return fmt.Errorf("checkpoint: marshal messages: %w", err)
	}

	effectiveWorkdir := strings.TrimSpace(session.Workdir)
	now := time.Now()

	record := agentsession.CheckpointRecord{
		CheckpointID:      checkpointID,
		WorkspaceKey:      agentsession.WorkspacePathKey(effectiveWorkdir),
		SessionID:         session.ID,
		RunID:             runID,
		Workdir:           effectiveWorkdir,
		CreatedAt:         now,
		Reason:            agentsession.CheckpointReasonPreWrite,
		CodeCheckpointRef: ref,
		Restorable:        true,
		Status:            agentsession.CheckpointStatusCreating,
	}

	sessionCP := agentsession.SessionCheckpoint{
		ID:           agentsession.NewID("sc"),
		SessionID:    session.ID,
		HeadJSON:     string(headJSON),
		MessagesJSON: string(messagesJSON),
		CreatedAt:    now,
	}

	input := checkpoint.CreateCheckpointInput{
		Record:    record,
		SessionCP: sessionCP,
	}

	saved, err := s.checkpointStore.CreateCheckpoint(ctx, input)
	if err != nil {
		_ = s.shadowRepo.DeleteRef(ctx, ref)
		return fmt.Errorf("checkpoint: db write: %w", err)
	}

	s.emitRunScoped(ctx, EventCheckpointCreated, state, CheckpointCreatedPayload{
		CheckpointID:         saved.CheckpointID,
		CodeCheckpointRef:    saved.CodeCheckpointRef,
		SessionCheckpointRef: saved.SessionCheckpointRef,
		CommitHash:           commitHash,
		Reason:               string(saved.Reason),
	})
	return nil
}

// createDegradedCheckpoint 创建 session-only checkpoint（无代码快照），用于 shadowRepo 不可用时。
func (s *Service) createDegradedCheckpoint(ctx context.Context, session agentsession.Session, runID string) error {
	checkpointID := agentsession.NewID("checkpoint")
	now := time.Now()

	head := session.HeadSnapshot()
	headJSON, err := json.Marshal(head)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal degraded head: %w", err)
	}
	messagesJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal degraded messages: %w", err)
	}

	record := agentsession.CheckpointRecord{
		CheckpointID: checkpointID,
		WorkspaceKey: agentsession.WorkspacePathKey(session.Workdir),
		SessionID:    session.ID,
		RunID:        runID,
		Workdir:      session.Workdir,
		CreatedAt:    now,
		Reason:       agentsession.CheckpointReasonPreWriteDegraded,
		Restorable:   true,
		Status:       agentsession.CheckpointStatusCreating,
	}
	sessionCP := agentsession.SessionCheckpoint{
		ID:           agentsession.NewID("sc"),
		SessionID:    session.ID,
		HeadJSON:     string(headJSON),
		MessagesJSON: string(messagesJSON),
		CreatedAt:    now,
	}

	saved, err := s.checkpointStore.CreateCheckpoint(ctx, checkpoint.CreateCheckpointInput{
		Record:    record,
		SessionCP: sessionCP,
	})
	if err != nil {
		return fmt.Errorf("checkpoint: degraded create: %w", err)
	}

	s.emitRunScoped(ctx, EventCheckpointCreated, nil, CheckpointCreatedPayload{
		CheckpointID:         saved.CheckpointID,
		CodeCheckpointRef:    "",
		SessionCheckpointRef: saved.SessionCheckpointRef,
		CommitHash:           "",
		Reason:               string(saved.Reason),
	})
	return nil
}
