package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/subagent"
)

type failingSubAgentFactory struct {
	err error
}

func (f failingSubAgentFactory) Create(role subagent.Role) (subagent.WorkerRuntime, error) {
	return nil, f.err
}

func TestServiceRunSubAgentTaskSuccess(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			if input.StepIndex == 1 {
				return subagent.StepOutput{
					Delta: "step-1",
					Done:  false,
				}, nil
			}
			return subagent.StepOutput{
				Delta: "step-2",
				Done:  true,
				Output: subagent.Output{
					Summary:     "task completed",
					Findings:    []string{"f1"},
					Patches:     []string{"p1"},
					Risks:       []string{"r1"},
					NextActions: []string{"n1"},
					Artifacts:   []string{"a1"},
				},
			}, nil
		})
	}))

	result, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID:     "sub-run-success",
		SessionID: "session-1",
		Role:      subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-1",
			Goal: "implement feature",
		},
		Budget: subagent.Budget{
			MaxSteps: 3,
		},
	})
	if err != nil {
		t.Fatalf("RunSubAgentTask() error = %v", err)
	}
	if result.State != subagent.StateSucceeded {
		t.Fatalf("result state = %q, want %q", result.State, subagent.StateSucceeded)
	}
	if result.StepCount != 2 {
		t.Fatalf("result step count = %d, want 2", result.StepCount)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{
		EventSubAgentStarted,
		EventSubAgentProgress,
		EventSubAgentProgress,
		EventSubAgentCompleted,
	})
	assertEventsRunID(t, events, "sub-run-success")
	for _, evt := range events {
		if evt.Type != EventSubAgentProgress {
			continue
		}
		if evt.Timestamp.IsZero() {
			t.Fatalf("progress event timestamp should be set: %+v", evt)
		}
		if evt.PayloadVersion != controlplane.PayloadVersion {
			t.Fatalf("progress event payload version = %d, want %d", evt.PayloadVersion, controlplane.PayloadVersion)
		}
	}
}

func TestServiceRunSubAgentTaskFailureFlows(t *testing.T) {
	t.Parallel()

	t.Run("factory create failed", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(nil, nil, nil, nil, nil)
		service.SetSubAgentFactory(failingSubAgentFactory{err: errors.New("create failed")})
		_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
			RunID: "sub-run-factory-failed",
			Role:  subagent.RoleResearcher,
			Task: subagent.Task{
				ID:   "task-f",
				Goal: "research",
			},
		})
		if err == nil {
			t.Fatalf("expected create error")
		}
		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentFailed})
	})

	t.Run("worker step failed", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(nil, nil, nil, nil, nil)
		service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
			return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
				return subagent.StepOutput{}, errors.New("step failed")
			})
		}))
		_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
			RunID: "sub-run-step-failed",
			Role:  subagent.RoleReviewer,
			Task: subagent.Task{
				ID:   "task-step-f",
				Goal: "review",
			},
		})
		if err == nil {
			t.Fatalf("expected step error")
		}
		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{
			EventSubAgentStarted,
			EventSubAgentProgress,
			EventSubAgentFailed,
		})
	})

	t.Run("context canceled should emit canceled", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(nil, nil, nil, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
			return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
				cancel()
				<-ctx.Done()
				return subagent.StepOutput{}, ctx.Err()
			})
		}))

		result, err := service.RunSubAgentTask(ctx, SubAgentTaskInput{
			RunID: "sub-run-canceled",
			Role:  subagent.RoleReviewer,
			Task: subagent.Task{
				ID:   "task-cancel",
				Goal: "review",
			},
		})
		if err == nil {
			t.Fatalf("expected canceled error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		if result.State != subagent.StateCanceled {
			t.Fatalf("result state = %q, want %q", result.State, subagent.StateCanceled)
		}
		if result.StopReason != subagent.StopReasonCanceled {
			t.Fatalf("stop reason = %q, want %q", result.StopReason, subagent.StopReasonCanceled)
		}
		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{
			EventSubAgentStarted,
			EventSubAgentProgress,
			EventSubAgentCanceled,
		})
	})

	t.Run("worker start failed by disallowed capability", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(nil, nil, nil, nil, nil)
		_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
			RunID: "sub-run-start-failed",
			Role:  subagent.RoleReviewer,
			Task: subagent.Task{
				ID:   "task-start-failed",
				Goal: "review",
			},
			Capability: subagent.Capability{
				AllowedTools: []string{"bash"},
			},
		})
		if err == nil {
			t.Fatalf("expected start error")
		}
		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentFailed})
	})

	t.Run("custom worker failed without explicit error should return fallback", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(nil, nil, nil, nil, nil)
		service.SetSubAgentFactory(stubSubAgentFactory{
			create: func(role subagent.Role) (subagent.WorkerRuntime, error) {
				return &stubSubAgentWorker{
					result: subagent.Result{
						Role:       role,
						TaskID:     "task-fallback-error",
						State:      subagent.StateFailed,
						StopReason: subagent.StopReasonError,
					},
					stepResult: subagent.StepResult{
						State: subagent.StateFailed,
						Done:  true,
						Step:  1,
					},
				}, nil
			},
		})

		_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
			RunID: "sub-run-fallback-error",
			Role:  subagent.RoleReviewer,
			Task: subagent.Task{
				ID:   "task-fallback-error",
				Goal: "review",
			},
		})
		if err == nil {
			t.Fatalf("expected fallback error")
		}
		if !strings.Contains(err.Error(), "state=failed") || !strings.Contains(err.Error(), "stop_reason=error") {
			t.Fatalf("error = %q, want state/stop_reason fallback", err.Error())
		}
	})
}

