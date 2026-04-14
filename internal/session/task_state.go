package session

import (
	"strings"
	"time"
)

const (
	// CurrentSchemaVersion 表示当前会话持久化结构的唯一合法版本。
	CurrentSchemaVersion = 1
)

// TaskState 表示会话级、可持久化的任务续航状态。
type TaskState struct {
	Goal            string    `json:"goal"`
	Progress        []string  `json:"progress"`
	OpenItems       []string  `json:"open_items"`
	NextStep        string    `json:"next_step"`
	Blockers        []string  `json:"blockers"`
	KeyArtifacts    []string  `json:"key_artifacts"`
	Decisions       []string  `json:"decisions"`
	UserConstraints []string  `json:"user_constraints"`
	LastUpdatedAt   time.Time `json:"last_updated_at"`
}

// Clone 返回任务状态的深拷贝，避免切片字段共享底层存储。
func (s TaskState) Clone() TaskState {
	s.Progress = append([]string(nil), s.Progress...)
	s.OpenItems = append([]string(nil), s.OpenItems...)
	s.Blockers = append([]string(nil), s.Blockers...)
	s.KeyArtifacts = append([]string(nil), s.KeyArtifacts...)
	s.Decisions = append([]string(nil), s.Decisions...)
	s.UserConstraints = append([]string(nil), s.UserConstraints...)
	return s
}

// Established 判断当前任务状态是否已经建立了可供续航使用的有效内容。
func (s TaskState) Established() bool {
	return strings.TrimSpace(s.Goal) != "" ||
		len(s.Progress) > 0 ||
		len(s.OpenItems) > 0 ||
		strings.TrimSpace(s.NextStep) != "" ||
		len(s.Blockers) > 0 ||
		len(s.KeyArtifacts) > 0 ||
		len(s.Decisions) > 0 ||
		len(s.UserConstraints) > 0
}

// NormalizeTaskState 统一收敛任务状态中的空白、重复项和零散文本格式。
func NormalizeTaskState(state TaskState) TaskState {
	state.Goal = strings.TrimSpace(state.Goal)
	state.NextStep = strings.TrimSpace(state.NextStep)
	state.Progress = normalizeTaskStateList(state.Progress)
	state.OpenItems = normalizeTaskStateList(state.OpenItems)
	state.Blockers = normalizeTaskStateList(state.Blockers)
	state.KeyArtifacts = normalizeTaskStateList(state.KeyArtifacts)
	state.Decisions = normalizeTaskStateList(state.Decisions)
	state.UserConstraints = normalizeTaskStateList(state.UserConstraints)
	return state
}

// normalizeTaskStateList 对任务状态中的字符串列表做去空、去重并保留顺序。
func normalizeTaskStateList(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := trimmed
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
