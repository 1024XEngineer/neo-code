package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	providertypes "neo-code/internal/provider/types"
)

type sqliteSessionRow struct {
	ID               string
	Title            string
	Provider         string
	Model            string
	CreatedAtMS      int64
	UpdatedAtMS      int64
	Workdir          string
	TaskStateJSON    string
	ActivatedJSON    string
	TodosJSON        string
	TokenInputTotal  int
	TokenOutputTotal int
}

type sqliteMessageRow struct {
	Role             string
	PartsJSON        string
	ToolCallsJSON    string
	ToolCallID       string
	IsError          bool
	ToolMetadataJSON string
}

// SQLiteStore 使用单个工作区级 SQLite 数据库持久化会话。
type SQLiteStore struct {
	projectDir string
	assetsDir  string
	dbPath     string

	once    sync.Once
	initErr error
	db      *sql.DB
}

// Close 释放数据库连接，供测试和上层生命周期管理复用。
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CreateSession 创建并持久化一个新的空会话头。
func (s *SQLiteStore) CreateSession(ctx context.Context, input CreateSessionInput) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return Session{}, err
	}

	session, err := normalizeCreateSessionInput(input)
	if err != nil {
		return Session{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("session: begin create session tx: %w", err)
	}
	defer rollbackTx(tx)

	_, err = tx.ExecContext(ctx, `
INSERT INTO sessions (
	id, title, created_at_ms, updated_at_ms, provider, model, workdir,
	task_state_json, todos_json, activated_skills_json,
	token_input_total, token_output_total, last_seq, message_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)
`,
		session.ID,
		session.Title,
		toUnixMillis(session.CreatedAt),
		toUnixMillis(session.UpdatedAt),
		session.Provider,
		session.Model,
		session.Workdir,
		mustJSONString(session.TaskState),
		mustJSONString(session.Todos),
		mustJSONString(session.ActivatedSkills),
		session.TokenInputTotal,
		session.TokenOutputTotal,
	)
	if err != nil {
		return Session{}, fmt.Errorf("session: insert session %s: %w", session.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("session: commit create session %s: %w", session.ID, err)
	}

	return cloneSessionValue(session), nil
}

// LoadSession 加载完整会话头和全部消息。
func (s *SQLiteStore) LoadSession(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if err := validateStorageID("session id", id); err != nil {
		return Session{}, fmt.Errorf("session: %w", err)
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return Session{}, err
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Session{}, fmt.Errorf("session: begin load session tx: %w", err)
	}
	defer rollbackTx(tx)

	row, err := loadSessionRow(ctx, tx, stringsTrimSpace(id))
	if err != nil {
		return Session{}, err
	}
	messages, err := loadMessages(ctx, tx, stringsTrimSpace(id))
	if err != nil {
		return Session{}, err
	}
	session, err := buildSessionFromRow(row, messages)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("session: commit load session %s: %w", id, err)
	}
	return session, nil
}

