package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"neo-code/internal/checkpoint"
	"neo-code/internal/repository"
	agentsession "neo-code/internal/session"
)

// buildRunBaselineKey 生成 run 级缓存 key，保证同一会话不同 run 的基线隔离。
func buildRunBaselineKey(sessionID, runID string) string {
	return strings.TrimSpace(sessionID) + "\n" + strings.TrimSpace(runID)
}

// ensureRunBaselineMapsLocked 懒初始化 run 级基线与漂移缓存，允许测试中用字面量 Service 直接调用。
func (s *Service) ensureRunBaselineMapsLocked() {
	if s.runRollbackBaselineByRunKey == nil {
		s.runRollbackBaselineByRunKey = make(map[string]string)
	}
	if s.runWorkspaceDriftByRunKey == nil {
		s.runWorkspaceDriftByRunKey = make(map[string]bool)
	}
	if s.lastRunFingerprintByWorkspaceKey == nil {
		s.lastRunFingerprintByWorkspaceKey = make(map[string]repository.WorkdirFingerprint)
	}
}

type workspaceFingerprintPersistenceStore interface {
	SaveWorkspaceFingerprint(ctx context.Context, workspaceKey string, fingerprintPayload string, updatedAt time.Time) error
	LoadWorkspaceFingerprint(ctx context.Context, workspaceKey string) (string, bool, error)
}

type workspaceCheckpointStatePersistenceStore interface {
	SaveWorkspaceCheckpointState(ctx context.Context, state checkpoint.WorkspaceCheckpointState) error
	LoadWorkspaceCheckpointState(ctx context.Context, workspaceKey string) (checkpoint.WorkspaceCheckpointState, bool, error)
	SaveRunCheckpointBaseline(ctx context.Context, baseline checkpoint.RunCheckpointBaseline) error
	LoadRunCheckpointBaseline(ctx context.Context, sessionID, runID string) (checkpoint.RunCheckpointBaseline, bool, error)
}

// workspaceKeyFromWorkdir 统一把工作目录映射为跨会话稳定 key。
func workspaceKeyFromWorkdir(workdir string) string {
	return strings.TrimSpace(agentsession.WorkspacePathKey(strings.TrimSpace(workdir)))
}

// setRunRollbackBaseline 记录当前 run 的权威回退基线 checkpoint_id（由后端计算，前端仅消费）。
func (s *Service) setRunRollbackBaseline(sessionID, runID, checkpointID string) {
	key := buildRunBaselineKey(sessionID, runID)
	if key == "\n" {
		return
	}
	s.rollbackBaselineMu.Lock()
	defer s.rollbackBaselineMu.Unlock()
	s.ensureRunBaselineMapsLocked()
	if strings.TrimSpace(checkpointID) == "" {
		delete(s.runRollbackBaselineByRunKey, key)
		return
	}
	s.runRollbackBaselineByRunKey[key] = strings.TrimSpace(checkpointID)
}

// getRunRollbackBaseline 返回 run 的权威回退基线 checkpoint_id。
func (s *Service) getRunRollbackBaseline(sessionID, runID string) string {
	key := buildRunBaselineKey(sessionID, runID)
	if key == "\n" {
		return ""
	}
	s.rollbackBaselineMu.Lock()
	defer s.rollbackBaselineMu.Unlock()
	s.ensureRunBaselineMapsLocked()
	return s.runRollbackBaselineByRunKey[key]
}

// getPersistentRunRollbackBaseline 从内存或 SQLite 读取 run 的权威回退基线。
func (s *Service) getPersistentRunRollbackBaseline(ctx context.Context, sessionID, runID string) (string, bool) {
	if baseline := s.getRunRollbackBaseline(sessionID, runID); baseline != "" {
		return baseline, s.getRunWorkspaceDrift(sessionID, runID)
	}
	store, ok := s.checkpointStore.(workspaceCheckpointStatePersistenceStore)
	if !ok {
		return "", false
	}
	loaded, found, err := store.LoadRunCheckpointBaseline(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(runID))
	if err != nil || !found {
		return "", false
	}
	s.setRunRollbackBaseline(sessionID, runID, loaded.CheckpointID)
	s.setRunWorkspaceDrift(sessionID, runID, loaded.Drifted)
	return strings.TrimSpace(loaded.CheckpointID), loaded.Drifted
}

