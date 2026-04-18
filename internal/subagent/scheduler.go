package subagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
)

// Scheduler 负责按 Todo DAG 依赖关系驱动子代理任务执行与状态回写。
type Scheduler struct {
	store   TodoStore
	factory Factory
	cfg     SchedulerConfig
}

type runningTask struct {
	id      string
	attempt int
}

type taskOutcome struct {
	id      string
	attempt int
	result  Result
	err     error
}

type schedulerState struct {
	running    map[string]runningTask
	readySince map[string]time.Time
	started    map[string]int
	progress   map[string]string
	outcomes   chan taskOutcome
}

var errSchedulerFailFast = errors.New("subagent: scheduler fail-fast triggered")

// NewScheduler 创建 Task DAG 调度器，并校验核心依赖是否可用。
func NewScheduler(store TodoStore, factory Factory, cfg SchedulerConfig) (*Scheduler, error) {
	if store == nil {
		return nil, errorsf("scheduler todo store is nil")
	}
	if factory == nil {
		return nil, errorsf("scheduler factory is nil")
	}
	return &Scheduler{
		store:   store,
		factory: factory,
		cfg:     cfg.normalize(),
	}, nil
}

// Run 执行一次调度轮次，直到所有任务终态、上下文取消或出现不可恢复错误。
func (s *Scheduler) Run(ctx context.Context) (ScheduleResult, error) {
	if err := ctx.Err(); err != nil {
		return ScheduleResult{}, err
	}

	recovered, recoveredFailed, err := s.recoverInterruptedTodos()
	if err != nil {
		return ScheduleResult{}, err
	}

	initial := s.store.ListTodos()
	graph, err := buildTaskGraph(initial)
	if err != nil {
		return ScheduleResult{}, err
	}

	now := s.cfg.Clock()
	result := ScheduleResult{
		StartedAt: now,
		Total:     len(graph.order),
		Retried:   make(map[string]int),
		Recovered: append([]string(nil), recovered...),
		Failed:    append([]string(nil), recoveredFailed...),
	}
	state := newSchedulerState(s.cfg.MaxConcurrency)
	finalize := func(current ScheduleResult) ScheduleResult {
		current.EndedAt = s.cfg.Clock()
		current.BlockedLeft = collectBlockedLeft(graph.order, s.store.ListTodos(), state.running)
		s.emit(SchedulerEvent{
			Type:      SchedulerEventFinished,
			QueueSize: len(current.BlockedLeft),
			Running:   len(state.running),
			At:        current.EndedAt,
		})
		return current
	}

	for {
		if err := ctx.Err(); err != nil {
			s.cancelRunningTodos(state, err)
			return finalize(result), err
		}

		snapshot := mapTodosByID(s.store.ListTodos())
		ready, err := s.collectReadyTasks(snapshot, graph, state)
		if err != nil {
			s.cancelRunningTodos(state, err)
			return finalize(result), err
		}

		s.pruneReadySince(state, ready)
		s.sortReadyTasks(ready, state.readySince)

		started, err := s.startReadyTasks(ctx, ready, snapshot, state)
		if err != nil {
			s.cancelRunningTodos(state, err)
			return finalize(result), err
		}
		if started > 0 {
			continue
		}

		if len(state.running) == 0 {
			if !hasSchedulablePotential(graph.order, snapshot) {
				return finalize(result), nil
			}
			if err := waitWithContext(ctx, s.nextPollDelay(snapshot)); err != nil {
				s.cancelRunningTodos(state, err)
				return finalize(result), err
			}
			continue
		}

		if err := s.handleOneOutcome(ctx, state, &result); err != nil {
			s.cancelRunningTodos(state, err)
			return finalize(result), err
		}
	}
}

