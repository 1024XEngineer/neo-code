package runtime

import (
	"sync"
	"time"

	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/decider"
	runtimefacts "neo-code/internal/runtime/facts"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
)

// runState 汇总单次 Run 生命周期内会变化的会话与计量状态。
type runState struct {
	mu                             sync.Mutex
	runID                          string
	runToken                       uint64
	session                        agentsession.Session
	compactCount                   int
	reactiveCompactAttempts        int
	rememberedThisRun              bool
	planningEnabled                bool
	taskID                         string
	agentID                        string
	capabilityToken                *security.CapabilityToken
	nextAttemptSeq                 int
	turn                           int
	baseLifecycle                  controlplane.RunState
	lifecycle                      controlplane.RunState
	waitingPermissionCount         int
	compactingCount                int
	stopEmitted                    bool
	budgetExceeded                 bool
	maxTurnsReached                bool
	maxTurnsLimit                  int
	finalInterceptStreak           int
	pendingFinalProgress           bool
	mustUseToolAfterFinalContinue  bool
	noToolAfterFinalContinueStreak int
	lastAcceptanceBlockedReason    string
	taskKind                       decider.TaskKind
	userGoal                       string
	factsCollector                 *runtimefacts.Collector
	lastDeciderDecision            decider.Decision
	terminalStatus                 controlplane.TerminalStatus
	terminalStopReason             controlplane.StopReason
	terminalStopDetail             string
	terminalSet                    bool
	hasUnknownUsage                bool
	completion                     controlplane.CompletionState
	progress                       controlplane.ProgressState
	hookAnnotations                []string
	hookNotifications              []queuedHookNotification
	hookNotificationSeen           map[string]time.Time
	hookNotificationOmitted        int
	reportedMissingSkills          map[string]struct{}
}

// newRunState 基于持久化会话创建一次运行的内存状态镜像。
func newRunState(runID string, session agentsession.Session) runState {
	return runState{
		runID:                 runID,
		session:               session,
		nextAttemptSeq:        1,
		completion:            controlplane.CompletionState{TodoOnlyTaskCandidate: true},
		reportedMissingSkills: make(map[string]struct{}),
		taskKind:              "",
		factsCollector:        runtimefacts.NewCollector(),
		hookNotificationSeen:  make(map[string]time.Time),
	}
}

// recordUsage 累加本轮 provider 返回的 token 使用量。
func (s *runState) recordUsage(inputTokens int, outputTokens int) {
	if s == nil {
		return
	}
	s.session.TokenInputTotal += inputTokens
	s.session.TokenOutputTotal += outputTokens
}

// resetTokenTotals 在 compact 应用成功后清零当前运行的 token 账本。
func (s *runState) resetTokenTotals() {
	if s == nil {
		return
	}
	s.session.TokenInputTotal = 0
	s.session.TokenOutputTotal = 0
	s.session.HasUnknownUsage = false
	s.hasUnknownUsage = false
}

// touchSession 更新会话修改时间。
func (s *runState) touchSession() {
	if s == nil {
		return
	}
	s.session.UpdatedAt = time.Now()
}

// markSkillMissingReported 记录并返回某个缺失 skill 是否首次在当前 run 中上报。
func (s *runState) markSkillMissingReported(skillID string) bool {
	if s == nil {
		return true
	}
	normalized := normalizeRuntimeSkillID(skillID)
	if normalized == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.reportedMissingSkills[normalized]; exists {
		return false
	}
	s.reportedMissingSkills[normalized] = struct{}{}
	return true
}

// markTerminalDecision 记录本次运行的唯一终态裁决结果，供统一 stop reason 发射使用。
func (s *runState) markTerminalDecision(status controlplane.TerminalStatus, reason controlplane.StopReason, detail string) {
	if s == nil {
		return
	}
	s.terminalStatus = status
	s.terminalStopReason = reason
	s.terminalStopDetail = detail
	s.terminalSet = true
}
