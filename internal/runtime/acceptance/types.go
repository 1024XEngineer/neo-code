package acceptance

import (
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/decider"
	"neo-code/internal/runtime/verify"
)

// CompletionGateDecision 表示 completion gate 评估结果。
type CompletionGateDecision struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// AcceptanceStatus 表示 final 验收的统一决策状态。
type AcceptanceStatus string

const (
	AcceptanceAccepted   AcceptanceStatus = "accepted"
	AcceptanceContinue   AcceptanceStatus = "continue"
	AcceptanceIncomplete AcceptanceStatus = "incomplete"
	AcceptanceFailed     AcceptanceStatus = "failed"
)

// AcceptanceDecision 表示 runtime beforeAcceptFinal 的结构化输出。
type AcceptanceDecision struct {
	Status                  AcceptanceStatus            `json:"status"`
	StopReason              controlplane.StopReason     `json:"stop_reason,omitempty"`
	ErrorClass              verify.ErrorClass           `json:"error_class,omitempty"`
	CompletionPassed        bool                        `json:"completion_passed,omitempty"`
	VerificationPassed      bool                        `json:"verification_passed,omitempty"`
	CompletionBlockedReason string                      `json:"completion_blocked_reason,omitempty"`
	MissingFacts            []decider.MissingFact       `json:"missing_facts,omitempty"`
	RequiredNextActions     []decider.RequiredAction    `json:"required_next_actions,omitempty"`
	RequiredInput           *decider.RequiredInput      `json:"required_input,omitempty"`
	IntentHint              decider.TaskKind            `json:"intent_hint,omitempty"`
	EffectiveTaskKind       decider.TaskKind            `json:"effective_task_kind,omitempty"`
	UserVisibleSummary      string                      `json:"user_visible_summary,omitempty"`
	InternalSummary         string                      `json:"internal_summary,omitempty"`
	ContinueHint            string                      `json:"continue_hint,omitempty"`
	VerifierResults         []verify.VerificationResult `json:"verifier_results,omitempty"`
	HasProgress             bool                        `json:"has_progress,omitempty"`
	Retryable               bool                        `json:"retryable,omitempty"`
	WaitingExternal         bool                        `json:"waiting_external,omitempty"`
}

// FinalAcceptanceInput 表示 beforeAcceptFinal 需要的输入快照。
type FinalAcceptanceInput struct {
	CompletionGate     CompletionGateDecision  `json:"completion_gate"`
	VerificationInput  verify.FinalVerifyInput `json:"verification_input"`
	NoProgressExceeded bool                    `json:"no_progress_exceeded,omitempty"`
	MaxTurnsReached    bool                    `json:"max_turns_reached,omitempty"`
	MaxTurnsLimit      int                     `json:"max_turns_limit,omitempty"`
}
