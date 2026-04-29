package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/subagent"
)

// SubAgentTaskInput 描述一次子代理任务执行请求。
type SubAgentTaskInput struct {
	RunID      string
	SessionID  string
	AgentID    string
	Role       subagent.Role
	Task       subagent.Task
	Budget     subagent.Budget
	Capability subagent.Capability
}

// RunSubAgentTask 使用当前 runtime 注册的工厂执行一条子代理任务。
func (s *Service) RunSubAgentTask(ctx context.Context, input SubAgentTaskInput) (subagent.Result, error) {
	if err := ctx.Err(); err != nil {
		return subagent.Result{}, err
	}
	if strings.TrimSpace(input.RunID) == "" {
		return subagent.Result{}, errors.New("runtime: subagent run id is empty")
	}
	if !input.Role.Valid() {
		return subagent.Result{}, fmt.Errorf("runtime: invalid subagent role %q", input.Role)
	}
	if err := input.Task.Validate(); err != nil {
		return subagent.Result{}, err
	}
	task := input.Task
	task.RunID = strings.TrimSpace(input.RunID)
	task.SessionID = strings.TrimSpace(input.SessionID)
	task.AgentID = resolveSubAgentExecutionAgentID(input)
	if strings.TrimSpace(task.Workspace) == "" && s != nil && s.configManager != nil {
		cfg := s.configManager.Get()
		task.Workspace = strings.TrimSpace(cfg.Workdir)
	}

	startHookOutput := s.runHookPoint(ctx, nil, runtimehooks.HookPointSubAgentStart, runtimehooks.HookContext{
		RunID:     strings.TrimSpace(input.RunID),
		SessionID: strings.TrimSpace(input.SessionID),
		Metadata: map[string]any{
			"task_id":   strings.TrimSpace(task.ID),
			"role":      string(input.Role),
			"workspace": strings.TrimSpace(task.Workspace),
			"tool_name": "spawn_subagent",
			"trigger":   "subagent_start",
			"workdir":   strings.TrimSpace(task.Workspace),
		},
	})
	if startHookOutput.Blocked {
		reason := findHookBlockMessage(startHookOutput)
		_ = s.emit(ctx, EventHookBlocked, strings.TrimSpace(input.RunID), strings.TrimSpace(input.SessionID), HookBlockedPayload{
			HookID:   strings.TrimSpace(startHookOutput.BlockedBy),
			Source:   string(findHookBlockSource(startHookOutput)),
			Point:    string(runtimehooks.HookPointSubAgentStart),
			Reason:   reason,
			Enforced: true,
		})
		return subagent.Result{}, errors.New(reason)
	}

	factory := s.SubAgentFactory()
	worker, err := factory.Create(input.Role)
	if err != nil {
		emitSubAgentFailed(s, ctx, input.RunID, input.SessionID, input.Role, input.Task.ID, err)
		return subagent.Result{}, err
	}

	if err := worker.Start(task, input.Budget, input.Capability); err != nil {
		emitSubAgentFailed(s, ctx, input.RunID, input.SessionID, input.Role, input.Task.ID, err)
		return subagent.Result{}, err
	}

	_ = s.emit(ctx, EventSubAgentStarted, input.RunID, input.SessionID, SubAgentEventPayload{
		Role:   input.Role,
		TaskID: task.ID,
		State:  worker.State(),
	})

	for {
		stepResult, stepErr := worker.Step(ctx)
		if stepResult.State == "" {
			stepResult.State = worker.State()
		}
		emitSubAgentProgress(s, input.RunID, input.SessionID, input.Role, task.ID, stepResult, stepErr)

		if stepErr != nil {
			if errors.Is(stepErr, context.DeadlineExceeded) {
				_ = worker.Stop(subagent.StopReasonTimeout)
				result, resultErr := worker.Result()
				if resultErr != nil {
					result = fallbackSubAgentResult(input.Role, task.ID, subagent.StateFailed, subagent.StopReasonTimeout, stepErr)
				}
				emitSubAgentTerminal(s, ctx, input, result)
				emitSubAgentStopHook(s, ctx, input, result)
				return result, stepErr
			}
			if errors.Is(stepErr, context.Canceled) {
				_ = worker.Stop(subagent.StopReasonCanceled)
				result, resultErr := worker.Result()
				if resultErr != nil {
					result = fallbackSubAgentResult(input.Role, task.ID, subagent.StateCanceled, subagent.StopReasonCanceled, stepErr)
				}
				emitSubAgentTerminal(s, ctx, input, result)
				emitSubAgentStopHook(s, ctx, input, result)
				return result, stepErr
			}

			result, resultErr := worker.Result()
			if resultErr != nil {
				fallback := fallbackSubAgentResult(input.Role, task.ID, subagent.StateFailed, subagent.StopReasonError, stepErr)
				emitSubAgentFailed(s, ctx, input.RunID, input.SessionID, input.Role, task.ID, stepErr)
				emitSubAgentStopHook(s, ctx, input, fallback)
				return fallback, stepErr
			}
			emitSubAgentTerminal(s, ctx, input, result)
			emitSubAgentStopHook(s, ctx, input, result)
			return result, stepErr
		}

		if !stepResult.Done {
			continue
		}

		result, err := worker.Result()
		if err != nil {
			fallback := fallbackSubAgentResult(input.Role, task.ID, subagent.StateFailed, subagent.StopReasonError, err)
			emitSubAgentFailed(s, ctx, input.RunID, input.SessionID, input.Role, task.ID, err)
			emitSubAgentStopHook(s, ctx, input, fallback)
			return fallback, err
		}
		emitSubAgentTerminal(s, ctx, input, result)
		emitSubAgentStopHook(s, ctx, input, result)
		if result.State == subagent.StateSucceeded {
			return result, nil
		}
		return result, subAgentResultError(result)
	}
}