// recoverInterruptedTodos 在调度开始前恢复遗留 in_progress 任务，确保重启后可继续推进。
func (s *Scheduler) recoverInterruptedTodos() ([]string, []string, error) {
	items := s.store.ListTodos()
	if len(items) == 0 {
		return nil, nil, nil
	}

	now := s.cfg.Clock()
	recovered := make([]string, 0, len(items))
	failed := make([]string, 0, len(items))
	for _, item := range items {
		if !todoDispatchableBySubAgent(item) {
			continue
		}
		if item.Status != agentsession.TodoStatusInProgress {
			continue
		}

		reason := strings.TrimSpace(s.cfg.RecoveryReason)
		if reason == "" {
			reason = "recovered interrupted subagent execution"
		}
		retryLimit := item.RetryLimit
		if retryLimit <= 0 {
			retryLimit = s.cfg.MaxRetries
		}
		nextRetry := item.RetryCount + 1
		ownerType := ""
		ownerID := ""

		var (
			status      agentsession.TodoStatus
			nextRetryAt time.Time
		)
		if s.cfg.recoveryAsFailure() || (retryLimit > 0 && nextRetry > retryLimit) {
			status = agentsession.TodoStatusFailed
			nextRetryAt = time.Time{}
		} else {
			status = agentsession.TodoStatusBlocked
			nextRetryAt = now
		}

		patch := agentsession.TodoPatch{
			Status:        &status,
			OwnerType:     &ownerType,
			OwnerID:       &ownerID,
			FailureReason: &reason,
			RetryCount:    &nextRetry,
			RetryLimit:    &retryLimit,
			NextRetryAt:   &nextRetryAt,
		}
		if err := s.store.UpdateTodo(item.ID, patch, item.Revision); err != nil {
			if isRevisionConflict(err) {
				continue
			}
			return nil, nil, fmt.Errorf("subagent: recover interrupted todo %q: %w", item.ID, err)
		}
		recovered = append(recovered, item.ID)
		if status == agentsession.TodoStatusFailed {
			failed = append(failed, item.ID)
		}

		if status == agentsession.TodoStatusBlocked {
			s.emit(SchedulerEvent{
				Type:    SchedulerEventSubAgentRetried,
				TaskID:  item.ID,
				Attempt: nextRetry,
				Reason:  "recovered",
				At:      now,
			})
		} else {
			s.emit(SchedulerEvent{
				Type:    SchedulerEventSubAgentFailed,
				TaskID:  item.ID,
				Attempt: nextRetry,
				Reason:  reason,
				At:      now,
			})
		}
	}
	return recovered, failed, nil
}

// newSchedulerState 初始化调度运行态，避免循环中重复分配映射与通道。
func newSchedulerState(maxConcurrency int) *schedulerState {
	buffer := maxConcurrency
	if buffer < 1 {
		buffer = 1
	}
	return &schedulerState{
		running:    make(map[string]runningTask, maxConcurrency),
		readySince: make(map[string]time.Time, 32),
		started:    make(map[string]int, 32),
		progress:   make(map[string]string, 32),
		outcomes:   make(chan taskOutcome, buffer),
	}
}

// collectReadyTasks 基于依赖关系、重试窗口与当前状态筛选可执行任务。
func (s *Scheduler) collectReadyTasks(
	snapshot map[string]agentsession.TodoItem,
	graph *taskGraph,
	state *schedulerState,
) ([]agentsession.TodoItem, error) {
	now := s.cfg.Clock()
	ready := make([]agentsession.TodoItem, 0, len(graph.order))

	for _, id := range graph.order {
		item, ok := snapshot[id]
		if !ok || item.Status.IsTerminal() {
			continue
		}
		if !todoDispatchableBySubAgent(item) {
			continue
		}
		if _, running := state.running[id]; running {
			continue
		}

		depsSatisfied := dependenciesCompleted(item, snapshot)
		if !depsSatisfied {
			if err := s.ensureBlocked(item, "dependency_unmet", state); err != nil {
				return nil, err
			}
			continue
		}

		item, ok, err := s.ensureReadyStatus(item)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		if !item.NextRetryAt.IsZero() && now.Before(item.NextRetryAt) {
			s.emit(SchedulerEvent{
				Type:      SchedulerEventBlocked,
				TaskID:    item.ID,
				Reason:    "retry_backoff",
				QueueSize: len(ready),
				Running:   len(state.running),
				At:        now,
			})
			s.emitProgressIfChanged(state, SchedulerEvent{
				Type:      SchedulerEventSubAgentProgress,
				TaskID:    item.ID,
				Attempt:   item.RetryCount + 1,
				QueueSize: len(ready),
				Running:   len(state.running),
				Reason:    "retry_backoff",
				At:        now,
			})
			continue
		}
		ready = append(ready, item.Clone())
		if _, exists := state.readySince[item.ID]; !exists {
			state.readySince[item.ID] = now
		}
	}
	return ready, nil
}

