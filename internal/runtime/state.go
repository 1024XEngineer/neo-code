package runtime

import (
	"sync"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

// maxReactiveCompactAttempts 限制 reactive compact 最大尝试次数，超出后放弃降级并返回错误。
const maxReactiveCompactAttempts = 3

// runState 汇总单次 Run 生命周期内会变化的会话与计量状态。
type runState struct {
	mu                      sync.Mutex
	runID                   string
	session                 agentsession.Session
	compactApplied          bool
	reactiveCompactAttempts int
	rememberedThisRun       bool
	turn                    int
	phase                   controlplane.Phase
	stopEmitted             bool
	progress                controlplane.ProgressState
	reportedMissingSkills   map[string]struct{}
}

// newRunState 基于持久化会话创建一次运行的内存状态镜像。
func newRunState(runID string, session agentsession.Session) runState {
	return runState{
		runID:                 runID,
		session:               session,
		reportedMissingSkills: make(map[string]struct{}),
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

// turnSnapshot 冻结单轮推理所需的配置、上下文与 provider 请求。
type turnSnapshot struct {
	config         config.Config
	providerConfig provider.RuntimeConfig
	model          string
	workdir        string
	toolTimeout    time.Duration
	request        providertypes.GenerateRequest
}

// providerTurnResult 表示单轮 provider 调用成功后的结构化结果。
type providerTurnResult struct {
	assistant    providertypes.Message
	inputTokens  int
	outputTokens int
}
