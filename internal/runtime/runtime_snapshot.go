package runtime

import (
	"context"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
)

// RuntimeSnapshot 描述当前运行态的统一快照，供 TUI/Gateway/Desktop 实时展示。
type RuntimeSnapshot struct {
	RunID     string           `json:"run_id"`
	SessionID string           `json:"session_id"`
	Phase     string           `json:"phase,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
	Todos     TodoSnapshot     `json:"todos"`
	Decision  DecisionSnapshot `json:"decision,omitempty"`
	SubAgents SubAgentSnapshot `json:"subagents,omitempty"`
	// PendingUserQuestion 表示当前 run 是否存在待回答 ask_user 问题。
	PendingUserQuestion *UserQuestionRequestedPayload `json:"pending_user_question,omitempty"`
}

// DecisionSnapshot 是终态裁决快照。
type DecisionSnapshot struct {
	Status     string   `json:"status,omitempty"`
	StopReason string   `json:"stop_reason,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Details    []string `json:"details,omitempty"`
}

// SubAgentSnapshot 汇总当前 run 内由 spawn_subagent 产生的子代理结果。
type SubAgentSnapshot struct {
	StartedCount   int `json:"started_count"`
	CompletedCount int `json:"completed_count"`
	FailedCount    int `json:"failed_count"`
}

// RuntimeSnapshotUpdatedPayload 用于 runtime_snapshot_updated 事件。
type RuntimeSnapshotUpdatedPayload struct {
	Reason   string          `json:"reason,omitempty"`
	Snapshot RuntimeSnapshot `json:"snapshot"`
}

// SubAgentSnapshotUpdatedPayload 表示子代理聚合快照更新事件。
type SubAgentSnapshotUpdatedPayload struct {
	Reason   string           `json:"reason,omitempty"`
	SubAgent SubAgentSnapshot `json:"subagent"`
}

// buildRuntimeSnapshot 基于当前 runState 构建统一快照。
func buildRuntimeSnapshot(state *runState) RuntimeSnapshot {
	if state == nil {
		return RuntimeSnapshot{}
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	decisionSnapshot := DecisionSnapshot{}
	if state.terminalSet || state.terminalStatus != "" || state.terminalStopReason != "" {
		decisionSnapshot = DecisionSnapshot{
			Status:     strings.TrimSpace(string(state.terminalStatus)),
			StopReason: strings.TrimSpace(string(state.terminalStopReason)),
			Summary:    strings.TrimSpace(state.terminalStopDetail),
		}
	}

	return RuntimeSnapshot{
		RunID:               strings.TrimSpace(state.runID),
		SessionID:           strings.TrimSpace(state.session.ID),
		Phase:               strings.TrimSpace(string(state.lifecycle)),
		UpdatedAt:           time.Now(),
		Todos:               buildTodoSnapshotFromItems(cloneTodosForPersistence(state.session.Todos)),
		Decision:            decisionSnapshot,
		SubAgents:           state.subAgentSnapshot.snapshot(),
		PendingUserQuestion: clonePendingUserQuestion(state.pendingUserQuestion),
	}
}

// emitRuntimeSnapshotUpdated 发出统一快照事件，并缓存为会话级最近快照。
func (s *Service) emitRuntimeSnapshotUpdated(ctx context.Context, state *runState, reason string) {
	if s == nil || state == nil {
		return
	}
	snapshot := buildRuntimeSnapshot(state)
	s.cacheRuntimeSnapshot(snapshot)
	s.emitRunScopedOptional(EventRuntimeSnapshotUpdated, state, RuntimeSnapshotUpdatedPayload{
		Reason:   strings.TrimSpace(reason),
		Snapshot: snapshot,
	})
}

// emitSubAgentSnapshotUpdated 发出独立的子代理聚合快照事件，供 UI 展示当前 run 总览。
func (s *Service) emitSubAgentSnapshotUpdated(state *runState, reason string) {
	if s == nil || state == nil {
		return
	}
	state.mu.Lock()
	snapshot := state.subAgentSnapshot.snapshot()
	state.mu.Unlock()
	s.emitRunScopedOptional(EventSubAgentSnapshotUpdated, state, SubAgentSnapshotUpdatedPayload{
		Reason:   strings.TrimSpace(reason),
		SubAgent: snapshot,
	})
}

// cacheRuntimeSnapshot 维护最近一次会话快照，供查询 API 初始化/重连使用。
func (s *Service) cacheRuntimeSnapshot(snapshot RuntimeSnapshot) {
	if s == nil {
		return
	}
	sessionID := strings.TrimSpace(snapshot.SessionID)
	if sessionID == "" {
		return
	}
	s.runtimeSnapshotMu.Lock()
	defer s.runtimeSnapshotMu.Unlock()
	if s.runtimeSnapshots == nil {
		s.runtimeSnapshots = make(map[string]RuntimeSnapshot)
	}
	s.runtimeSnapshots[sessionID] = snapshot
}

// GetRuntimeSnapshot 返回指定会话的最新 runtime 快照；无运行态时回退为会话持久化快照。
func (s *Service) GetRuntimeSnapshot(ctx context.Context, sessionID string) (RuntimeSnapshot, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return RuntimeSnapshot{}, agentsession.ErrSessionNotFound
	}

	s.runtimeSnapshotMu.Lock()
	cached, ok := s.runtimeSnapshots[normalizedSessionID]
	s.runtimeSnapshotMu.Unlock()
	if ok {
		return cached, nil
	}

	session, err := s.LoadSession(ctx, normalizedSessionID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	snapshot := RuntimeSnapshot{
		SessionID:           normalizedSessionID,
		UpdatedAt:           session.UpdatedAt,
		Todos:               buildTodoSnapshotFromItems(session.ListTodos()),
		PendingUserQuestion: nil,
	}
	s.cacheRuntimeSnapshot(snapshot)
	return snapshot, nil
}

// clonePendingUserQuestion 复制待回答 ask_user 快照，避免共享可变引用到外部读取方。
func clonePendingUserQuestion(question *UserQuestionRequestedPayload) *UserQuestionRequestedPayload {
	if question == nil {
		return nil
	}
	cloned := *question
	cloned.Options = append([]any(nil), question.Options...)
	return &cloned
}
