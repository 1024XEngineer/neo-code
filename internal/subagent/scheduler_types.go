package subagent

import (
	"time"

	agentsession "neo-code/internal/session"
)

// TodoStore 定义调度器读写 Todo 的最小依赖。
type TodoStore interface {
	ListTodos() []agentsession.TodoItem
	FindTodo(id string) (agentsession.TodoItem, bool)
	UpdateTodo(id string, patch agentsession.TodoPatch, expectedRevision int64) error
	ClaimTodo(id string, ownerType string, ownerID string, expectedRevision int64) error
	CompleteTodo(id string, artifacts []string, expectedRevision int64) error
	FailTodo(id string, reason string, expectedRevision int64) error
}

// RoleSelector 根据 Todo 节点选择执行角色。
type RoleSelector func(todo agentsession.TodoItem) Role

// BudgetSelector 根据 Todo 节点生成预算。
type BudgetSelector func(todo agentsession.TodoItem, defaults Budget) Budget

// CapabilitySelector 根据 Todo 节点生成能力边界。
type CapabilitySelector func(todo agentsession.TodoItem) Capability

// ContextSliceBuilder 根据任务快照构建可消费的上下文切片。
type ContextSliceBuilder func(input TaskContextSliceInput) TaskContextSlice

// ContextSkillSelector 根据任务快照选择激活技能列表。
type ContextSkillSelector func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []string

// ContextFileSelector 根据任务快照选择相关文件摘要。
type ContextFileSelector func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []TaskContextFileSummary

// RetryBackoff 计算第 N 次重试的退避时长。
type RetryBackoff func(attempt int) time.Duration

// SchedulerFailureStrategy 描述任务失败后的调度策略。
type SchedulerFailureStrategy string

const (
	// SchedulerFailureContinueOnError 表示失败任务仅影响自身，调度继续推进其他可执行任务。
	SchedulerFailureContinueOnError SchedulerFailureStrategy = "continue_on_error"
	// SchedulerFailureFailFast 表示任一任务进入不可重试失败后，立即中断本轮调度。
	SchedulerFailureFailFast SchedulerFailureStrategy = "fail_fast"
)

// SchedulerRecoveryStrategy 描述调度器启动时如何处理遗留 in_progress 任务。
type SchedulerRecoveryStrategy string

const (
	// SchedulerRecoveryRetry 表示把遗留 in_progress 任务恢复到可重试状态。
	SchedulerRecoveryRetry SchedulerRecoveryStrategy = "retry"
	// SchedulerRecoveryFail 表示把遗留 in_progress 任务直接标记失败。
	SchedulerRecoveryFail SchedulerRecoveryStrategy = "fail"
)

// SchedulerEventType 描述调度器可观测事件类型。
type SchedulerEventType string

const (
	// SchedulerEventSubAgentStarted 对齐 issue #278 的标准开始事件。
	SchedulerEventSubAgentStarted SchedulerEventType = "subagent_started"
	// SchedulerEventSubAgentProgress 对齐 issue #278 的标准进度事件。
	SchedulerEventSubAgentProgress SchedulerEventType = "subagent_progress"
	// SchedulerEventSubAgentCompleted 对齐 issue #278 的标准完成事件。
	SchedulerEventSubAgentCompleted SchedulerEventType = "subagent_completed"
	// SchedulerEventSubAgentFailed 对齐 issue #278 的标准失败事件。
	SchedulerEventSubAgentFailed SchedulerEventType = "subagent_failed"
	// SchedulerEventSubAgentCanceled 对齐 issue #278 的标准取消事件。
	SchedulerEventSubAgentCanceled SchedulerEventType = "subagent_canceled"
	// SchedulerEventSubAgentRetried 对齐 issue #278 的标准重试事件。
	SchedulerEventSubAgentRetried SchedulerEventType = "subagent_retried"

	// SchedulerEventQueued 表示任务进入可调度队列。
	SchedulerEventQueued SchedulerEventType = "queued"
	// SchedulerEventClaimed 表示任务已经被领取。
	SchedulerEventClaimed SchedulerEventType = "claimed"
	// SchedulerEventRunning 表示任务开始执行。
	SchedulerEventRunning SchedulerEventType = "running"
	// SchedulerEventCompleted 表示任务成功完成。
	SchedulerEventCompleted SchedulerEventType = "completed"
	// SchedulerEventRetryScheduled 表示任务失败后进入重试窗口。
	SchedulerEventRetryScheduled SchedulerEventType = "retry_scheduled"
	// SchedulerEventFailed 表示任务失败且不再重试。
	SchedulerEventFailed SchedulerEventType = "failed"
	// SchedulerEventCanceled 表示任务被取消。
	SchedulerEventCanceled SchedulerEventType = "canceled"
	// SchedulerEventBlocked 表示任务因依赖或退避暂不可执行。
	SchedulerEventBlocked SchedulerEventType = "blocked"
	// SchedulerEventFinished 表示调度轮次结束。
	SchedulerEventFinished SchedulerEventType = "finished"
)

// SchedulerEvent 描述单次调度决策事件。
type SchedulerEvent struct {
	Type      SchedulerEventType
	TaskID    string
	Attempt   int
	Running   int
	QueueSize int
	Reason    string
	At        time.Time
}

// SchedulerObserver 消费调度事件。
type SchedulerObserver func(event SchedulerEvent)

// ScheduleResult 汇总一次调度执行结果。
type ScheduleResult struct {
	StartedAt   time.Time
	EndedAt     time.Time
	Total       int
	Succeeded   []string
	Failed      []string
	Retried     map[string]int
	Recovered   []string
	BlockedLeft []string
}

