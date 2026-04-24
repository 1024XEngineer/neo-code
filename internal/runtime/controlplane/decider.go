package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// TerminalStatus 表示 runtime run 的唯一终态裁决结果。
type TerminalStatus string

const (
	TerminalStatusCompleted  TerminalStatus = "completed"
	TerminalStatusContinue   TerminalStatus = "continue"
	TerminalStatusIncomplete TerminalStatus = "incomplete"
	TerminalStatusFailed     TerminalStatus = "failed"
)

// StopInput 汇总最终 stop 决议所需的信号。
type StopInput struct {
	PreDecidedReason StopReason
	PreDecidedDetail string

	UserInterrupted    bool
	BudgetExceeded     bool
	MaxTurnsReached    bool
	MaxTurnsLimit      int
	RetryExhausted     bool
	VerificationFailed bool
	FatalError         error
	Completed          bool
}

// DecideStopReason 按固定优先级返回唯一的最终 stop 原因。
func DecideStopReason(in StopInput) (StopReason, string) {
	if in.PreDecidedReason != "" {
		return in.PreDecidedReason, strings.TrimSpace(in.PreDecidedDetail)
	}
	if in.UserInterrupted {
		return StopReasonUserInterrupt, ""
	}
	if in.FatalError != nil {
		if errors.Is(in.FatalError, context.Canceled) {
			return StopReasonUserInterrupt, ""
		}
		return StopReasonFatalError, strings.TrimSpace(in.FatalError.Error())
	}
	if in.BudgetExceeded {
		return StopReasonBudgetExceeded, ""
	}
	if in.MaxTurnsReached {
		if in.MaxTurnsLimit > 0 {
			return StopReasonMaxTurnExceeded, fmt.Sprintf("runtime: max turn limit reached (%d)", in.MaxTurnsLimit)
		}
		return StopReasonMaxTurnExceeded, ""
	}
	if in.RetryExhausted {
		return StopReasonRetryExhausted, ""
	}
	if in.VerificationFailed {
		return StopReasonVerificationFailed, ""
	}
	if in.Completed {
		return StopReasonAccepted, ""
	}
	return StopReasonFatalError, "runtime: stop reason undetermined"
}
