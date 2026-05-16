package runtime

import (
	"strings"

	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

// subAgentSnapshotState 维护当前 run 内由 spawn_subagent 产生的聚合计数。
type subAgentSnapshotState struct {
	started   map[string]struct{}
	completed map[string]struct{}
	failed    map[string]struct{}
}

// applySpawnResult 吸收一次 spawn_subagent 结果，并返回聚合计数是否发生变化。
func (s *subAgentSnapshotState) applySpawnResult(result tools.ToolResult) bool {
	if s == nil {
		return false
	}
	taskID := metadataString(result.Metadata, "task_id")
	if taskID == "" {
		return false
	}
	role := metadataString(result.Metadata, "role")
	startKey := taskID + ":" + role
	changed := addSubAgentSnapshotKey(&s.started, startKey)

	state := subagent.State(metadataString(result.Metadata, "state"))
	switch state {
	case subagent.StateSucceeded:
		changed = addSubAgentSnapshotKey(&s.completed, taskID) || changed
	case subagent.StateFailed, subagent.StateCanceled:
		changed = addSubAgentSnapshotKey(&s.failed, taskID+":"+metadataString(result.Metadata, "stop_reason")) || changed
	}
	return changed
}

// snapshot 把内部集合压缩为对外稳定的聚合计数。
func (s *subAgentSnapshotState) snapshot() SubAgentSnapshot {
	if s == nil {
		return SubAgentSnapshot{}
	}
	return SubAgentSnapshot{
		StartedCount:   len(s.started),
		CompletedCount: len(s.completed),
		FailedCount:    len(s.failed),
	}
}

// addSubAgentSnapshotKey 把唯一键写入集合，并返回本次是否新增。
func addSubAgentSnapshotKey(target *map[string]struct{}, key string) bool {
	if target == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if *target == nil {
		*target = make(map[string]struct{})
	}
	if _, exists := (*target)[key]; exists {
		return false
	}
	(*target)[key] = struct{}{}
	return true
}

// metadataString 读取工具结果 metadata 中的字符串字段。
func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
