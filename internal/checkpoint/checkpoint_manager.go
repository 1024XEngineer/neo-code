package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

// CheckpointStore 定义 checkpoint 持久化的意图型接口。
type CheckpointStore interface {
	CreateCheckpoint(ctx context.Context, input CreateCheckpointInput) (session.CheckpointRecord, error)
	ListCheckpoints(ctx context.Context, sessionID string, opts ListCheckpointOpts) ([]session.CheckpointRecord, error)
	GetCheckpoint(ctx context.Context, checkpointID string) (session.CheckpointRecord, *session.SessionCheckpoint, error)
	UpdateCheckpointStatus(ctx context.Context, checkpointID string, status session.CheckpointStatus) error
	GetLatestResumeCheckpoint(ctx context.Context, sessionID string) (*session.ResumeCheckpoint, error)
	RestoreCheckpoint(ctx context.Context, input RestoreCheckpointInput) error
	SetResumeCheckpoint(ctx context.Context, rc session.ResumeCheckpoint) error
	PruneExpiredCheckpoints(ctx context.Context, sessionID string, maxAutoKeep int) (int, error)
	RepairCreatingCheckpoints(ctx context.Context) (int, error)
}

// CreateCheckpointInput 描述一次 checkpoint 创建的完整输入。
type CreateCheckpointInput struct {
	Record    session.CheckpointRecord
	SessionCP session.SessionCheckpoint
}

// ListCheckpointOpts 描述 checkpoint 列表查询选项。
type ListCheckpointOpts struct {
	Limit          int
	RestorableOnly bool
}

// RestoreCheckpointInput 描述一次 restore 操作的完整输入。
type RestoreCheckpointInput struct {
	SessionID        string
	Head             session.SessionHead
	Messages         []providertypes.Message
	UpdatedAt        time.Time
	MarkAvailableIDs []string
	MarkRestoredIDs  []string
}

// SQLiteCheckpointStore 基于 SQLite 实现 checkpoint 持久化。
type SQLiteCheckpointStore struct {
	dbPath string
	initMu sync.Mutex
	db     *sql.DB
	ownsDB bool // true 表示本实例打开的连接，Close 时需释放
}

// NewSQLiteCheckpointStore 创建 checkpoint 存储实例。
// dbPath 为 session.db 文件路径，可通过 session.DatabasePath 获取。
func NewSQLiteCheckpointStore(dbPath string) *SQLiteCheckpointStore {
	return &SQLiteCheckpointStore{
		dbPath: dbPath,
	}
}

// NewSQLiteCheckpointStoreWithDB 创建 checkpoint 存储实例，复用已有的 *sql.DB 连接。
// 适用于与 session store 共享同一数据库文件的场景，避免 Windows 上多连接文件锁定。
// Close 不会关闭传入的连接，由调用方管理连接生命周期。
func NewSQLiteCheckpointStoreWithDB(db *sql.DB) *SQLiteCheckpointStore {
	return &SQLiteCheckpointStore{
		db:     db,
		ownsDB: false,
	}
}

// Close 释放数据库连接。仅当本实例拥有连接时（ownsDB=true）才实际关闭。
func (s *SQLiteCheckpointStore) Close() error {
	if s == nil || s.db == nil || !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteCheckpointStore) ensureDB(ctx context.Context) (*sql.DB, error) {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open sqlite db: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
	}
	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("checkpoint: apply pragma %q: %w", pragma, err)
		}
	}
	s.db = db
	s.ownsDB = true
	return db, nil
}

