package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/checkpoint"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// GatewayRestoreInput 描述来自 Gateway 的 checkpoint 恢复请求。
type GatewayRestoreInput struct {
	SessionID    string `json:"session_id"`
	CheckpointID string `json:"checkpoint_id"`
	Force        bool   `json:"force,omitempty"`
}

// RestoreResult 描述 restore/undo 操作的结果。
type RestoreResult struct {
	CheckpointID string                     `json:"checkpoint_id"`
	SessionID    string                     `json:"session_id"`
	Conflict     *checkpoint.ConflictResult `json:"conflict,omitempty"`
}

// RestoreCheckpoint 恢复指定 checkpoint 的会话和工作区状态。
func (s *Service) RestoreCheckpoint(ctx context.Context, input GatewayRestoreInput) (RestoreResult, error) {
	if s.checkpointStore == nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: store not available")
	}

	sessionID := strings.TrimSpace(input.SessionID)
	checkpointID := strings.TrimSpace(input.CheckpointID)
	if sessionID == "" || checkpointID == "" {
		return RestoreResult{}, fmt.Errorf("checkpoint: session_id and checkpoint_id required")
	}

	// 1. Load checkpoint record
	record, sessionCP, err := s.checkpointStore.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return RestoreResult{}, err
	}
	if record.SessionID != sessionID {
		return RestoreResult{}, fmt.Errorf("checkpoint: session mismatch")
	}
	if record.Status != agentsession.CheckpointStatusAvailable {
		return RestoreResult{}, fmt.Errorf("checkpoint: status is %s, expected available", record.Status)
	}
	if !record.Restorable {
		return RestoreResult{}, fmt.Errorf("checkpoint: not restorable")
	}

	// 2. Conflict detection
	if s.shadowRepo != nil && record.CodeCheckpointRef != "" && !input.Force {
		commitHash, resolveErr := s.resolveCommitHashForRef(ctx, record.CodeCheckpointRef)
		if resolveErr == nil && commitHash != "" {
			conflict, conflictErr := s.shadowRepo.DetectConflicts(ctx, commitHash)
			if conflictErr == nil && conflict.HasConflict {
				return RestoreResult{
					CheckpointID: checkpointID,
					SessionID:    sessionID,
					Conflict:     &conflict,
				}, fmt.Errorf("checkpoint: conflicts detected, use force to override")
			}
		}
	}

	// 3. Create pre-restore guard checkpoint (current state snapshot)
	guardID := agentsession.NewID("checkpoint")
	guardRef := checkpoint.RefForCheckpoint(sessionID, guardID)
	guardCommitHash := ""

	if s.shadowRepo != nil && s.shadowRepo.IsAvailable() {
		guardCommitHash, _ = s.shadowRepo.Snapshot(ctx, guardRef, fmt.Sprintf("pre_restore_guard for session %s", sessionID))
	}

	guardRecord, guardErr := s.createGuardCheckpoint(ctx, sessionID, record.RunID, guardID, guardRef, guardCommitHash)
	if guardErr != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: create guard: %w", guardErr)
	}

	// 4. Restore code (git checkout)
	if s.shadowRepo != nil && record.CodeCheckpointRef != "" {
		restoreCommitHash, resolveErr := s.resolveCommitHashForRef(ctx, record.CodeCheckpointRef)
		if resolveErr == nil && restoreCommitHash != "" {
			if err := s.shadowRepo.Restore(ctx, restoreCommitHash); err != nil {
				return RestoreResult{}, fmt.Errorf("checkpoint: restore code: %w", err)
			}
		}
	}

	// 5. Unmarshal session checkpoint
	if sessionCP == nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: no session checkpoint data")
	}
	var head agentsession.SessionHead
	if err := json.Unmarshal([]byte(sessionCP.HeadJSON), &head); err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: unmarshal head: %w", err)
	}
	var messages []providertypes.Message
	if err := json.Unmarshal([]byte(sessionCP.MessagesJSON), &messages); err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: unmarshal messages: %w", err)
	}

	// 6. Determine checkpoint IDs to mark
	markAvailableIDs := []string{guardRecord.CheckpointID}
	var markRestoredIDs []string
	allRecords, listErr := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{})
	if listErr == nil {
		for _, r := range allRecords {
			if r.CreatedAt.After(record.CreatedAt) && r.Status == agentsession.CheckpointStatusAvailable && r.Reason != agentsession.CheckpointReasonGuard {
				markRestoredIDs = append(markRestoredIDs, r.CheckpointID)
			}
		}
	}

	// 7. Restore session + update checkpoint statuses (single transaction)
	restoreInput := checkpoint.RestoreCheckpointInput{
		SessionID:        sessionID,
		Head:             head,
		Messages:         messages,
		UpdatedAt:        time.Now(),
		MarkAvailableIDs: markAvailableIDs,
		MarkRestoredIDs:  markRestoredIDs,
	}
	if err := s.checkpointStore.RestoreCheckpoint(ctx, restoreInput); err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: restore: %w", err)
	}

	// 8. Update runtime session if it's the current session
	s.updateRuntimeSessionAfterRestore(sessionID, head, messages)

	s.emitRunScoped(ctx, EventCheckpointRestored, nil, CheckpointRestoredPayload{
		CheckpointID:      checkpointID,
		SessionID:         sessionID,
		GuardCheckpointID: guardRecord.CheckpointID,
	})
	return RestoreResult{
		CheckpointID: checkpointID,
		SessionID:    sessionID,
	}, nil
}

