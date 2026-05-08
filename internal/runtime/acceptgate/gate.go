package acceptgate

import (
	"context"
	"fmt"
	"strings"

	"neo-code/internal/runtime/controlplane"
	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

// Outcome 表示 Accept Gate 的二元终态结果。
type Outcome string

const (
	// OutcomeAccepted 表示所有必需验收项均已满足。
	OutcomeAccepted Outcome = "accepted"
	// OutcomeFailed 表示至少一个必需验收项缺少运行期证据或状态未收敛。
	OutcomeFailed Outcome = "failed"
)

// Input 汇总最终验收所需的运行期事实和 plan 状态。
type Input struct {
	PlanVerify        agentsession.AcceptChecks
	Facts             runtimefacts.RuntimeFacts
	Todos             []agentsession.TodoItem
	LastAssistantText string
}

// CheckResult 描述单个验收项的判定结果。
type CheckResult struct {
	Passed bool   `json:"passed"`
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Report 描述 Accept Gate 的完整判定报告。
type Report struct {
	Outcome    Outcome                 `json:"status"`
	StopReason controlplane.StopReason `json:"stop_reason,omitempty"`
	Summary    string                  `json:"summary,omitempty"`
	Results    []CheckResult           `json:"results,omitempty"`
}

// Evaluate 按固定顺序检查 plan-owned todo 与 Plan.Verify 运行期证据。
func Evaluate(ctx context.Context, input Input) Report {
	if err := ctx.Err(); err != nil {
		return Report{
			Outcome:    OutcomeFailed,
			StopReason: controlplane.StopReasonFatalError,
			Summary:    err.Error(),
		}
	}

	report := Report{
		Outcome:    OutcomeAccepted,
		StopReason: controlplane.StopReasonAccepted,
	}

	report.add(checkRequiredTodoFailures(input.Todos))
	report.add(checkRequiredTodoConvergence(input.Todos))

	checks := input.PlanVerify.Normalize()
	if len(checks) == 0 {
		checks = agentsession.AcceptChecks{{Kind: agentsession.AcceptCheckOutputOnly, Required: true}}
	}
	for _, check := range checks {
		report.add(evaluateAcceptCheck(input, check))
	}
	report.finalize()
	return report
}

func (r *Report) add(result CheckResult) {
	if strings.TrimSpace(result.Name) == "" {
		return
	}
	r.Results = append(r.Results, result)
	if result.Passed {
		return
	}
	r.Outcome = OutcomeFailed
	switch result.Name {
	case "required_todo_failed":
		r.StopReason = controlplane.StopReasonRequiredTodoFailed
	case "required_todo_convergence":
		if r.StopReason != controlplane.StopReasonRequiredTodoFailed {
			r.StopReason = controlplane.StopReasonTodoNotConverged
		}
	default:
		if r.StopReason == "" || r.StopReason == controlplane.StopReasonAccepted {
			r.StopReason = controlplane.StopReasonAcceptCheckFailed
		}
	}
}

func (r *Report) finalize() {
	if r.Outcome == OutcomeAccepted {
		r.StopReason = controlplane.StopReasonAccepted
		r.Summary = "acceptance checks passed"
		return
	}
	if r.StopReason == "" || r.StopReason == controlplane.StopReasonAccepted {
		r.StopReason = controlplane.StopReasonAcceptCheckFailed
	}
	failures := make([]string, 0, len(r.Results))
	for _, result := range r.Results {
		if result.Passed {
			continue
		}
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "failed"
		}
		failures = append(failures, fmt.Sprintf("%s: %s", result.Name, reason))
	}
	r.Summary = strings.Join(failures, "; ")
}
