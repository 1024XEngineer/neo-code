package runtime

import (
	"strings"
	"time"
)

// AskSession 是 Ask 模式的轻量会话模型，仅保存问答上下文与技能激活状态。
type AskSession struct {
	ID        string
	Workdir   string
	Skills    []string
	Messages  []AskMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AskMessage 表示 Ask 会话中的一条消息。
type AskMessage struct {
	Role    string
	Content string
}

// Clone 返回 AskSession 的深拷贝，避免共享切片导致并发污染。
func (s AskSession) Clone() AskSession {
	cloned := AskSession{
		ID:        strings.TrimSpace(s.ID),
		Workdir:   strings.TrimSpace(s.Workdir),
		Skills:    append([]string(nil), s.Skills...),
		Messages:  append([]AskMessage(nil), s.Messages...),
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
	return cloned
}

// normalizeAskMessageRole 统一规范 Ask 消息角色，非法值回退为 assistant。
func normalizeAskMessageRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return "assistant"
	}
}

