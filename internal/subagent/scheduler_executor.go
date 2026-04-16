package subagent

import (
	"context"
	"errors"
	"strings"
)

// scheduleTaskInput 描述调度器发起的单任务执行请求。
type scheduleTaskInput struct {
	Task       Task
	Role       Role
	Budget     Budget
	Capability Capability
}

// executeTaskWithFactory 通过 WorkerFactory 执行单任务并返回标准化结果。
func executeTaskWithFactory(ctx context.Context, factory Factory, input scheduleTaskInput) (Result, error) {
	worker, err := factory.Create(input.Role)
	if err != nil {
		return Result{}, err
	}
	if err := worker.Start(input.Task, input.Budget, input.Capability); err != nil {
		return Result{}, err
	}

	for {
		stepResult, stepErr := worker.Step(ctx)
		if stepErr != nil {
			if errors.Is(stepErr, context.Canceled) || errors.Is(stepErr, context.DeadlineExceeded) {
				_ = worker.Stop(StopReasonCanceled)
				result, resultErr := worker.Result()
				if resultErr == nil {
					return result, stepErr
				}
				return Result{
					Role:       input.Role,
					TaskID:     input.Task.ID,
					State:      StateCanceled,
					StopReason: StopReasonCanceled,
					Error:      strings.TrimSpace(stepErr.Error()),
				}, stepErr
			}

			result, resultErr := worker.Result()
			if resultErr == nil {
				return result, stepErr
			}
			return Result{
				Role:       input.Role,
				TaskID:     input.Task.ID,
				State:      StateFailed,
				StopReason: StopReasonError,
				Error:      strings.TrimSpace(stepErr.Error()),
			}, stepErr
		}
		if !stepResult.Done {
			continue
		}

		result, resultErr := worker.Result()
		if resultErr != nil {
			return Result{}, resultErr
		}
		if result.State == StateSucceeded {
			return result, nil
		}
		if strings.TrimSpace(result.Error) != "" {
			return result, errors.New(result.Error)
		}
		return result, errorsf("worker finished with state=%s stop_reason=%s", result.State, result.StopReason)
	}
}
