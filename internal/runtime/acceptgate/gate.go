package acceptgate

import (
	"context"
	"fmt"
	"strings"

	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

// Outcome 表示 Accept Gate 的系统预检结果。
type Outcome string

const (
	// OutcomeAccepted 表示系统预检已通过。
	OutcomeAccepted Outcome = "accepted"
	// OutcomeContinue 表示存在可恢复问题，应提示模型继续工作。
	OutcomeContinue Outcome = "continued"
	// OutcomeFailed 表示存在不可恢复问题，应终止本轮。
	OutcomeFailed Outcome = "failed"
)

// Input 汇总系统预检所需的最小运行态。
type Input struct {
	Todos             []agentsession.TodoItem
	LastAssistantText string
}

// CheckResult 描述单个系统预检项的判定结果。
type CheckResult struct {
	Passed bool   `json:"passed"`
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

// Report 描述 Accept Gate 的完整判定报告。
type Report struct {
	Outcome      Outcome                 `json:"status"`
	StopReason   controlplane.StopReason `json:"stop_reason,omitempty"`
	Summary      string                  `json:"summary,omitempty"`
	ContinueHint string                  `json:"continue_hint,omitempty"`
	Results      []CheckResult           `json:"results,omitempty"`
}

// Evaluate 执行收尾前的系统预检，只处理框架级状态，不再承担内容正确性验证。
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
	report.add(checkOutputOnly(input.LastAssistantText))
	report.add(checkRequiredTodoFailures(input.Todos))
	report.add(checkRequiredTodoConvergence(input.Todos))
	report.finalize()
	return report
}

// add 记录系统预检结果，并按可恢复性更新终态。
func (r *Report) add(result CheckResult) {
	if strings.TrimSpace(result.Name) == "" {
		return
	}
	r.Results = append(r.Results, result)
	if result.Passed {
		return
	}
	switch result.Name {
	case "required_todo_failed":
		r.Outcome = OutcomeFailed
		r.StopReason = controlplane.StopReasonRequiredTodoFailed
	case "output_only":
		if r.Outcome != OutcomeFailed {
			r.Outcome = OutcomeContinue
			r.StopReason = controlplane.StopReasonAcceptContinue
			r.ContinueHint = "你刚才没有给出可见回复，请继续完成任务并给出明确结果。"
		}
	case "required_todo_convergence":
		if r.Outcome != OutcomeFailed {
			r.Outcome = OutcomeContinue
			r.StopReason = controlplane.StopReasonAcceptContinue
			r.ContinueHint = "仍有 required todo 未完成，请继续处理后再结束。"
		}
	}
}

// finalize 汇总失败原因，形成对上层展示稳定的判定摘要。
func (r *Report) finalize() {
	if r.Outcome == OutcomeAccepted {
		r.StopReason = controlplane.StopReasonAccepted
		r.Summary = "acceptance prechecks passed"
		return
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