// CreateCheckpoint 在单一事务内写入 checkpoint record + session checkpoint。
// 事务内完成 record INSERT → session_cp INSERT → record UPDATE（设置 session_checkpoint_ref + status=available）。
func (s *SQLiteCheckpointStore) CreateCheckpoint(ctx context.Context, input CreateCheckpointInput) (session.CheckpointRecord, error) {
	if err := ctx.Err(); err != nil {
		return session.CheckpointRecord{}, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return session.CheckpointRecord{}, err
	}

	record := input.Record
	sessionCP := input.SessionCP

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return session.CheckpointRecord{}, fmt.Errorf("checkpoint: begin create tx: %w", err)
	}
	defer rollbackTx(tx)

	// INSERT checkpoint_record (status = creating)
	_, err = tx.ExecContext(ctx, `
INSERT INTO checkpoint_records (
	id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		record.CheckpointID,
		record.WorkspaceKey,
		record.SessionID,
		record.RunID,
		record.Workdir,
		toUnixMillis(record.CreatedAt),
		string(record.Reason),
		record.CodeCheckpointRef,
		"", // session_checkpoint_ref filled below
		record.ResumeCheckpointRef,
		record.TranscriptRevision,
		boolToInt(record.Restorable),
		string(session.CheckpointStatusCreating),
	)
	if err != nil {
		return session.CheckpointRecord{}, fmt.Errorf("checkpoint: insert record %s: %w", record.CheckpointID, err)
	}

	// INSERT session_checkpoint
	_, err = tx.ExecContext(ctx, `
INSERT INTO session_checkpoints (id, session_id, head_json, messages_json, created_at_ms)
VALUES (?, ?, ?, ?, ?)
`,
		sessionCP.ID,
		sessionCP.SessionID,
		sessionCP.HeadJSON,
		sessionCP.MessagesJSON,
		toUnixMillis(sessionCP.CreatedAt),
	)
	if err != nil {
		return session.CheckpointRecord{}, fmt.Errorf("checkpoint: insert session cp %s: %w", sessionCP.ID, err)
	}

	// UPDATE checkpoint_record: set session_checkpoint_ref + status = available
	_, err = tx.ExecContext(ctx, `
UPDATE checkpoint_records
SET session_checkpoint_ref = ?, status = ?
WHERE id = ?
`,
		sessionCP.ID,
		string(session.CheckpointStatusAvailable),
		record.CheckpointID,
	)
	if err != nil {
		return session.CheckpointRecord{}, fmt.Errorf("checkpoint: update record %s: %w", record.CheckpointID, err)
	}

	if err := tx.Commit(); err != nil {
		return session.CheckpointRecord{}, fmt.Errorf("checkpoint: commit create %s: %w", record.CheckpointID, err)
	}

	record.SessionCheckpointRef = sessionCP.ID
	record.Status = session.CheckpointStatusAvailable
	return record, nil
}

// ListCheckpoints 查询指定会话的 checkpoint 记录列表。
func (s *SQLiteCheckpointStore) ListCheckpoints(ctx context.Context, sessionID string, opts ListCheckpointOpts) ([]session.CheckpointRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, err
	}

	query := `
SELECT id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
FROM checkpoint_records
WHERE session_id = ?
`
	args := []any{sessionID}
	if opts.RestorableOnly {
		query += ` AND restorable = 1 AND status = ?`
		args = append(args, string(session.CheckpointStatusAvailable))
	}
	query += ` ORDER BY created_at_ms DESC`
	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list checkpoints for %s: %w", sessionID, err)
	}
	defer rows.Close()

	var records []session.CheckpointRecord
	for rows.Next() {
		var r session.CheckpointRecord
		var createdAtMS int64
		var reason, status string
		if err := rows.Scan(
			&r.CheckpointID, &r.WorkspaceKey, &r.SessionID, &r.RunID, &r.Workdir, &createdAtMS,
			&reason, &r.CodeCheckpointRef, &r.SessionCheckpointRef, &r.ResumeCheckpointRef,
			&r.TranscriptRevision, &r.Restorable, &status,
		); err != nil {
			return nil, fmt.Errorf("checkpoint: scan record: %w", err)
		}
		r.CreatedAt = fromUnixMillis(createdAtMS)
		r.Reason = session.CheckpointReason(reason)
		r.Status = session.CheckpointStatus(status)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoint: iterate records: %w", err)
	}
	return records, nil
}

// GetCheckpoint 查询单条 checkpoint record 及其关联的 session checkpoint。
func (s *SQLiteCheckpointStore) GetCheckpoint(ctx context.Context, checkpointID string) (session.CheckpointRecord, *session.SessionCheckpoint, error) {
	if err := ctx.Err(); err != nil {
		return session.CheckpointRecord{}, nil, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return session.CheckpointRecord{}, nil, err
	}

	var r session.CheckpointRecord
	var createdAtMS int64
	var reason, status string
	err = db.QueryRowContext(ctx, `
SELECT id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
FROM checkpoint_records
WHERE id = ?
`, checkpointID).Scan(
		&r.CheckpointID, &r.WorkspaceKey, &r.SessionID, &r.RunID, &r.Workdir, &createdAtMS,
		&reason, &r.CodeCheckpointRef, &r.SessionCheckpointRef, &r.ResumeCheckpointRef,
		&r.TranscriptRevision, &r.Restorable, &status,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return session.CheckpointRecord{}, nil, fmt.Errorf("checkpoint: record %s not found", checkpointID)
		}
		return session.CheckpointRecord{}, nil, fmt.Errorf("checkpoint: query record %s: %w", checkpointID, err)
	}
	r.CreatedAt = fromUnixMillis(createdAtMS)
	r.Reason = session.CheckpointReason(reason)
	r.Status = session.CheckpointStatus(status)

	if r.SessionCheckpointRef == "" {
		return r, nil, nil
	}

	var sc session.SessionCheckpoint
	var scCreatedAtMS int64
	err = db.QueryRowContext(ctx, `
SELECT id, session_id, head_json, messages_json, created_at_ms
FROM session_checkpoints
WHERE id = ?
`, r.SessionCheckpointRef).Scan(
		&sc.ID, &sc.SessionID, &sc.HeadJSON, &sc.MessagesJSON, &scCreatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, nil, nil
		}
		return session.CheckpointRecord{}, nil, fmt.Errorf("checkpoint: query session cp %s: %w", r.SessionCheckpointRef, err)
	}
	sc.CreatedAt = fromUnixMillis(scCreatedAtMS)
	return r, &sc, nil
}

// UpdateCheckpointStatus 更新 checkpoint 的生命周期状态。
func (s *SQLiteCheckpointStore) UpdateCheckpointStatus(ctx context.Context, checkpointID string, status session.CheckpointStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	result, err := db.ExecContext(ctx, `UPDATE checkpoint_records SET status = ? WHERE id = ?`, string(status), checkpointID)
	if err != nil {
		return fmt.Errorf("checkpoint: update status %s: %w", checkpointID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checkpoint: inspect rows affected for %s: %w", checkpointID, err)
	}
	if affected == 0 {
		return fmt.Errorf("checkpoint: record %s not found", checkpointID)
	}
	return nil
}

// GetLatestResumeCheckpoint 查询指定会话最新的 resume checkpoint。
func (s *SQLiteCheckpointStore) GetLatestResumeCheckpoint(ctx context.Context, sessionID string) (*session.ResumeCheckpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, err
	}

	var rc session.ResumeCheckpoint
	var updatedAtMS int64
	err = db.QueryRowContext(ctx, `
SELECT id, workspace_key, run_id, session_id, turn, phase, completion_state, transcript_revision, updated_at_ms
FROM resume_checkpoints
WHERE session_id = ?
ORDER BY updated_at_ms DESC
LIMIT 1
`, sessionID).Scan(
		&rc.ID, &rc.WorkspaceKey, &rc.RunID, &rc.SessionID,
		&rc.Turn, &rc.Phase, &rc.CompletionState,
		&rc.TranscriptRevision, &updatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("checkpoint: query resume checkpoint for %s: %w", sessionID, err)
	}
	rc.UpdatedAt = fromUnixMillis(updatedAtMS)
	return &rc, nil
}

// RestoreCheckpoint 在单一事务内恢复会话消息和头状态，并批量更新 checkpoint 状态。
func (s *SQLiteCheckpointStore) RestoreCheckpoint(ctx context.Context, input RestoreCheckpointInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkpoint: begin restore tx: %w", err)
	}
	defer rollbackTx(tx)

	// DELETE existing messages
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, input.SessionID); err != nil {
		return fmt.Errorf("checkpoint: delete messages %s: %w", input.SessionID, err)
	}

	// Re-insert messages
	now := input.UpdatedAt
	for i, msg := range input.Messages {
		seq := i + 1
		toolCallsJSON := "[]"
		if len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				toolCallsJSON = string(data)
			}
		}
		toolMetadataJSON := "{}"
		if msg.ToolMetadata != nil {
			if data, err := json.Marshal(msg.ToolMetadata); err == nil {
				toolMetadataJSON = string(data)
			}
		}
		partsJSON := "[]"
		if len(msg.Parts) > 0 {
			if data, err := json.Marshal(msg.Parts); err == nil {
				partsJSON = string(data)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (session_id, seq, role, parts_json, tool_calls_json, tool_call_id, is_error, tool_metadata_json, created_at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.SessionID, seq, msg.Role, partsJSON, toolCallsJSON, msg.ToolCallID, boolToInt(msg.IsError), toolMetadataJSON, toUnixMillis(now),
		); err != nil {
			return fmt.Errorf("checkpoint: insert message %s/%d: %w", input.SessionID, seq, err)
		}
	}

	// UPDATE session head
	h := input.Head
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at_ms=?, provider=?, model=?, workdir=?, task_state_json=?, todos_json=?, activated_skills_json=?, token_input_total=?, token_output_total=?, has_unknown_usage=?, agent_mode=?, current_plan_json=?, last_full_plan_revision=?, plan_approval_pending_full_align=?, plan_completion_pending_full_review=?, plan_context_dirty=?, plan_restore_pending_align=?, last_seq=?, message_count=? WHERE id=?`,
		toUnixMillis(input.UpdatedAt), h.Provider, h.Model, h.Workdir,
		marshalHeadField(h.TaskState), marshalHeadField(h.Todos), marshalHeadField(h.ActivatedSkills),
		h.TokenInputTotal, h.TokenOutputTotal, boolToInt(h.HasUnknownUsage), h.AgentMode,
		marshalPlanField(h.CurrentPlan), h.LastFullPlanRevision,
		boolToInt(h.PlanApprovalPendingFullAlign), boolToInt(h.PlanCompletionPendingFullReview),
		boolToInt(h.PlanContextDirty), boolToInt(h.PlanRestorePendingAlign),
		len(input.Messages), len(input.Messages), input.SessionID,
	)
	if err != nil {
		return fmt.Errorf("checkpoint: update session %s: %w", input.SessionID, err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("checkpoint: session %s not found", input.SessionID)
	}

	// Mark available
	for _, id := range input.MarkAvailableIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE checkpoint_records SET status=? WHERE id=?`, string(session.CheckpointStatusAvailable), id); err != nil {
			return fmt.Errorf("checkpoint: mark available %s: %w", id, err)
		}
	}

	// Mark restored
	for _, id := range input.MarkRestoredIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE checkpoint_records SET status=? WHERE id=?`, string(session.CheckpointStatusRestored), id); err != nil {
			return fmt.Errorf("checkpoint: mark restored %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoint: commit restore %s: %w", input.SessionID, err)
	}
	return nil
}

