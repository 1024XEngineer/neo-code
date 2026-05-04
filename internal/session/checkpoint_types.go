package session

import "time"

// CheckpointReason 描述 checkpoint 的创建原因。
type CheckpointReason string

const (
	CheckpointReasonPreWrite         CheckpointReason = "pre_write"
	CheckpointReasonCompact          CheckpointReason = "compact"
	CheckpointReasonPlanMode         CheckpointReason = "plan_mode"
	CheckpointReasonManual           CheckpointReason = "manual"
	CheckpointReasonGuard            CheckpointReason = "pre_restore_guard"
	CheckpointReasonPreWriteDegraded CheckpointReason = "pre_write_degraded"
	CheckpointReasonEndOfTurn        CheckpointReason = "end_of_turn"
)

// CheckpointStatus 描述 checkpoint 的生命周期状态。
type CheckpointStatus string

const (
	CheckpointStatusCreating  CheckpointStatus = "creating"
	CheckpointStatusAvailable CheckpointStatus = "available"
	CheckpointStatusBroken    CheckpointStatus = "broken"
	CheckpointStatusRestored  CheckpointStatus = "restored"
	CheckpointStatusPruned    CheckpointStatus = "pruned"
)

// CheckpointRecord 遵循 RFC 6.2 定义，包含所有恢复相关引用。
type CheckpointRecord struct {
	CheckpointID         string
	WorkspaceKey         string
	SessionID            string
	RunID                string
	Workdir              string
	CreatedAt            time.Time
	Reason               CheckpointReason
	CodeCheckpointRef    string
	SessionCheckpointRef string
	ResumeCheckpointRef  string
	TranscriptRevision   int64
	Restorable           bool
	Status               CheckpointStatus
}

// SessionCheckpoint 保存完整 durable 会话上下文快照。
type SessionCheckpoint struct {
	ID           string
	SessionID    string
	HeadJSON     string
	MessagesJSON string
	CreatedAt    time.Time
}

// ResumeCheckpoint 记录运行恢复所需的最小闭环信息。
type ResumeCheckpoint struct {
	ID                 string
	WorkspaceKey       string
	RunID              string
	SessionID          string
	Turn               int
	Phase              string
	CompletionState    string
	TranscriptRevision int64
	UpdatedAt          time.Time
}
