package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
	"neo-code/internal/subagent"
)

const (
	defaultSubAgentDispatchConcurrency = 2
	defaultSubAgentDispatchPollDelay   = 100 * time.Millisecond
)

// dispatchTodos 在当前轮次执行一次 Todo DAG 调度，并把子代理事件映射到 runtime 事件流。
// 返回值表示本轮是否产生了可观测进展（成功/失败/重试/恢复），用于驱动 runtime 后续控制流。
func (s *Service) dispatchTodos(ctx context.Context, state *runState, snapshot turnSnapshot) (bool, error) {
	if s == nil || state == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	store := newRuntimeSessionMutator(ctx, s, state)
	if store == nil {
		return false, errors.New("runtime: subagent dispatch session mutator is unavailable")
	}
	todos := store.ListTodos()
	if !hasDispatchableSubAgentTodo(todos) {
		return false, nil
	}

	scheduler, err := subagent.NewScheduler(
		store,
		newRuntimeSchedulerFactory(s, state, strings.TrimSpace(snapshot.workdir)),
		subagent.SchedulerConfig{
			MaxConcurrency: resolveSubAgentDispatchConcurrency(),
			PollInterval:   defaultSubAgentDispatchPollDelay,
			FailureMode:    subagent.SchedulerFailureContinueOnError,
			RecoveryMode:   subagent.SchedulerRecoveryRetry,
			Observer: func(event subagent.SchedulerEvent) {
				s.emitSubAgentSchedulerEvent(ctx, state, event)
			},
		},
	)
	if err != nil {
		return false, fmt.Errorf("runtime: create subagent scheduler: %w", err)
	}

	result, err := scheduler.Run(ctx)
	if err != nil {
		return false, fmt.Errorf("runtime: run subagent scheduler: %w", err)
	}
	progressed := len(result.Succeeded) > 0 ||
		len(result.Failed) > 0 ||
		len(result.Recovered) > 0 ||
		len(result.Retried) > 0
	return progressed, nil
}

// hasDispatchableSubAgentTodo 判断当前会话是否存在需要调度的 SubAgent 任务。
func hasDispatchableSubAgentTodo(items []agentsession.TodoItem) bool {
	for _, item := range items {
		if item.Status.IsTerminal() {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Executor), agentsession.TodoExecutorSubAgent) {
			return true
		}
	}
	return false
}

// resolveSubAgentDispatchConcurrency 返回调度并发上限。
func resolveSubAgentDispatchConcurrency() int {
	if defaultSubAgentDispatchConcurrency <= 0 {
		return 1
	}
	return defaultSubAgentDispatchConcurrency
}

// emitSubAgentSchedulerEvent 把 scheduler 事件映射为 runtime 事件。
func (s *Service) emitSubAgentSchedulerEvent(ctx context.Context, state *runState, event subagent.SchedulerEvent) {
	if s == nil || state == nil {
		return
	}

	payload := SubAgentEventPayload{
		TaskID: strings.TrimSpace(event.TaskID),
		Step:   event.Attempt,
		Reason: strings.TrimSpace(event.Reason),
		Delta:  strings.TrimSpace(event.Reason),
	}

	switch event.Type {
	case subagent.SchedulerEventSubAgentStarted:
		payload.State = subagent.StateRunning
		_ = s.emitRunScoped(ctx, EventSubAgentStarted, state, payload)
	case subagent.SchedulerEventSubAgentProgress:
		payload.State = subagent.StateRunning
		_ = s.emitRunScoped(ctx, EventSubAgentProgress, state, payload)
	case subagent.SchedulerEventSubAgentRetried:
		payload.State = subagent.StateRunning
		_ = s.emitRunScoped(ctx, EventSubAgentRetried, state, payload)
	case subagent.SchedulerEventSubAgentCompleted:
		payload.State = subagent.StateSucceeded
		_ = s.emitRunScoped(ctx, EventSubAgentCompleted, state, payload)
	case subagent.SchedulerEventSubAgentFailed:
		payload.State = subagent.StateFailed
		payload.Error = payload.Reason
		_ = s.emitRunScoped(ctx, EventSubAgentFailed, state, payload)
	case subagent.SchedulerEventSubAgentCanceled:
		payload.State = subagent.StateCanceled
		payload.Error = payload.Reason
		_ = s.emitRunScoped(ctx, EventSubAgentCanceled, state, payload)
	case subagent.SchedulerEventBlocked:
		payload.State = subagent.StateRunning
		_ = s.emitRunScoped(ctx, EventSubAgentBlocked, state, payload)
	case subagent.SchedulerEventFinished:
		payload.State = subagent.StateRunning
		payload.Delta = fmt.Sprintf("blocked_left=%d running=%d", event.QueueSize, event.Running)
		_ = s.emitRunScoped(ctx, EventSubAgentFinished, state, payload)
	}
}

// runtimeSchedulerFactory 复用 RunSubAgentTask 链路执行调度任务，保证 provider/tools/security 主链路一致。
type runtimeSchedulerFactory struct {
	service   *Service
	runID     string
	sessionID string
	agentID   string
	workdir   string
}