// persistRunRollbackBaseline 双写 run 级回退基线，保证异步 diff 在 run 结束后仍可读取。
func (s *Service) persistRunRollbackBaseline(ctx context.Context, sessionID, runID, checkpointID string, drifted bool) {
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	checkpointID = strings.TrimSpace(checkpointID)
	if sessionID == "" || runID == "" || checkpointID == "" {
		return
	}
	s.setRunRollbackBaseline(sessionID, runID, checkpointID)
	s.setRunWorkspaceDrift(sessionID, runID, drifted)
	store, ok := s.checkpointStore.(workspaceCheckpointStatePersistenceStore)
	if !ok {
		return
	}
	_ = store.SaveRunCheckpointBaseline(ctx, checkpoint.RunCheckpointBaseline{
		SessionID:    sessionID,
		RunID:        runID,
		CheckpointID: checkpointID,
		Drifted:      drifted,
		UpdatedAt:    time.Now(),
	})
}

// setRunWorkspaceDrift 标记当前 run 是否检测到空闲期工作区漂移。
func (s *Service) setRunWorkspaceDrift(sessionID, runID string, drifted bool) {
	key := buildRunBaselineKey(sessionID, runID)
	if key == "\n" {
		return
	}
	s.rollbackBaselineMu.Lock()
	defer s.rollbackBaselineMu.Unlock()
	s.ensureRunBaselineMapsLocked()
	if drifted {
		s.runWorkspaceDriftByRunKey[key] = true
		return
	}
	delete(s.runWorkspaceDriftByRunKey, key)
}

// getRunWorkspaceDrift 返回 run 是否检测到空闲期工作区漂移。
func (s *Service) getRunWorkspaceDrift(sessionID, runID string) bool {
	key := buildRunBaselineKey(sessionID, runID)
	if key == "\n" {
		return false
	}
	s.rollbackBaselineMu.Lock()
	defer s.rollbackBaselineMu.Unlock()
	s.ensureRunBaselineMapsLocked()
	return s.runWorkspaceDriftByRunKey[key]
}

// clearRunCheckpointCaches 清理 run 级 checkpoint 缓存，避免内存与跨 run 污染。
func (s *Service) clearRunCheckpointCaches(sessionID, runID string) {
	key := buildRunBaselineKey(sessionID, runID)
	if key == "\n" {
		return
	}
	s.rollbackBaselineMu.Lock()
	defer s.rollbackBaselineMu.Unlock()
	s.ensureRunBaselineMapsLocked()
	delete(s.runRollbackBaselineByRunKey, key)
	delete(s.runWorkspaceDriftByRunKey, key)
}

