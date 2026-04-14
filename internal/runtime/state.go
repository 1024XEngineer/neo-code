package runtime

import (
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// maxReactiveCompactAttempts 限制 reactive compact 最大尝试次数，超出后放弃降级并返回错误。
const maxReactiveCompactAttempts = 3

// runState 汇总单次 Run 生命周期内会变化的会话与计量状态。
type runState struct {
	runID                   string
	session                 agentsession.Session
	tokenInputTotal         int
	tokenOutputTotal        int
	compactApplied          bool
	reactiveCompactAttempts int
	rememberedThisRun       bool
}

// newRunState 基于持久化会话创建一次运行的内存状态镜像。
func newRunState(runID string, session agentsession.Session) runState {
	return runState{
		runID:            runID,
		session:          session,
		tokenInputTotal:  session.TokenInputTotal,
		tokenOutputTotal: session.TokenOutputTotal,
	}
}

// syncSessionTokenTotals 将运行期 token 计数同步回会话对象。
func (s *runState) syncSessionTokenTotals() {
	if s == nil {
		return
	}
	s.session.TokenInputTotal = s.tokenInputTotal
	s.session.TokenOutputTotal = s.tokenOutputTotal
}

// recordUsage 累加本轮 provider 返回的 token 使用量。
func (s *runState) recordUsage(inputTokens int, outputTokens int) {
	if s == nil {
		return
	}
	s.tokenInputTotal += inputTokens
	s.tokenOutputTotal += outputTokens
}

// resetTokenTotals 在 compact 应用成功后清零当前运行的 token 账本。
func (s *runState) resetTokenTotals() {
	if s == nil {
		return
	}
	s.tokenInputTotal = 0
	s.tokenOutputTotal = 0
	s.syncSessionTokenTotals()
}

// touchSession 更新会话修改时间并同步最新 token 累计值。
func (s *runState) touchSession() {
	if s == nil {
		return
	}
	s.syncSessionTokenTotals()
	s.session.UpdatedAt = time.Now()
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
