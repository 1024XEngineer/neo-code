package controlplane

// ProgressEvidenceKind 标识 runtime 聚合得到的结构化进展证据。
type ProgressEvidenceKind string

const (
	// EvidenceTaskStateChanged 表示任务状态发生合法迁移。
	EvidenceTaskStateChanged ProgressEvidenceKind = "TASK_STATE_CHANGED"
	// EvidenceTodoStateChanged 表示 todo 列表发生结构化变化。
	EvidenceTodoStateChanged ProgressEvidenceKind = "TODO_STATE_CHANGED"
	// EvidenceWriteApplied 表示本轮产生了有效文件改动。
	EvidenceWriteApplied ProgressEvidenceKind = "WRITE_APPLIED"
	// EvidenceVerifyPassed 表示本轮存在明确的验证成功信号（仅与写入证据组合后算业务推进）。
	EvidenceVerifyPassed ProgressEvidenceKind = "VERIFY_PASSED"
	// EvidenceNewInfoNonDup 表示本轮引入了去重后的新信息。
	EvidenceNewInfoNonDup ProgressEvidenceKind = "NEW_INFO_NON_DUP"
)

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

// StalledProgressState 表示当前进展是否已进入软卡住状态。
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
	// ReminderKindNoProgress 表示应注入无进展提醒。
	ReminderKindNoProgress ReminderKind = "REMINDER_NO_PROGRESS"
	// ReminderKindRepeatCycle 表示应注入重复循环提醒。
	ReminderKindRepeatCycle ReminderKind = "REMINDER_REPEAT_CYCLE"
	// ReminderKindGenericStalled 表示应注入通用 stalled 提醒。
	ReminderKindGenericStalled ReminderKind = "REMINDER_GENERIC_STALLED"
)

// ProgressEvidenceRecord 描述一条结构化进展证据。
type ProgressEvidenceRecord struct {
	Kind   ProgressEvidenceKind `json:"kind"`
	Detail string               `json:"detail,omitempty"`
}

// ProgressScore 表示一次 progress 评估后的完整快照。
type ProgressScore struct {
	HasBusinessProgress    bool                 `json:"has_business_progress"`
	HasExplorationProgress bool                 `json:"has_exploration_progress"`
	StrongEvidenceCount    int                  `json:"strong_evidence_count"`
	MediumEvidenceCount    int                  `json:"medium_evidence_count"`
	WeakEvidenceCount      int                  `json:"weak_evidence_count"`
	ExplorationStreak      int                  `json:"exploration_streak"`
	NoProgressStreak       int                  `json:"no_progress_streak"`
	RepeatCycleStreak      int                  `json:"repeat_cycle_streak"`
	SameToolSignature      bool                 `json:"same_tool_signature"`
	SameResultFingerprint  bool                 `json:"same_result_fingerprint"`
	SameSubgoal            SubgoalRelation      `json:"same_subgoal"`
	StalledProgressState   StalledProgressState `json:"stalled_progress_state"`
	ReminderKind           ReminderKind         `json:"reminder_kind,omitempty"`
}

// ProgressState 保存跨轮 progress 判定所需的历史快照。
type ProgressState struct {
	LastScore              ProgressScore `json:"last_score"`
	LastToolSignature      string        `json:"last_tool_signature,omitempty"`
	LastResultFingerprint  string        `json:"last_result_fingerprint,omitempty"`
	LastSubgoalFingerprint string        `json:"last_subgoal_fingerprint,omitempty"`
}

// ProgressInput 描述一次 progress 评估所需的事实输入。
type ProgressInput struct {
	RunState             RunState
	Evidence             []ProgressEvidenceRecord
	CurrentToolSignature string
	ResultFingerprint    string
	SubgoalFingerprint   string
	NoProgressLimit      int
	RepeatCycleLimit     int
}