// SchedulerConfig 描述调度策略参数。
type SchedulerConfig struct {
	MaxConcurrency int
	DefaultRole    Role
	DefaultBudget  Budget
	MaxRetries     int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	WorkerIDPrefix string
	PollInterval   time.Duration
	FailureMode    SchedulerFailureStrategy
	RecoveryMode   SchedulerRecoveryStrategy
	RecoveryReason string
	Clock          func() time.Time
	RoleSelector   RoleSelector
	BudgetSelector BudgetSelector
	Backoff        RetryBackoff
	Capabilities   CapabilitySelector

	ContextBuilder ContextSliceBuilder
	ContextSkills  ContextSkillSelector
	ContextFiles   ContextFileSelector

	ContextMaxChars               int
	ContextMaxTodoFragments       int
	ContextMaxDependencyArtifacts int
	ContextMaxRelatedFiles        int

	Observer SchedulerObserver
}

// normalize 返回带默认值的配置副本，避免执行阶段出现隐式零值。
func (c SchedulerConfig) normalize() SchedulerConfig {
	out := c
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 2
	}
	if !out.DefaultRole.Valid() {
		out.DefaultRole = RoleCoder
	}
	out.DefaultBudget = out.DefaultBudget.normalize(Budget{MaxSteps: 6, Timeout: 30 * time.Second})
	if out.MaxRetries < 0 {
		out.MaxRetries = 0
	}
	if out.RetryBaseDelay <= 0 {
		out.RetryBaseDelay = time.Second
	}
	if out.RetryMaxDelay <= 0 {
		out.RetryMaxDelay = 30 * time.Second
	}
	if out.RetryMaxDelay < out.RetryBaseDelay {
		out.RetryMaxDelay = out.RetryBaseDelay
	}
	if out.PollInterval <= 0 {
		out.PollInterval = 200 * time.Millisecond
	}
	if out.FailureMode != SchedulerFailureFailFast {
		out.FailureMode = SchedulerFailureContinueOnError
	}
	if out.RecoveryMode != SchedulerRecoveryFail {
		out.RecoveryMode = SchedulerRecoveryRetry
	}
	if out.RecoveryReason == "" {
		out.RecoveryReason = "recovered interrupted subagent execution"
	}
	if out.WorkerIDPrefix == "" {
		out.WorkerIDPrefix = "subagent"
	}
	if out.Clock == nil {
		out.Clock = time.Now
	}
	if out.RoleSelector == nil {
		out.RoleSelector = defaultRoleSelector(out.DefaultRole)
	}
	if out.BudgetSelector == nil {
		out.BudgetSelector = defaultBudgetSelector
	}
	if out.Backoff == nil {
		out.Backoff = defaultRetryBackoffWithBounds(out.RetryBaseDelay, out.RetryMaxDelay)
	}
	if out.Capabilities == nil {
		out.Capabilities = func(todo agentsession.TodoItem) Capability {
			_ = todo
			return Capability{}
		}
	}
	if out.ContextBuilder == nil {
		out.ContextBuilder = BuildTaskContextSlice
	}
	if out.ContextSkills == nil {
		out.ContextSkills = func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []string {
			_ = todo
			_ = snapshot
			return nil
		}
	}
	if out.ContextFiles == nil {
		out.ContextFiles = func(todo agentsession.TodoItem, snapshot map[string]agentsession.TodoItem) []TaskContextFileSummary {
			_ = todo
			_ = snapshot
			return nil
		}
	}
	if out.ContextMaxChars <= 0 {
		out.ContextMaxChars = defaultTaskContextMaxChars
	}
	if out.ContextMaxTodoFragments <= 0 {
		out.ContextMaxTodoFragments = defaultTaskContextMaxTodoFragments
	}
	if out.ContextMaxDependencyArtifacts <= 0 {
		out.ContextMaxDependencyArtifacts = defaultTaskContextMaxDependencyArtifacts
	}
	if out.ContextMaxRelatedFiles <= 0 {
		out.ContextMaxRelatedFiles = defaultTaskContextMaxRelatedFiles
	}
	if out.Observer == nil {
		out.Observer = func(SchedulerEvent) {}
	}
	return out
}

// defaultRoleSelector 返回默认角色选择策略。
func defaultRoleSelector(defaultRole Role) RoleSelector {
	return func(todo agentsession.TodoItem) Role {
		_ = todo
		return defaultRole
	}
}

// defaultBudgetSelector 返回默认预算策略。
func defaultBudgetSelector(todo agentsession.TodoItem, defaults Budget) Budget {
	_ = todo
	return defaults
}

// defaultRetryBackoff 提供指数退避默认策略。
func defaultRetryBackoff(attempt int) time.Duration {
	return defaultRetryBackoffWithBounds(time.Second, 30*time.Second)(attempt)
}

// defaultRetryBackoffWithBounds 提供指数退避并带上限保护。
func defaultRetryBackoffWithBounds(base, max time.Duration) RetryBackoff {
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	if max < base {
		max = base
	}

	return func(attempt int) time.Duration {
		if attempt <= 0 {
			return 0
		}
		delay := base
		for i := 1; i < attempt; i++ {
			if delay >= max/2 {
				delay = max
				break
			}
			delay *= 2
		}
		if delay > max {
			return max
		}
		return delay
	}
}

// failFastEnabled 判断调度器是否开启 fail-fast 策略。
func (c SchedulerConfig) failFastEnabled() bool {
	return c.FailureMode == SchedulerFailureFailFast
}

// recoveryAsFailure 判断恢复阶段是否直接失败遗留任务。
func (c SchedulerConfig) recoveryAsFailure() bool {
	return c.RecoveryMode == SchedulerRecoveryFail
}
