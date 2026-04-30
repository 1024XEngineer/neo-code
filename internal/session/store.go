package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
)

const (
	sessionDatabaseFileName = "session.db"
	assetsDirName           = "assets"
	sqliteSchemaVersion     = 5

	// MaxSessionMessages 定义单个会话允许持久化的最大消息数，超出时自动裁剪最旧消息。
	MaxSessionMessages = 8192

	// DefaultSessionMaxAge 定义默认的会话过期时间，30 天未更新的会话将被自动清理。
	DefaultSessionMaxAge = 30 * 24 * time.Hour
)

var storageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// ErrSessionNotFound 表示会话在存储层不存在，用于 runtime 做精确错误分流。
var ErrSessionNotFound = errors.New("session: session not found")

// ErrSessionAlreadyExists 表示会话在存储层已存在，用于 runtime 处理并发创建冲突。
var ErrSessionAlreadyExists = errors.New("session: session already exists")

// Session 表示单个会话的运行态与持久化聚合模型。
type Session struct {
	ID                              string
	Title                           string
	Provider                        string
	Model                           string
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
	Workdir                         string
	TaskState                       TaskState
	ActivatedSkills                 []SkillActivation
	TodoVersion                     int
	Todos                           []TodoItem
	Messages                        []providertypes.Message
	TokenInputTotal                 int
	TokenOutputTotal                int
	HasUnknownUsage                 bool
	AgentMode                       AgentMode
	CurrentPlan                     *PlanArtifact
	LastFullPlanRevision            int
	PlanApprovalPendingFullAlign    bool
	PlanCompletionPendingFullReview bool
	PlanContextDirty                bool
	PlanRestorePendingAlign         bool
}

// SessionHead 表示可独立持久化、可整体替换的会话头状态快照。
type SessionHead struct {
	Provider                        string
	Model                           string
	Workdir                         string
	TaskState                       TaskState
	ActivatedSkills                 []SkillActivation
	Todos                           []TodoItem
	TokenInputTotal                 int
	TokenOutputTotal                int
	HasUnknownUsage                 bool
	AgentMode                       AgentMode
	CurrentPlan                     *PlanArtifact
	LastFullPlanRevision            int
	PlanApprovalPendingFullAlign    bool
	PlanCompletionPendingFullReview bool
	PlanContextDirty                bool
	PlanRestorePendingAlign         bool
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
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Head      SessionHead
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
	HasUnknownUsage  bool
}

// UpdateSessionStateInput 描述一次只更新会话头状态的写入。
type UpdateSessionStateInput struct {
	SessionID string
	Title     string
	UpdatedAt time.Time
	Head      SessionHead
}

// UpdateSessionWorkdirInput 描述一次仅更新会话 workdir 的最小粒度写入。
type UpdateSessionWorkdirInput struct {
	SessionID string
	UpdatedAt time.Time
	Workdir   string
}

// ReplaceTranscriptInput 描述 compact 后整段 transcript 的原子替换。
type ReplaceTranscriptInput struct {
	SessionID string
	Messages  []providertypes.Message
	UpdatedAt time.Time
	Head      SessionHead
}

// Store 定义会话持久化的意图型接口。
type Store interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (Session, error)
	LoadSession(ctx context.Context, id string) (Session, error)
	ListSummaries(ctx context.Context) ([]Summary, error)
	AppendMessages(ctx context.Context, input AppendMessagesInput) error
	UpdateSessionWorkdir(ctx context.Context, input UpdateSessionWorkdirInput) error
	UpdateSessionState(ctx context.Context, input UpdateSessionStateInput) error
	ReplaceTranscript(ctx context.Context, input ReplaceTranscriptInput) error
	// CleanupExpiredSessions 删除超过指定时长未更新的会话及其关联数据，返回删除数量。
	CleanupExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error)
}

// NewSQLiteStore 创建基于 SQLite 的会话存储实现。
func NewSQLiteStore(baseDir string, workspaceRoot string) *SQLiteStore {
	return &SQLiteStore{
		projectDir:  projectDirectory(baseDir, workspaceRoot),
		assetsDir:   assetsDirectory(baseDir, workspaceRoot),
		dbPath:      databasePath(baseDir, workspaceRoot),
		assetPolicy: DefaultAssetPolicy(),
	}
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
		AgentMode:       AgentModeBuild,
	}
}