// UndoRestoreCheckpoint 撤销最近一次 restore，通过 pre_restore_guard 恢复到 restore 前的状态。
func (s *Service) UndoRestoreCheckpoint(ctx context.Context, sessionID string) (RestoreResult, error) {
	if s.checkpointStore == nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: store not available")
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return RestoreResult{}, fmt.Errorf("checkpoint: session_id required")
	}

	// Find latest guard checkpoint
	records, err := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{
		Limit:          20,
		RestorableOnly: true,
	})
	if err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: list for undo: %w", err)
	}

	var guardRecord *agentsession.CheckpointRecord
	for _, r := range records {
		if r.Reason == agentsession.CheckpointReasonGuard {
			guardRecord = &r
			break
		}
	}
	if guardRecord == nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: no guard checkpoint found for undo")
	}

	// Recursively call RestoreCheckpoint with force
	result, err := s.RestoreCheckpoint(ctx, GatewayRestoreInput{
		SessionID:    sessionID,
		CheckpointID: guardRecord.CheckpointID,
		Force:        true,
	})
	if err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: undo restore: %w", err)
	}

	s.emitRunScoped(ctx, EventCheckpointUndoRestore, nil, CheckpointUndoRestorePayload{
		GuardCheckpointID: guardRecord.CheckpointID,
		SessionID:         sessionID,
	})
	return result, nil
}

// createGuardCheckpoint 创建 pre_restore_guard 类型的 checkpoint。
func (s *Service) createGuardCheckpoint(ctx context.Context, sessionID, runID, checkpointID, ref, commitHash string) (agentsession.CheckpointRecord, error) {
	session, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err != nil {
		return agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: load session for guard: %w", err)
	}

	head := session.HeadSnapshot()
	headJSON, err := json.Marshal(head)
	if err != nil {
		return agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: marshal guard head: %w", err)
	}
	messagesJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: marshal guard messages: %w", err)
	}

	now := time.Now()
	record := agentsession.CheckpointRecord{
		CheckpointID:      checkpointID,
		WorkspaceKey:      agentsession.WorkspacePathKey(session.Workdir),
		SessionID:         sessionID,
		RunID:             runID,
		Workdir:           session.Workdir,
		CreatedAt:         now,
		Reason:            agentsession.CheckpointReasonGuard,
		CodeCheckpointRef: ref,
		Restorable:        true,
		Status:            agentsession.CheckpointStatusCreating,
	}
	sessionCP := agentsession.SessionCheckpoint{
		ID:           agentsession.NewID("sc"),
		SessionID:    sessionID,
		HeadJSON:     string(headJSON),
		MessagesJSON: string(messagesJSON),
		CreatedAt:    now,
	}

	saved, err := s.checkpointStore.CreateCheckpoint(ctx, checkpoint.CreateCheckpointInput{
		Record:    record,
		SessionCP: sessionCP,
	})
	if err != nil {
		return agentsession.CheckpointRecord{}, err
	}

	if commitHash != "" {
		s.emitRunScoped(ctx, EventCheckpointCreated, nil, CheckpointCreatedPayload{
			CheckpointID:         saved.CheckpointID,
			CodeCheckpointRef:    saved.CodeCheckpointRef,
			SessionCheckpointRef: saved.SessionCheckpointRef,
			CommitHash:           commitHash,
			Reason:               string(saved.Reason),
		})
	}
	return saved, nil
}

// resolveCommitHashForRef 通过 git rev-parse 解析 ref 对应的 commit hash。
func (s *Service) resolveCommitHashForRef(ctx context.Context, ref string) (string, error) {
	if s.shadowRepo == nil || ref == "" {
		return "", fmt.Errorf("shadow repo not available")
	}
	// Use the shadow repo's existing git infrastructure
	return s.shadowRepo.ResolveRef(ctx, ref)
}

// ListCheckpoints 查询指定会话的 checkpoint 列表。
func (s *Service) ListCheckpoints(ctx context.Context, sessionID string, opts checkpoint.ListCheckpointOpts) ([]agentsession.CheckpointRecord, error) {
	if s.checkpointStore == nil {
		return nil, fmt.Errorf("checkpoint: store not available")
	}
	return s.checkpointStore.ListCheckpoints(ctx, sessionID, opts)
}

// updateRuntimeSessionAfterRestore 在 restore 后更新运行时会话状态。
// 当前实现不做运行时状态直接修改，依赖下次 session 加载时从 DB 读取恢复后的状态。
func (s *Service) updateRuntimeSessionAfterRestore(sessionID string, head agentsession.SessionHead, messages []providertypes.Message) {
}
