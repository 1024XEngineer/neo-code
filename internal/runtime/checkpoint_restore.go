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
// per-edit 后端只还原本快照覆盖的文件，因此 Conflict 字段恒为空，仅保留以维持网关契约。
type RestoreResult struct {
	CheckpointID string                     `json:"checkpoint_id"`
	SessionID    string                     `json:"session_id"`
	Conflict     *checkpoint.ConflictResult `json:"conflict,omitempty"`
}

// restoreCheckpointCore 执行 checkpoint 恢复的核心逻辑，不发出任何事件。
// 调用方负责在合适的时机发出 checkpoint_restored / checkpoint_undo_restore 事件。
func (s *Service) restoreCheckpointCore(ctx context.Context, sessionID, checkpointID string) (RestoreResult, agentsession.CheckpointRecord, error) {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: store not available")
	}

	sessionID = strings.TrimSpace(sessionID)
	checkpointID = strings.TrimSpace(checkpointID)
	if sessionID == "" || checkpointID == "" {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: session_id and checkpoint_id required")
	}

	// 1. Load checkpoint record
	record, sessionCP, err := s.checkpointStore.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, err
	}
	if record.SessionID != sessionID {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: session mismatch")
	}
	if record.Status != agentsession.CheckpointStatusAvailable {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: status is %s, expected available", record.Status)
	}
	if !record.Restorable {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: not restorable")
	}

	// 2. Pre-restore guard checkpoint：把当前 pending 固化为 guard cp，以便 undo 回到 restore 之前。
	guardID := agentsession.NewID("checkpoint")
	guardWritten, finalizeErr := s.perEditStore.Finalize(guardID)
	if finalizeErr != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: finalize guard: %w", finalizeErr)
	}
	if guardWritten {
		s.perEditStore.Reset()
	}
	guardRecord, guardErr := s.createGuardCheckpoint(ctx, sessionID, record.RunID, guardID, guardWritten)
	if guardErr != nil {
		if guardWritten {
			_ = s.perEditStore.DeleteCheckpoint(guardID)
		}
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: create guard: %w", guardErr)
	}

	// 3. Restore code via per-edit store（不在 cp.FileVersions 中的文件保持不变）。
	// Guard checkpoint 恢复时使用 RestoreExact：guard 中存储的 version 就是 restore 前的 pre-write 状态，
	// 而 Restore 的 v_next 语义在 guard 上通常是 no-op（guard 之后没有新的 capture）。
	isGuardRestore := record.Reason == agentsession.CheckpointReasonGuard
	if checkpoint.IsPerEditRef(record.CodeCheckpointRef) {
		perEditID := checkpoint.PerEditCheckpointIDFromRef(record.CodeCheckpointRef)
		if perEditID != "" {
			if isGuardRestore {
				if err := s.perEditStore.RestoreExact(ctx, perEditID); err != nil {
					return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: restore code: %w", err)
				}
			} else {
				if err := s.perEditStore.Restore(ctx, perEditID); err != nil {
					return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: restore code: %w", err)
				}
			}
		}
	}

	// 4. Unmarshal session checkpoint
	if sessionCP == nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: no session checkpoint data")
	}
	var head agentsession.SessionHead
	if err := json.Unmarshal([]byte(sessionCP.HeadJSON), &head); err != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: unmarshal head: %w", err)
	}
	var messages []providertypes.Message
	if err := json.Unmarshal([]byte(sessionCP.MessagesJSON), &messages); err != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: unmarshal messages: %w", err)
	}

	// 5. Determine checkpoint IDs to mark
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

	// 6. Restore session + update checkpoint statuses (single transaction)
	restoreInput := checkpoint.RestoreCheckpointInput{
		SessionID:        sessionID,
		Head:             head,
		Messages:         messages,
		UpdatedAt:        time.Now(),
		MarkAvailableIDs: markAvailableIDs,
		MarkRestoredIDs:  markRestoredIDs,
	}
	if err := s.checkpointStore.RestoreCheckpoint(ctx, restoreInput); err != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: restore: %w", err)
	}

	// 7. Update runtime session if it's the current session
	s.updateRuntimeSessionAfterRestore(sessionID, head, messages)

	return RestoreResult{
		CheckpointID: checkpointID,
		SessionID:    sessionID,
	}, guardRecord, nil
}

// RestoreCheckpoint 恢复指定 checkpoint 的会话和工作区状态，并发出 checkpoint_restored 事件。
func (s *Service) RestoreCheckpoint(ctx context.Context, input GatewayRestoreInput) (RestoreResult, error) {
	result, guardRecord, err := s.restoreCheckpointCore(ctx, input.SessionID, input.CheckpointID)
	if err != nil {
		return RestoreResult{}, err
	}

	_ = s.emit(ctx, EventCheckpointRestored, "", result.SessionID, CheckpointRestoredPayload{
		CheckpointID:      result.CheckpointID,
		SessionID:         result.SessionID,
		GuardCheckpointID: guardRecord.CheckpointID,
	})
	return result, nil
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

	result, _, err := s.restoreCheckpointCore(ctx, sessionID, guardRecord.CheckpointID)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("checkpoint: undo restore: %w", err)
	}

	_ = s.emit(ctx, EventCheckpointUndoRestore, "", sessionID, CheckpointUndoRestorePayload{
		GuardCheckpointID: guardRecord.CheckpointID,
		SessionID:         sessionID,
	})
	return result, nil
}

