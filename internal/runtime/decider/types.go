package decider

import "neo-code/internal/runtime/facts"

// TaskKind 描述任务验收的主类型。
type TaskKind string

const (
	// TaskKindChatAnswer 表示普通问答任务。
	TaskKindChatAnswer TaskKind = "chat_answer"
	// TaskKindTodoState 表示 todo 状态任务。
	TaskKindTodoState TaskKind = "todo_state"
	// TaskKindWorkspaceWrite 表示工作区写入任务。
	TaskKindWorkspaceWrite TaskKind = "workspace_write"
	// TaskKindSubAgent 表示显式子代理任务。
	TaskKindSubAgent TaskKind = "subagent"
	// TaskKindReadOnly 表示只读分析任务。
	TaskKindReadOnly TaskKind = "read_only"
	// TaskKindMixed 表示混合任务。
	TaskKindMixed TaskKind = "mixed"
)

// DecisionStatus 表示终态决策状态。
type DecisionStatus string

const (
	// DecisionAccepted 表示满足收尾条件。
	DecisionAccepted DecisionStatus = "accepted"
	// DecisionContinue 表示仍需继续执行。
	DecisionContinue DecisionStatus = "continue"
	// DecisionFailed 表示任务失败终止。
	DecisionFailed DecisionStatus = "failed"
	// DecisionBlocked 表示被外部条件阻塞。
	DecisionBlocked DecisionStatus = "blocked"
	// DecisionIncomplete 表示长时间无进展后未完成终止。
	DecisionIncomplete DecisionStatus = "incomplete"
)

// MissingFact 描述 continue 场景下缺失的客观事实。
type MissingFact struct {
	Kind     string         `json:"kind"`
	Target   string         `json:"target,omitempty"`
	Expected string         `json:"expected,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

// RequiredAction 描述下一轮建议工具动作。
type RequiredAction struct {
	Tool     string         `json:"tool"`
	ArgsHint map[string]any `json:"args_hint,omitempty"`
}

// RequiredInput 描述继续执行前必须补充的人类输入。
type RequiredInput struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// HookGuardSignal 描述 before_completion_decision user/repo hook 产生的守卫信号。
type HookGuardSignal struct {
	HookID  string `json:"hook_id,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// TaskIntent 描述由用户文本推断出的弱意图线索。
type TaskIntent struct {
	Hint       TaskKind `json:"hint,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

// Decision 描述最终裁决结果。
type Decision struct {
	Status              DecisionStatus   `json:"status"`
	StopReason          string           `json:"stop_reason,omitempty"`
	MissingFacts        []MissingFact    `json:"missing_facts,omitempty"`
	RequiredNextActions []RequiredAction `json:"required_next_actions,omitempty"`
	RequiredInput       *RequiredInput   `json:"required_input,omitempty"`
	IntentHint          TaskKind         `json:"intent_hint,omitempty"`
	EffectiveTaskKind   TaskKind         `json:"effective_task_kind,omitempty"`
	UserVisibleSummary  string           `json:"user_visible_summary,omitempty"`
	InternalSummary     string           `json:"internal_summary,omitempty"`
}

// TodoViewItem 描述决策所需 todo 快照条目。
type TodoViewItem struct {
	ID            string
	Content       string
	Status        string
	Required      bool
	Artifacts     []string
	FailureReason string
	BlockedReason string
	Revision      int64
}

// TodoSummary 描述决策所需 todo 汇总。
type TodoSummary struct {
	Total             int
	RequiredTotal     int
	RequiredCompleted int
	RequiredFailed    int
	RequiredOpen      int
}

// TodoSnapshot 描述决策所需 todo 快照。
type TodoSnapshot struct {
	Items   []TodoViewItem
	Summary TodoSummary
}

// ProgressSnapshot 描述 no-progress 判定所需信息。
type ProgressSnapshot struct {
	FactCount int
}

// DecisionInput 描述终态裁决输入。
type DecisionInput struct {
	RunID              string
	SessionID          string
	TaskKind           TaskKind
	UserGoal           string
	Facts              facts.RuntimeFacts
	Todos              TodoSnapshot
	Progress           ProgressSnapshot
	LastAssistantText  string
	CompletionPassed   bool
	CompletionReason   string
	NoProgressExceeded bool
	HookAnnotations    []string
	HookGuards         []HookGuardSignal
}
