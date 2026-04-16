package subagent

import (
	"time"

	agentsession "neo-code/internal/session"
)

// TodoStore 定义调度器读写 Todo 的最小依赖，便于复用 runtime/session 不同实现。
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

// BudgetSelector 根据 Todo 节点生成本任务预算。
type BudgetSelector func(todo agentsession.TodoItem, defaults Budget) Budget

// CapabilitySelector 根据 Todo 节点生成本任务能力边界。
type CapabilitySelector func(todo agentsession.TodoItem) Capability

// RetryBackoff 计算第 N 次重试应等待的退避时长。
type RetryBackoff func(attempt int) time.Duration

// SchedulerEventType 描述调度器可观测事件类型。
type SchedulerEventType string

const (
	// SchedulerEventQueued 表示任务进入可调度队列。
	SchedulerEventQueued SchedulerEventType = "queued"
	// SchedulerEventClaimed 表示任务已被领取并进入执行中。
	SchedulerEventClaimed SchedulerEventType = "claimed"
	// SchedulerEventRunning 表示任务开始执行。
	SchedulerEventRunning SchedulerEventType = "running"
	// SchedulerEventCompleted 表示任务执行成功并回写完成。
	SchedulerEventCompleted SchedulerEventType = "completed"
	// SchedulerEventRetryScheduled 表示任务失败后已安排重试。
	SchedulerEventRetryScheduled SchedulerEventType = "retry_scheduled"
	// SchedulerEventFailed 表示任务执行失败且不再重试。
	SchedulerEventFailed SchedulerEventType = "failed"
	// SchedulerEventCanceled 表示任务因外部取消被回写为 canceled。
	SchedulerEventCanceled SchedulerEventType = "canceled"
	// SchedulerEventBlocked 表示任务因依赖未满足处于阻塞态。
	SchedulerEventBlocked SchedulerEventType = "blocked"
	// SchedulerEventFinished 表示调度器本轮结束。
	SchedulerEventFinished SchedulerEventType = "finished"
)

// SchedulerEvent 描述单次调度决策事件，供日志或 runtime 事件桥接使用。
type SchedulerEvent struct {
	Type      SchedulerEventType
	TaskID    string
	Attempt   int
	Running   int
	QueueSize int
	Reason    string
	At        time.Time
}

// SchedulerObserver 用于消费调度器事件。
type SchedulerObserver func(event SchedulerEvent)

// ScheduleResult 汇总一次调度执行的结果统计。
type ScheduleResult struct {
	StartedAt   time.Time
	EndedAt     time.Time
	Total       int
	Succeeded   []string
	Failed      []string
	Retried     map[string]int
	BlockedLeft []string
}

// SchedulerConfig 描述调度器策略参数。
type SchedulerConfig struct {
	MaxConcurrency int
	DefaultRole    Role
	DefaultBudget  Budget
	MaxRetries     int
	WorkerIDPrefix string
	PollInterval   time.Duration
	Clock          func() time.Time
	RoleSelector   RoleSelector
	BudgetSelector BudgetSelector
	Backoff        RetryBackoff
	Capabilities   CapabilitySelector
	Observer       SchedulerObserver
}

// normalize 返回带默认值的配置副本，保证调度器执行阶段不出现隐式零值。
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
	if out.PollInterval <= 0 {
		out.PollInterval = 200 * time.Millisecond
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
		out.Backoff = defaultRetryBackoff
	}
	if out.Capabilities == nil {
		out.Capabilities = func(todo agentsession.TodoItem) Capability {
			_ = todo
			return Capability{}
		}
	}
	if out.Observer == nil {
		out.Observer = func(SchedulerEvent) {}
	}
	return out
}

// defaultRoleSelector 生成默认角色选择器：仅使用调度器可信默认角色，避免读取任务可变元数据。
func defaultRoleSelector(defaultRole Role) RoleSelector {
	return func(todo agentsession.TodoItem) Role {
		_ = todo
		return defaultRole
	}
}

// defaultBudgetSelector 对每个任务统一返回标准预算，可被配置覆盖。
func defaultBudgetSelector(todo agentsession.TodoItem, defaults Budget) Budget {
	_ = todo
	return defaults
}

// defaultRetryBackoff 提供线性重试退避，避免瞬时失败造成热循环。
func defaultRetryBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return time.Duration(attempt) * time.Second
}
