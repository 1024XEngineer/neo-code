package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const commandSuccessVerifierName = "command_success"

// CommandSuccessVerifier 负责执行配置命令并基于退出码做验收。
type CommandSuccessVerifier struct {
	VerifierName string
	Executor     CommandExecutor
}

// Name 返回 verifier 名称。
func (v CommandSuccessVerifier) Name() string {
	normalized := strings.TrimSpace(v.VerifierName)
	if normalized == "" {
		return commandSuccessVerifierName
	}
	return normalized
}

// VerifyFinal 执行命令并返回 pass/fail/soft_block 结果。
func (v CommandSuccessVerifier) VerifyFinal(ctx context.Context, input FinalVerifyInput) (VerificationResult, error) {
	name := v.Name()
	executor := v.Executor
	if executor == nil {
		executor = PolicyCommandExecutor{}
	}

	cfg := input.VerificationConfig.Verifiers[name]
	if len(cfg.Command) == 0 {
		cfg = input.VerificationConfig.Verifiers[commandSuccessVerifierName]
	}
	argv := compactStrings(cfg.Command)
	if len(argv) == 0 {
		return VerificationResult{
			Name:       name,
			Status:     VerificationFail,
			Summary:    "verification command is missing",
			Reason:     "missing verifier command configuration",
			ErrorClass: ErrorClassEnvMissing,
		}, nil
	}

	result, err := executor.Execute(ctx, CommandExecutionRequest{
		Argv:          argv,
		Workdir:       input.Workdir,
		TimeoutSec:    cfg.TimeoutSec,
		OutputCapByte: cfg.OutputCapBytes,
		Policy:        input.VerificationConfig.ExecutionPolicy,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrVerificationExecutionDenied):
			return VerificationResult{
				Name:       name,
				Status:     VerificationFail,
				Summary:    err.Error(),
				Reason:     "verification command denied by execution policy",
				ErrorClass: ErrorClassPermissionDenied,
				Evidence:   commandEvidence(argv, result),
			}, nil
		default:
			errorClass := classifyCommandExecutionError(err)
			return VerificationResult{
				Name:       name,
				Status:     VerificationFail,
				Summary:    err.Error(),
				Reason:     "verification command execution failed",
				ErrorClass: errorClass,
				Retryable:  false,
				Evidence:   commandEvidence(argv, result),
			}, nil
		}
	}

	if result.ExitCode == 0 {
		return VerificationResult{
			Name:     name,
			Status:   VerificationPass,
			Summary:  "verification command succeeded",
			Reason:   "command exit code is 0",
			Evidence: commandEvidence(argv, result),
		}, nil
	}
	return VerificationResult{
		Name:       name,
		Status:     VerificationFail,
		Summary:    fmt.Sprintf("verification command failed with exit code %d", result.ExitCode),
		Reason:     "command exit code is non-zero",
		ErrorClass: classifyCommandFailure(name, result),
		Retryable:  true,
		Evidence:   commandEvidence(argv, result),
	}, nil
}

// commandEvidence 将命令执行结果收敛为可观测证据。
func commandEvidence(argv []string, result CommandExecutionResult) map[string]any {
	return map[string]any{
		"argv":         append([]string(nil), argv...),
		"command_name": result.CommandName,
		"exit_code":    result.ExitCode,
		"timed_out":    result.TimedOut,
		"truncated":    result.Truncated,
		"duration_ms":  result.DurationMS,
		"stdout":       strings.TrimSpace(result.Stdout),
		"stderr":       strings.TrimSpace(result.Stderr),
	}
}

// classifyCommandExecutionError 将命令执行错误映射为统一 ErrorClass。
func classifyCommandExecutionError(err error) ErrorClass {
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(normalized, "timeout"):
		return ErrorClassTimeout
	case strings.Contains(normalized, "not found"):
		return ErrorClassCommandNotFound
	case strings.Contains(normalized, "permission"), strings.Contains(normalized, "access is denied"):
		return ErrorClassPermissionDenied
	default:
		return ErrorClassUnknown
	}
}

// classifyCommandFailure 将命令领域失败按 verifier 语义映射到稳定错误分类。
func classifyCommandFailure(verifierName string, result CommandExecutionResult) ErrorClass {
	if result.TimedOut {
		return ErrorClassTimeout
	}
	switch strings.ToLower(strings.TrimSpace(verifierName)) {
	case "build":
		return ErrorClassCompileError
	case "test":
		return ErrorClassTestFailure
	case "lint":
		return ErrorClassLintFailure
	case "typecheck":
		return ErrorClassTypeError
	default:
		return ErrorClassUnknown
	}
}
