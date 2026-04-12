package memo

import (
	"context"
	"time"

	providertypes "neo-code/internal/provider/types"
)

// Type 定义记忆条目的分类。
type Type string

const (
	// TypeUser 表示用户画像、偏好、专长类记忆。
	TypeUser Type = "user"
	// TypeFeedback 表示用户纠正与指导类记忆。
	TypeFeedback Type = "feedback"
	// TypeProject 表示项目事实、决策、进行中工作类记忆。
	TypeProject Type = "project"
	// TypeReference 表示外部资源指针类记忆。
	TypeReference Type = "reference"
)

// SourceAutoExtract 表示记忆由提取器自动生成。
const SourceAutoExtract = "extractor_auto"

// SourceUserManual 表示记忆由用户手动创建。
const SourceUserManual = "user_manual"

// SourceToolInitiated 表示记忆由模型通过工具主动创建。
const SourceToolInitiated = "tool_initiated"

// Entry 表示单个持久化记忆条目。
type Entry struct {
	// ID 唯一标识，格式为 <type>_<timestamp_unix>_<short_hash>。
	ID string
	// Type 记忆分类。
	Type Type
	// Title 索引行显示内容，约 150 字符以内。
	Title string
	// Content 详细内容，存入 topic 文件。
	Content string
	// Keywords 关键词列表，用于搜索和去重。
	Keywords []string
	// Source 记忆来源：extractor_auto / user_manual / tool_initiated。
	Source string
	// TopicFile 对应的 topic 文件名（如 user_profile.md）。
	TopicFile string
	// CreatedAt 创建时间。
	CreatedAt time.Time
	// UpdatedAt 最后更新时间。
	UpdatedAt time.Time
}

// Index 表示 MEMO.md 索引文件的内存模型。
type Index struct {
	// Entries 索引中的所有记忆条目。
	Entries []Entry
	// UpdatedAt 索引最后更新时间。
	UpdatedAt time.Time
}

// Store 定义记忆持久化的最小抽象。
type Store interface {
	// LoadIndex 加载工作区级别的 MEMO.md 索引。
	LoadIndex(ctx context.Context) (*Index, error)
	// SaveIndex 持久化索引到 MEMO.md。
	SaveIndex(ctx context.Context, index *Index) error
	// LoadTopic 加载指定 topic 文件的内容。
	LoadTopic(ctx context.Context, filename string) (string, error)
	// SaveTopic 持久化 topic 文件内容。
	SaveTopic(ctx context.Context, filename string, content string) error
	// DeleteTopic 删除指定 topic 文件。
	DeleteTopic(ctx context.Context, filename string) error
	// ListTopics 列出所有 topic 文件名。
	ListTopics(ctx context.Context) ([]string, error)
}

// Extractor 定义从对话中提取记忆的最小能力。
type Extractor interface {
	// Extract 从对话消息中提取值得记忆的信息。
	Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error)
}

// ValidTypes 返回所有合法的记忆类型列表。
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