// newRuntimeSchedulerFactory 创建调度器使用的 subagent 工厂适配器。
func newRuntimeSchedulerFactory(service *Service, state *runState, workdir string) subagent.Factory {
	if state == nil {
		return runtimeSchedulerFactory{service: service}
	}
	return runtimeSchedulerFactory{
		service:   service,
		runID:     strings.TrimSpace(state.runID),
		sessionID: strings.TrimSpace(state.session.ID),
		agentID:   strings.TrimSpace(state.agentID),
		workdir:   strings.TrimSpace(workdir),
	}
}

// Create 按角色创建运行时调度 worker。
func (f runtimeSchedulerFactory) Create(role subagent.Role) (subagent.WorkerRuntime, error) {
	policy, err := subagent.DefaultRolePolicy(role)
	if err != nil {
		return nil, err
	}
	return &runtimeSchedulerWorker{
		service:   f.service,
		role:      role,
		policy:    policy,
		runID:     f.runID,
		sessionID: f.sessionID,
		agentID:   f.agentID,
		workdir:   f.workdir,
		state:     subagent.StateIdle,
	}, nil
}

// runtimeSchedulerWorker 把 scheduler 单任务执行桥接到 RunSubAgentTask。
type runtimeSchedulerWorker struct {
	service    *Service
	role       subagent.Role
	policy     subagent.RolePolicy
	runID      string
	sessionID  string
	agentID    string
	workdir    string
	started    bool
	completed  bool
	task       subagent.Task
	budget     subagent.Budget
	capability subagent.Capability
	state      subagent.State
	result     subagent.Result
	resultErr  error
}

// Start 记录调度输入并进入运行态。
func (w *runtimeSchedulerWorker) Start(task subagent.Task, budget subagent.Budget, capability subagent.Capability) error {
	if w == nil {
		return errors.New("runtime: subagent scheduler worker is nil")
	}
	if err := task.Validate(); err != nil {
		return err
	}
	w.task = task
	w.budget = budget
	w.capability = capability
	w.started = true
	w.completed = false
	w.result = subagent.Result{}
	w.resultErr = nil
	w.state = subagent.StateRunning
	return nil
}

// Step 触发一次 RunSubAgentTask 执行，并以单步完成结果返回给 scheduler。
func (w *runtimeSchedulerWorker) Step(ctx context.Context) (subagent.StepResult, error) {
	if w == nil {
		return subagent.StepResult{}, errors.New("runtime: subagent scheduler worker is nil")
	}
	if !w.started {
		return subagent.StepResult{}, errors.New("runtime: subagent scheduler worker not started")
	}
	if w.completed {
		return subagent.StepResult{}, errors.New("runtime: subagent scheduler worker is not running")
	}
	if err := ctx.Err(); err != nil {
		return subagent.StepResult{}, err
	}
	if w.service == nil {
		return subagent.StepResult{}, errors.New("runtime: subagent scheduler worker service is nil")
	}

	task := w.task
	if strings.TrimSpace(task.Workspace) == "" {
		task.Workspace = w.workdir
	}
	agentID := strings.TrimSpace(w.agentID)
	if agentID == "" {
		agentID = "subagent-dispatch"
	}
	agentID = agentID + ":" + strings.TrimSpace(task.ID)

	result, err := w.service.RunSubAgentTask(ctx, SubAgentTaskInput{
		RunID:      strings.TrimSpace(w.runID),
		SessionID:  strings.TrimSpace(w.sessionID),
		AgentID:    agentID,
		Role:       w.role,
		Task:       task,
		Budget:     w.budget,
		Capability: w.capability,
	})
	if err != nil && strings.TrimSpace(result.TaskID) == "" {
		result = subagent.Result{
			Role:       w.role,
			TaskID:     strings.TrimSpace(task.ID),
			State:      subagent.StateFailed,
			StopReason: subagent.StopReasonError,
			Error:      strings.TrimSpace(err.Error()),
		}
	}

	w.result = result
	w.resultErr = err
	w.completed = true
	w.state = result.State
	if w.state == "" {
		w.state = subagent.StateFailed
	}
	return subagent.StepResult{
		State: w.state,
		Done:  true,
		Step:  result.StepCount,
		Delta: strings.TrimSpace(result.Output.Summary),
	}, err
}

// Stop 将当前 worker 标记为终态。
func (w *runtimeSchedulerWorker) Stop(reason subagent.StopReason) error {
	if w == nil {
		return errors.New("runtime: subagent scheduler worker is nil")
	}
	switch reason {
	case subagent.StopReasonCanceled:
		w.state = subagent.StateCanceled
	case subagent.StopReasonCompleted:
		w.state = subagent.StateSucceeded
	default:
		w.state = subagent.StateFailed
	}
	w.completed = true
	return nil
}

// Result 返回最后一次执行结果。
func (w *runtimeSchedulerWorker) Result() (subagent.Result, error) {
	if w == nil {
		return subagent.Result{}, errors.New("runtime: subagent scheduler worker is nil")
	}
	if !w.completed {
		return subagent.Result{}, errors.New("runtime: subagent scheduler worker is not finished")
	}
	return w.result, w.resultErr
}

// State 返回 worker 当前状态。
func (w *runtimeSchedulerWorker) State() subagent.State {
	if w == nil {
		return subagent.StateIdle
	}
	return w.state
}

// Policy 返回 worker 角色策略快照。
func (w *runtimeSchedulerWorker) Policy() subagent.RolePolicy {
	if w == nil {
		return subagent.RolePolicy{}
	}
	return w.policy
}
