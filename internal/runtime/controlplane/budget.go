package controlplane

// TurnBudgetAction 表示预算控制面对单次发送尝试做出的唯一动作。
type TurnBudgetAction string

const (
	TurnBudgetActionAllow   TurnBudgetAction = "allow"
	TurnBudgetActionCompact TurnBudgetAction = "compact"
	TurnBudgetActionStop    TurnBudgetAction = "stop"
)

const (
	// TurnBudgetGatePolicyGateable 表示估算可作为预算硬停门禁依据。
	TurnBudgetGatePolicyGateable = "gateable"
	// TurnBudgetGatePolicyAdvisory 表示估算仅用于提示或触发 compact，不能硬停。
	TurnBudgetGatePolicyAdvisory = "advisory"
)

const (
	// BudgetDecisionReasonWithinBudget 表示估算在预算范围内。
	BudgetDecisionReasonWithinBudget = "within_budget"
	// BudgetDecisionReasonEstimateFailedBypass 表示估算失败后跳过预算门禁并放行。
	BudgetDecisionReasonEstimateFailedBypass = "estimate_failed_bypass"
	// BudgetDecisionReasonExceedsBudgetFirstTime 表示首次超预算，需要先 compact。
	BudgetDecisionReasonExceedsBudgetFirstTime = "exceeds_budget_first_time"
	// BudgetDecisionReasonExceedsBudgetAfterCompactStop 表示 compact 后仍超预算且可门禁，必须停止。
	BudgetDecisionReasonExceedsBudgetAfterCompactStop = "exceeds_budget_after_compact_stop"
	// BudgetDecisionReasonExceedsBudgetAfterCompactAllowAdvisory 表示 compact 后仍超预算但仅 advisory，允许放行。
	BudgetDecisionReasonExceedsBudgetAfterCompactAllowAdvisory = "exceeds_budget_after_compact_allow_advisory"
)

// TurnBudgetID 标识一次冻结预算尝试，避免 estimate、decision 与 usage observation 串用。
type TurnBudgetID struct {
	AttemptSeq  int    `json:"attempt_seq"`
	RequestHash string `json:"request_hash"`
}

// TurnBudgetEstimate 描述 runtime 对冻结请求输入 token 的主干估算事实。
type TurnBudgetEstimate struct {
	ID                   TurnBudgetID `json:"id"`
	EstimatedInputTokens int          `json:"estimated_input_tokens"`
	EstimateSource       string       `json:"estimate_source,omitempty"`
	GatePolicy           string       `json:"gate_policy,omitempty"`
}

// TurnBudgetDecision 描述冻结请求在当前预算事实下的决策结果。
type TurnBudgetDecision struct {
	ID                   TurnBudgetID     `json:"id"`
	Action               TurnBudgetAction `json:"action"`
	Reason               string           `json:"reason,omitempty"`
	EstimatedInputTokens int              `json:"estimated_input_tokens"`
	PromptBudget         int              `json:"prompt_budget"`
	EstimateSource       string           `json:"estimate_source,omitempty"`
	EstimateGatePolicy   string           `json:"estimate_gate_policy,omitempty"`
}

// DecideTurnBudget 根据输入预算事实输出 allow、compact 或 stop 三种动作。
func DecideTurnBudget(
	estimate TurnBudgetEstimate,
	promptBudget int,
	compactCount int,
) TurnBudgetDecision {
	decision := TurnBudgetDecision{
		ID:                   estimate.ID,
		EstimatedInputTokens: estimate.EstimatedInputTokens,
		PromptBudget:         promptBudget,
		EstimateSource:       estimate.EstimateSource,
		EstimateGatePolicy:   estimate.GatePolicy,
	}
	if estimate.EstimatedInputTokens <= promptBudget {
		decision.Action = TurnBudgetActionAllow
		decision.Reason = BudgetDecisionReasonWithinBudget
		return decision
	}
	if compactCount == 0 {
		decision.Action = TurnBudgetActionCompact
		decision.Reason = BudgetDecisionReasonExceedsBudgetFirstTime
		return decision
	}
	if estimate.GatePolicy == TurnBudgetGatePolicyGateable {
		decision.Action = TurnBudgetActionStop
		decision.Reason = BudgetDecisionReasonExceedsBudgetAfterCompactStop
		return decision
	}
	decision.Action = TurnBudgetActionAllow
	decision.Reason = BudgetDecisionReasonExceedsBudgetAfterCompactAllowAdvisory
	return decision
}
