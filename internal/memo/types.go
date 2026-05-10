package memo

import (
	"context"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
)

// Type 定义记忆条目的闭合分类。
type Type string

const (
	// TypeUser 表示用户画像、偏好和背景信息。
	TypeUser Type = "user"
	// TypeFeedback 表示用户对 Agent 行为的纠正与指导。
	TypeFeedback Type = "feedback"
	// TypeProject 表示项目事实、决策和进行中的工作。
	TypeProject Type = "project"
	// TypeReference 表示外部资源和信息入口。
	TypeReference Type = "reference"
)

const (
	// SourceAutoExtract 表示记忆由自动提取器生成。
	SourceAutoExtract = "extractor_auto"
	// SourceUserManual 表示记忆由用户手动创建。
	SourceUserManual = "user_manual"
	// SourceToolInitiated 表示记忆由模型主动调用工具创建。
	SourceToolInitiated = "tool_initiated"
)

// Entry 表示单个持久化记忆条目。
type Entry struct {
	ID        string
	Type      Type
	Title     string
	Content   string
	Keywords  []string
	Source    string
	TopicFile string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// evictionPriority 返回条目在裁剪时的优先级权重，值越大越不容易被裁剪。
// user > feedback > project > reference，且手动创建优先于自动提取。
func (e Entry) evictionPriority() int {
	base := 0
	switch e.Type {
	case TypeUser:
		base = 40
	case TypeFeedback:
		base = 30
	case TypeProject:
		base = 20
	case TypeReference:
		base = 10
	}
	if e.Source == SourceUserManual || e.Source == SourceToolInitiated {
		base += 50
	}
	return base
}

// Scope 表示 memo 的逻辑分层范围。
type Scope string

const (
	// ScopeAll 表示同时覆盖 user 与 project 两层。
	ScopeAll Scope = "all"
	// ScopeUser 表示仅 user 记忆层。
	ScopeUser Scope = "user"
	// ScopeProject 表示 project 记忆层，承载 feedback/project/reference。
	ScopeProject Scope = "project"
)

// Index 表示 MEMO.md 索引文件的内存模型。
type Index struct {
	Entries   []Entry
	UpdatedAt time.Time
}

// RecalledEntry 表示一次 recall 命中的结构化结果。
type RecalledEntry struct {
	Scope   Scope
	Entry   Entry
	Content string
}

// ExtractionAction 表示自动提取器对单条候选记忆的持久化决策。
type ExtractionAction string

const (
	// ExtractionActionCreate 表示新增一条记忆。
	ExtractionActionCreate ExtractionAction = "create"
	// ExtractionActionUpdate 表示合并更新一条既有自动提取记忆。
	ExtractionActionUpdate ExtractionAction = "update"
	// ExtractionActionSkip 表示跳过重复或不值得沉淀的内容。
	ExtractionActionSkip ExtractionAction = "skip"
)

// ExtractionCandidate 表示提供给模型做语义去重的既有记忆快照。
type ExtractionCandidate struct {
	Ref      string   `json:"ref"`
	Scope    Scope    `json:"scope"`
	Type     Type     `json:"type"`
	Source   string   `json:"source"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords,omitempty"`
	Content  string   `json:"content"`
}

// ExtractionDecision 表示模型针对新旧记忆关系返回的结构化决策。
type ExtractionDecision struct {
	Action ExtractionAction
	Ref    string
	Entry  Entry
}

// Store 定义记忆持久化的最小抽象。
type Store interface {
	LoadIndex(ctx context.Context, scope Scope) (*Index, error)
	SaveIndex(ctx context.Context, scope Scope, index *Index) error
	LoadTopic(ctx context.Context, scope Scope, filename string) (string, error)
	SaveTopic(ctx context.Context, scope Scope, filename string, content string) error
	DeleteTopic(ctx context.Context, scope Scope, filename string) error
	ListTopics(ctx context.Context, scope Scope) ([]string, error)
}

// Extractor 定义从对话消息中提取记忆的最小能力。
type Extractor interface {
	Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error)
}

// DecisionResolver 定义针对单条候选记忆做去重决策的最小能力。
type DecisionResolver interface {
	ResolveDecision(
		ctx context.Context,
		candidate Entry,
		existing []ExtractionCandidate,
	) (ExtractionDecision, error)
}

// DecisionExtractor 定义带既有记忆快照的语义提取能力。
type DecisionExtractor interface {
	ExtractDecisions(
		ctx context.Context,
		messages []providertypes.Message,
		existing []ExtractionCandidate,
	) ([]ExtractionDecision, error)
}

// TextGenerator 定义调用 LLM 生成文本的最小能力，用于记忆提取。
// 该接口隔离 provider 细节，避免 memo 包直接依赖 provider 基础设施。
type TextGenerator interface {
	Generate(ctx context.Context, prompt string, messages []providertypes.Message) (string, error)
}

// ValidTypes 返回所有合法的记忆类型。
func ValidTypes() []Type {
	return []Type{TypeUser, TypeFeedback, TypeProject, TypeReference}
}

// IsValidType 检查给定类型是否合法。
func IsValidType(t Type) bool {
	switch t {
	case TypeUser, TypeFeedback, TypeProject, TypeReference:
		return true
	default:
		return false
	}
}

// ParseType 将字符串解析为 Type，不合法时返回 false。
func ParseType(s string) (Type, bool) {
	t := Type(s)
	return t, IsValidType(t)
}

// NormalizeScope 将外部输入收敛为受支持的 memo 作用范围。
func NormalizeScope(scope Scope) Scope {
	switch Scope(strings.ToLower(string(scope))) {
	case ScopeUser:
		return ScopeUser
	case ScopeProject:
		return ScopeProject
	default:
		return ScopeAll
	}
}

// ScopeForType 返回给定类型固定落到的记忆分层。
func ScopeForType(t Type) Scope {
	switch t {
	case TypeUser:
		return ScopeUser
	case TypeFeedback, TypeProject, TypeReference:
		return ScopeProject
	default:
		return ScopeProject
	}
}

// supportedStorageScopes 返回当前实现实际落盘的所有 memo 分层。
func supportedStorageScopes() []Scope {
	return []Scope{ScopeUser, ScopeProject}
}