// ensureBlocked 将未满足依赖的任务收敛到 blocked 状态并发出可观测事件。
func (s *Scheduler) ensureBlocked(item agentsession.TodoItem, reason string, state *schedulerState) error {
	if item.Status != agentsession.TodoStatusBlocked {
		status := agentsession.TodoStatusBlocked
		patch := agentsession.TodoPatch{Status: &status}
		if err := s.store.UpdateTodo(item.ID, patch, item.Revision); err != nil && !isRevisionConflict(err) {
			return fmt.Errorf("subagent: mark todo blocked: %w", err)
		}
	}

	running := 0
	if state != nil {
		running = len(state.running)
	}
	now := s.cfg.Clock()
	s.emit(SchedulerEvent{
		Type:    SchedulerEventBlocked,
		TaskID:  item.ID,
		Reason:  reason,
		Running: running,
		At:      now,
	})
	s.emitProgressIfChanged(state, SchedulerEvent{
		Type:    SchedulerEventSubAgentProgress,
		TaskID:  item.ID,
		Attempt: item.RetryCount + 1,
		Reason:  reason,
		Running: running,
		At:      now,
	})
	return nil
}

// ensureReadyStatus 处理 blocked 到 pending 的解锁与可执行状态判定。
func (s *Scheduler) ensureReadyStatus(item agentsession.TodoItem) (agentsession.TodoItem, bool, error) {
	switch item.Status {
	case agentsession.TodoStatusPending, agentsession.TodoStatusInProgress:
		return item, true, nil
	case agentsession.TodoStatusBlocked:
		if !item.NextRetryAt.IsZero() && s.cfg.Clock().Before(item.NextRetryAt) {
			return item, false, nil
		}
		status := agentsession.TodoStatusPending
		zeroRetry := time.Time{}
		patch := agentsession.TodoPatch{
			Status:      &status,
			NextRetryAt: &zeroRetry,
		}
		if err := s.store.UpdateTodo(item.ID, patch, item.Revision); err != nil {
			if isRevisionConflict(err) {
				return item, false, nil
			}
			return item, false, fmt.Errorf("subagent: unlock blocked todo: %w", err)
		}
		next, ok := s.store.FindTodo(item.ID)
		if !ok {
			return item, false, nil
		}
		return next, true, nil
	default:
		return item, false, nil
	}
}