func marshalHeadField(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

// marshalPlanField 将可选计划字段编码为 session 兼容的持久化格式，nil 计划统一写为空串。
func marshalPlanField(value any) string {
	if value == nil {
		return ""
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// SetResumeCheckpoint 写入或更新 ResumeCheckpoint（一个 session 只保留一条）。
func (s *SQLiteCheckpointStore) SetResumeCheckpoint(ctx context.Context, rc session.ResumeCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkpoint: begin set resume tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM resume_checkpoints WHERE session_id=?`, rc.SessionID); err != nil {
		return fmt.Errorf("checkpoint: delete old resume cp %s: %w", rc.SessionID, err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO resume_checkpoints (id, workspace_key, run_id, session_id, turn, phase, completion_state, transcript_revision, updated_at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rc.ID, rc.WorkspaceKey, rc.RunID, rc.SessionID, rc.Turn, rc.Phase, rc.CompletionState, rc.TranscriptRevision, toUnixMillis(rc.UpdatedAt),
	); err != nil {
		return fmt.Errorf("checkpoint: insert resume cp %s: %w", rc.SessionID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoint: commit set resume cp %s: %w", rc.SessionID, err)
	}
	return nil
}

// PruneExpiredCheckpoints 窗口裁剪，将超出 maxAutoKeep 的旧自动 checkpoint 标记为 pruned。
func (s *SQLiteCheckpointStore) PruneExpiredCheckpoints(ctx context.Context, sessionID string, maxAutoKeep int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return 0, err
	}

	rows, err := db.QueryContext(ctx, `SELECT id, session_checkpoint_ref FROM checkpoint_records WHERE session_id=? AND restorable=1 AND status=? AND reason NOT IN (?, ?) ORDER BY created_at_ms DESC`,
		sessionID, string(session.CheckpointStatusAvailable), string(session.CheckpointReasonManual), string(session.CheckpointReasonGuard),
	)
	if err != nil {
		return 0, fmt.Errorf("checkpoint: query prune candidates %s: %w", sessionID, err)
	}
	defer rows.Close()

	type pruneTarget struct {
		ID           string
		SessionCPRef string
	}
	var targets []pruneTarget
	idx := 0
	for rows.Next() {
		var t pruneTarget
		if err := rows.Scan(&t.ID, &t.SessionCPRef); err != nil {
			return 0, fmt.Errorf("checkpoint: scan prune candidate: %w", err)
		}
		if idx >= maxAutoKeep {
			targets = append(targets, t)
		}
		idx++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("checkpoint: iterate prune candidates: %w", err)
	}
	if len(targets) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("checkpoint: begin prune tx: %w", err)
	}
	defer rollbackTx(tx)

	for _, t := range targets {
		if _, err := tx.ExecContext(ctx, `UPDATE checkpoint_records SET restorable=0, status=? WHERE id=?`, string(session.CheckpointStatusPruned), t.ID); err != nil {
			return 0, fmt.Errorf("checkpoint: prune record %s: %w", t.ID, err)
		}
		if t.SessionCPRef != "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM session_checkpoints WHERE id=?`, t.SessionCPRef); err != nil {
				return 0, fmt.Errorf("checkpoint: delete session cp %s: %w", t.SessionCPRef, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("checkpoint: commit prune %s: %w", sessionID, err)
	}
	return len(targets), nil
}

// RepairCreatingCheckpoints 修复残留的 creating 状态 checkpoint。
func (s *SQLiteCheckpointStore) RepairCreatingCheckpoints(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return 0, err
	}

	rows, err := db.QueryContext(ctx, `SELECT id, session_checkpoint_ref FROM checkpoint_records WHERE status=?`, string(session.CheckpointStatusCreating))
	if err != nil {
		return 0, fmt.Errorf("checkpoint: query creating records: %w", err)
	}
	defer rows.Close()

	type repairTarget struct {
		ID           string
		SessionCPRef string
	}
	var targets []repairTarget
	for rows.Next() {
		var t repairTarget
		if err := rows.Scan(&t.ID, &t.SessionCPRef); err != nil {
			return 0, fmt.Errorf("checkpoint: scan creating record: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("checkpoint: iterate creating records: %w", err)
	}
	if len(targets) == 0 {
		return 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("checkpoint: begin repair tx: %w", err)
	}
	defer rollbackTx(tx)

	for _, t := range targets {
		if t.SessionCPRef != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE checkpoint_records SET status=? WHERE id=?`, string(session.CheckpointStatusAvailable), t.ID); err != nil {
				return 0, fmt.Errorf("checkpoint: repair available %s: %w", t.ID, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `DELETE FROM checkpoint_records WHERE id=?`, t.ID); err != nil {
				return 0, fmt.Errorf("checkpoint: delete orphan %s: %w", t.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("checkpoint: commit repair: %w", err)
	}
	return len(targets), nil
}

func toUnixMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func fromUnixMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
