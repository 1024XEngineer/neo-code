package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// worker 是 WorkerRuntime 的默认实现，负责封装单任务生命周期。
type worker struct {
	mu         sync.RWMutex
	role       Role
	policy     RolePolicy
	engine     Engine
	state      State
	task       Task
	budget     Budget
	capability Capability
	stepCount  int
	trace      []string
	startedAt  time.Time
	endedAt    time.Time
	stopReason StopReason
	output     Output
	lastErr    string
}

const traceWindowSize = 16

// NewWorker 根据角色、策略与引擎创建一个 WorkerRuntime 实例。
func NewWorker(role Role, policy RolePolicy, engine Engine) (WorkerRuntime, error) {
	if !role.Valid() {
		return nil, errorsf("invalid role %q", role)
	}
	if policy.Role == "" {
		policy.Role = role
	}
	if policy.Role != role {
		return nil, errorsf("policy role %q does not match worker role %q", policy.Role, role)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if engine == nil {
		engine = defaultEngine{}
	}

	return &worker{
		role:   role,
		policy: policy,
		engine: engine,
		state:  StateIdle,
	}, nil
}

// Start 初始化任务执行上下文并进入运行态。
func (w *worker) Start(task Task, budget Budget, capability Capability) error {
	if w == nil {
		return errors.New("subagent: worker is nil")
	}
	if err := task.Validate(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != StateIdle {
		return errorsf("worker already started")
	}

	w.task = task
	w.budget = budget.normalize(w.policy.DefaultBudget)
	effectiveCapability, err := bindCapabilityToPolicy(capability.normalize(), w.policy)
	if err != nil {
		return err
	}
	w.capability = effectiveCapability
	w.trace = nil
	w.stepCount = 0
	w.output = Output{}
	w.lastErr = ""
	w.stopReason = ""
	w.startedAt = time.Now()
	w.endedAt = time.Time{}
	w.state = StateRunning
	return nil
}

// Step 执行一次引擎步骤，并在满足终止条件时更新终态结果。
func (w *worker) Step(ctx context.Context) (StepResult, error) {
	if w == nil {
		return StepResult{}, errors.New("subagent: worker is nil")
	}
	if err := ctx.Err(); err != nil {
		return StepResult{}, err
	}

	w.mu.Lock()
	if w.state != StateRunning {
		state := w.state
		w.mu.Unlock()
		return StepResult{}, errorsf("worker is not running, current state=%s", state)
	}
	if w.budget.Timeout > 0 && time.Since(w.startedAt) >= w.budget.Timeout {
		result := w.finishLocked(StateFailed, StopReasonTimeout, Output{}, errorsf("worker timeout"))
		w.mu.Unlock()
		return StepResult{State: result.State, Done: true, Step: result.StepCount}, nil
	}
	if w.stepCount >= w.budget.MaxSteps {
		result := w.finishLocked(StateFailed, StopReasonMaxSteps, Output{}, errorsf("worker reached max steps"))
		w.mu.Unlock()
		return StepResult{State: result.State, Done: true, Step: result.StepCount}, nil
	}

	input := StepInput{
		Role:       w.role,
		Policy:     w.policy,
		Task:       w.task,
		Budget:     w.budget,
		Capability: w.capability,
		StepIndex:  w.stepCount + 1,
		Trace:      cloneRecentTrace(w.trace, traceWindowSize),
	}
	w.mu.Unlock()

	stepOutput, err := w.engine.RunStep(ctx, input)
	if err != nil {
		w.mu.Lock()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result := w.finishLocked(StateCanceled, StopReasonCanceled, Output{}, nil)
			w.mu.Unlock()
			return StepResult{State: result.State, Done: true, Step: result.StepCount}, err
		}
		result := w.finishLocked(StateFailed, StopReasonError, Output{}, err)
		w.mu.Unlock()
		return StepResult{State: result.State, Done: true, Step: result.StepCount}, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != StateRunning {
		return StepResult{}, errorsf("worker is not running, current state=%s", w.state)
	}

	w.stepCount++
	delta := strings.TrimSpace(stepOutput.Delta)
	if delta != "" {
		w.trace = appendTraceBounded(w.trace, delta, traceWindowSize)
	}

	if stepOutput.Done {
		if err := validateOutputContract(w.policy, stepOutput.Output); err != nil {
			result := w.finishLocked(StateFailed, StopReasonError, Output{}, err)
			return StepResult{State: result.State, Done: true, Step: result.StepCount, Delta: delta}, err
		}
		result := w.finishLocked(StateSucceeded, StopReasonCompleted, stepOutput.Output, nil)
		return StepResult{State: result.State, Done: true, Step: result.StepCount, Delta: delta}, nil
	}

	if w.stepCount >= w.budget.MaxSteps {
		result := w.finishLocked(StateFailed, StopReasonMaxSteps, Output{}, errorsf("worker reached max steps"))
		return StepResult{State: result.State, Done: true, Step: result.StepCount, Delta: delta}, nil
	}

	return StepResult{
		State: StateRunning,
		Done:  false,
		Step:  w.stepCount,
		Delta: delta,
	}, nil
}

// bindCapabilityToPolicy 将 capability 约束在角色策略允许的工具集合内。
func bindCapabilityToPolicy(capability Capability, policy RolePolicy) (Capability, error) {
	if len(capability.AllowedPaths) > 0 {
		return Capability{}, errorsf("capability allowed paths is not supported yet")
	}

	allowedPolicyTools := dedupeAndTrim(policy.AllowedTools)
	allowedSet := make(map[string]struct{}, len(allowedPolicyTools))
	for _, tool := range allowedPolicyTools {
		allowedSet[strings.ToLower(strings.TrimSpace(tool))] = struct{}{}
	}

	if len(capability.AllowedTools) == 0 {
		capability.AllowedTools = append([]string(nil), allowedPolicyTools...)
		return capability, nil
	}

	effective := make([]string, 0, len(capability.AllowedTools))
	disallowed := make([]string, 0)
	for _, tool := range capability.AllowedTools {
		normalized := strings.ToLower(strings.TrimSpace(tool))
		if _, ok := allowedSet[normalized]; !ok {
			disallowed = append(disallowed, tool)
			continue
		}
		effective = append(effective, tool)
	}
	if len(disallowed) > 0 {
		return Capability{}, errorsf("capability contains disallowed tools: %s", strings.Join(disallowed, ", "))
	}
	capability.AllowedTools = effective
	return capability, nil
}

// appendTraceBounded 将新增 trace 追加到切片尾部，并保证内部存储长度不超过上限。
func appendTraceBounded(trace []string, delta string, limit int) []string {
	trace = append(trace, delta)
	if limit <= 0 || len(trace) <= limit {
		return trace
	}
	start := len(trace) - limit
	copy(trace, trace[start:])
	return trace[:limit]
}

// cloneRecentTrace 复制最近 limit 条 trace，避免每步复制完整历史导致复杂度放大。
func cloneRecentTrace(trace []string, limit int) []string {
	if len(trace) == 0 {
		return nil
	}
	if limit <= 0 || len(trace) <= limit {
		return append([]string(nil), trace...)
	}
	start := len(trace) - limit
	return append([]string(nil), trace[start:]...)
}

// Stop 主动终止运行中的 worker，并按终止原因映射最终状态。
func (w *worker) Stop(reason StopReason) error {
	if w == nil {
		return errors.New("subagent: worker is nil")
	}
	if strings.TrimSpace(string(reason)) == "" {
		return errorsf("stop reason is required")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != StateRunning {
		if w.state.Terminal() {
			return nil
		}
		return errorsf("worker is not running, current state=%s", w.state)
	}

	switch reason {
	case StopReasonCompleted:
		if err := validateOutputContract(w.policy, w.output); err != nil {
			return err
		}
		w.finishLocked(StateSucceeded, reason, w.output, nil)
	case StopReasonCanceled:
		w.finishLocked(StateCanceled, reason, w.output, nil)
	case StopReasonTimeout, StopReasonMaxSteps, StopReasonError:
		w.finishLocked(StateFailed, reason, w.output, nil)
	default:
		return errorsf("unsupported stop reason %q", reason)
	}
	return nil
}

// Result 返回 worker 的最终结构化结果。
func (w *worker) Result() (Result, error) {
	if w == nil {
		return Result{}, errors.New("subagent: worker is nil")
	}

	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.state.Terminal() {
		return Result{}, errorsf("worker is not finished")
	}
	return w.snapshotLocked(), nil
}

// State 返回当前 worker 生命周期状态。
func (w *worker) State() State {
	if w == nil {
		return StateIdle
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

// Policy 返回当前 worker 角色策略副本。
func (w *worker) Policy() RolePolicy {
	if w == nil {
		return RolePolicy{}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.policy
}

// finishLocked 在持有写锁时将 worker 切换为终态并返回结果快照。
func (w *worker) finishLocked(state State, reason StopReason, output Output, err error) Result {
	w.state = state
	w.stopReason = reason
	w.endedAt = time.Now()
	w.output = output.normalize()
	if err != nil {
		w.lastErr = err.Error()
	}
	return w.snapshotLocked()
}

// snapshotLocked 在持有读锁或写锁时构造稳定结果快照。
func (w *worker) snapshotLocked() Result {
	return Result{
		Role:       w.role,
		TaskID:     w.task.ID,
		State:      w.state,
		StopReason: w.stopReason,
		StartedAt:  w.startedAt,
		EndedAt:    w.endedAt,
		StepCount:  w.stepCount,
		Budget:     w.budget,
		Capability: w.capability,
		Output:     w.output,
		Error:      w.lastErr,
	}
}