// ListSummaries 仅查询会话摘要元数据。
func (s *SQLiteStore) ListSummaries(ctx context.Context) ([]Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
SELECT id, title, created_at_ms, updated_at_ms
FROM sessions
ORDER BY updated_at_ms DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("session: list summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]Summary, 0)
	for rows.Next() {
		var summary Summary
		var createdAtMS int64
		var updatedAtMS int64
		if err := rows.Scan(&summary.ID, &summary.Title, &createdAtMS, &updatedAtMS); err != nil {
			return nil, fmt.Errorf("session: scan summary: %w", err)
		}
		summary.CreatedAt = fromUnixMillis(createdAtMS)
		summary.UpdatedAt = fromUnixMillis(updatedAtMS)
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate summaries: %w", err)
	}
	return summaries, nil
}

// AppendMessages 在单事务内追加消息并更新会话头增量字段。
func (s *SQLiteStore) AppendMessages(ctx context.Context, input AppendMessagesInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStorageID("session id", input.SessionID); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if len(input.Messages) == 0 {
		return errors.New("session: append messages input is empty")
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	normalizedMessages, err := normalizeMessages(input.Messages)
	if err != nil {
		return err
	}
	updatedAt := resolveUpdatedAt(input.UpdatedAt)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session: begin append messages tx: %w", err)
	}
	defer rollbackTx(tx)

	lastSeq, err := currentLastSeq(ctx, tx, input.SessionID)
	if err != nil {
		return err
	}

	for _, message := range normalizedMessages {
		lastSeq++
		if err := insertMessage(ctx, tx, input.SessionID, lastSeq, updatedAt, message); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
UPDATE sessions
SET updated_at_ms = ?,
	provider = ?,
	model = ?,
	workdir = ?,
	token_input_total = token_input_total + ?,
	token_output_total = token_output_total + ?,
	last_seq = ?,
	message_count = message_count + ?
WHERE id = ?
`,
		toUnixMillis(updatedAt),
		stringsTrimSpace(input.Provider),
		stringsTrimSpace(input.Model),
		stringsTrimSpace(input.Workdir),
		input.TokenInputDelta,
		input.TokenOutputDelta,
		lastSeq,
		len(normalizedMessages),
		input.SessionID,
	)
	if err != nil {
		return fmt.Errorf("session: update session after append %s: %w", input.SessionID, err)
	}
	if err := expectRowsAffected(result, input.SessionID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit append messages %s: %w", input.SessionID, err)
	}
	return nil
}

// UpdateSessionState 仅更新会话头字段，不写入消息。
func (s *SQLiteStore) UpdateSessionState(ctx context.Context, input UpdateSessionStateInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	row, err := normalizeUpdateSessionStateInput(input)
	if err != nil {
		return err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	result, err := db.ExecContext(ctx, `
UPDATE sessions
SET title = ?,
	updated_at_ms = ?,
	provider = ?,
	model = ?,
	workdir = ?,
	task_state_json = ?,
	todos_json = ?,
	activated_skills_json = ?,
	token_input_total = ?,
	token_output_total = ?
WHERE id = ?
`,
		row.Title,
		row.UpdatedAtMS,
		row.Provider,
		row.Model,
		row.Workdir,
		row.TaskStateJSON,
		row.TodosJSON,
		row.ActivatedJSON,
		row.TokenInputTotal,
		row.TokenOutputTotal,
		row.ID,
	)
	if err != nil {
		return fmt.Errorf("session: update session state %s: %w", row.ID, err)
	}
	return expectRowsAffected(result, row.ID)
}

// ReplaceTranscript 用于 compact 后整段 transcript 的原子替换。
func (s *SQLiteStore) ReplaceTranscript(ctx context.Context, input ReplaceTranscriptInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	row, messages, err := normalizeReplaceTranscriptInput(input)
	if err != nil {
		return err
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session: begin replace transcript tx: %w", err)
	}
	defer rollbackTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, row.ID); err != nil {
		return fmt.Errorf("session: delete transcript %s: %w", row.ID, err)
	}
	if _, err := currentLastSeq(ctx, tx, row.ID); err != nil {
		return err
	}

	lastSeq := 0
	for _, message := range messages {
		lastSeq++
		if err := insertMessage(ctx, tx, row.ID, lastSeq, fromUnixMillis(row.UpdatedAtMS), message); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
UPDATE sessions
SET updated_at_ms = ?,
	provider = ?,
	model = ?,
	workdir = ?,
	task_state_json = ?,
	todos_json = ?,
	activated_skills_json = ?,
	token_input_total = ?,
	token_output_total = ?,
	last_seq = ?,
	message_count = ?
WHERE id = ?
`,
		row.UpdatedAtMS,
		row.Provider,
		row.Model,
		row.Workdir,
		row.TaskStateJSON,
		row.TodosJSON,
		row.ActivatedJSON,
		row.TokenInputTotal,
		row.TokenOutputTotal,
		lastSeq,
		len(messages),
		row.ID,
	)
	if err != nil {
		return fmt.Errorf("session: update session during replace transcript %s: %w", row.ID, err)
	}
	if err := expectRowsAffected(result, row.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit replace transcript %s: %w", row.ID, err)
	}
	return nil
}

// SaveAsset 将附件二进制内容落盘，并将元数据写入数据库。
func (s *SQLiteStore) SaveAsset(ctx context.Context, sessionID string, r io.Reader, mimeType string) (AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}
	if r == nil {
		return AssetMeta{}, errors.New("session: asset reader is nil")
	}
	if err := validateStorageID("session id", sessionID); err != nil {
		return AssetMeta{}, fmt.Errorf("session: %w", err)
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return AssetMeta{}, err
	}

	meta, err := newAssetMeta(mimeType)
	if err != nil {
		return AssetMeta{}, err
	}

	assetDir := filepath.Join(s.assetsDir, sessionID)
	if err := ensurePathWithinBase(s.projectDir, assetDir); err != nil {
		return AssetMeta{}, fmt.Errorf("session: resolve assets dir path: %w", err)
	}
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return AssetMeta{}, fmt.Errorf("session: create assets dir: %w", err)
	}

	tempFile, tempPath, err := createTempFile(assetDir, "asset-*.tmp", "create temp asset")
	if err != nil {
		return AssetMeta{}, err
	}

	written, copyErr := io.Copy(tempFile, io.LimitReader(r, providertypes.MaxSessionAssetBytes+1))
	syncErr := tempFile.Sync()
	closeErr := tempFile.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, fmt.Errorf("session: write temp asset: %w", copyErr)
	}
	if written > providertypes.MaxSessionAssetBytes {
		_ = os.Remove(tempPath)
		return AssetMeta{}, fmt.Errorf("session: asset size exceeds %d bytes", providertypes.MaxSessionAssetBytes)
	}
	if syncErr != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, fmt.Errorf("session: sync temp asset: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, fmt.Errorf("session: close temp asset: %w", closeErr)
	}

	target := filepath.Join(assetDir, meta.ID+".bin")
	if err := ensurePathWithinBase(s.projectDir, target); err != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, fmt.Errorf("session: resolve asset file path: %w", err)
	}
	if err := replaceFileWithTemp(tempPath, target, "asset file"); err != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, err
	}

	meta.Size = written
	relativePath, err := filepath.Rel(s.projectDir, target)
	if err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, fmt.Errorf("session: compute relative asset path: %w", err)
	}

	result, err := db.ExecContext(ctx, `
INSERT INTO session_assets (id, session_id, mime_type, size_bytes, relative_path, created_at_ms)
VALUES (?, ?, ?, ?, ?, ?)
`,
		meta.ID,
		sessionID,
		meta.MimeType,
		meta.Size,
		filepath.ToSlash(relativePath),
		toUnixMillis(time.Now()),
	)
	if err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, fmt.Errorf("session: insert asset meta %s: %w", meta.ID, err)
	}
	if err := expectRowsAffected(result, sessionID); err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, err
	}
	return meta, nil
}

