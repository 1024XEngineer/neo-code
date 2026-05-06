package subagent

import (
	"context"
	"errors"
	"path/filepath"
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
	execCtx    ExecutionContext
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

// WorkerOption 定义 Worker 的可选注入项。
type WorkerOption func(*worker)

// withExecutionContext 注入 Worker 运行期上下文。
func withExecutionContext(execCtx ExecutionContext) WorkerOption {
	return func(w *worker) {
		if w == nil {
			return
		}
		w.execCtx = execCtx
	}
}

// NewWorker 根据角色、策略与引擎创建一个 WorkerRuntime 实例。
func NewWorker(role Role, policy RolePolicy, engine Engine, opts ...WorkerOption) (WorkerRuntime, error) {
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

	instance := &worker{
		role:   role,
		policy: policy,
		engine: engine,
		state:  StateIdle,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(instance)
		}
	}
	return instance, nil
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
	if task.TaskType.Valid() {
		w.policy.RequiredSections = RequiredSectionsForTaskType(task.TaskType)
	}
	w.budget = budget.normalize(w.policy.DefaultBudget)
	capabilityInput := capability.normalize()
	if capabilityInput.ToolUseMode != "" {
		if !capabilityInput.ToolUseMode.Valid() {
			return errorsf("capability tool use mode %q is invalid", capabilityInput.ToolUseMode)
		}
		w.policy.ToolUseMode = capabilityInput.ToolUseMode
	}
	if len(capabilityInput.AllowedPaths) == 0 && strings.TrimSpace(task.Workspace) != "" {
		workspace := strings.TrimSpace(task.Workspace)
		if err := validateDefaultWorkspacePath(workspace); err != nil {
			return err
		}
		capabilityInput.AllowedPaths = []string{workspace}
	}
	effectiveCapability, err := bindCapabilityToPolicy(capabilityInput, w.policy)
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
		return terminalStepResult(result, ""), nil
	}
	if w.stepCount >= w.budget.MaxSteps {
		result := w.finishLocked(StateFailed, StopReasonMaxSteps, Output{}, errorsf("worker reached max steps"))
		w.mu.Unlock()
		return terminalStepResult(result, ""), nil
	}

	input := StepInput{
		Role:       w.role,
		Policy:     w.policy,
		Task:       w.task,
		Budget:     w.budget,
		Capability: w.capability,
		RunID:      strings.TrimSpace(w.task.RunID),
		SessionID:  strings.TrimSpace(w.task.SessionID),
		AgentID:    strings.TrimSpace(w.task.AgentID),
		Workdir:    resolveStepWorkdir(w.task.Workspace),
		Executor:   w.execCtx.ToolExecutor,
		StepIndex:  w.stepCount + 1,
		Trace:      cloneRecentTrace(w.trace, traceWindowSize),
	}
	w.mu.Unlock()

	stepOutput, err := w.engine.RunStep(ctx, input)
	if err != nil {
		w.mu.Lock()
		if errors.Is(err, context.DeadlineExceeded) {
			result := w.finishLocked(StateFailed, StopReasonTimeout, Output{}, err)
			w.mu.Unlock()
			return terminalStepResult(result, ""), err
		}
		if errors.Is(err, context.Canceled) {
			result := w.finishLocked(StateCanceled, StopReasonCanceled, Output{}, nil)
			w.mu.Unlock()
			return terminalStepResult(result, ""), err
		}
		result := w.finishLocked(StateFailed, StopReasonError, Output{}, err)
		w.mu.Unlock()
		return terminalStepResult(result, ""), err
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
			return terminalStepResult(result, delta), err
		}
		result := w.finishLocked(StateSucceeded, StopReasonCompleted, stepOutput.Output, nil)
		return terminalStepResult(result, delta), nil
	}

	if w.stepCount >= w.budget.MaxSteps {
		result := w.finishLocked(StateFailed, StopReasonMaxSteps, Output{}, errorsf("worker reached max steps"))
		return terminalStepResult(result, delta), nil
	}

	return StepResult{
		State: StateRunning,
		Done:  false,
		Step:  w.stepCount,
		Delta: delta,
	}, nil
}

// terminalStepResult 将 worker 终态结果映射为单步返回，确保终态字段构造一致。
func terminalStepResult(result Result, delta string) StepResult {
	return StepResult{
		State: result.State,
		Done:  true,
		Step:  result.StepCount,
		Delta: delta,
	}
}

// bindCapabilityToPolicy 将 capability 约束在角色策略允许的工具集合内。
func bindCapabilityToPolicy(capability Capability, policy RolePolicy) (Capability, error) {
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

// validateDefaultWorkspacePath 校验默认注入 capability 的工作区路径，阻断危险根路径绑定。
func validateDefaultWorkspacePath(workspace string) error {
	cleaned := filepath.Clean(strings.TrimSpace(workspace))
	if cleaned == "." {
		return errorsf("task workspace must not be current directory")
	}
	if cleaned == string(filepath.Separator) {
		return errorsf("task workspace must not be filesystem root")
	}
	volume := filepath.VolumeName(cleaned)
	if volume != "" {
		suffix := strings.TrimPrefix(cleaned, volume)
		if suffix == "" || suffix == string(filepath.Separator) {
			return errorsf("task workspace must not be filesystem root")
		}
	}
	return nil
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

// resolveStepWorkdir 解析单步执行所使用的工作目录。
func resolveStepWorkdir(workspace string) string {
	return strings.TrimSpace(workspace)
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
