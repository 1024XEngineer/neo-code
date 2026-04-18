package controlplane

// Phase 表示单轮 ReAct 内的显式阶段（plan -> execute -> dispatch -> verify）。
type Phase string

const (
	// PhasePlan 规划阶段：构建上下文、调用 provider 直至得到 assistant 消息（含工具调用决策）。
	PhasePlan Phase = "plan"
	// PhaseExecute 执行阶段：执行本批次全部工具调用。
	PhaseExecute Phase = "execute"
	// PhaseDispatch 调度阶段：执行 Todo 驱动的子代理任务派发。
	PhaseDispatch Phase = "dispatch"
	// PhaseVerify 验证阶段：工具结果已回灌，等待下一轮 provider 校验或收尾。
	PhaseVerify Phase = "verify"
)