// Open 读取指定会话附件的二进制内容与元数据。
func (s *SQLiteStore) Open(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, AssetMeta{}, err
	}
	meta, path, err := s.loadAssetMeta(ctx, sessionID, assetID)
	if err != nil {
		return nil, AssetMeta{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, AssetMeta{}, err
	}
	return file, meta, nil
}

// Stat 返回指定会话附件的元数据。
func (s *SQLiteStore) Stat(ctx context.Context, sessionID string, assetID string) (AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}
	meta, _, err := s.loadAssetMeta(ctx, sessionID, assetID)
	if err != nil {
		return AssetMeta{}, err
	}
	return meta, nil
}

// ensureDB 懒加载数据库并执行 schema 初始化。
func (s *SQLiteStore) ensureDB(ctx context.Context) (*sql.DB, error) {
	s.once.Do(func() {
		s.initErr = s.initialize(ctx)
	})
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.db, nil
}

// initialize 打开数据库、设置 PRAGMA 并初始化 schema。
func (s *SQLiteStore) initialize(ctx context.Context) error {
	if err := os.MkdirAll(s.projectDir, 0o755); err != nil {
		return fmt.Errorf("session: create project dir: %w", err)
	}
	if err := os.MkdirAll(s.assetsDir, 0o755); err != nil {
		return fmt.Errorf("session: create assets dir: %w", err)
	}

	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("session: open sqlite db: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := applySQLitePragmas(ctx, db); err != nil {
		_ = db.Close()
		return err
	}
	if err := initializeSQLiteSchema(ctx, db); err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	return nil
}

