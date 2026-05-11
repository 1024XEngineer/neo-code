package controlplane

// SubgoalRelation 表示当前轮子目标与上一轮的关系。
type SubgoalRelation string

const (
	// SubgoalRelationSame 表示子目标可证明相同。
	SubgoalRelationSame SubgoalRelation = "same"
	// SubgoalRelationDifferent 表示子目标可证明不同。
	SubgoalRelationDifferent SubgoalRelation = "different"
	// SubgoalRelationUnknown 表示当前无法稳定判断子目标关系。
	SubgoalRelationUnknown SubgoalRelation = "unknown"
)

// StalledProgressState 表示当前重复循环检测是否已进入软卡住状态。
type StalledProgressState string

const (
	// StalledProgressHealthy 表示当前未进入 stalled。
	StalledProgressHealthy StalledProgressState = "healthy"
	// StalledProgressStalled 表示当前已进入 stalled。
	StalledProgressStalled StalledProgressState = "stalled"
)

// ReminderKind 标识应向模型注入的纠偏提醒类型。
type ReminderKind string

const (
	// ReminderKindNone 表示当前轮无需注入提醒。
	ReminderKindNone ReminderKind = ""
	// ReminderKindRepeatCycle 表示应注入重复循环提醒。
	ReminderKindRepeatCycle ReminderKind = "REMINDER_REPEAT_CYCLE"
)

// ProgressScore 表示一次重复循环检测后的快照。
type ProgressScore struct {
	RepeatCycleStreak     int                  `json:"repeat_cycle_streak"`
	SameToolSignature     bool                 `json:"same_tool_signature"`
	SameResultFingerprint bool                 `json:"same_result_fingerprint"`
	SameSubgoal           SubgoalRelation      `json:"same_subgoal"`
	StalledProgressState  StalledProgressState `json:"stalled_progress_state"`
	ReminderKind          ReminderKind         `json:"reminder_kind,omitempty"`
	ShouldTerminate       bool                 `json:"should_terminate"`
	TerminateReason       StopReason           `json:"terminate_reason,omitempty"`
}

// ProgressState 保存跨轮重复循环检测所需的历史快照。
type ProgressState struct {
	LastScore              ProgressScore `json:"last_score"`
	LastToolSignature      string        `json:"last_tool_signature,omitempty"`
	LastResultFingerprint  string        `json:"last_result_fingerprint,omitempty"`
	LastSubgoalFingerprint string        `json:"last_subgoal_fingerprint,omitempty"`
}

// ProgressInput 描述一次重复循环检测所需的指纹输入。
type ProgressInput struct {
	CurrentToolSignature string
	ResultFingerprint    string
	SubgoalFingerprint   string
	RepeatCycleLimit     int
}

// EvaluateProgress 基于上一轮指纹和本轮指纹检测 agent 是否陷入重复循环。
func EvaluateProgress(state ProgressState, input ProgressInput) ProgressState {
	next := ProgressScore{}
	next.SameToolSignature = input.CurrentToolSignature != "" &&
		state.LastToolSignature != "" &&
		input.CurrentToolSignature == state.LastToolSignature
	next.SameResultFingerprint = input.ResultFingerprint != "" &&
		state.LastResultFingerprint != "" &&
		input.ResultFingerprint == state.LastResultFingerprint
	next.SameSubgoal = compareSubgoalFingerprint(state.LastSubgoalFingerprint, input.SubgoalFingerprint)

	if next.SameToolSignature && next.SameResultFingerprint && next.SameSubgoal == SubgoalRelationSame {
		next.RepeatCycleStreak = state.LastScore.RepeatCycleStreak + 1
	} else {
		next.RepeatCycleStreak = 0
	}

	if shouldStall(next, input.RepeatCycleLimit) {
		next.StalledProgressState = StalledProgressStalled
		next.ReminderKind = ReminderKindRepeatCycle
	} else {
		next.StalledProgressState = StalledProgressHealthy
		next.ReminderKind = ReminderKindNone
	}
	if shouldTerminateAfterStalledReminder(state.LastScore, next) {
		next.ShouldTerminate = true
		next.TerminateReason = StopReasonRepeatCycle
	}

	return ProgressState{
		LastScore:              next,
		LastToolSignature:      input.CurrentToolSignature,
		LastResultFingerprint:  input.ResultFingerprint,
		LastSubgoalFingerprint: input.SubgoalFingerprint,
	}
}

// shouldTerminateAfterStalledReminder 只在 repeat stalled 已提醒过一轮后才允许硬终止。
func shouldTerminateAfterStalledReminder(previous ProgressScore, current ProgressScore) bool {
	if current.StalledProgressState != StalledProgressStalled || current.ReminderKind == ReminderKindNone {
		return false
	}
	return previous.StalledProgressState == StalledProgressStalled &&
		previous.ReminderKind == current.ReminderKind
}

// compareSubgoalFingerprint 判断当前轮与上一轮的子目标关系。
func compareSubgoalFingerprint(previous string, current string) SubgoalRelation {
	if previous == "" && current == "" {
		return SubgoalRelationUnknown
	}
	if previous == "" || current == "" {
		return SubgoalRelationUnknown
	}
	if previous == current {
		return SubgoalRelationSame
	}
	return SubgoalRelationDifferent
}

// shouldStall 判断当前快照是否应进入 repeat stalled。
func shouldStall(score ProgressScore, repeatLimit int) bool {
	return repeatLimit > 0 && score.RepeatCycleStreak >= repeatLimit
}