// emitSubAgentStopHook 在子代理结束后触发 subagent_stop 观测挂点。
func emitSubAgentStopHook(s *Service, ctx context.Context, input SubAgentTaskInput, result subagent.Result) {
	if s == nil {
		return
	}
	_ = s.runHookPoint(ctx, nil, runtimehooks.HookPointSubAgentStop, runtimehooks.HookContext{
		RunID:     strings.TrimSpace(input.RunID),
		SessionID: strings.TrimSpace(input.SessionID),
		Metadata: map[string]any{
			"task_id":     strings.TrimSpace(result.TaskID),
			"role":        string(result.Role),
			"state":       string(result.State),
			"stop_reason": string(result.StopReason),
			"step_count":  result.StepCount,
			"error":       strings.TrimSpace(result.Error),
		},
	})
}

// emitSubAgentFailed 统一发射子代理失败事件，避免重复构造相同载荷。
func emitSubAgentFailed(
	s *Service,
	ctx context.Context,
	runID string,
	sessionID string,
	role subagent.Role,
	taskID string,
	err error,
) {
	if s == nil {
		return
	}
	_ = s.emit(ctx, EventSubAgentFailed, runID, sessionID, SubAgentEventPayload{
		Role:   role,
		TaskID: taskID,
		State:  subagent.StateFailed,
		Error:  errorText(err),
	})
}

// emitSubAgentProgress 非阻塞发射进度事件，避免慢消费者反压执行路径。
func emitSubAgentProgress(
	s *Service,
	runID string,
	sessionID string,
	role subagent.Role,
	taskID string,
	stepResult subagent.StepResult,
	stepErr error,
) {
	payload := SubAgentEventPayload{
		Role:   role,
		TaskID: strings.TrimSpace(taskID),
		State:  stepResult.State,
		Step:   stepResult.Step,
		Delta:  stepResult.Delta,
		Error:  errorText(stepErr),
	}
	event := RuntimeEvent{
		Type:           EventSubAgentProgress,
		RunID:          strings.TrimSpace(runID),
		SessionID:      strings.TrimSpace(sessionID),
		Turn:           turnUnspecified,
		Timestamp:      time.Now(),
		PayloadVersion: controlplane.PayloadVersion,
		Payload:        payload,
	}
	select {
	case s.events <- event:
	default:
	}
}

// emitSubAgentTerminal 按子代理终态发射最终事件。
func emitSubAgentTerminal(s *Service, ctx context.Context, input SubAgentTaskInput, result subagent.Result) {
	payload := SubAgentEventPayload{
		Role:       result.Role,
		TaskID:     result.TaskID,
		State:      result.State,
		StopReason: result.StopReason,
		Step:       result.StepCount,
		Error:      strings.TrimSpace(result.Error),
	}

	switch result.State {
	case subagent.StateSucceeded:
		_ = s.emit(ctx, EventSubAgentCompleted, input.RunID, input.SessionID, payload)
	case subagent.StateCanceled:
		_ = s.emit(ctx, EventSubAgentCanceled, input.RunID, input.SessionID, payload)
	default:
		_ = s.emit(ctx, EventSubAgentFailed, input.RunID, input.SessionID, payload)
	}
}

// fallbackSubAgentResult 在 worker 结果不可用时构造保底终态，确保调用方拿到稳定字段。
func fallbackSubAgentResult(role subagent.Role, taskID string, state subagent.State, reason subagent.StopReason, err error) subagent.Result {
	return subagent.Result{
		Role:       role,
		TaskID:     taskID,
		State:      state,
		StopReason: reason,
		Error:      errorText(err),
	}
}

// errorText 将 error 安全转换为事件可用文本。
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// subAgentResultError 将子代理终态结果转换为可诊断错误，避免空错误文本丢失上下文。
func subAgentResultError(result subagent.Result) error {
	if text := strings.TrimSpace(result.Error); text != "" {
		return errors.New(text)
	}
	return fmt.Errorf("subagent ended with state=%s stop_reason=%s", result.State, result.StopReason)
}

// resolveSubAgentExecutionAgentID 生成子代理执行身份，供权限链路透传审计。
func resolveSubAgentExecutionAgentID(input SubAgentTaskInput) string {
	role := strings.TrimSpace(string(input.Role))
	taskID := strings.TrimSpace(input.Task.ID)
	if role == "" {
		role = "subagent"
	}
	if taskID == "" {
		return role
	}
	return role + ":" + taskID
}