// loadAssetMeta 查询附件元数据并解析绝对路径。
func (s *SQLiteStore) loadAssetMeta(ctx context.Context, sessionID string, assetID string) (AssetMeta, string, error) {
	if err := validateStorageID("session id", sessionID); err != nil {
		return AssetMeta{}, "", fmt.Errorf("session: %w", err)
	}
	if err := validateStorageID("asset id", assetID); err != nil {
		return AssetMeta{}, "", fmt.Errorf("session: %w", err)
	}
	db, err := s.ensureDB(ctx)
	if err != nil {
		return AssetMeta{}, "", err
	}

	var meta AssetMeta
	var relativePath string
	err = db.QueryRowContext(ctx, `
SELECT mime_type, size_bytes, relative_path
FROM session_assets
WHERE session_id = ? AND id = ?
`,
		sessionID,
		assetID,
	).Scan(&meta.MimeType, &meta.Size, &relativePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AssetMeta{}, "", os.ErrNotExist
		}
		return AssetMeta{}, "", fmt.Errorf("session: query asset meta %s: %w", assetID, err)
	}
	meta.ID = assetID
	target := filepath.Join(s.projectDir, filepath.FromSlash(relativePath))
	if err := ensurePathWithinBase(s.projectDir, target); err != nil {
		return AssetMeta{}, "", fmt.Errorf("session: resolve asset file path: %w", err)
	}
	return meta, target, nil
}

// applySQLitePragmas 设置会话数据库的固定运行参数。
func applySQLitePragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
	}
	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("session: apply pragma %q: %w", pragma, err)
		}
	}
	return nil
}