// startReadyTasks 在并发上限内领取并启动可执行任务。
func (s *Scheduler) startReadyTasks(
	ctx context.Context,
	ready []agentsession.TodoItem,
	snapshot map[string]agentsession.TodoItem,
	state *schedulerState,
) (int, error) {
	if len(ready) == 0 {
		return 0, nil
	}
	started := 0
	for _, item := range ready {
		if len(state.running) >= s.cfg.MaxConcurrency {
			break
		}
		if _, exists := state.running[item.ID]; exists {
			continue
		}
		attempt := item.RetryCount + 1
		workerID := fmt.Sprintf("%s-%s", s.cfg.WorkerIDPrefix, item.ID)
		if err := s.store.ClaimTodo(item.ID, agentsession.TodoOwnerTypeSubAgent, workerID, item.Revision); err != nil {
			if isRevisionConflict(err) {
				continue
			}
			return started, fmt.Errorf("subagent: claim todo %q: %w", item.ID, err)
		}

		role := s.cfg.RoleSelector(item)
		budget := s.cfg.BudgetSelector(item, s.cfg.DefaultBudget).normalize(s.cfg.DefaultBudget)
		capability := s.cfg.Capabilities(item).normalize()
		contextSlice := s.cfg.ContextBuilder(TaskContextSliceInput{
			Task:                   item,
			Todos:                  snapshot,
			ReadOnlyTodos:          true,
			ActivatedSkills:        s.cfg.ContextSkills(item, snapshot),
			RelatedFiles:           s.cfg.ContextFiles(item, snapshot),
			MaxChars:               s.cfg.ContextMaxChars,
			MaxTodoFragments:       s.cfg.ContextMaxTodoFragments,
			MaxDependencyArtifacts: s.cfg.ContextMaxDependencyArtifacts,
			MaxRelatedFiles:        s.cfg.ContextMaxRelatedFiles,
		})
		task := Task{
			ID:             item.ID,
			Goal:           strings.TrimSpace(item.Content),
			ExpectedOutput: strings.Join(item.Acceptance, "\n"),
			ContextSlice:   contextSlice,
		}

		state.running[item.ID] = runningTask{id: item.ID, attempt: attempt}
		state.started[item.ID] = attempt
		started++

		s.emit(SchedulerEvent{
			Type:      SchedulerEventQueued,
			TaskID:    item.ID,
			Attempt:   attempt,
			QueueSize: len(ready),
			Running:   len(state.running),
			At:        s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventClaimed,
			TaskID:  item.ID,
			Attempt: attempt,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventRunning,
			TaskID:  item.ID,
			Attempt: attempt,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventSubAgentStarted,
			TaskID:  item.ID,
			Attempt: attempt,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emitProgressIfChanged(state, SchedulerEvent{
			Type:    SchedulerEventSubAgentProgress,
			TaskID:  item.ID,
			Attempt: attempt,
			Running: len(state.running),
			Reason:  "running",
			At:      s.cfg.Clock(),
		})

		go s.executeTaskAsync(ctx, state.outcomes, taskOutcome{
			id:      item.ID,
			attempt: attempt,
		}, scheduleTaskInput{
			Task:       task,
			Role:       role,
			Budget:     budget,
			Capability: capability,
		})
	}
	return started, nil
}

// executeTaskAsync 在独立 goroutine 中执行任务，并把结果回传给调度主循环。
func (s *Scheduler) executeTaskAsync(
	parent context.Context,
	out chan<- taskOutcome,
	base taskOutcome,
	input scheduleTaskInput,
) {
	execCtx := parent
	cancel := func() {}
	if input.Budget.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(parent, input.Budget.Timeout)
	}
	defer cancel()

	result, err := executeTaskWithFactory(execCtx, s.factory, input)
	base.result = result
	base.err = err
	select {
	case out <- base:
	default:
	}
}

// handleOneOutcome 消费单个任务执行结果并完成成功/失败/重试回写。
func (s *Scheduler) handleOneOutcome(ctx context.Context, state *schedulerState, summary *ScheduleResult) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outcome := <-state.outcomes:
		running, ok := state.running[outcome.id]
		if !ok {
			return nil
		}
		if outcome.attempt != running.attempt {
			return nil
		}
		delete(state.running, outcome.id)
		delete(state.readySince, outcome.id)
		delete(state.progress, outcome.id)
		return s.applyOutcome(outcome, state, summary)
	case <-time.After(s.cfg.PollInterval):
		return nil
	}
}

