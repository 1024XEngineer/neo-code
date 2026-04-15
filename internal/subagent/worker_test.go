package subagent

import (
	"context"
	"errors"
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
	}); err == nil {
		t.Fatalf("expected unsupported allowed paths to fail")
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
	if _, err := NewWorker(RoleCoder, RolePolicy{Role: RoleReviewer, SystemPrompt: "p", AllowedTools: []string{"bash"}, RequiredSections: []string{"summary"}}, nil); err == nil {
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