// initializeSQLiteSchema 初始化数据库 schema，并拒绝未知版本。
func initializeSQLiteSchema(ctx context.Context, db *sql.DB) error {
	var userVersion int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("session: read sqlite user_version: %w", err)
	}
	if userVersion != 0 && userVersion != sqliteSchemaVersion {
		return fmt.Errorf("session: unsupported sqlite schema version %d", userVersion)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session: begin schema tx: %w", err)
	}
	defer rollbackTx(tx)

	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			workdir TEXT NOT NULL DEFAULT '',
			task_state_json TEXT NOT NULL,
			todos_json TEXT NOT NULL,
			activated_skills_json TEXT NOT NULL,
			token_input_total INTEGER NOT NULL DEFAULT 0,
			token_output_total INTEGER NOT NULL DEFAULT 0,
			last_seq INTEGER NOT NULL DEFAULT 0,
			message_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			parts_json TEXT NOT NULL,
			tool_calls_json TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			is_error INTEGER NOT NULL DEFAULT 0,
			tool_metadata_json TEXT NOT NULL DEFAULT '',
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY(session_id, seq),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS session_assets (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at_ms DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_seq_desc ON messages(session_id, seq DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_session_id ON session_assets(session_id)`,
		fmt.Sprintf(`PRAGMA user_version=%d`, sqliteSchemaVersion),
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("session: apply schema statement: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: commit schema tx: %w", err)
	}
	return nil
}

// normalizeCreateSessionInput 规范化创建会话输入并生成最终会话头。
func normalizeCreateSessionInput(input CreateSessionInput) (Session, error) {
	session := Session{
		ID:               stringsTrimSpace(input.ID),
		Title:            sanitizeTitle(input.Title),
		Provider:         stringsTrimSpace(input.Provider),
		Model:            stringsTrimSpace(input.Model),
		CreatedAt:        input.CreatedAt,
		UpdatedAt:        input.UpdatedAt,
		Workdir:          stringsTrimSpace(input.Workdir),
		TaskState:        normalizeAndClampTaskState(input.TaskState),
		ActivatedSkills:  normalizeSkillActivations(input.ActivatedSkills),
		TokenInputTotal:  input.TokenInputTotal,
		TokenOutputTotal: input.TokenOutputTotal,
	}
	if session.ID == "" {
		session.ID = NewID("session")
	}
	if err := validateStorageID("session id", session.ID); err != nil {
		return Session{}, fmt.Errorf("session: %w", err)
	}
	now := time.Now()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.CreatedAt
	}
	todos, err := normalizeAndValidateTodos(input.Todos)
	if err != nil {
		return Session{}, err
	}
	session.Todos = todos
	if len(session.Todos) > 0 {
		session.TodoVersion = CurrentTodoVersion
	}
	return session, nil
}

// normalizeUpdateSessionStateInput 规范化会话头更新输入。
func normalizeUpdateSessionStateInput(input UpdateSessionStateInput) (sqliteSessionRow, error) {
	if err := validateStorageID("session id", input.SessionID); err != nil {
		return sqliteSessionRow{}, fmt.Errorf("session: %w", err)
	}
	todos, err := normalizeAndValidateTodos(input.Todos)
	if err != nil {
		return sqliteSessionRow{}, err
	}
	return sqliteSessionRow{
		ID:               stringsTrimSpace(input.SessionID),
		Title:            sanitizeTitle(input.Title),
		Provider:         stringsTrimSpace(input.Provider),
		Model:            stringsTrimSpace(input.Model),
		UpdatedAtMS:      toUnixMillis(resolveUpdatedAt(input.UpdatedAt)),
		Workdir:          stringsTrimSpace(input.Workdir),
		TaskStateJSON:    mustJSONString(normalizeAndClampTaskState(input.TaskState)),
		TodosJSON:        mustJSONString(todos),
		ActivatedJSON:    mustJSONString(normalizeSkillActivations(input.ActivatedSkills)),
		TokenInputTotal:  input.TokenInputTotal,
		TokenOutputTotal: input.TokenOutputTotal,
	}, nil
}

// normalizeReplaceTranscriptInput 规范化 compact 后的 transcript 替换输入。
func normalizeReplaceTranscriptInput(input ReplaceTranscriptInput) (sqliteSessionRow, []providertypes.Message, error) {
	row, err := normalizeUpdateSessionStateInput(UpdateSessionStateInput{
		SessionID:        input.SessionID,
		Title:            "",
		UpdatedAt:        input.UpdatedAt,
		Provider:         input.Provider,
		Model:            input.Model,
		Workdir:          input.Workdir,
		TaskState:        input.TaskState,
		ActivatedSkills:  input.ActivatedSkills,
		Todos:            input.Todos,
		TokenInputTotal:  input.TokenInputTotal,
		TokenOutputTotal: input.TokenOutputTotal,
	})
	if err != nil {
		return sqliteSessionRow{}, nil, err
	}
	messages, err := normalizeMessages(input.Messages)
	if err != nil {
		return sqliteSessionRow{}, nil, err
	}
	return row, messages, nil
}

// normalizeMessages 校验并深拷贝待持久化消息。
func normalizeMessages(messages []providertypes.Message) ([]providertypes.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	cloned := make([]providertypes.Message, len(messages))
	for idx, message := range messages {
		if err := providertypes.ValidateParts(message.Parts); err != nil {
			return nil, fmt.Errorf("session: invalid message parts at index %d: %w", idx, err)
		}
		cloned[idx] = cloneMessage(message)
	}
	return cloned, nil
}

// loadSessionRow 查询单条会话头记录。
func loadSessionRow(ctx context.Context, tx *sql.Tx, sessionID string) (sqliteSessionRow, error) {
	var row sqliteSessionRow
	err := tx.QueryRowContext(ctx, `
SELECT id, title, provider, model, created_at_ms, updated_at_ms, workdir,
	task_state_json, activated_skills_json, todos_json, token_input_total, token_output_total
FROM sessions
WHERE id = ?
`,
		sessionID,
	).Scan(
		&row.ID,
		&row.Title,
		&row.Provider,
		&row.Model,
		&row.CreatedAtMS,
		&row.UpdatedAtMS,
		&row.Workdir,
		&row.TaskStateJSON,
		&row.ActivatedJSON,
		&row.TodosJSON,
		&row.TokenInputTotal,
		&row.TokenOutputTotal,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqliteSessionRow{}, os.ErrNotExist
		}
		return sqliteSessionRow{}, fmt.Errorf("session: query session %s: %w", sessionID, err)
	}
	return row, nil
}

// loadMessages 查询指定会话的全部消息并按顺序返回。
func loadMessages(ctx context.Context, tx *sql.Tx, sessionID string) ([]sqliteMessageRow, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT role, parts_json, tool_calls_json, tool_call_id, is_error, tool_metadata_json
FROM messages
WHERE session_id = ?
ORDER BY seq ASC
`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session: query messages for %s: %w", sessionID, err)
	}
	defer rows.Close()

	messages := make([]sqliteMessageRow, 0)
	for rows.Next() {
		var row sqliteMessageRow
		if err := rows.Scan(
			&row.Role,
			&row.PartsJSON,
			&row.ToolCallsJSON,
			&row.ToolCallID,
			&row.IsError,
			&row.ToolMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("session: scan message row: %w", err)
		}
		messages = append(messages, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate messages: %w", err)
	}
	return messages, nil
}

// buildSessionFromRow 由数据库行构建完整会话对象。
func buildSessionFromRow(row sqliteSessionRow, messages []sqliteMessageRow) (Session, error) {
	var taskState TaskState
	if err := json.Unmarshal([]byte(row.TaskStateJSON), &taskState); err != nil {
		return Session{}, fmt.Errorf("session: decode task_state for %s: %w", row.ID, err)
	}
	var activated []SkillActivation
	if err := json.Unmarshal([]byte(row.ActivatedJSON), &activated); err != nil {
		return Session{}, fmt.Errorf("session: decode activated_skills for %s: %w", row.ID, err)
	}
	var todos []TodoItem
	if err := json.Unmarshal([]byte(row.TodosJSON), &todos); err != nil {
		return Session{}, fmt.Errorf("session: decode todos for %s: %w", row.ID, err)
	}
	normalizedTodos, err := normalizeAndValidateTodos(todos)
	if err != nil {
		return Session{}, err
	}

	result := Session{
		ID:               row.ID,
		Title:            row.Title,
		Provider:         row.Provider,
		Model:            row.Model,
		CreatedAt:        fromUnixMillis(row.CreatedAtMS),
		UpdatedAt:        fromUnixMillis(row.UpdatedAtMS),
		Workdir:          row.Workdir,
		TaskState:        normalizeAndClampTaskState(taskState),
		ActivatedSkills:  normalizeSkillActivations(activated),
		Todos:            normalizedTodos,
		TokenInputTotal:  row.TokenInputTotal,
		TokenOutputTotal: row.TokenOutputTotal,
	}
	if len(result.Todos) > 0 {
		result.TodoVersion = CurrentTodoVersion
	}

	if len(messages) == 0 {
		return result, nil
	}
	result.Messages = make([]providertypes.Message, 0, len(messages))
	for _, messageRow := range messages {
		message, err := buildMessageFromRow(messageRow)
		if err != nil {
			return Session{}, err
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
}

// buildMessageFromRow 由数据库消息行恢复 provider 消息结构。
func buildMessageFromRow(row sqliteMessageRow) (providertypes.Message, error) {
	var parts []providertypes.ContentPart
	if err := json.Unmarshal([]byte(row.PartsJSON), &parts); err != nil {
		return providertypes.Message{}, fmt.Errorf("session: decode message parts: %w", err)
	}
	var toolCalls []providertypes.ToolCall
	if row.ToolCallsJSON != "" {
		if err := json.Unmarshal([]byte(row.ToolCallsJSON), &toolCalls); err != nil {
			return providertypes.Message{}, fmt.Errorf("session: decode tool calls: %w", err)
		}
	}
	var metadata map[string]string
	if row.ToolMetadataJSON != "" {
		if err := json.Unmarshal([]byte(row.ToolMetadataJSON), &metadata); err != nil {
			return providertypes.Message{}, fmt.Errorf("session: decode tool metadata: %w", err)
		}
	}
	return providertypes.Message{
		Role:         row.Role,
		Parts:        parts,
		ToolCalls:    toolCalls,
		ToolCallID:   row.ToolCallID,
		IsError:      row.IsError,
		ToolMetadata: metadata,
	}, nil
}

// currentLastSeq 读取当前会话的最后消息序号。
func currentLastSeq(ctx context.Context, tx *sql.Tx, sessionID string) (int, error) {
	var lastSeq int
	err := tx.QueryRowContext(ctx, `SELECT last_seq FROM sessions WHERE id = ?`, sessionID).Scan(&lastSeq)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, os.ErrNotExist
		}
		return 0, fmt.Errorf("session: query last_seq for %s: %w", sessionID, err)
	}
	return lastSeq, nil
}

// insertMessage 在事务内插入单条消息记录。
func insertMessage(
	ctx context.Context,
	tx *sql.Tx,
	sessionID string,
	seq int,
	createdAt time.Time,
	message providertypes.Message,
) error {
	result, err := tx.ExecContext(ctx, `
INSERT INTO messages (
	session_id, seq, role, parts_json, tool_calls_json, tool_call_id, is_error, tool_metadata_json, created_at_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		sessionID,
		seq,
		message.Role,
		mustJSONString(message.Parts),
		mustJSONString(message.ToolCalls),
		message.ToolCallID,
		boolToInt(message.IsError),
		mustJSONString(message.ToolMetadata),
		toUnixMillis(createdAt),
	)
	if err != nil {
		return fmt.Errorf("session: insert message %s/%d: %w", sessionID, seq, err)
	}
	return expectRowsAffected(result, sessionID)
}

// expectRowsAffected 校验写操作是否命中会话记录。
func expectRowsAffected(result sql.Result, sessionID string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: inspect rows affected for %s: %w", sessionID, err)
	}
	if rowsAffected == 0 {
		return os.ErrNotExist
	}
	return nil
}

// cloneMessage 深拷贝消息，避免共享底层切片和映射。
func cloneMessage(message providertypes.Message) providertypes.Message {
	next := message
	next.Parts = providertypes.CloneParts(message.Parts)
	next.ToolCalls = append([]providertypes.ToolCall(nil), message.ToolCalls...)
	if len(message.ToolMetadata) > 0 {
		next.ToolMetadata = make(map[string]string, len(message.ToolMetadata))
		for key, value := range message.ToolMetadata {
			next.ToolMetadata[key] = value
		}
	} else {
		next.ToolMetadata = nil
	}
	return next
}

// cloneSessionValue 深拷贝会话值，确保调用方拿到独立副本。
func cloneSessionValue(session Session) Session {
	cloned := session
	cloned.TaskState = session.TaskState.Clone()
	cloned.ActivatedSkills = cloneSkillActivations(session.ActivatedSkills)
	cloned.Todos = session.ListTodos()
	if len(session.Messages) > 0 {
		cloned.Messages = make([]providertypes.Message, len(session.Messages))
		for idx, message := range session.Messages {
			cloned.Messages[idx] = cloneMessage(message)
		}
	}
	return cloned
}

// mustJSONString 将值编码为 JSON 字符串；调用方已保证输入可序列化。
func mustJSONString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "[]"
	case map[string]string:
		if typed == nil {
			return "{}"
		}
	case []providertypes.ToolCall:
		if typed == nil {
			return "[]"
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// resolveUpdatedAt 统一为写入选择更新时间，缺省时使用当前时间。
func resolveUpdatedAt(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}

// toUnixMillis 将时间转换为 UTC 毫秒时间戳。
func toUnixMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

// fromUnixMillis 将毫秒时间戳还原为 UTC 时间。
func fromUnixMillis(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

// boolToInt 将布尔值映射为 SQLite 整数布尔位。
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// rollbackTx 在返回前吞掉回滚错误，避免覆盖主错误。
func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

// stringsTrimSpace 集中收敛字符串字段的空白规范化。
func stringsTrimSpace(value string) string {
	return strings.TrimSpace(value)
}
