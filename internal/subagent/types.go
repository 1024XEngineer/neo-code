package subagent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Role 表示子代理的执行角色。
type Role string

const (
	// RoleResearcher 用于检索与分析任务。
	RoleResearcher Role = "researcher"
	// RoleCoder 用于实现与修复任务。
	RoleCoder Role = "coder"
	// RoleReviewer 用于审查与验收任务。
	RoleReviewer Role = "reviewer"
)

// Valid 判断角色是否受支持。
func (r Role) Valid() bool {
	switch r {
	case RoleResearcher, RoleCoder, RoleReviewer:
		return true
	default:
		return false
	}
}

// Budget 描述子代理执行预算。
type Budget struct {
	MaxSteps int
	Timeout  time.Duration
}

// normalize 归一化预算并应用默认值。
func (b Budget) normalize(defaults Budget) Budget {
	out := b
	if out.MaxSteps <= 0 {
		out.MaxSteps = defaults.MaxSteps
	}
	if out.MaxSteps <= 0 {
		out.MaxSteps = 6
	}
	if out.Timeout <= 0 {
		out.Timeout = defaults.Timeout
	}
	if out.Timeout <= 0 {
		out.Timeout = 30 * time.Second
	}
	return out
}

// Capability 描述子代理运行时可用能力边界。
type Capability struct {
	AllowedTools []string
	AllowedPaths []string
}

// normalize 归一化能力列表并去重。
func (c Capability) normalize() Capability {
	return Capability{
		AllowedTools: dedupeAndTrim(c.AllowedTools),
		AllowedPaths: dedupeAndTrim(c.AllowedPaths),
	}
}

// Task 表示单个子代理任务输入。
type Task struct {
	ID             string
	Goal           string
	ExpectedOutput string
	Workspace      string
}

// Validate 校验任务输入是否合法。
func (t Task) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return errorsf("task id is required")
	}
	if strings.TrimSpace(t.Goal) == "" {
		return errorsf("task goal is required")
	}
	return nil
}

// StopReason 表示子代理终止原因。
type StopReason string

const (
	// StopReasonCompleted 表示正常完成。
	StopReasonCompleted StopReason = "completed"
	// StopReasonCanceled 表示被取消。
	StopReasonCanceled StopReason = "canceled"
	// StopReasonTimeout 表示执行超时。
	StopReasonTimeout StopReason = "timeout"
	// StopReasonMaxSteps 表示达到步数上限。
	StopReasonMaxSteps StopReason = "max_steps"
	// StopReasonError 表示执行错误。
	StopReasonError StopReason = "error"
)

// State 表示子代理生命周期状态。
type State string

const (
	// StateIdle 表示尚未启动。
	StateIdle State = "idle"
	// StateRunning 表示执行中。
	StateRunning State = "running"
	// StateSucceeded 表示执行成功结束。
	StateSucceeded State = "succeeded"
	// StateFailed 表示执行失败结束。
	StateFailed State = "failed"
	// StateCanceled 表示被取消结束。
	StateCanceled State = "canceled"
)

// Terminal 判断当前状态是否为终态。
func (s State) Terminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCanceled:
		return true
	default:
		return false
	}
}

// Output 定义子代理标准结构化输出。
type Output struct {
	Summary     string
	Findings    []string
	Patches     []string
	Risks       []string
	NextActions []string
	Artifacts   []string
}

// normalize 归一化输出，避免重复与空项。
func (o Output) normalize() Output {
	o.Summary = strings.TrimSpace(o.Summary)
	o.Findings = dedupeAndTrim(o.Findings)
	o.Patches = dedupeAndTrim(o.Patches)
	o.Risks = dedupeAndTrim(o.Risks)
	o.NextActions = dedupeAndTrim(o.NextActions)
	o.Artifacts = dedupeAndTrim(o.Artifacts)
	return o
}

// StepInput 表示单步执行输入。
type StepInput struct {
	Role       Role
	Policy     RolePolicy
	Task       Task
	Budget     Budget
	Capability Capability
	StepIndex  int
	Trace      []string
}

// StepOutput 表示单步执行输出。
type StepOutput struct {
	Delta  string
	Done   bool
	Output Output
}

// StepResult 表示一次 Step 的可观测结果。
type StepResult struct {
	State State
	Done  bool
	Step  int
	Delta string
}

// Result 描述子代理完成后的结构化结果。
type Result struct {
	Role       Role
	TaskID     string
	State      State
	StopReason StopReason
	StartedAt  time.Time
	EndedAt    time.Time
	StepCount  int
	Budget     Budget
	Capability Capability
	Output     Output
	Error      string
}

// Engine 定义 WorkerRuntime 的单步执行引擎。
type Engine interface {
	RunStep(ctx context.Context, input StepInput) (StepOutput, error)
}

// EngineFunc 允许用函数实现 Engine。
type EngineFunc func(ctx context.Context, input StepInput) (StepOutput, error)

// RunStep 执行函数式引擎逻辑。
func (f EngineFunc) RunStep(ctx context.Context, input StepInput) (StepOutput, error) {
	return f(ctx, input)
}

// WorkerRuntime 定义子代理执行生命周期接口。
type WorkerRuntime interface {
	Start(task Task, budget Budget, capability Capability) error
	Step(ctx context.Context) (StepResult, error)
	Stop(reason StopReason) error
	Result() (Result, error)
	State() State
	Policy() RolePolicy
}

// Factory 定义 runtime 侧创建 WorkerRuntime 的入口。
type Factory interface {
	Create(role Role) (WorkerRuntime, error)
}

// errorsf 统一组装 subagent 模块错误前缀。
func errorsf(format string, args ...any) error {
	return fmt.Errorf("subagent: "+format, args...)
}
