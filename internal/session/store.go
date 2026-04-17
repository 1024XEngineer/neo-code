package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const (
	sessionDatabaseFileName = "session.db"
	assetsDirName           = "assets"
	sqliteSchemaVersion     = 1
)

var storageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// Session 表示单个会话的运行态与持久化聚合模型。
type Session struct {
	ID               string
	Title            string
	Provider         string
	Model            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Workdir          string
	TaskState        TaskState
	ActivatedSkills  []SkillActivation
	TodoVersion      int
	Todos            []TodoItem
	Messages         []providertypes.Message
	TokenInputTotal  int
	TokenOutputTotal int
}

// Summary 表示会话列表视图需要的轻量摘要。
type Summary struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateSessionInput 描述新建空会话头时需要写入的字段。
type CreateSessionInput struct {
	ID               string
	Title            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Provider         string
	Model            string
	Workdir          string
	TaskState        TaskState
	ActivatedSkills  []SkillActivation
	Todos            []TodoItem
	TokenInputTotal  int
	TokenOutputTotal int
}

// AppendMessagesInput 描述一次原子追加消息及会话头增量更新。
type AppendMessagesInput struct {
	SessionID        string
	Messages         []providertypes.Message
	UpdatedAt        time.Time
	Provider         string
	Model            string
	Workdir          string
	TokenInputDelta  int
	TokenOutputDelta int
}

// UpdateSessionStateInput 描述一次只更新会话头状态的写入。
type UpdateSessionStateInput struct {
	SessionID        string
	Title            string
	UpdatedAt        time.Time
	Provider         string
	Model            string
	Workdir          string
	TaskState        TaskState
	ActivatedSkills  []SkillActivation
	Todos            []TodoItem
	TokenInputTotal  int
	TokenOutputTotal int
}

// ReplaceTranscriptInput 描述 compact 后整段 transcript 的原子替换。
type ReplaceTranscriptInput struct {
	SessionID        string
	Messages         []providertypes.Message
	UpdatedAt        time.Time
	Provider         string
	Model            string
	Workdir          string
	TaskState        TaskState
	ActivatedSkills  []SkillActivation
	Todos            []TodoItem
	TokenInputTotal  int
	TokenOutputTotal int
}

// Store 定义会话持久化的意图型接口。
type Store interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (Session, error)
	LoadSession(ctx context.Context, id string) (Session, error)
	ListSummaries(ctx context.Context) ([]Summary, error)
	AppendMessages(ctx context.Context, input AppendMessagesInput) error
	UpdateSessionState(ctx context.Context, input UpdateSessionStateInput) error
	ReplaceTranscript(ctx context.Context, input ReplaceTranscriptInput) error
}

// NewSQLiteStore 创建基于 SQLite 的会话存储实现。
func NewSQLiteStore(baseDir string, workspaceRoot string) *SQLiteStore {
	return &SQLiteStore{
		projectDir: projectDirectory(baseDir, workspaceRoot),
		assetsDir:  assetsDirectory(baseDir, workspaceRoot),
		dbPath:     databasePath(baseDir, workspaceRoot),
	}
}

// NewStore 返回默认会话存储实现。
func NewStore(baseDir string, workspaceRoot string) *SQLiteStore {
	return NewSQLiteStore(baseDir, workspaceRoot)
}

// New 创建一个默认标题策略的新会话对象。
func New(title string) Session {
	return NewWithWorkdir(title, "")
}

// NewWithWorkdir 创建一个带运行目录的会话对象。
func NewWithWorkdir(title string, workdir string) Session {
	now := time.Now()
	return Session{
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

// sanitizeTitle 规范化会话标题，保证空标题和超长标题都有稳定表现。
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

// validateStorageID 校验会话和附件 ID，避免路径穿越与非法文件名。
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
