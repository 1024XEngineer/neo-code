package controlplane

// ProgressEvidenceKind 标识工具/适配器产出的证据类型，runtime 仅聚合不做语义推断。
type ProgressEvidenceKind string

const (
	// EvidenceNewInfoNonDup 表示本轮引入了非重复的新信息（用于 streak 回归约束）。
	EvidenceNewInfoNonDup ProgressEvidenceKind = "EVIDENCE_NEW_INFO_NON_DUP"
)

// ProgressEvidenceRecord 描述一条可计分的进展证据。
type ProgressEvidenceRecord struct {
	Kind   ProgressEvidenceKind `json:"kind"`
	Detail string               `json:"detail,omitempty"`
}

// ProgressScore 表示一次评估后的分值增量与 streak 快照。
type ProgressScore struct {
	ScoreDelta        int `json:"score_delta"`
	NoProgressStreak  int `json:"no_progress_streak"`
	RepeatCycleStreak int `json:"repeat_cycle_streak"`
}

// ProgressState 汇总当前运行期 progress 控制面状态。
type ProgressState struct {
	LastScore ProgressScore `json:"last_score"`
}

// ApplyProgressEvidence 根据证据更新分值与 streak。
func ApplyProgressEvidence(state ProgressState, records []ProgressEvidenceRecord) ProgressState {
	next := state.LastScore
	if len(records) == 0 {
		next.NoProgressStreak++
	} else {
		next.NoProgressStreak = 0
		next.ScoreDelta++
	}
	return ProgressState{LastScore: next}
}
