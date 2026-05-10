package verify

import (
	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
)

// VerificationStatus 表示单个 verifier 的结构化结果状态。
type VerificationStatus string

const (
	// VerificationPass 表示验证通过。
	VerificationPass VerificationStatus = "pass"
	// VerificationFail 表示验证失败。
	VerificationFail VerificationStatus = "fail"
)

// ErrorClass 表示验证失败的稳定错误分类。
type ErrorClass string

const (
	ErrorClassCompileError     ErrorClass = "compile_error"
	ErrorClassTestFailure      ErrorClass = "test_failure"
	ErrorClassLintFailure      ErrorClass = "lint_failure"
	ErrorClassTypeError        ErrorClass = "type_error"
	ErrorClassTimeout          ErrorClass = "timeout"
	ErrorClassPermissionDenied ErrorClass = "permission_denied"
	ErrorClassEnvMissing       ErrorClass = "env_missing"
	ErrorClassCommandNotFound  ErrorClass = "command_not_found"
	ErrorClassUnknown          ErrorClass = "unknown"
)

// VerificationResult 表示单个 verifier 的输出。
type VerificationResult struct {
	Name            string             `json:"name"`
	Status          VerificationStatus `json:"status"`
	Summary         string             `json:"summary,omitempty"`
	Reason          string             `json:"reason,omitempty"`
	ErrorClass      ErrorClass         `json:"error_class,omitempty"`
	Retryable       bool               `json:"retryable,omitempty"`
	WaitingExternal bool               `json:"waiting_external,omitempty"`
	Evidence        map[string]any     `json:"evidence,omitempty"`
}

// MessageLike 表示 verifier 所需的消息快照。
type MessageLike struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ToolResultLike 表示 verifier 可读取的工具结果快照。
type ToolResultLike struct {
	Name    string         `json:"name,omitempty"`
	Content string         `json:"content,omitempty"`
	IsError bool           `json:"is_error,omitempty"`
	Facts   map[string]any `json:"facts,omitempty"`
}

// TodoContentCheckSnapshot 表示 verifier 消费的结构化内容校验规则。
type TodoContentCheckSnapshot struct {
	Artifact string   `json:"artifact,omitempty"`
	Contains []string `json:"contains,omitempty"`
}

// TodoSnapshot 表示 verifier 所需的 todo 快照。
type TodoSnapshot struct {
	ID            string                     `json:"id"`
	Content       string                     `json:"content,omitempty"`
	Status        string                     `json:"status,omitempty"`
	Required      bool                       `json:"required"`
	BlockedReason string                     `json:"blocked_reason,omitempty"`
	Acceptance    []string                   `json:"acceptance,omitempty"`
	Artifacts     []string                   `json:"artifacts,omitempty"`
	Supersedes    []string                   `json:"supersedes,omitempty"`
	ContentChecks []TodoContentCheckSnapshot `json:"content_checks,omitempty"`
	RetryCount    int                        `json:"retry_count,omitempty"`
	RetryLimit    int                        `json:"retry_limit,omitempty"`
	FailureReason string                     `json:"failure_reason,omitempty"`
}

// TaskStateSnapshot 表示 verifier 所需的 task_state 结构化输入。
type TaskStateSnapshot struct {
	VerificationProfile string   `json:"verification_profile,omitempty"`
	KeyArtifacts        []string `json:"key_artifacts,omitempty"`
}

// FinalVerifyInput 表示一次 final 验证请求的完整输入。
type FinalVerifyInput struct {
	SessionID          string                    `json:"session_id,omitempty"`
	RunID              string                    `json:"run_id,omitempty"`
	TaskID             string                    `json:"task_id,omitempty"`
	Workdir            string                    `json:"workdir,omitempty"`
	Messages           []MessageLike             `json:"messages,omitempty"`
	Todos              []TodoSnapshot            `json:"todos,omitempty"`
	LastAssistantFinal string                    `json:"last_assistant_final,omitempty"`
	ToolResults        []ToolResultLike          `json:"tool_results,omitempty"`
	TaskState          TaskStateSnapshot         `json:"task_state,omitempty"`
	VerificationConfig config.VerificationConfig `json:"verification_config,omitempty"`
}

// VerificationGateDecision 表示聚合后的 verification gate 决议。
type VerificationGateDecision struct {
	Passed  bool                    `json:"passed"`
	Reason  controlplane.StopReason `json:"reason"`
	Results []VerificationResult    `json:"results,omitempty"`
}

// NormalizeResult 规整 verifier 输出，确保状态与错误分类始终可消费。
func NormalizeResult(result VerificationResult) VerificationResult {
	if result.Status == "" {
		result.Status = VerificationFail
	}
	if result.Status == VerificationPass {
		result.ErrorClass = ""
		return result
	}
	if result.Status == VerificationFail && result.ErrorClass == "" {
		result.ErrorClass = ErrorClassUnknown
	}
	return result
}
