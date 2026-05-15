package runtime

import (
	"context"
	"strings"

	"neo-code/internal/runtime/acceptgate"
	"neo-code/internal/runtime/controlplane"
)

type hookToolSummaryItem struct {
	Name    string `json:"name"`
	IsError bool   `json:"is_error,omitempty"`
}

// shouldRunAcceptGateHook 判断当前 run 是否需要进入用户验收 hook。
func (s *runState) shouldRunAcceptGateHook() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasRunWorkspaceWrite
}

// recordRecentToolSummary 记录最近一批工具摘要，供 accept_gate hook 获取最小上下文。
func (s *runState) recordRecentToolSummary(summary toolExecutionSummary) {
	if s == nil || len(summary.Results) == 0 {
		return
	}
	items := make([]hookToolSummaryItem, 0, len(summary.Results))
	for _, result := range summary.Results {
		name := strings.TrimSpace(result.Name)
		if name == "" {
			continue
		}
		items = append(items, hookToolSummaryItem{Name: name, IsError: result.IsError})
	}
	if len(items) == 0 {
		return
	}
	s.mu.Lock()
	s.recentToolSummary = append(s.recentToolSummary, items...)
	if len(s.recentToolSummary) > 20 {
		s.recentToolSummary = append([]hookToolSummaryItem(nil), s.recentToolSummary[len(s.recentToolSummary)-20:]...)
	}
	s.mu.Unlock()
}

// buildAcceptGateHookMetadata 组装用户验收 hook 可见的最小运行态摘要。
func buildAcceptGateHookMetadata(state *runState, assistantText string) map[string]any {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	todoSnapshot := buildTodoSnapshotFromItems(cloneTodosForPersistence(state.session.Todos))
	return map[string]any{
		"workdir":              strings.TrimSpace(state.effectiveWorkdir),
		"workspace_changed":    state.hasRunWorkspaceWrite,
		"assistant_text_empty": strings.TrimSpace(assistantText) == "",
		"todo_summary":         todoSnapshot.Summary,
		"recent_tool_summary":  append([]hookToolSummaryItem(nil), state.recentToolSummary...),
	}
}

// continuedAcceptGateReport 将 hook 阻断映射为统一的 Continue 判定。
func continuedAcceptGateReport(reason string) acceptgate.Report {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "accept gate hook blocked completion"
	}
	return acceptgate.Report{
		Outcome:      acceptgate.OutcomeContinue,
		StopReason:   controlplane.StopReasonAcceptContinue,
		Summary:      reason,
		ContinueHint: reason,
	}
}

// handleAcceptanceContinue 处理可恢复验收结果；返回 true 表示应继续下一轮。
func (s *Service) handleAcceptanceContinue(ctx context.Context, state *runState, report acceptgate.Report) bool {
	if state == nil {
		return false
	}
	hint := strings.TrimSpace(report.ContinueHint)
	if hint == "" {
		hint = strings.TrimSpace(report.Summary)
	}
	state.mu.Lock()
	state.acceptanceContinueCount++
	count := state.acceptanceContinueCount
	if count <= maxAcceptanceContinues {
		state.pendingSystemReminder = hint
	}
	state.mu.Unlock()
	if count <= maxAcceptanceContinues {
		return true
	}
	state.markTerminalDecision(
		controlplane.TerminalStatusIncomplete,
		controlplane.StopReasonAcceptContinueExhausted,
		"acceptance continue limit reached",
	)
	s.emitRunScopedOptional(EventVerificationFailed, state, VerificationFailedPayload{
		StopReason: controlplane.StopReasonAcceptContinueExhausted,
	})
	return false
}