type stubSubAgentFactory struct {
	create func(role subagent.Role) (subagent.WorkerRuntime, error)
}

func (s stubSubAgentFactory) Create(role subagent.Role) (subagent.WorkerRuntime, error) {
	return s.create(role)
}

type stubSubAgentWorker struct {
	startErr    error
	stepResult  subagent.StepResult
	stepErr     error
	result      subagent.Result
	resultErr   error
	current     subagent.State
	stopInvoked bool
}

func (s *stubSubAgentWorker) Start(task subagent.Task, budget subagent.Budget, capability subagent.Capability) error {
	if s.startErr != nil {
		return s.startErr
	}
	if s.current == "" {
		s.current = subagent.StateRunning
	}
	s.result.Role = firstNonEmptyRole(s.result.Role, subagent.RoleReviewer)
	if strings.TrimSpace(s.result.TaskID) == "" {
		s.result.TaskID = task.ID
	}
	return nil
}

func (s *stubSubAgentWorker) Step(ctx context.Context) (subagent.StepResult, error) {
	if s.stepResult.State == "" {
		s.stepResult.State = s.current
	}
	if s.stepResult.Done {
		s.current = s.result.State
	}
	return s.stepResult, s.stepErr
}

func (s *stubSubAgentWorker) Stop(reason subagent.StopReason) error {
	s.stopInvoked = true
	s.current = subagent.StateCanceled
	s.result.State = subagent.StateCanceled
	s.result.StopReason = reason
	return nil
}

func (s *stubSubAgentWorker) Result() (subagent.Result, error) {
	return s.result, s.resultErr
}

func (s *stubSubAgentWorker) State() subagent.State {
	if s.current == "" {
		return subagent.StateIdle
	}
	return s.current
}

func (s *stubSubAgentWorker) Policy() subagent.RolePolicy {
	return subagent.RolePolicy{}
}

func firstNonEmptyRole(role subagent.Role, fallback subagent.Role) subagent.Role {
	if role != "" {
		return role
	}
	return fallback
}

func TestServiceRunSubAgentTaskInputValidation(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)
	if _, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		Role: subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task",
			Goal: "goal",
		},
	}); err == nil {
		t.Fatalf("expected empty run id error")
	}

	if _, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID: "sub-run-invalid-role",
		Role:  subagent.Role("x"),
		Task: subagent.Task{
			ID:   "task",
			Goal: "goal",
		},
	}); err == nil {
		t.Fatalf("expected invalid role error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	if _, err := service.RunSubAgentTask(ctx, SubAgentTaskInput{
		RunID: "sub-run-timeout",
		Role:  subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-timeout",
			Goal: "goal",
		},
	}); err == nil {
		t.Fatalf("expected context error")
	}
}
