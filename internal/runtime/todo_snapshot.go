package runtime

import (
	"strings"

	agentsession "neo-code/internal/session"
)

// buildTodoSnapshotFromItems 基于会话内 Todo 列表构建结构化快照，供事件与查询接口复用。
func buildTodoSnapshotFromItems(items []agentsession.TodoItem) TodoSnapshot {
	if len(items) == 0 {
		return TodoSnapshot{}
	}

	viewItems := make([]TodoViewItem, 0, len(items))
	summary := TodoSummary{
		Total: len(items),
	}
	for _, item := range items {
		required := item.RequiredValue()
		if required {
			summary.RequiredTotal++
			switch item.Status {
			case agentsession.TodoStatusCompleted:
				summary.RequiredCompleted++
			case agentsession.TodoStatusFailed:
				summary.RequiredFailed++
			case agentsession.TodoStatusCanceled:
				// canceled 不计入 open，由 verifier 处理是否为可接受终态。
			default:
				summary.RequiredOpen++
			}
		}

		viewItems = append(viewItems, TodoViewItem{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        strings.TrimSpace(string(item.Status)),
			Required:      required,
			Artifacts:     append([]string(nil), item.Artifacts...),
			FailureReason: strings.TrimSpace(item.FailureReason),
			BlockedReason: strings.TrimSpace(string(item.BlockedReason)),
			Revision:      item.Revision,
		})
	}

	return TodoSnapshot{
		Items:   viewItems,
		Summary: summary,
	}
}