// createGuardCheckpoint 创建 pre_restore_guard 类型的 checkpoint。
// guardWritten=true 时 guardID 对应的 per-edit cp_<id>.json 已写入，CodeCheckpointRef 指向它；否则仅记 session 状态。
func (s *Service) createGuardCheckpoint(ctx context.Context, sessionID, runID, guardID string, guardWritten bool) (agentsession.CheckpointRecord, error) {
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

	var ref string
	if guardWritten {
		ref = checkpoint.RefForPerEditCheckpoint(guardID)
	}

	now := time.Now()
	record := agentsession.CheckpointRecord{
		CheckpointID:      guardID,
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

	_ = s.emit(ctx, EventCheckpointCreated, "", sessionID, CheckpointCreatedPayload{
		CheckpointID:         saved.CheckpointID,
		CodeCheckpointRef:    saved.CodeCheckpointRef,
		SessionCheckpointRef: saved.SessionCheckpointRef,
		CommitHash:           "",
		Reason:               string(saved.Reason),
	})
	return saved, nil
}

// ListCheckpoints 查询指定会话的 checkpoint 列表。
func (s *Service) ListCheckpoints(ctx context.Context, sessionID string, opts checkpoint.ListCheckpointOpts) ([]agentsession.CheckpointRecord, error) {
	if s.checkpointStore == nil {
		return nil, fmt.Errorf("checkpoint: store not available")
	}
	return s.checkpointStore.ListCheckpoints(ctx, sessionID, opts)
}

// updateRuntimeSessionAfterRestore 使运行时快照缓存失效。
// GetRuntimeSnapshot 会从 DB 重新加载恢复后的状态，而非返回旧缓存。
func (s *Service) updateRuntimeSessionAfterRestore(sessionID string, head agentsession.SessionHead, messages []providertypes.Message) {
	normalized := strings.TrimSpace(sessionID)
	if normalized == "" {
		return
	}
	s.runtimeSnapshotMu.Lock()
	delete(s.runtimeSnapshots, normalized)
	s.runtimeSnapshotMu.Unlock()
}

// CheckpointDiffInput 描述 checkpoint diff 查询请求。
type CheckpointDiffInput struct {
	SessionID    string `json:"session_id"`
	CheckpointID string `json:"checkpoint_id,omitempty"` // 可选，为空则查最新代码检查点
}

// CheckpointDiffResult 描述两个相邻代码检查点之间的差异。
type CheckpointDiffResult struct {
	CheckpointID     string    `json:"checkpoint_id"`
	PrevCheckpointID string    `json:"prev_checkpoint_id,omitempty"`
	CommitHash       string    `json:"commit_hash,omitempty"`
	PrevCommitHash   string    `json:"prev_commit_hash,omitempty"`
	Files            FileDiffs `json:"files"`
	Patch            string    `json:"patch,omitempty"`
}

// FileDiffs 描述 diff 中的文件变更列表。
type FileDiffs struct {
	Added    []string `json:"added,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
	Modified []string `json:"modified,omitempty"`
}

// CheckpointDiff 查询两个相邻代码检查点之间的差异，单一 per-edit 后端路径。
func (s *Service) CheckpointDiff(ctx context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error) {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: store not available")
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: session_id required")
	}

	records, err := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{Limit: 20})
	if err != nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: list for diff: %w", err)
	}

	targetID := strings.TrimSpace(input.CheckpointID)
	var targetRecord *agentsession.CheckpointRecord
	if targetID != "" {
		for i := range records {
			if records[i].CheckpointID != targetID {
				continue
			}
			if !checkpoint.IsPerEditRef(records[i].CodeCheckpointRef) {
				continue
			}
			targetRecord = &records[i]
			break
		}
		if targetRecord == nil {
			return CheckpointDiffResult{}, fmt.Errorf("checkpoint: %s not found or has no code snapshot", targetID)
		}
	} else {
		for i := range records {
			if !checkpoint.IsPerEditRef(records[i].CodeCheckpointRef) {
				continue
			}
			targetRecord = &records[i]
			break
		}
		if targetRecord == nil {
			return CheckpointDiffResult{}, fmt.Errorf("checkpoint: no code checkpoint found")
		}
	}

	var prevRecord *agentsession.CheckpointRecord
	for i := range records {
		if records[i].CheckpointID == targetRecord.CheckpointID {
			continue
		}
		if !records[i].CreatedAt.Before(targetRecord.CreatedAt) {
			continue
		}
		if !checkpoint.IsPerEditRef(records[i].CodeCheckpointRef) {
			continue
		}
		prevRecord = &records[i]
		break
	}

	result := CheckpointDiffResult{
		CheckpointID: targetRecord.CheckpointID,
	}
	if prevRecord == nil {
		return result, nil
	}
	result.PrevCheckpointID = prevRecord.CheckpointID

	fromID := checkpoint.PerEditCheckpointIDFromRef(prevRecord.CodeCheckpointRef)
	toID := checkpoint.PerEditCheckpointIDFromRef(targetRecord.CodeCheckpointRef)
	patch, err := s.perEditStore.Diff(ctx, fromID, toID)
	if err != nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: per-edit diff: %w", err)
	}
	result.Patch = patch

	changes, err := s.perEditStore.ChangedFiles(ctx, fromID, toID)
	if err != nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: per-edit changed files: %w", err)
	}
	for _, c := range changes {
		switch c.Kind {
		case checkpoint.FileChangeAdded:
			result.Files.Added = append(result.Files.Added, c.Path)
		case checkpoint.FileChangeDeleted:
			result.Files.Deleted = append(result.Files.Deleted, c.Path)
		case checkpoint.FileChangeModified:
			result.Files.Modified = append(result.Files.Modified, c.Path)
		}
	}

	return result, nil
}
