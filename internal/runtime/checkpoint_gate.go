package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"neo-code/internal/checkpoint"
	agentsession "neo-code/internal/session"
)

// createStartOfTurnCheckpoint 在每轮 turn 开始时创建检查点。
// 把上一轮 turn 的 pending capture 固化为 cp_<id>.json；pending 为空时退化为 session-only。
// 返回 error 由调用方发 warning event；失败不阻塞执行。
func (s *Service) createStartOfTurnCheckpoint(ctx context.Context, state *runState) error {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return nil
	}

	state.mu.Lock()
	session := state.session
	runID := state.runID
	state.mu.Unlock()

	checkpointID := agentsession.NewID("checkpoint")
	written, err := s.perEditStore.Finalize(checkpointID)
	if err != nil {
		return fmt.Errorf("checkpoint: finalize per-edit: %w", err)
	}

	if !written {
		return s.createSessionOnlyCheckpoint(ctx, session, runID, state, agentsession.CheckpointReasonPreWrite)
	}
	defer s.perEditStore.Reset()
	return s.createCheckpointRecord(ctx, session, runID, state, checkpointID, agentsession.CheckpointReasonPreWrite)
}

// createEndOfTurnCheckpoint 在工具执行完成后创建代码检查点。
// hasWorkspaceWrite=false 时不创建（避免空 checkpoint）；为 true 时 Finalize 当前 pending。
// 失败仅 log，不阻塞主流程。
func (s *Service) createEndOfTurnCheckpoint(ctx context.Context, state *runState, hasWorkspaceWrite bool) {
	if s.checkpointStore == nil || s.perEditStore == nil {
		return
	}
	if !hasWorkspaceWrite {
		return
	}

	state.mu.Lock()
	session := state.session
	runID := state.runID
	state.mu.Unlock()

	checkpointID := agentsession.NewID("checkpoint")
	written, err := s.perEditStore.FinalizeWithExactState(checkpointID)
	if err != nil {
		log.Printf("checkpoint: end-of-turn finalize: %v", err)
		return
	}
	if !written {
		return
	}
	defer s.perEditStore.Reset()
	if err := s.createCheckpointRecord(ctx, session, runID, state, checkpointID, agentsession.CheckpointReasonEndOfTurn); err != nil {
		log.Printf("checkpoint: end-of-turn record: %v", err)
		return
	}
	state.mu.Lock()
	state.lastEndOfTurnCheckpointID = checkpointID
	state.mu.Unlock()
}

// createCheckpointRecord 写入 SQLite checkpoint 记录 + session 快照，并发出 EventCheckpointCreated。
// CodeCheckpointRef 复用为 "peredit:<checkpointID>"，由 per-edit 后端解释为版本化文件历史的引用。
func (s *Service) createCheckpointRecord(
	ctx context.Context,
	session agentsession.Session,
	runID string,
	state *runState,
	checkpointID string,
	reason agentsession.CheckpointReason,
) error {
	head := session.HeadSnapshot()
	headJSON, err := json.Marshal(head)
	if err != nil {
		_ = s.perEditStore.DeleteCheckpoint(checkpointID)
		return fmt.Errorf("checkpoint: marshal head: %w", err)
	}
	messagesJSON, err := json.Marshal(session.Messages)
	if err != nil {
		_ = s.perEditStore.DeleteCheckpoint(checkpointID)
		return fmt.Errorf("checkpoint: marshal messages: %w", err)
	}

	effectiveWorkdir := strings.TrimSpace(session.Workdir)
	now := time.Now()
	ref := checkpoint.RefForPerEditCheckpoint(checkpointID)

	record := agentsession.CheckpointRecord{
		CheckpointID:      checkpointID,
		WorkspaceKey:      agentsession.WorkspacePathKey(effectiveWorkdir),
		SessionID:         session.ID,
		RunID:             runID,
		Workdir:           effectiveWorkdir,
		CreatedAt:         now,
		Reason:            reason,
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

	saved, err := s.checkpointStore.CreateCheckpoint(ctx, checkpoint.CreateCheckpointInput{
		Record:    record,
		SessionCP: sessionCP,
	})
	if err != nil {
		_ = s.perEditStore.DeleteCheckpoint(checkpointID)
		return fmt.Errorf("checkpoint: db write: %w", err)
	}

	s.emitRunScoped(ctx, EventCheckpointCreated, state, CheckpointCreatedPayload{
		CheckpointID:         saved.CheckpointID,
		CodeCheckpointRef:    saved.CodeCheckpointRef,
		SessionCheckpointRef: saved.SessionCheckpointRef,
		CommitHash:           "",
		Reason:               string(saved.Reason),
	})
	return nil
}

// createSessionOnlyCheckpoint 创建仅含 session 状态的 checkpoint（无代码引用），用于无 pending 写入时的边界标记。
func (s *Service) createSessionOnlyCheckpoint(
	ctx context.Context,
	session agentsession.Session,
	runID string,
	state *runState,
	reason agentsession.CheckpointReason,
) error {
	checkpointID := agentsession.NewID("checkpoint")
	now := time.Now()

	head := session.HeadSnapshot()
	headJSON, err := json.Marshal(head)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal session-only head: %w", err)
	}
	messagesJSON, err := json.Marshal(session.Messages)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal session-only messages: %w", err)
	}

	record := agentsession.CheckpointRecord{
		CheckpointID: checkpointID,
		WorkspaceKey: agentsession.WorkspacePathKey(session.Workdir),
		SessionID:    session.ID,
		RunID:        runID,
		Workdir:      session.Workdir,
		CreatedAt:    now,
		Reason:       reason,
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
		return fmt.Errorf("checkpoint: session-only create: %w", err)
	}

	s.emitRunScoped(ctx, EventCheckpointCreated, state, CheckpointCreatedPayload{
		CheckpointID:         saved.CheckpointID,
		CodeCheckpointRef:    "",
		SessionCheckpointRef: saved.SessionCheckpointRef,
		CommitHash:           "",
		Reason:               string(saved.Reason),
	})
	return nil
}

// findPreviousEndOfTurnCheckpoint 查询指定 session 中、不属于当前 run 的最新可用 end_of_turn checkpoint。
// 用于 run-scoped diff 的 baseline 定位；找不到时返回空字符串，不报错。
func (s *Service) findPreviousEndOfTurnCheckpoint(ctx context.Context, sessionID string, currentRunID string) string {
	if s.checkpointStore == nil {
		return ""
	}
	records, err := s.checkpointStore.ListCheckpoints(ctx, sessionID, checkpoint.ListCheckpointOpts{
		Limit:          50,
		RestorableOnly: true,
	})
	if err != nil {
		log.Printf("checkpoint: find previous end-of-turn list failed: %v", err)
		return ""
	}
	for _, r := range records {
		if r.Reason != agentsession.CheckpointReasonEndOfTurn {
			continue
		}
		if !checkpoint.IsPerEditRef(r.CodeCheckpointRef) {
			continue
		}
		if r.RunID == currentRunID {
			continue
		}
		return checkpoint.PerEditCheckpointIDFromRef(r.CodeCheckpointRef)
	}
	return ""
}
