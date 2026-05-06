package subagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
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
	AllowedTools    []string
	AllowedPaths    []string
	ToolUseMode     ToolUseMode
	CapabilityToken *security.CapabilityToken
}

// normalize 归一化能力列表并去重。
func (c Capability) normalize() Capability {
	var token *security.CapabilityToken
	if c.CapabilityToken != nil {
		normalized := c.CapabilityToken.Normalize()
		token = &normalized
	}
	return Capability{
		AllowedTools:    dedupeAndTrim(c.AllowedTools),
		AllowedPaths:    dedupeAndTrim(c.AllowedPaths),
		ToolUseMode:     ToolUseMode(strings.ToLower(strings.TrimSpace(string(c.ToolUseMode)))),
		CapabilityToken: token,
	}
}

// Task 表示单个子代理任务输入。
type Task struct {
	ID             string
	TaskType       TaskType
	Goal           string
	ExpectedOutput string
	Workspace      string
	RunID          string
	SessionID      string
	AgentID        string
	ContextSlice   TaskContextSlice
}

// Validate 校验任务输入是否合法。
func (t Task) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return errorsf("task id is required")
	}
	if strings.TrimSpace(t.Goal) == "" {
		return errorsf("task goal is required")
	}
	if strings.TrimSpace(string(t.TaskType)) != "" && !t.TaskType.Valid() {
		return errorsf("task type %q is invalid", t.TaskType)
	}
	contextTaskID := strings.TrimSpace(t.ContextSlice.TaskID)
	if contextTaskID != "" && !strings.EqualFold(contextTaskID, strings.TrimSpace(t.ID)) {
		return errorsf("task context slice task id %q mismatched task id %q", contextTaskID, strings.TrimSpace(t.ID))
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
	Report      string
	Status      string
	Logs        []string
	Findings    []string
	Patches     []string
	Risks       []string
	NextActions []string
	Artifacts   []string
}

// normalize 归一化输出，避免重复与空项。
func (o Output) normalize() Output {
	o.Summary = strings.TrimSpace(o.Summary)
	o.Report = strings.TrimSpace(o.Report)
	o.Status = strings.TrimSpace(o.Status)
	o.Logs = dedupeAndTrim(o.Logs)
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
	RunID      string
	SessionID  string
	AgentID    string
	Workdir    string
	Executor   ToolExecutor
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

// ToolSpecListInput 描述列举子代理可见工具 schema 的输入上下文。
type ToolSpecListInput struct {
	SessionID    string
	Role         Role
	AllowedTools []string
}

// ToolExecutionInput 描述一次子代理工具执行请求。
type ToolExecutionInput struct {
	RunID           string
	SessionID       string
	TaskID          string
	Role            Role
	AgentID         string
	Workdir         string
	Timeout         time.Duration
	Call            providertypes.ToolCall
	Capability      Capability
	CapabilityToken *security.CapabilityToken
}

// ToolExecutionResult 描述子代理工具执行后的标准结果。
type ToolExecutionResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Decision   string
	Metadata   map[string]any
}

// ToolExecutor 定义子代理访问 runtime 工具能力的最小桥接接口。
type ToolExecutor interface {
	ListToolSpecs(ctx context.Context, input ToolSpecListInput) ([]providertypes.ToolSpec, error)
	ExecuteTool(ctx context.Context, input ToolExecutionInput) (ToolExecutionResult, error)
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
