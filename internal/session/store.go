package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const sessionsDirName = "sessions"

const (
	sessionFileName = "session.json"
	assetsDirName   = "assets"
)

var storageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// Session 表示单个会话的持久化模型，包含基础元数据与消息历史。
// Provider / Model 用于在 compact 等流程中优先复用会话最近一次成功运行的模型配置。
type Session struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	// Provider 记录最近一次成功运行会话时使用的 provider，用于 compact 优先复用历史配置。
	Provider string `json:"provider,omitempty"`
	// Model 记录最近一次成功运行会话时使用的 model，用于 compact 优先复用历史配置。
	Model            string                  `json:"model,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Workdir          string                  `json:"workdir,omitempty"`
	TaskState        TaskState               `json:"task_state"`
	ActivatedSkills  []SkillActivation       `json:"activated_skills,omitempty"`
	TodoVersion      int                     `json:"todo_version,omitempty"`
	Todos            []TodoItem              `json:"todos,omitempty"`
	Messages         []providertypes.Message `json:"messages"`
	TokenInputTotal  int                     `json:"token_input_total,omitempty"`
	TokenOutputTotal int                     `json:"token_output_total,omitempty"`
}

// Summary 表示会话列表视图所需的轻量摘要信息。
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store 定义会话持久化抽象。
type Store interface {
	Save(ctx context.Context, session *Session) error
	Load(ctx context.Context, id string) (Session, error)
	ListSummaries(ctx context.Context) ([]Summary, error)
	DeleteSession(ctx context.Context, id string) error
}

// JSONStore 是基于 JSON 文件的会话存储实现。
type JSONStore struct {
	mu      sync.RWMutex
	baseDir string
}

// NewJSONStore 创建 JSONStore，实际会话目录为 {baseDir}/sessions。
func NewJSONStore(baseDir string, workspaceRoot string) *JSONStore {
	return &JSONStore{
		baseDir: sessionDirectory(baseDir, workspaceRoot),
	}
}

// NewStore 返回默认会话存储实现（当前为 JSONStore）。
func NewStore(baseDir string, workspaceRoot string) *JSONStore {
	return NewJSONStore(baseDir, workspaceRoot)
}

// Save 持久化会话到 JSON 文件，采用临时文件 + 原子替换策略。
func (s *JSONStore) Save(ctx context.Context, session *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session == nil {
		return errors.New("session: session is nil")
	}
	if err := validateSessionSchema(*session); err != nil {
		return err
	}
	if err := validateStorageID("session id", session.ID); err != nil {
		return fmt.Errorf("session: %w", err)
	}

	session.TaskState = normalizeAndClampTaskState(session.TaskState)
	session.ActivatedSkills = normalizeSkillActivations(session.ActivatedSkills)
	normalizedTodos, err := normalizeAndValidateTodos(session.Todos)
	if err != nil {
		return err
	}
	session.Todos = normalizedTodos
	if len(session.Todos) > 0 && session.TodoVersion <= 0 {
		session.TodoVersion = CurrentTodoVersion
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return fmt.Errorf("session: create sessions dir: %w", err)
	}

	payload, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal session: %w", err)
	}
	payload = append(payload, '\n')

	target := s.filePath(session.ID)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return fmt.Errorf("session: resolve session file path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("session: create session dir: %w", err)
	}
	if err := writeFileAtomically(target, "session-*.tmp", payload, 0o644); err != nil {
		return err
	}

	return nil
}

// Load 读取并反序列化指定 ID 的会话文件。
func (s *JSONStore) Load(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	if err := validateStorageID("session id", id); err != nil {
		return Session{}, fmt.Errorf("session: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	target := s.filePath(id)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return Session{}, fmt.Errorf("session: resolve session file path: %w", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return Session{}, err
	}

	session, err := decodeStoredSession(data)
	if err != nil {
		return Session{}, fmt.Errorf("session: decode session %s: %w", id, err)
	}
	return session, nil
}

// ListSummaries 列出所有会话摘要，并按 UpdatedAt 倒序返回。
func (s *JSONStore) ListSummaries(ctx context.Context) ([]Summary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("session: create sessions dir: %w", err)
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("session: list sessions dir: %w", err)
	}

	summaries := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		target := filepath.Join(s.baseDir, entry.Name(), sessionFileName)
		if err := ensurePathWithinBase(s.baseDir, target); err != nil {
			continue
		}

		data, readErr := os.ReadFile(target)
		if readErr != nil {
			continue
		}

		summary, err := decodeStoredSummary(data)
		if err != nil {
			continue
		}
		if strings.TrimSpace(summary.ID) == "" {
			continue
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

// DeleteSession 删除指定会话目录及其附件，供创建后失败回滚等场景复用。
func (s *JSONStore) DeleteSession(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStorageID("session id", id); err != nil {
		return fmt.Errorf("session: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.sessionDir(id)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return fmt.Errorf("session: resolve session dir path: %w", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("session: delete session dir: %w", err)
	}
	return nil
}

// filePath 生成会话 ID 对应的 JSON 文件路径。
func (s *JSONStore) filePath(id string) string {
	return filepath.Join(s.sessionDir(id), sessionFileName)
}

// sessionDir 返回指定会话在当前工作区分桶下的目录路径。
func (s *JSONStore) sessionDir(id string) string {
	return filepath.Join(s.baseDir, id)
}

// assetsDir 返回指定会话附件目录路径。
func (s *JSONStore) assetsDir(sessionID string) string {
	return filepath.Join(s.sessionDir(sessionID), assetsDirName)
}

// assetPath 返回指定会话附件二进制文件路径。
func (s *JSONStore) assetPath(sessionID string, assetID string) string {
	return filepath.Join(s.assetsDir(sessionID), assetID+".bin")
}

// assetMetaPath 返回指定会话附件元数据文件路径。
func (s *JSONStore) assetMetaPath(sessionID string, assetID string) string {
	return filepath.Join(s.assetsDir(sessionID), assetID+".json")
}

// SaveAsset 将会话附件二进制内容写入当前工作区会话目录，并返回附件元数据。
func (s *JSONStore) SaveAsset(ctx context.Context, sessionID string, r io.Reader, mimeType string) (AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}
	if r == nil {
		return AssetMeta{}, errors.New("session: asset reader is nil")
	}
	if err := validateStorageID("session id", sessionID); err != nil {
		return AssetMeta{}, fmt.Errorf("session: %w", err)
	}

	meta, err := newAssetMeta(mimeType)
	if err != nil {
		return AssetMeta{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	assetDir := s.assetsDir(sessionID)
	if err := ensurePathWithinBase(s.baseDir, assetDir); err != nil {
		return AssetMeta{}, fmt.Errorf("session: resolve assets dir path: %w", err)
	}
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return AssetMeta{}, fmt.Errorf("session: create assets dir: %w", err)
	}

	target := s.assetPath(sessionID, meta.ID)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return AssetMeta{}, fmt.Errorf("session: resolve asset file path: %w", err)
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

	meta.Size = written
	if err := replaceFileWithTemp(tempPath, target, "asset file"); err != nil {
		_ = os.Remove(tempPath)
		return AssetMeta{}, err
	}

	metaData, err := encodeStoredAssetMeta(meta)
	if err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, err
	}
	metaTarget := s.assetMetaPath(sessionID, meta.ID)
	if err := ensurePathWithinBase(s.baseDir, metaTarget); err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, fmt.Errorf("session: resolve asset meta file path: %w", err)
	}
	if err := writeFileAtomically(metaTarget, "asset-meta-*.tmp", metaData, 0o644); err != nil {
		_ = os.Remove(target)
		return AssetMeta{}, err
	}

	return meta, nil
}

// Open 读取会话附件二进制内容并返回可关闭流与附件元数据。
func (s *JSONStore) Open(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, AssetMeta{}, err
	}
	if err := validateStorageID("session id", sessionID); err != nil {
		return nil, AssetMeta{}, fmt.Errorf("session: %w", err)
	}
	if err := validateStorageID("asset id", assetID); err != nil {
		return nil, AssetMeta{}, fmt.Errorf("session: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, err := s.statUnlocked(sessionID, assetID)
	if err != nil {
		return nil, AssetMeta{}, err
	}

	target := s.assetPath(sessionID, assetID)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return nil, AssetMeta{}, fmt.Errorf("session: resolve asset file path: %w", err)
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, AssetMeta{}, err
	}
	return file, meta, nil
}

// Stat 返回会话附件的元数据而不读取实际内容。
func (s *JSONStore) Stat(ctx context.Context, sessionID string, assetID string) (AssetMeta, error) {
	if err := ctx.Err(); err != nil {
		return AssetMeta{}, err
	}
	if err := validateStorageID("session id", sessionID); err != nil {
		return AssetMeta{}, fmt.Errorf("session: %w", err)
	}
	if err := validateStorageID("asset id", assetID); err != nil {
		return AssetMeta{}, fmt.Errorf("session: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.statUnlocked(sessionID, assetID)
}

// statUnlocked 在调用方已持有读锁时读取附件元数据，避免重复加锁导致死锁风险。
func (s *JSONStore) statUnlocked(sessionID string, assetID string) (AssetMeta, error) {
	target := s.assetMetaPath(sessionID, assetID)
	if err := ensurePathWithinBase(s.baseDir, target); err != nil {
		return AssetMeta{}, fmt.Errorf("session: resolve asset meta file path: %w", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return AssetMeta{}, err
	}
	return decodeStoredAssetMeta(data)
}

// New 创建一个默认标题策略的新会话对象。
func New(title string) Session {
	return NewWithWorkdir(title, "")
}

// NewWithWorkdir 创建一个包含运行目录的会话对象。
func NewWithWorkdir(title string, workdir string) Session {
	now := time.Now()
	return Session{
		SchemaVersion:   CurrentSchemaVersion,
		ID:              NewID("session"),
		Title:           sanitizeTitle(title),
		CreatedAt:       now,
		UpdatedAt:       now,
		Workdir:         strings.TrimSpace(workdir),
		TaskState:       TaskState{},
		ActivatedSkills: []SkillActivation{},
		Todos:           []TodoItem{},
		Messages:        []providertypes.Message{},
	}
}

// sanitizeTitle 规范化会话标题：去空白、空标题回退默认值、超长截断。
func sanitizeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "New Session"
	}
	runes := []rune(title)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return title
}

// validateSessionSchema 校验会话持久化版本，开发阶段只接受当前结构版本。
func validateSessionSchema(session Session) error {
	if session.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf(
			"session: unsupported schema_version %d, expected %d",
			session.SchemaVersion,
			CurrentSchemaVersion,
		)
	}
	return nil
}

// decodeStoredSession 严格校验持久化会话所需字段，并拒绝缺少 schema_version 或 task_state 的旧数据。
func decodeStoredSession(data []byte) (Session, error) {
	type storedSession struct {
		SchemaVersion   *int                    `json:"schema_version"`
		ID              string                  `json:"id"`
		Title           string                  `json:"title"`
		Provider        string                  `json:"provider,omitempty"`
		Model           string                  `json:"model,omitempty"`
		CreatedAt       time.Time               `json:"created_at"`
		UpdatedAt       time.Time               `json:"updated_at"`
		Workdir         string                  `json:"workdir,omitempty"`
		TaskState       *TaskState              `json:"task_state"`
		ActivatedSkills []SkillActivation       `json:"activated_skills,omitempty"`
		TodoVersion     *int                    `json:"todo_version,omitempty"`
		Todos           []TodoItem              `json:"todos,omitempty"`
		Messages        []providertypes.Message `json:"messages"`
		TokenInput      int                     `json:"token_input_total,omitempty"`
		TokenOutput     int                     `json:"token_output_total,omitempty"`
	}

	var stored storedSession
	if err := json.Unmarshal(data, &stored); err != nil {
		return Session{}, err
	}

	if stored.SchemaVersion == nil {
		return Session{}, errors.New("missing required field schema_version")
	}
	if stored.TaskState == nil {
		return Session{}, errors.New("missing required field task_state")
	}

	session := Session{
		SchemaVersion:    *stored.SchemaVersion,
		ID:               stored.ID,
		Title:            stored.Title,
		Provider:         stored.Provider,
		Model:            stored.Model,
		CreatedAt:        stored.CreatedAt,
		UpdatedAt:        stored.UpdatedAt,
		Workdir:          stored.Workdir,
		TaskState:        *stored.TaskState,
		ActivatedSkills:  stored.ActivatedSkills,
		TodoVersion:      0,
		Todos:            stored.Todos,
		Messages:         stored.Messages,
		TokenInputTotal:  stored.TokenInput,
		TokenOutputTotal: stored.TokenOutput,
	}
	if stored.TodoVersion != nil {
		session.TodoVersion = *stored.TodoVersion
	}
	if err := validateSessionSchema(session); err != nil {
		return Session{}, err
	}
	session.TaskState = normalizeAndClampTaskState(session.TaskState)
	session.ActivatedSkills = normalizeSkillActivations(session.ActivatedSkills)
	normalizedTodos, err := normalizeAndValidateTodos(session.Todos)
	if err != nil {
		return Session{}, err
	}
	session.Todos = normalizedTodos
	if len(session.Todos) > 0 && session.TodoVersion <= 0 {
		session.TodoVersion = CurrentTodoVersion
	}
	return session, nil
}

// normalizeAndClampTaskState 先规范化再限幅，保证持久化前后的 task_state 行为一致。
func normalizeAndClampTaskState(state TaskState) TaskState {
	return ClampTaskStateBoundaries(NormalizeTaskState(state))
}

// decodeStoredSummary 只解析会话列表所需的摘要元数据，避免为列表视图反序列化完整消息历史。
func decodeStoredSummary(data []byte) (Summary, error) {
	var stored struct {
		SchemaVersion *int            `json:"schema_version"`
		ID            string          `json:"id"`
		Title         string          `json:"title"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
		TaskState     json.RawMessage `json:"task_state"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return Summary{}, err
	}
	if stored.SchemaVersion == nil {
		return Summary{}, errors.New("missing required field schema_version")
	}
	if len(stored.TaskState) == 0 {
		return Summary{}, errors.New("missing required field task_state")
	}
	if err := validateSessionSchema(Session{SchemaVersion: *stored.SchemaVersion}); err != nil {
		return Summary{}, err
	}
	return Summary{
		ID:        stored.ID,
		Title:     stored.Title,
		CreatedAt: stored.CreatedAt,
		UpdatedAt: stored.UpdatedAt,
	}, nil
}

// validateStorageID 校验会话/附件 ID，避免路径穿越和非法文件名。
func validateStorageID(label string, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", label)
	}
	if !storageIDPattern.MatchString(trimmed) {
		return fmt.Errorf("%s %q contains unsupported characters", label, id)
	}
	return nil
}

// ensurePathWithinBase 校验目标路径在给定基目录内，作为 ID 白名单之外的二次路径约束。
func ensurePathWithinBase(baseDir string, target string) error {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base dir %q: %w", baseDir, err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target path %q: %w", target, err)
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return fmt.Errorf("compute relative path %q -> %q: %w", baseAbs, targetAbs, err)
	}
	if rel == "." {
		return nil
	}
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("target path %q escapes base dir %q", targetAbs, baseAbs)
	}
	return nil
}

// createTempFile 在目标目录创建唯一临时文件，避免固定 *.tmp 命名在并发场景下冲突。
func createTempFile(dir string, pattern string, op string) (*os.File, string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, "", fmt.Errorf("session: %s: %w", op, err)
	}
	if err := ensurePathWithinBase(dir, file.Name()); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, "", fmt.Errorf("session: %s: %w", op, err)
	}
	return file, file.Name(), nil
}

// replaceFileWithTemp 使用原子重命名替换目标文件，兼容 Windows 需要先删除旧文件的行为。
func replaceFileWithTemp(tempPath string, target string, label string) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("session: replace %s: %w", label, err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("session: commit %s: %w", label, err)
	}
	return nil
}

// writeFileAtomically 将字节数据写入唯一临时文件并原子替换目标文件，避免中间态文件暴露。
func writeFileAtomically(target string, tempPattern string, payload []byte, perm os.FileMode) error {
	dir := filepath.Dir(target)
	tempFile, tempPath, err := createTempFile(dir, tempPattern, "create temp file")
	if err != nil {
		return err
	}

	_, writeErr := tempFile.Write(payload)
	syncErr := tempFile.Sync()
	closeErr := tempFile.Close()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("session: write temp file: %w", writeErr)
	}
	if syncErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("session: sync temp file: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("session: close temp file: %w", closeErr)
	}
	if err := os.Chmod(tempPath, perm); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("session: chmod temp file: %w", err)
	}
	if err := replaceFileWithTemp(tempPath, target, "file"); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
