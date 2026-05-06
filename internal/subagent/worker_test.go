package subagent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerLifecycleCompleted(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	w, err := NewWorker(RoleCoder, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
		return StepOutput{
			Delta: "patched files",
			Done:  true,
			Output: Output{
				Summary:     "done",
				Findings:    []string{"root cause fixed"},
				Patches:     []string{"a.go"},
				Risks:       []string{"need integration verify"},
				NextActions: []string{"run tests"},
				Artifacts:   []string{"test report"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	err = w.Start(Task{ID: "t1", Goal: "fix bug"}, Budget{MaxSteps: 3}, Capability{
		AllowedTools: []string{"bash", "bash", " "},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	step, err := w.Step(context.Background())
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if !step.Done || step.State != StateSucceeded {
		t.Fatalf("unexpected step result: %+v", step)
	}

	result, err := w.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if result.StopReason != StopReasonCompleted {
		t.Fatalf("stop reason = %q, want %q", result.StopReason, StopReasonCompleted)
	}
	if result.StepCount != 1 {
		t.Fatalf("step count = %d, want 1", result.StepCount)
	}
	if len(result.Capability.AllowedTools) != 1 {
		t.Fatalf("expected capability dedupe, got %+v", result.Capability)
	}
}

func TestWorkerLifecycleFailures(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleResearcher)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	t.Run("engine error", func(t *testing.T) {
		t.Parallel()

		w, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{}, errors.New("boom")
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := w.Start(Task{ID: "t2", Goal: "research"}, Budget{MaxSteps: 2}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if _, err := w.Step(context.Background()); err == nil {
			t.Fatalf("expected step error")
		}
		result, err := w.Result()
		if err != nil {
			t.Fatalf("Result() error = %v", err)
		}
		if result.State != StateFailed || result.StopReason != StopReasonError {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("max steps", func(t *testing.T) {
		t.Parallel()

		w, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{Delta: "not done", Done: false}, nil
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := w.Start(Task{ID: "t3", Goal: "research"}, Budget{MaxSteps: 1}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		step, err := w.Step(context.Background())
		if err != nil {
			t.Fatalf("Step() error = %v", err)
		}
		if !step.Done || step.State != StateFailed {
			t.Fatalf("expected first step to finish by max steps, got %+v", step)
		}

		result, err := w.Result()
		if err != nil {
			t.Fatalf("Result() error = %v", err)
		}
		if result.StopReason != StopReasonMaxSteps {
			t.Fatalf("stop reason = %q, want %q", result.StopReason, StopReasonMaxSteps)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		w, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{Done: false}, nil
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := w.Start(Task{ID: "t4", Goal: "research"}, Budget{MaxSteps: 5, Timeout: time.Nanosecond}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		time.Sleep(2 * time.Millisecond)

		step, err := w.Step(context.Background())
		if err != nil {
			t.Fatalf("Step() error = %v", err)
		}
		if !step.Done || step.State != StateFailed {
			t.Fatalf("unexpected timeout step: %+v", step)
		}
		result, err := w.Result()
		if err != nil {
			t.Fatalf("Result() error = %v", err)
		}
		if result.StopReason != StopReasonTimeout {
			t.Fatalf("stop reason = %q, want %q", result.StopReason, StopReasonTimeout)
		}
	})
}

func TestWorkerStopAndGuards(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleReviewer)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	w, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	if _, err := w.Result(); err == nil {
		t.Fatalf("expected result before finish to fail")
	}
	if _, err := w.Step(context.Background()); err == nil {
		t.Fatalf("expected step before start to fail")
	}
	if err := w.Start(Task{ID: "review", Goal: "review"}, Budget{}, Capability{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := w.Start(Task{ID: "review2", Goal: "review2"}, Budget{}, Capability{}); err == nil {
		t.Fatalf("expected double start to fail")
	}
	if err := w.Stop(StopReasonCanceled); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if w.State() != StateCanceled {
		t.Fatalf("state = %q, want %q", w.State(), StateCanceled)
	}
	if err := w.Stop(StopReasonCanceled); err != nil {
		t.Fatalf("terminal stop should be idempotent, got %v", err)
	}
}

func TestWorkerFactoryCreate(t *testing.T) {
	t.Parallel()

	factory := NewWorkerFactory(func(role Role, policy RolePolicy) Engine {
		return EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{
				Done: true,
				Output: Output{
					Summary:     "ok",
					Findings:    []string{"f1"},
					Patches:     []string{"p1"},
					Risks:       []string{"r1"},
					NextActions: []string{"n1"},
					Artifacts:   []string{"a1"},
				},
			}, nil
		})
	})

	w, err := factory.Create(RoleCoder)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if w.Policy().Role != RoleCoder {
		t.Fatalf("policy role = %q, want %q", w.Policy().Role, RoleCoder)
	}

	if _, err := factory.Create(Role("invalid")); err == nil {
		t.Fatalf("expected invalid role create to fail")
	}
}

func TestWorkerRejectsInvalidOutputContract(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	w, err := NewWorker(RoleCoder, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
		return StepOutput{
			Done: true,
			Output: Output{
				Summary: "   ",
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := w.Start(Task{ID: "t-invalid-output", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := w.Step(context.Background()); err == nil {
		t.Fatalf("expected invalid output contract error")
	}
	result, err := w.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if result.State != StateFailed || result.StopReason != StopReasonError {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestValidateDefaultWorkspacePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workspace string
		wantErr   bool
	}{
		{name: "current directory", workspace: ".", wantErr: true},
		{name: "filesystem root", workspace: string(filepath.Separator), wantErr: true},
		{name: "normal path", workspace: filepath.Join(string(filepath.Separator), "tmp", "workspace"), wantErr: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateDefaultWorkspacePath(tt.workspace)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for workspace=%q", tt.workspace)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for workspace=%q: %v", tt.workspace, err)
			}
		})
	}
}

func TestWithExecutionContextOnNilWorker(t *testing.T) {
	t.Parallel()

	opt := withExecutionContext(ExecutionContext{ToolExecutor: noopToolExecutor{}})
	opt(nil)
}

func TestWorkerStartCapabilityPolicyGuard(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleReviewer)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	w, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	if err := w.Start(Task{ID: "t-cap", Goal: "goal"}, Budget{}, Capability{
		AllowedTools: []string{"filesystem_read_file", "bash"},
	}); err == nil {
		t.Fatalf("expected disallowed capability tool to fail")
	}

	wPath, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := wPath.Start(Task{ID: "t-cap-path", Goal: "goal"}, Budget{}, Capability{
		AllowedPaths: []string{"/tmp/workspace"},
	}); err != nil {
		t.Fatalf("expected allowed paths to pass, got %v", err)
	}
	if err := wPath.Stop(StopReasonCanceled); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	resultWithPath, err := wPath.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if len(resultWithPath.Capability.AllowedPaths) != 1 || resultWithPath.Capability.AllowedPaths[0] != "/tmp/workspace" {
		t.Fatalf("capability paths = %v, want [/tmp/workspace]", resultWithPath.Capability.AllowedPaths)
	}

	w2, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := w2.Start(Task{ID: "t-cap-ok", Goal: "goal"}, Budget{}, Capability{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := w2.Stop(StopReasonCanceled); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	result, err := w2.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if len(result.Capability.AllowedTools) != len(policy.AllowedTools) {
		t.Fatalf("capability tools = %v, want policy tools %v", result.Capability.AllowedTools, policy.AllowedTools)
	}

	w3, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := w3.Start(Task{ID: "t-cap-workspace-default", Goal: "goal", Workspace: "/tmp/sub-task"}, Budget{}, Capability{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := w3.Stop(StopReasonCanceled); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	resultWithWorkspace, err := w3.Result()
	if err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	if len(resultWithWorkspace.Capability.AllowedPaths) != 1 || resultWithWorkspace.Capability.AllowedPaths[0] != "/tmp/sub-task" {
		t.Fatalf("workspace capability path = %v, want [/tmp/sub-task]", resultWithWorkspace.Capability.AllowedPaths)
	}

	w4, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := w4.Start(Task{ID: "t-cap-workspace-dot", Goal: "goal", Workspace: "."}, Budget{}, Capability{}); err == nil ||
		!strings.Contains(err.Error(), "must not be current directory") {
		t.Fatalf("expected workspace dot rejection, got %v", err)
	}

	w5, err := NewWorker(RoleReviewer, policy, nil)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	rootWorkspace := string(filepath.Separator)
	if err := w5.Start(Task{ID: "t-cap-workspace-root", Goal: "goal", Workspace: rootWorkspace}, Budget{}, Capability{}); err == nil ||
		!strings.Contains(err.Error(), "must not be filesystem root") {
		t.Fatalf("expected workspace root rejection, got %v", err)
	}
}

func TestWorkerTraceWindow(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleResearcher)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	var observedTraceLen int
	w, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
		observedTraceLen = len(input.Trace)
		return StepOutput{
			Done: true,
			Output: Output{
				Summary:     "done",
				Findings:    []string{"f1"},
				Patches:     []string{"p1"},
				Risks:       []string{"r1"},
				NextActions: []string{"n1"},
				Artifacts:   []string{"a1"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	impl, ok := w.(*worker)
	if !ok {
		t.Fatalf("expected *worker implementation")
	}
	impl.trace = make([]string, traceWindowSize+4)
	for i := range impl.trace {
		impl.trace[i] = "trace"
	}
	impl.state = StateRunning
	impl.task = Task{ID: "trace", Goal: "goal"}
	impl.budget = Budget{MaxSteps: 5, Timeout: time.Second}
	impl.capability = Capability{}
	impl.startedAt = time.Now()

	if _, err := w.Step(context.Background()); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if observedTraceLen != traceWindowSize {
		t.Fatalf("trace len = %d, want %d", observedTraceLen, traceWindowSize)
	}
}

func TestWorkerTraceStorageBounded(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleResearcher)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	w, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
		if input.StepIndex < traceWindowSize+8 {
			return StepOutput{Delta: "delta", Done: false}, nil
		}
		return StepOutput{
			Delta: "delta",
			Done:  true,
			Output: Output{
				Summary:     "done",
				Findings:    []string{"f1"},
				Patches:     []string{"p1"},
				Risks:       []string{"r1"},
				NextActions: []string{"n1"},
				Artifacts:   []string{"a1"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := w.Start(Task{ID: "t-trace-bounded", Goal: "goal"}, Budget{MaxSteps: traceWindowSize + 16}, Capability{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for {
		step, stepErr := w.Step(context.Background())
		if stepErr != nil {
			t.Fatalf("Step() error = %v", stepErr)
		}
		if step.Done {
			break
		}
	}

	impl, ok := w.(*worker)
	if !ok {
		t.Fatalf("expected *worker implementation")
	}
	if len(impl.trace) != traceWindowSize {
		t.Fatalf("trace storage len = %d, want %d", len(impl.trace), traceWindowSize)
	}
}

func TestWorkerNilAndValidationBranches(t *testing.T) {
	t.Parallel()

	if _, err := NewWorker(Role("bad"), RolePolicy{}, nil); err == nil {
		t.Fatalf("expected invalid role error")
	}
	if _, err := NewWorker(RoleCoder, RolePolicy{
		Role:                RoleReviewer,
		SystemPrompt:        "p",
		AllowedTools:        []string{"bash"},
		MaxToolCallsPerStep: 1,
		RequiredSections:    []string{"summary"},
	}, nil); err == nil {
		t.Fatalf("expected role mismatch error")
	}

	var nilWorker *worker
	if err := nilWorker.Start(Task{}, Budget{}, Capability{}); err == nil {
		t.Fatalf("expected nil worker start error")
	}
	if _, err := nilWorker.Step(context.Background()); err == nil {
		t.Fatalf("expected nil worker step error")
	}
	if err := nilWorker.Stop(StopReasonCanceled); err == nil {
		t.Fatalf("expected nil worker stop error")
	}
	if _, err := nilWorker.Result(); err == nil {
		t.Fatalf("expected nil worker result error")
	}
	if nilWorker.State() != StateIdle {
		t.Fatalf("nil worker state = %q, want %q", nilWorker.State(), StateIdle)
	}
	nilPolicy := nilWorker.Policy()
	if nilPolicy.Role != "" || nilPolicy.SystemPrompt != "" || len(nilPolicy.AllowedTools) != 0 || len(nilPolicy.RequiredSections) != 0 {
		t.Fatalf("nil worker policy should be empty, got %+v", nilPolicy)
	}
}

func TestWorkerStopReasonBranches(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	newRunningWorker := func(t *testing.T) *worker {
		t.Helper()
		wr, err := NewWorker(RoleCoder, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{Done: false}, nil
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		impl, ok := wr.(*worker)
		if !ok {
			t.Fatalf("expected *worker implementation")
		}
		if err := impl.Start(Task{ID: "t-stop", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		return impl
	}

	t.Run("completed requires valid output", func(t *testing.T) {
		t.Parallel()
		w := newRunningWorker(t)
		w.output = Output{Summary: " "}
		if err := w.Stop(StopReasonCompleted); err == nil {
			t.Fatalf("expected invalid completed output error")
		}
	})

	for _, reason := range []StopReason{StopReasonTimeout, StopReasonMaxSteps, StopReasonError} {
		reason := reason
		t.Run("failed reason "+string(reason), func(t *testing.T) {
			t.Parallel()
			w := newRunningWorker(t)
			if err := w.Stop(reason); err != nil {
				t.Fatalf("Stop(%s) error = %v", reason, err)
			}
			if w.State() != StateFailed {
				t.Fatalf("state=%q, want %q", w.State(), StateFailed)
			}
		})
	}

	t.Run("unsupported reason", func(t *testing.T) {
		t.Parallel()
		w := newRunningWorker(t)
		if err := w.Stop(StopReason("invalid-reason")); err == nil {
			t.Fatalf("expected unsupported reason error")
		}
	})

	t.Run("idle worker stop fails", func(t *testing.T) {
		t.Parallel()
		wr, err := NewWorker(RoleCoder, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Stop(StopReasonCanceled); err == nil {
			t.Fatalf("expected non-running stop error")
		}
	})
}

func TestWorkerStepCancellationBranches(t *testing.T) {
	t.Parallel()

	policy, err := DefaultRolePolicy(RoleResearcher)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}

	t.Run("step with canceled context before run", func(t *testing.T) {
		t.Parallel()
		wr, err := NewWorker(RoleResearcher, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(Task{ID: "t-step-cancel-1", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := wr.Step(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("engine cancellation transitions canceled state", func(t *testing.T) {
		t.Parallel()
		wr, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{}, context.Canceled
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(Task{ID: "t-step-cancel-2", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if _, err := wr.Step(context.Background()); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
		if wr.State() != StateCanceled {
			t.Fatalf("state=%q, want %q", wr.State(), StateCanceled)
		}
	})

	t.Run("engine deadline transitions timeout state", func(t *testing.T) {
		t.Parallel()
		wr, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			_ = ctx
			_ = input
			return StepOutput{}, context.DeadlineExceeded
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(Task{ID: "t-step-timeout", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if _, err := wr.Step(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
		if wr.State() != StateFailed {
			t.Fatalf("state=%q, want %q", wr.State(), StateFailed)
		}
		result, err := wr.Result()
		if err != nil {
			t.Fatalf("Result() error = %v", err)
		}
		if result.StopReason != StopReasonTimeout {
			t.Fatalf("stop reason=%q, want %q", result.StopReason, StopReasonTimeout)
		}
	})
}

func TestWorkerAdditionalUncoveredBranches(t *testing.T) {
	t.Parallel()

	t.Run("new worker fills empty policy role", func(t *testing.T) {
		t.Parallel()
		policy := RolePolicy{
			SystemPrompt:        "review",
			AllowedTools:        []string{"bash"},
			MaxToolCallsPerStep: 1,
			RequiredSections:    []string{"summary"},
		}
		wr, err := NewWorker(RoleReviewer, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if wr.Policy().Role != RoleReviewer {
			t.Fatalf("policy role=%q, want %q", wr.Policy().Role, RoleReviewer)
		}
	})

	t.Run("new worker policy validate error", func(t *testing.T) {
		t.Parallel()
		_, err := NewWorker(RoleReviewer, RolePolicy{
			Role:                RoleReviewer,
			SystemPrompt:        "",
			AllowedTools:        []string{"bash"},
			MaxToolCallsPerStep: 1,
			RequiredSections:    []string{"summary"},
		}, nil)
		if err == nil {
			t.Fatalf("expected policy validate error")
		}
	})

	t.Run("start invalid task", func(t *testing.T) {
		t.Parallel()
		policy, err := DefaultRolePolicy(RoleCoder)
		if err != nil {
			t.Fatalf("DefaultRolePolicy() error = %v", err)
		}
		wr, err := NewWorker(RoleCoder, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(Task{}, Budget{}, Capability{}); err == nil {
			t.Fatalf("expected task validate error")
		}
	})

	t.Run("step hits pre-run max-steps guard", func(t *testing.T) {
		t.Parallel()
		policy, err := DefaultRolePolicy(RoleResearcher)
		if err != nil {
			t.Fatalf("DefaultRolePolicy() error = %v", err)
		}
		wr, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			return StepOutput{Done: false}, nil
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(Task{ID: "t-max-pre", Goal: "goal"}, Budget{MaxSteps: 2}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		impl := wr.(*worker)
		impl.mu.Lock()
		impl.stepCount = impl.budget.MaxSteps
		impl.mu.Unlock()
		step, err := wr.Step(context.Background())
		if err != nil {
			t.Fatalf("Step() error = %v", err)
		}
		if !step.Done || step.State != StateFailed {
			t.Fatalf("expected max-steps failure, got %+v", step)
		}
	})

	t.Run("step detects state change after engine returns", func(t *testing.T) {
		t.Parallel()
		policy, err := DefaultRolePolicy(RoleResearcher)
		if err != nil {
			t.Fatalf("DefaultRolePolicy() error = %v", err)
		}
		var impl *worker
		wr, err := NewWorker(RoleResearcher, policy, EngineFunc(func(ctx context.Context, input StepInput) (StepOutput, error) {
			impl.mu.Lock()
			impl.state = StateCanceled
			impl.mu.Unlock()
			return StepOutput{Done: false}, nil
		}))
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		var ok bool
		impl, ok = wr.(*worker)
		if !ok {
			t.Fatalf("expected *worker implementation")
		}
		if err := wr.Start(Task{ID: "t-state-change", Goal: "goal"}, Budget{MaxSteps: 3}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if _, err := wr.Step(context.Background()); err == nil {
			t.Fatalf("expected state changed error")
		}
	})

	t.Run("stop completed success and empty reason error", func(t *testing.T) {
		t.Parallel()
		policy, err := DefaultRolePolicy(RoleCoder)
		if err != nil {
			t.Fatalf("DefaultRolePolicy() error = %v", err)
		}
		wr, err := NewWorker(RoleCoder, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		impl := wr.(*worker)
		if err := impl.Start(Task{ID: "t-stop-complete", Goal: "goal"}, Budget{MaxSteps: 2}, Capability{}); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err := impl.Stop(StopReason(" ")); err == nil {
			t.Fatalf("expected empty stop reason error")
		}
		impl.output = Output{
			Summary:     "done",
			Findings:    []string{"f1"},
			Patches:     []string{"p1"},
			Risks:       []string{"r1"},
			NextActions: []string{"n1"},
			Artifacts:   []string{"a1"},
		}
		if err := impl.Stop(StopReasonCompleted); err != nil {
			t.Fatalf("Stop(Completed) error = %v", err)
		}
		if impl.State() != StateSucceeded {
			t.Fatalf("state=%q, want %q", impl.State(), StateSucceeded)
		}
	})

	t.Run("capability tool_use_mode can override policy", func(t *testing.T) {
		t.Parallel()
		policy, err := DefaultRolePolicy(RoleReviewer)
		if err != nil {
			t.Fatalf("DefaultRolePolicy() error = %v", err)
		}
		wr, err := NewWorker(RoleReviewer, policy, nil)
		if err != nil {
			t.Fatalf("NewWorker() error = %v", err)
		}
		if err := wr.Start(
			Task{ID: "t-mode-override", Goal: "goal"},
			Budget{MaxSteps: 1},
			Capability{ToolUseMode: ToolUseModeDisabled},
		); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if got := wr.Policy().ToolUseMode; got != ToolUseModeDisabled {
			t.Fatalf("ToolUseMode = %q, want %q", got, ToolUseModeDisabled)
		}
	})
}