// EvaluateProgress 基于上一轮状态和本轮事实生成新的 progress 快照。
func EvaluateProgress(state ProgressState, input ProgressInput) ProgressState {
	next := ProgressScore{}
	flags := summarizeEvidence(input.Evidence)

	next.StrongEvidenceCount = flags.strongCount
	next.MediumEvidenceCount = flags.mediumCount
	next.WeakEvidenceCount = flags.weakCount
	next.HasBusinessProgress = flags.strongCount > 0 || (flags.hasWrite && flags.hasVerify)
	next.HasExplorationProgress = !next.HasBusinessProgress && isExplorationProgress(input.RunState, flags)
	next.SameToolSignature = input.CurrentToolSignature != "" &&
		state.LastToolSignature != "" &&
		input.CurrentToolSignature == state.LastToolSignature
	next.SameResultFingerprint = input.ResultFingerprint != "" &&
		state.LastResultFingerprint != "" &&
		input.ResultFingerprint == state.LastResultFingerprint
	next.SameSubgoal = compareSubgoalFingerprint(state.LastSubgoalFingerprint, input.SubgoalFingerprint)

	if next.HasBusinessProgress {
		next.ExplorationStreak = 0
		next.NoProgressStreak = 0
	} else if next.HasExplorationProgress {
		next.ExplorationStreak = state.LastScore.ExplorationStreak + 1
		next.NoProgressStreak = state.LastScore.NoProgressStreak
		if next.ExplorationStreak > explorationWindowForPhase(input.RunState) {
			next.NoProgressStreak++
		}
	} else {
		next.ExplorationStreak = 0
		next.NoProgressStreak = state.LastScore.NoProgressStreak + 1
	}

	if next.HasBusinessProgress {
		next.RepeatCycleStreak = 0
	} else if next.SameToolSignature && next.SameResultFingerprint && next.SameSubgoal == SubgoalRelationSame {
		next.RepeatCycleStreak = state.LastScore.RepeatCycleStreak + 1
	} else {
		next.RepeatCycleStreak = 0
	}

	if shouldStall(next, input.NoProgressLimit, input.RepeatCycleLimit) {
		next.StalledProgressState = StalledProgressStalled
		next.ReminderKind = selectReminderKind(next)
	} else {
		next.StalledProgressState = StalledProgressHealthy
		next.ReminderKind = ReminderKindNone
	}

	return ProgressState{
		LastScore:              next,
		LastToolSignature:      input.CurrentToolSignature,
		LastResultFingerprint:  input.ResultFingerprint,
		LastSubgoalFingerprint: input.SubgoalFingerprint,
	}
}

type evidenceFlags struct {
	strongCount int
	mediumCount int
	weakCount   int
	hasWrite    bool
	hasVerify   bool
}

// summarizeEvidence 汇总本轮 evidence 的强中弱计数与关键标记。
func summarizeEvidence(records []ProgressEvidenceRecord) evidenceFlags {
	var flags evidenceFlags
	for _, record := range records {
		switch record.Kind {
		case EvidenceTaskStateChanged, EvidenceTodoStateChanged:
			flags.strongCount++
		case EvidenceWriteApplied, EvidenceVerifyPassed:
			flags.mediumCount++
		case EvidenceNewInfoNonDup:
			flags.weakCount++
		}

		switch record.Kind {
		case EvidenceWriteApplied:
			flags.hasWrite = true
		case EvidenceVerifyPassed:
			flags.hasVerify = true
		}
	}
	return flags
}

// isExplorationProgress 判断本轮是否属于可被宽容窗口吸收的探索型推进。
func isExplorationProgress(runState RunState, flags evidenceFlags) bool {
	if runState != RunStatePlan && runState != RunStateExecute {
		return false
	}
	return flags.weakCount > 0
}

// explorationWindowForPhase 返回不同阶段允许的 exploration 宽容窗口。
func explorationWindowForPhase(runState RunState) int {
	switch runState {
	case RunStatePlan:
		return 4
	case RunStateExecute:
		return 2
	default:
		return 0
	}
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

// shouldStall 判断当前快照是否应进入 stalled。
func shouldStall(score ProgressScore, noProgressLimit int, repeatLimit int) bool {
	if repeatLimit > 0 && score.RepeatCycleStreak >= repeatLimit {
		return true
	}
	if noProgressLimit > 0 && score.NoProgressStreak >= noProgressLimit {
		return true
	}
	return false
}

// selectReminderKind 选择 stalled 场景下应注入的提醒类型。
func selectReminderKind(score ProgressScore) ReminderKind {
	if score.RepeatCycleStreak > 0 && score.SameToolSignature && score.SameResultFingerprint {
		return ReminderKindRepeatCycle
	}
	if score.NoProgressStreak > 0 {
		return ReminderKindNoProgress
	}
	return ReminderKindGenericStalled
}
