package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/subagent"
)

// SubAgentTaskInput 描述一次子代理任务执行请求。
type SubAgentTaskInput struct {
	RunID      string
	SessionID  string
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

	factory := s.SubAgentFactory()
	worker, err := factory.Create(input.Role)
	if err != nil {
		_ = s.emit(ctx, EventSubAgentFailed, input.RunID, input.SessionID, SubAgentEventPayload{
			Role:   input.Role,
			TaskID: input.Task.ID,
			State:  subagent.StateFailed,
			Error:  err.Error(),
		})
		return subagent.Result{}, err
	}

	if err := worker.Start(input.Task, input.Budget, input.Capability); err != nil {
		_ = s.emit(ctx, EventSubAgentFailed, input.RunID, input.SessionID, SubAgentEventPayload{
			Role:   input.Role,
			TaskID: input.Task.ID,
			State:  subagent.StateFailed,
			Error:  err.Error(),
		})
		return subagent.Result{}, err
	}

	_ = s.emit(ctx, EventSubAgentStarted, input.RunID, input.SessionID, SubAgentEventPayload{
		Role:   input.Role,
		TaskID: input.Task.ID,
		State:  worker.State(),
	})

	for {
		stepResult, stepErr := worker.Step(ctx)
		if stepResult.State == "" {
			stepResult.State = worker.State()
		}
		emitSubAgentProgress(s, input, stepResult, stepErr)

		if stepErr != nil {
			if errors.Is(stepErr, context.Canceled) || errors.Is(stepErr, context.DeadlineExceeded) {
				_ = worker.Stop(subagent.StopReasonCanceled)
				result, resultErr := worker.Result()
				if resultErr != nil {
					result = subagent.Result{
						Role:       input.Role,
						TaskID:     input.Task.ID,
						State:      subagent.StateCanceled,
						StopReason: subagent.StopReasonCanceled,
						Error:      errorText(stepErr),
					}
				}
				emitSubAgentTerminal(s, ctx, input, result)
				return result, stepErr
			}

			result, resultErr := worker.Result()
			if resultErr != nil {
				_ = s.emit(ctx, EventSubAgentFailed, input.RunID, input.SessionID, SubAgentEventPayload{
					Role:   input.Role,
					TaskID: input.Task.ID,
					State:  subagent.StateFailed,
					Error:  stepErr.Error(),
				})
				return subagent.Result{}, stepErr
			}
			emitSubAgentTerminal(s, ctx, input, result)
			return result, stepErr
		}

		if !stepResult.Done {
			continue
		}

		result, err := worker.Result()
		if err != nil {
			_ = s.emit(ctx, EventSubAgentFailed, input.RunID, input.SessionID, SubAgentEventPayload{
				Role:   input.Role,
				TaskID: input.Task.ID,
				State:  subagent.StateFailed,
				Error:  err.Error(),
			})
			return subagent.Result{}, err
		}
		emitSubAgentTerminal(s, ctx, input, result)
		if result.State == subagent.StateSucceeded {
			return result, nil
		}
		return result, subAgentResultError(result)
	}
}

// emitSubAgentProgress 非阻塞发射进度事件，避免慢消费者反压执行路径。
func emitSubAgentProgress(s *Service, input SubAgentTaskInput, stepResult subagent.StepResult, stepErr error) {
	payload := SubAgentEventPayload{
		Role:   input.Role,
		TaskID: input.Task.ID,
		State:  stepResult.State,
		Step:   stepResult.Step,
		Delta:  stepResult.Delta,
		Error:  errorText(stepErr),
	}
	event := RuntimeEvent{
		Type:           EventSubAgentProgress,
		RunID:          input.RunID,
		SessionID:      input.SessionID,
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