// recordRunStartFingerprint 在 run 开始时比对上次 run 结束指纹，返回是否发生空闲期漂移及详细差异。
func (s *Service) recordRunStartFingerprint(
	ctx context.Context,
	sessionID, runID, workdir string,
) (bool, repository.FingerprintDiff) {
	emptyDiff := repository.FingerprintDiff{}
	normalizedRunID := strings.TrimSpace(runID)
	normalizedWorkdir := strings.TrimSpace(workdir)
	workspaceKey := workspaceKeyFromWorkdir(normalizedWorkdir)
	if strings.TrimSpace(sessionID) == "" || normalizedRunID == "" || normalizedWorkdir == "" || workspaceKey == "" {
		return false, emptyDiff
	}
	current, _, err := repository.ScanWorkdir(ctx, normalizedWorkdir, repository.DefaultFingerprintOptions())
	if err != nil {
		return false, emptyDiff
	}

	s.rollbackBaselineMu.Lock()
	s.ensureRunBaselineMapsLocked()
	previous, ok := s.lastRunFingerprintByWorkspaceKey[workspaceKey]
	s.rollbackBaselineMu.Unlock()
	var currentCheckpointID string
	if !ok {
		if store, okStore := s.checkpointStore.(workspaceCheckpointStatePersistenceStore); okStore {
			state, found, loadErr := store.LoadWorkspaceCheckpointState(ctx, workspaceKey)
			if loadErr == nil && found {
				currentCheckpointID = strings.TrimSpace(state.CurrentCheckpointID)
				var restored repository.WorkdirFingerprint
				if err := json.Unmarshal([]byte(state.FingerprintPayload), &restored); err == nil {
					s.rollbackBaselineMu.Lock()
					s.ensureRunBaselineMapsLocked()
					s.lastRunFingerprintByWorkspaceKey[workspaceKey] = restored
					s.rollbackBaselineMu.Unlock()
					previous = restored
					ok = true
				}
			}
		}
		if !ok {
			if store, okStore := s.checkpointStore.(workspaceFingerprintPersistenceStore); okStore {
				payload, found, loadErr := store.LoadWorkspaceFingerprint(ctx, workspaceKey)
				if loadErr == nil && found {
					var restored repository.WorkdirFingerprint
					if err := json.Unmarshal([]byte(payload), &restored); err == nil {
						s.rollbackBaselineMu.Lock()
						s.ensureRunBaselineMapsLocked()
						s.lastRunFingerprintByWorkspaceKey[workspaceKey] = restored
						s.rollbackBaselineMu.Unlock()
						previous = restored
						ok = true
					}
				}
			}
		}
		if !ok {
			return false, emptyDiff
		}
	}
	diff := repository.DiffFingerprints(previous, current)
	drifted := len(diff.Added) > 0 || len(diff.Modified) > 0 || len(diff.Deleted) > 0
	if drifted {
		s.setRunWorkspaceDrift(sessionID, normalizedRunID, true)
		return true, diff
	}
	if currentCheckpointID != "" {
		s.persistRunRollbackBaseline(ctx, sessionID, normalizedRunID, currentCheckpointID, false)
	}
	return false, diff
}

// recordRunEndFingerprint 在 run 结束后保存最新指纹，供下次 run 开始时进行漂移检测。
func (s *Service) recordRunEndFingerprint(ctx context.Context, sessionID, workdir string) {
	s.recordRunEndWorkspaceState(ctx, sessionID, workdir, "")
}

// recordRunEndWorkspaceState 保存最新指纹和当前 checkpoint；checkpointID 为空时保留已有基线。
func (s *Service) recordRunEndWorkspaceState(ctx context.Context, sessionID, workdir, checkpointID string) {
	normalizedWorkdir := strings.TrimSpace(workdir)
	workspaceKey := workspaceKeyFromWorkdir(normalizedWorkdir)
	if strings.TrimSpace(sessionID) == "" || normalizedWorkdir == "" || workspaceKey == "" {
		return
	}
	current, _, err := repository.ScanWorkdir(ctx, normalizedWorkdir, repository.DefaultFingerprintOptions())
	if err != nil {
		return
	}
	s.rollbackBaselineMu.Lock()
	s.ensureRunBaselineMapsLocked()
	s.lastRunFingerprintByWorkspaceKey[workspaceKey] = current
	s.rollbackBaselineMu.Unlock()

	store, ok := s.checkpointStore.(workspaceFingerprintPersistenceStore)
	if !ok {
		return
	}
	payload, err := json.Marshal(current)
	if err != nil {
		return
	}
	now := time.Now()
	_ = store.SaveWorkspaceFingerprint(ctx, workspaceKey, string(payload), now)

	stateStore, ok := s.checkpointStore.(workspaceCheckpointStatePersistenceStore)
	if !ok {
		return
	}
	currentCheckpointID := strings.TrimSpace(checkpointID)
	if currentCheckpointID == "" {
		if existing, found, err := stateStore.LoadWorkspaceCheckpointState(ctx, workspaceKey); err == nil && found {
			currentCheckpointID = strings.TrimSpace(existing.CurrentCheckpointID)
		}
	}
	_ = stateStore.SaveWorkspaceCheckpointState(ctx, checkpoint.WorkspaceCheckpointState{
		WorkspaceKey:        workspaceKey,
		CurrentCheckpointID: currentCheckpointID,
		FingerprintPayload:  string(payload),
		UpdatedAt:           now,
	})
}
