package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"neo-code/internal/checkpoint"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// GatewayRestoreInput 描述来自 Gateway 的 checkpoint 恢复请求。
type GatewayRestoreInput struct {
	SessionID    string   `json:"session_id"`
	CheckpointID string   `json:"checkpoint_id"`
	Force        bool     `json:"force,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	Paths        []string `json:"paths,omitempty"`
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

	isGuardRestore := record.Reason == agentsession.CheckpointReasonGuard
	perEditID := checkpoint.PerEditCheckpointIDFromRef(record.CodeCheckpointRef)
	relatedPerEditIDs := s.restoreRelatedPerEditIDs(ctx, sessionID, record.RunID, record.CreatedAt)

	// 2. Pre-restore guard checkpoint：只固化本次 restore 影响文件的当前精确状态，供 undo 使用。
	guardID := agentsession.NewID("checkpoint")
	guardInputs := make([]string, 0, 1+len(relatedPerEditIDs))
	if perEditID != "" {
		guardInputs = append(guardInputs, perEditID)
	}
	if !isGuardRestore {
		guardInputs = append(guardInputs, relatedPerEditIDs...)
	}
	guardWritten, finalizeErr := s.perEditStore.FinalizeExactForCheckpoints(guardID, guardInputs)
	if finalizeErr != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: finalize guard: %w", finalizeErr)
	}
	guardRecord, guardErr := s.createGuardCheckpoint(ctx, sessionID, record.RunID, guardID, guardWritten, "")
	if guardErr != nil {
		if guardWritten {
			_ = s.perEditStore.DeleteCheckpoint(guardID)
		}
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: create guard: %w", guardErr)
	}

	// 3. Restore code via per-edit store：只处理目标 run 内 checkpoint 与 guard 涉及的文件。
	if perEditID != "" {
		if isGuardRestore {
			if err := s.perEditStore.RestoreExact(ctx, perEditID); err != nil {
				return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: restore code: %w", err)
			}
		} else {
			guardCheckpointID := ""
			if guardWritten {
				guardCheckpointID = guardID
			}
			if err := s.perEditStore.Restore(ctx, perEditID, guardCheckpointID, relatedPerEditIDs...); err != nil {
				return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: restore code: %w", err)
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

// restoreRelatedPerEditIDs 收集同一 run 内目标 checkpoint 之后仍 available 的代码 checkpoint，用于限定 restore 清理范围。
func (s *Service) restoreRelatedPerEditIDs(ctx context.Context, sessionID, runID string, targetCreatedAt time.Time) []string {
	if s.checkpointStore == nil {
		return nil
	}
	records, err := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, record := range records {
		if strings.TrimSpace(record.RunID) != strings.TrimSpace(runID) {
			continue
		}
		if !record.CreatedAt.After(targetCreatedAt) {
			continue
		}
		if record.Status != "" && record.Status != agentsession.CheckpointStatusAvailable {
			continue
		}
		if record.Reason == agentsession.CheckpointReasonGuard {
			continue
		}
		perEditID := checkpoint.PerEditCheckpointIDFromRef(record.CodeCheckpointRef)
		if perEditID != "" {
			out = append(out, perEditID)
		}
	}
	return out
}

// RestoreCheckpoint 恢复指定 checkpoint 的会话和工作区状态，并发出 checkpoint_restored 事件。
func (s *Service) RestoreCheckpoint(ctx context.Context, input GatewayRestoreInput) (RestoreResult, error) {
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" || mode == "exact" {
		result, guardRecord, err := s.restoreCheckpointCore(ctx, input.SessionID, input.CheckpointID)
		if err != nil {
			return RestoreResult{}, err
		}

		_ = s.emit(ctx, EventCheckpointRestored, "", result.SessionID, CheckpointRestoredPayload{
			CheckpointID:      result.CheckpointID,
			SessionID:         result.SessionID,
			GuardCheckpointID: guardRecord.CheckpointID,
			Mode:              "exact",
		})
		return result, nil
	}
	if mode == "baseline" {
		paths := normalizeBaselineRestorePaths(input.Paths)
		result, guardRecord, err := s.restoreCheckpointBaseline(ctx, input.SessionID, input.CheckpointID, paths)
		if err != nil {
			return RestoreResult{}, err
		}
		_ = s.emit(ctx, EventCheckpointRestored, "", result.SessionID, CheckpointRestoredPayload{
			CheckpointID:      result.CheckpointID,
			SessionID:         result.SessionID,
			GuardCheckpointID: guardRecord.CheckpointID,
			Mode:              "baseline",
			Paths:             paths,
		})
		return result, nil
	}
	return RestoreResult{}, fmt.Errorf("checkpoint: unsupported restore mode %q", input.Mode)
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
// guardWritten=true 时 guardID 对应的 per-edit cp_<id>.json 已写入，CodeCheckpointRef 指向它；
// guardWritten=false 时若 fallbackRef 非空，则用它作为 CodeCheckpointRef 以保证 undo 可走代码恢复路径。
// fallbackRef 应为完整的 "peredit:<id>" 格式引用。
func (s *Service) createGuardCheckpoint(ctx context.Context, sessionID, runID, guardID string, guardWritten bool, fallbackRef string) (agentsession.CheckpointRecord, error) {
	session, err := s.LoadSession(ctx, sessionID)
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
	} else if fallbackRef != "" {
		ref = fallbackRef
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

// restoreCheckpointBaseline 基于 checkpoint 的首触碰基线恢复指定文件，不恢复会话消息。
func (s *Service) restoreCheckpointBaseline(
	ctx context.Context,
	sessionID, checkpointID string,
	paths []string,
) (RestoreResult, agentsession.CheckpointRecord, error) {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: store not available")
	}
	sessionID = strings.TrimSpace(sessionID)
	checkpointID = strings.TrimSpace(checkpointID)
	if sessionID == "" || checkpointID == "" {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: session_id and checkpoint_id required")
	}
	if len(paths) == 0 {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: baseline restore paths required")
	}
	record, _, err := s.checkpointStore.GetCheckpoint(ctx, checkpointID)
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
	perEditID := checkpoint.PerEditCheckpointIDFromRef(record.CodeCheckpointRef)
	if perEditID == "" {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: %s has no code snapshot", checkpointID)
	}
	relPaths := normalizeBaselineRestorePaths(paths)
	if len(relPaths) == 0 {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: baseline restore paths required")
	}
	guardID := agentsession.NewID("checkpoint")
	guardWritten, finalizeErr := s.perEditStore.FinalizeExactForCheckpointPaths(guardID, perEditID, relPaths)
	if finalizeErr != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: finalize baseline guard: %w", finalizeErr)
	}
	guardRecord, guardErr := s.createGuardCheckpoint(ctx, sessionID, record.RunID, guardID, guardWritten, "")
	if guardErr != nil {
		if guardWritten {
			_ = s.perEditStore.DeleteCheckpoint(guardID)
		}
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: create baseline guard: %w", guardErr)
	}
	if err := s.perEditStore.RestoreBaseline(ctx, perEditID, relPaths); err != nil {
		return RestoreResult{}, agentsession.CheckpointRecord{}, fmt.Errorf("checkpoint: baseline restore code: %w", err)
	}
	return RestoreResult{CheckpointID: checkpointID, SessionID: sessionID}, guardRecord, nil
}

// normalizeBaselineRestorePaths 统一 baseline 文件回退路径，保证恢复执行与事件通知使用同一组相对路径。
func normalizeBaselineRestorePaths(paths []string) []string {
	relPaths := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(strings.TrimSpace(path))
		for strings.HasPrefix(clean, "./") {
			clean = strings.TrimPrefix(clean, "./")
		}
		if clean == "" || clean == "." {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		relPaths = append(relPaths, clean)
	}
	return relPaths
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
	Scope        string `json:"scope,omitempty"`         // 可选，"run" 表示 run 级聚合 diff
	RunID        string `json:"run_id,omitempty"`        // scope=run 时指定目标 run
}

// CheckpointDiffResult 描述两个相邻代码检查点之间的差异。
type CheckpointDiffResult struct {
	CheckpointID     string                    `json:"checkpoint_id"`
	PrevCheckpointID string                    `json:"prev_checkpoint_id,omitempty"`
	CommitHash       string                    `json:"commit_hash,omitempty"`
	PrevCommitHash   string                    `json:"prev_commit_hash,omitempty"`
	Files            FileDiffs                 `json:"files"`
	FileEntries      []CheckpointDiffFileEntry `json:"file_entries,omitempty"`
	Patch            string                    `json:"patch,omitempty"`
	Warning          string                    `json:"warning,omitempty"`
}

// CheckpointDiffFileEntry 描述 run diff 中单文件的变更与回退 checkpoint 绑定关系。
type CheckpointDiffFileEntry struct {
	Path                 string `json:"path"`
	Kind                 string `json:"kind"`
	RollbackCheckpointID string `json:"rollback_checkpoint_id,omitempty"`
	CanRollback          bool   `json:"can_rollback"`
}

// FileDiffs 描述 diff 中的文件变更列表。
type FileDiffs struct {
	Added    []string `json:"added,omitempty"`
	Deleted  []string `json:"deleted,omitempty"`
	Modified []string `json:"modified,omitempty"`
}

// CheckpointDiff 查询 checkpoint 间差异或按 run 聚合 diff。
// 当 input.Scope == "run" 时，收集 run_id 下的代码 checkpoint，
// 以本 run 文件首触碰前状态作为 baseline，对比目标 checkpoint 的固化结束态。
func (s *Service) CheckpointDiff(ctx context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error) {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: store not available")
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: session_id required")
	}

	if strings.EqualFold(strings.TrimSpace(input.Scope), "run") {
		return s.checkpointDiffForRun(ctx, input, sessionID)
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
		result.FileEntries = append(result.FileEntries, CheckpointDiffFileEntry{
			Path:                 c.Path,
			Kind:                 string(c.Kind),
			RollbackCheckpointID: targetRecord.CheckpointID,
			CanRollback:          true,
		})
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

// checkpointDiffForRun 汇总指定 run 内的代码 checkpoint，返回本 run 触碰文件从首次触碰前到目标 checkpoint 的净变更。
func (s *Service) checkpointDiffForRun(ctx context.Context, input CheckpointDiffInput, sessionID string) (CheckpointDiffResult, error) {
	records, err := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: list for run diff: %w", err)
	}

	targetID := strings.TrimSpace(input.CheckpointID)
	runID := strings.TrimSpace(input.RunID)
	var targetRecord *agentsession.CheckpointRecord
	if targetID != "" {
		for i := range records {
			if records[i].CheckpointID == targetID {
				targetRecord = &records[i]
				break
			}
		}
		if targetRecord == nil || !checkpoint.IsPerEditRef(targetRecord.CodeCheckpointRef) {
			return CheckpointDiffResult{}, fmt.Errorf("checkpoint: %s not found or has no code snapshot", targetID)
		}
		if runID == "" {
			runID = strings.TrimSpace(targetRecord.RunID)
		}
	}
	if runID == "" {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: run_id required for run scope diff")
	}
	if targetRecord != nil && strings.TrimSpace(targetRecord.RunID) != runID {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: target checkpoint %s does not belong to run %s", targetRecord.CheckpointID, runID)
	}

	codeRecords := make([]agentsession.CheckpointRecord, 0)
	for _, record := range records {
		if strings.TrimSpace(record.RunID) != runID {
			continue
		}
		if !checkpoint.IsPerEditRef(record.CodeCheckpointRef) {
			continue
		}
		if record.Reason == agentsession.CheckpointReasonGuard {
			continue
		}
		if targetRecord != nil && record.CreatedAt.After(targetRecord.CreatedAt) {
			continue
		}
		codeRecords = append(codeRecords, record)
	}
	if len(codeRecords) == 0 {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: no code checkpoint found for run %s", runID)
	}
	sort.Slice(codeRecords, func(i, j int) bool {
		return codeRecords[i].CreatedAt.Before(codeRecords[j].CreatedAt)
	})
	if targetRecord == nil {
		targetRecord = &codeRecords[len(codeRecords)-1]
	}

	perEditIDs := make([]string, 0, len(codeRecords))
	for _, record := range codeRecords {
		perEditID := checkpoint.PerEditCheckpointIDFromRef(record.CodeCheckpointRef)
		if perEditID != "" {
			perEditIDs = append(perEditIDs, perEditID)
		}
	}
	targetPerEditID := checkpoint.PerEditCheckpointIDFromRef(targetRecord.CodeCheckpointRef)
	patch, changes, err := s.perEditStore.DiffCheckpointsToCheckpoint(ctx, perEditIDs, targetPerEditID)
	if err != nil {
		return CheckpointDiffResult{}, fmt.Errorf("checkpoint: per-edit run diff: %w", err)
	}

	result := CheckpointDiffResult{
		CheckpointID: targetRecord.CheckpointID,
		Patch:        patch,
	}

	for _, c := range changes {
		result.FileEntries = append(result.FileEntries, CheckpointDiffFileEntry{
			Path:                 c.Path,
			Kind:                 string(c.Kind),
			RollbackCheckpointID: targetRecord.CheckpointID,
			CanRollback:          true,
		})
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