// HeadSnapshot 返回当前会话头状态的深拷贝，用于持久化输入与跨层传递。
func (s Session) HeadSnapshot() SessionHead {
	return SessionHead{
		Provider:                        strings.TrimSpace(s.Provider),
		Model:                           strings.TrimSpace(s.Model),
		Workdir:                         strings.TrimSpace(s.Workdir),
		TaskState:                       s.TaskState.Clone(),
		ActivatedSkills:                 cloneSkillActivations(s.ActivatedSkills),
		Todos:                           cloneTodoItems(s.Todos),
		TokenInputTotal:                 s.TokenInputTotal,
		TokenOutputTotal:                s.TokenOutputTotal,
		HasUnknownUsage:                 s.HasUnknownUsage,
		AgentMode:                       NormalizeAgentMode(s.AgentMode),
		CurrentPlan:                     s.CurrentPlan.Clone(),
		LastFullPlanRevision:            s.LastFullPlanRevision,
		PlanApprovalPendingFullAlign:    s.CurrentPlan != nil && s.PlanApprovalPendingFullAlign,
		PlanCompletionPendingFullReview: s.CurrentPlan != nil && s.PlanCompletionPendingFullReview,
		PlanContextDirty:                s.CurrentPlan != nil && s.PlanContextDirty,
		PlanRestorePendingAlign:         s.CurrentPlan != nil && s.PlanRestorePendingAlign,
	}
}

// clone 返回会话头状态的深拷贝，避免跨层共享底层切片。
func (h SessionHead) clone() SessionHead {
	return SessionHead{
		Provider:                        strings.TrimSpace(h.Provider),
		Model:                           strings.TrimSpace(h.Model),
		Workdir:                         strings.TrimSpace(h.Workdir),
		TaskState:                       h.TaskState.Clone(),
		ActivatedSkills:                 cloneSkillActivations(h.ActivatedSkills),
		Todos:                           cloneTodoItems(h.Todos),
		TokenInputTotal:                 h.TokenInputTotal,
		TokenOutputTotal:                h.TokenOutputTotal,
		HasUnknownUsage:                 h.HasUnknownUsage,
		AgentMode:                       NormalizeAgentMode(h.AgentMode),
		CurrentPlan:                     h.CurrentPlan.Clone(),
		LastFullPlanRevision:            h.LastFullPlanRevision,
		PlanApprovalPendingFullAlign:    h.CurrentPlan != nil && h.PlanApprovalPendingFullAlign,
		PlanCompletionPendingFullReview: h.CurrentPlan != nil && h.PlanCompletionPendingFullReview,
		PlanContextDirty:                h.CurrentPlan != nil && h.PlanContextDirty,
		PlanRestorePendingAlign:         h.CurrentPlan != nil && h.PlanRestorePendingAlign,
	}
}

// applyToSession 将会话头状态整体写回会话对象，避免调用方逐字段手动拼装。
func (h SessionHead) applyToSession(session *Session) {
	if session == nil {
		return
	}
	cloned := h.clone()
	session.Provider = cloned.Provider
	session.Model = cloned.Model
	session.Workdir = cloned.Workdir
	session.TaskState = cloned.TaskState
	session.ActivatedSkills = cloned.ActivatedSkills
	session.Todos = cloned.Todos
	session.TokenInputTotal = cloned.TokenInputTotal
	session.TokenOutputTotal = cloned.TokenOutputTotal
	session.HasUnknownUsage = cloned.HasUnknownUsage
	session.AgentMode = cloned.AgentMode
	session.CurrentPlan = cloned.CurrentPlan
	session.LastFullPlanRevision = cloned.LastFullPlanRevision
	session.PlanApprovalPendingFullAlign = cloned.PlanApprovalPendingFullAlign
	session.PlanCompletionPendingFullReview = cloned.PlanCompletionPendingFullReview
	session.PlanContextDirty = cloned.PlanContextDirty
	session.PlanRestorePendingAlign = cloned.PlanRestorePendingAlign
}

// cloneTodoItems 深拷贝 Todo 列表，避免会话头快照共享底层切片。
func cloneTodoItems(items []TodoItem) []TodoItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TodoItem, len(items))
	for idx, item := range items {
		cloned[idx] = item.Clone()
	}
	return cloned
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
