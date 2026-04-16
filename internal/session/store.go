package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const sessionsDirName = "sessions"

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
	temp := target + ".tmp"
	if err := os.WriteFile(temp, payload, 0o644); err != nil {
		return fmt.Errorf("session: write temp session: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("session: replace session file: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("session: commit session file: %w", err)
	}

	return nil
}

// Load 读取并反序列化指定 ID 的会话文件。
func (s *JSONStore) Load(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath(id))
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data, readErr := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
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

// filePath 生成会话 ID 对应的 JSON 文件路径。
func (s *JSONStore) filePath(id string) string {
	return filepath.Join(s.baseDir, id+".json")
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
