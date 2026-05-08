package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// ExecuteSystemTool 通过 runtime 统一执行一次确定性系统工具调用，不进入 provider/ReAct 主循环。
func (s *Service) ExecuteSystemTool(ctx context.Context, input SystemToolInput) (tools.ToolResult, error) {
	if s == nil {
		return tools.ToolResult{}, fmt.Errorf("runtime: service is nil")
	}
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{}, err
	}

	toolName := strings.TrimSpace(input.ToolName)
	if toolName == "" {
		return tools.ToolResult{}, fmt.Errorf("runtime: tool name is empty")
	}

	sessionID := strings.TrimSpace(input.SessionID)
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = newSystemToolRunID(toolName)
	}

	cfg := s.configManager.Get()
	workdir := strings.TrimSpace(input.Workdir)
	if workdir == "" {
		workdir = cfg.Workdir
	}

	var (
		state  *runState
		loaded agentsession.Session
	)
	if sessionID != "" {
		sessionMu, releaseLockRef := s.acquireSessionLock(sessionID)
		sessionMu.RLock()

		session, err := s.LoadSession(ctx, sessionID)
		if err != nil {
			sessionMu.RUnlock()
			releaseLockRef()
			return tools.ToolResult{}, err
		}
		loaded = session
		if workdir == "" {
			workdir = strings.TrimSpace(session.Workdir)
		}
		runStateValue := newRunState(runID, session)
		state = &runStateValue
		sessionMu.RUnlock()
		releaseLockRef()
	}

	call := providertypes.ToolCall{
		ID:        newSystemToolCallID(toolName),
		Name:      toolName,
		Arguments: string(input.Arguments),
	}

	if state != nil {
		s.emitRunScoped(ctx, EventToolStart, state, call)
	} else {
		_ = s.emit(ctx, EventToolStart, runID, sessionID, call)
	}

	result, execErr := s.executeToolCallWithPermission(ctx, permissionExecutionInput{
		RunID:       runID,
		SessionID:   sessionID,
		State:       state,
		Call:        call,
		Workdir:     workdir,
		ToolTimeout: time.Duration(cfg.ToolTimeoutSec) * time.Second,
	})

	if strings.TrimSpace(result.ToolCallID) == "" {
		result.ToolCallID = call.ID
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = toolName
	}
	if execErr != nil {
		result.IsError = true
	}

	if state != nil {
		if loaded.ID != "" {
			state.session = loaded
		}
		s.emitRunScoped(ctx, EventToolResult, state, result)
		s.emitTodoToolEvent(ctx, state, call, result, execErr)
	} else {
		_ = s.emit(ctx, EventToolResult, runID, sessionID, result)
	}

	return result, execErr
}

// normalizeToolName 将工具名标准化，空值回退为 "tool"。
func normalizeToolName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "tool"
	}
	return normalized
}

// newSystemToolRunID 为系统工具调用生成稳定前缀的运行标识，便于事件与日志定位。
func newSystemToolRunID(toolName string) string {
	return formatSystemToolID("system-tool", toolName)
}

// newSystemToolCallID 为系统工具调用生成单次执行唯一的 tool call id。
func newSystemToolCallID(toolName string) string {
	return formatSystemToolID("call", toolName)
}

// formatSystemToolID 统一构造系统工具相关 ID，避免不同类型 ID 生成逻辑分散重复。
func formatSystemToolID(prefix, toolName string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, normalizeToolName(toolName), time.Now().UnixNano())
}