// applyOutcome 根据执行结果写回 Todo 状态，并更新调度统计与事件。
func (s *Scheduler) applyOutcome(outcome taskOutcome, state *schedulerState, summary *ScheduleResult) error {
	current, ok := s.store.FindTodo(outcome.id)
	if !ok {
		return nil
	}
	if current.Status.IsTerminal() {
		return nil
	}

	if outcome.err == nil && outcome.result.State == StateSucceeded {
		artifacts := append([]string(nil), outcome.result.Output.Artifacts...)
		if err := s.store.CompleteTodo(current.ID, artifacts, current.Revision); err != nil {
			if isRevisionConflict(err) {
				return nil
			}
			return fmt.Errorf("subagent: complete todo %q: %w", current.ID, err)
		}
		appendUniqueString(&summary.Succeeded, current.ID)
		s.emit(SchedulerEvent{
			Type:    SchedulerEventCompleted,
			TaskID:  current.ID,
			Attempt: outcome.attempt,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventSubAgentCompleted,
			TaskID:  current.ID,
			Attempt: outcome.attempt,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		return nil
	}

	return s.handleTaskFailure(current, outcome, state, summary)
}

// handleTaskFailure 处理失败回写，按重试预算决定重排或终态失败。
func (s *Scheduler) handleTaskFailure(
	current agentsession.TodoItem,
	outcome taskOutcome,
	state *schedulerState,
	summary *ScheduleResult,
) error {
	reason := resolveTaskFailureReason(outcome)
	retryLimit := current.RetryLimit
	if retryLimit <= 0 {
		retryLimit = s.cfg.MaxRetries
	}
	nextRetryCount := current.RetryCount + 1

	if nextRetryCount <= retryLimit {
		backoff := s.cfg.Backoff(nextRetryCount)
		nextRetryAt := s.cfg.Clock().Add(backoff)
		status := agentsession.TodoStatusBlocked
		ownerType := ""
		ownerID := ""
		patch := agentsession.TodoPatch{
			Status:        &status,
			OwnerType:     &ownerType,
			OwnerID:       &ownerID,
			FailureReason: &reason,
			RetryCount:    &nextRetryCount,
			RetryLimit:    &retryLimit,
			NextRetryAt:   &nextRetryAt,
		}
		if err := s.store.UpdateTodo(current.ID, patch, current.Revision); err != nil {
			if isRevisionConflict(err) {
				return nil
			}
			return fmt.Errorf("subagent: schedule retry for todo %q: %w", current.ID, err)
		}

		summary.Retried[current.ID] = nextRetryCount
		s.emit(SchedulerEvent{
			Type:    SchedulerEventRetryScheduled,
			TaskID:  current.ID,
			Attempt: nextRetryCount,
			Reason:  reason,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventSubAgentRetried,
			TaskID:  current.ID,
			Attempt: nextRetryCount,
			Reason:  reason,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		return nil
	}

	status := agentsession.TodoStatusFailed
	zeroRetryAt := time.Time{}
	ownerType := ""
	ownerID := ""
	patch := agentsession.TodoPatch{
		Status:        &status,
		OwnerType:     &ownerType,
		OwnerID:       &ownerID,
		FailureReason: &reason,
		RetryCount:    &nextRetryCount,
		RetryLimit:    &retryLimit,
		NextRetryAt:   &zeroRetryAt,
	}
	if err := s.store.UpdateTodo(current.ID, patch, current.Revision); err != nil {
		if isRevisionConflict(err) {
			return nil
		}
		return fmt.Errorf("subagent: fail todo %q: %w", current.ID, err)
	}

	appendUniqueString(&summary.Failed, current.ID)
	s.emit(SchedulerEvent{
		Type:    SchedulerEventFailed,
		TaskID:  current.ID,
		Attempt: nextRetryCount,
		Reason:  reason,
		Running: len(state.running),
		At:      s.cfg.Clock(),
	})
	s.emit(SchedulerEvent{
		Type:    SchedulerEventSubAgentFailed,
		TaskID:  current.ID,
		Attempt: nextRetryCount,
		Reason:  reason,
		Running: len(state.running),
		At:      s.cfg.Clock(),
	})
	if s.cfg.failFastEnabled() {
		return fmt.Errorf("%w: task=%s reason=%s", errSchedulerFailFast, current.ID, reason)
	}
	return nil
}

// cancelRunningTodos 在调度中断时把仍在执行的任务统一回写为 canceled。
func (s *Scheduler) cancelRunningTodos(state *schedulerState, cause error) {
	if state == nil || len(state.running) == 0 {
		return
	}
	reason := "scheduler canceled"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = strings.TrimSpace(cause.Error())
	}
	status := agentsession.TodoStatusCanceled
	for id := range state.running {
		item, ok := s.store.FindTodo(id)
		if !ok || item.Status.IsTerminal() {
			continue
		}
		ownerType := ""
		ownerID := ""
		patch := agentsession.TodoPatch{
			Status:        &status,
			OwnerType:     &ownerType,
			OwnerID:       &ownerID,
			FailureReason: &reason,
		}
		if err := s.store.UpdateTodo(item.ID, patch, item.Revision); err != nil && !isRevisionConflict(err) {
			continue
		}
		s.emit(SchedulerEvent{
			Type:    SchedulerEventCanceled,
			TaskID:  item.ID,
			Reason:  reason,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
		s.emit(SchedulerEvent{
			Type:    SchedulerEventSubAgentCanceled,
			TaskID:  item.ID,
			Reason:  reason,
			Running: len(state.running),
			At:      s.cfg.Clock(),
		})
	}
}

// sortReadyTasks 按优先级与等待时间排序，兼顾高优先级与公平性。
func (s *Scheduler) sortReadyTasks(ready []agentsession.TodoItem, readySince map[string]time.Time) {
	now := s.cfg.Clock()
	agingWindow := 5 * s.cfg.PollInterval
	if agingWindow <= 0 {
		agingWindow = time.Second
	}
	sort.SliceStable(ready, func(i, j int) bool {
		left := ready[i]
		right := ready[j]
		lp := effectivePriority(left, readySince[left.ID], now, agingWindow)
		rp := effectivePriority(right, readySince[right.ID], now, agingWindow)
		if lp != rp {
			return lp > rp
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})
}

// pruneReadySince 清理当前不可调度任务的 ready 时间戳，避免无界增长。
func (s *Scheduler) pruneReadySince(state *schedulerState, ready []agentsession.TodoItem) {
	if len(state.readySince) == 0 {
		return
	}
	allowed := make(map[string]struct{}, len(ready))
	for _, item := range ready {
		allowed[item.ID] = struct{}{}
	}
	for id := range state.readySince {
		if _, ok := allowed[id]; !ok {
			delete(state.readySince, id)
		}
	}
}

// nextPollDelay 计算下一次轮询等待时间，优先对齐最近重试窗口。
func (s *Scheduler) nextPollDelay(snapshot map[string]agentsession.TodoItem) time.Duration {
	now := s.cfg.Clock()
	minDelay := s.cfg.PollInterval
	if minDelay <= 0 {
		minDelay = 200 * time.Millisecond
	}
	for _, item := range snapshot {
		if item.Status.IsTerminal() || item.NextRetryAt.IsZero() {
			continue
		}
		if !item.NextRetryAt.After(now) {
			return 0
		}
		delay := item.NextRetryAt.Sub(now)
		if delay < minDelay {
			minDelay = delay
		}
	}
	return minDelay
}

// emit 发射调度事件，统一补齐时间戳并调用观察器。
func (s *Scheduler) emit(event SchedulerEvent) {
	if event.At.IsZero() {
		event.At = s.cfg.Clock()
	}
	s.cfg.Observer(event)
}

// emitProgressIfChanged 仅在任务进度状态变化时发射 progress 事件，避免轮询路径的重复噪声。
func (s *Scheduler) emitProgressIfChanged(state *schedulerState, event SchedulerEvent) {
	if event.Type != SchedulerEventSubAgentProgress {
		s.emit(event)
		return
	}
	if state == nil {
		s.emit(event)
		return
	}

	key := fmt.Sprintf("%d|%s", event.Attempt, event.Reason)
	if state.progress[event.TaskID] == key {
		return
	}
	state.progress[event.TaskID] = key
	s.emit(event)
}

// mapTodosByID 将 todo 列表转为 ID 索引映射，便于依赖与状态查询。
func mapTodosByID(items []agentsession.TodoItem) map[string]agentsession.TodoItem {
	result := make(map[string]agentsession.TodoItem, len(items))
	for _, item := range items {
		result[item.ID] = item
	}
	return result
}

// dependenciesCompleted 判断任务依赖是否全部处于 completed 状态。
func dependenciesCompleted(item agentsession.TodoItem, byID map[string]agentsession.TodoItem) bool {
	for _, depID := range item.Dependencies {
		dependency, ok := byID[depID]
		if !ok || dependency.Status != agentsession.TodoStatusCompleted {
			return false
		}
	}
	return true
}

// todoDispatchableBySubAgent 判断任务是否应由 SubAgent 调度器执行。
func todoDispatchableBySubAgent(item agentsession.TodoItem) bool {
	return strings.EqualFold(strings.TrimSpace(item.Executor), agentsession.TodoExecutorSubAgent)
}

// hasSchedulablePotential 判断当前非终态任务是否仍可能通过调度推进到可执行状态。
func hasSchedulablePotential(order []string, byID map[string]agentsession.TodoItem) bool {
	memo := make(map[string]bool, len(byID))
	visiting := make(map[string]bool, len(byID))

	var satisfiable func(id string) bool
	satisfiable = func(id string) bool {
		item, ok := byID[id]
		if !ok {
			return false
		}
		if item.Status == agentsession.TodoStatusCompleted {
			return true
		}
		if !todoDispatchableBySubAgent(item) {
			return false
		}
		if item.Status == agentsession.TodoStatusFailed || item.Status == agentsession.TodoStatusCanceled {
			return false
		}
		if known, ok := memo[id]; ok {
			return known
		}
		if visiting[id] {
			return false
		}

		visiting[id] = true
		defer delete(visiting, id)
		for _, dependencyID := range item.Dependencies {
			if !satisfiable(dependencyID) {
				memo[id] = false
				return false
			}
		}
		memo[id] = true
		return true
	}

	for _, id := range order {
		item, ok := byID[id]
		if !ok || item.Status.IsTerminal() {
			continue
		}
		if !todoDispatchableBySubAgent(item) {
			continue
		}
		if satisfiable(id) {
			return true
		}
	}
	return false
}

// collectBlockedLeft 汇总结束时仍处于非终态的任务 ID。
func collectBlockedLeft(order []string, items []agentsession.TodoItem, running map[string]runningTask) []string {
	byID := mapTodosByID(items)
	left := make([]string, 0)
	for _, id := range order {
		if _, ok := running[id]; ok {
			left = append(left, id)
			continue
		}
		item, ok := byID[id]
		if !ok || item.Status.IsTerminal() {
			continue
		}
		left = append(left, id)
	}
	return left
}

// effectivePriority 基于原始优先级与等待时长计算动态调度优先级。
func effectivePriority(item agentsession.TodoItem, readySince time.Time, now time.Time, agingWindow time.Duration) int {
	priority := item.Priority
	if readySince.IsZero() || agingWindow <= 0 {
		return priority
	}
	if now.Before(readySince) {
		return priority
	}
	ageBoost := int(now.Sub(readySince) / agingWindow)
	return priority + ageBoost
}

// appendUniqueString 追加去重字符串，避免调度统计结果重复计数。
func appendUniqueString(dst *[]string, value string) {
	for _, current := range *dst {
		if current == value {
			return
		}
	}
	*dst = append(*dst, value)
}

// resolveTaskFailureReason 统一提取失败原因，保证回写文本稳定可读。
func resolveTaskFailureReason(outcome taskOutcome) string {
	switch outcome.result.StopReason {
	case StopReasonMaxSteps:
		return "budget exhausted: max steps reached"
	case StopReasonTimeout:
		return "budget exhausted: timeout exceeded"
	}
	if outcome.err != nil {
		if text := strings.TrimSpace(outcome.err.Error()); text != "" {
			return text
		}
	}
	if text := strings.TrimSpace(outcome.result.Error); text != "" {
		return text
	}
	return "subagent task execution failed"
}

// isRevisionConflict 判断错误是否为 revision 竞争冲突，便于调度层重试。
func isRevisionConflict(err error) bool {
	return errors.Is(err, agentsession.ErrRevisionConflict)
}

// waitWithContext 在保留可取消语义的前提下等待指定时长。
func waitWithContext(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
