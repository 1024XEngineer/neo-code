package runtime

import (
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
)

// TurnBudgetSnapshot 冻结单次预算尝试需要的 request、provider 配置与预算事实上下文。
type TurnBudgetSnapshot struct {
	ID                     controlplane.TurnBudgetID
	Config                 config.Config
	ProviderConfig         provider.RuntimeConfig
	Model                  string
	Workdir                string
	ToolTimeout            time.Duration
	PromptBudget           int
	BudgetSource           string
	CompactCount           int
	NoProgressStreakLimit  int
	RepeatCycleStreakLimit int
	InjectFullPlan         bool
	ContextWindow          int
	Request                providertypes.GenerateRequest
}

// TurnBudgetUsageObservation 描述 provider 调用完成后可被证明的 usage 观察结果。
type TurnBudgetUsageObservation struct {
	ID             controlplane.TurnBudgetID
	InputTokens    int
	OutputTokens   int
	InputObserved  bool
	OutputObserved bool
}

// turnProviderOutput 汇总一次 provider 调用返回的 assistant 消息与预算 usage observation。
type turnProviderOutput struct {
	assistant        providertypes.Message
	usageObservation TurnBudgetUsageObservation
}

// ledgerReconcileResult 描述单轮 usage 调和后写入账本与事件的结果。
type ledgerReconcileResult struct {
	inputTokens     int
	inputSource     string
	outputTokens    int
	outputSource    string
	hasUnknownUsage bool
}

// newTurnBudgetSnapshot 构造本次发送尝试的冻结预算快照。
func newTurnBudgetSnapshot(
	attemptSeq int,
	cfg config.Config,
	providerConfig provider.RuntimeConfig,
	model string,
	workdir string,
	toolTimeout time.Duration,
	promptBudget int,
	budgetSource string,
	compactCount int,
	noProgressStreakLimit int,
	repeatCycleStreakLimit int,
	injectFullPlan bool,
	contextWindow int,
	request providertypes.GenerateRequest,
) TurnBudgetSnapshot {
	if attemptSeq <= 0 {
		attemptSeq = 1
	}
	return TurnBudgetSnapshot{
		ID: controlplane.TurnBudgetID{
			AttemptSeq:  attemptSeq,
			RequestHash: computeRequestHash(request),
		},
		Config:                 cfg,
		ProviderConfig:         providerConfig,
		Model:                  model,
		Workdir:                workdir,
		ToolTimeout:            toolTimeout,
		PromptBudget:           promptBudget,
		BudgetSource:           budgetSource,
		CompactCount:           compactCount,
		NoProgressStreakLimit:  noProgressStreakLimit,
		RepeatCycleStreakLimit: repeatCycleStreakLimit,
		InjectFullPlan:         injectFullPlan,
		ContextWindow:          contextWindow,
		Request:                request,
	}
}

// newTurnBudgetEstimate 将 provider signal 包装为 runtime 预算主干估算对象。
func newTurnBudgetEstimate(
	id controlplane.TurnBudgetID,
	estimate providertypes.BudgetEstimate,
) controlplane.TurnBudgetEstimate {
	gatePolicy := controlplane.TurnBudgetGatePolicyAdvisory
	if estimate.GatePolicy == provider.EstimateGateGateable {
		gatePolicy = controlplane.TurnBudgetGatePolicyGateable
	}
	return controlplane.TurnBudgetEstimate{
		ID:                   id,
		EstimatedInputTokens: estimate.EstimatedInputTokens,
		EstimateSource:       estimate.EstimateSource,
		GatePolicy:           gatePolicy,
	}
}

// newTurnBudgetUsageObservation 构造单次 provider 调用对应的 usage observation。
func newTurnBudgetUsageObservation(
	id controlplane.TurnBudgetID,
	inputTokens int,
	outputTokens int,
	inputObserved bool,
	outputObserved bool,
) TurnBudgetUsageObservation {
	return TurnBudgetUsageObservation{
		ID:             id,
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
		InputObserved:  inputObserved,
		OutputObserved: outputObserved,
	}
}
