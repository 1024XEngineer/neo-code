package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	commandSuccessVerifierName = "command_success"
)

// CommandSuccessVerifier 负责执行配置命令并基于退出码做验证。
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

	cfg, exists := input.VerificationConfig.Verifiers[name]
	if !exists {
		cfg = input.VerificationConfig.Verifiers[commandSuccessVerifierName]
	}
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		if cfg.Required {
			status := VerificationSoftBlock
			summary := "verification command is required but missing"
			reason := "missing verifier command configuration"
			errorClass := ErrorClassEnvMissing
			if cfg.FailClosed {
				status = VerificationFail
			}
			if cfg.FailOpen {
				status = VerificationPass
				summary = "verification command missing but ignored by fail_open policy"
				reason = "optionalized by fail_open policy"
				errorClass = ""
			}
			return VerificationResult{
				Name:       name,
				Status:     status,
				Summary:    summary,
				Reason:     reason,
				ErrorClass: errorClass,
			}, nil
		}
		return VerificationResult{
			Name:    name,
			Status:  VerificationPass,
			Summary: "verification command is not configured, skip",
			Reason:  "optional verifier skipped",
		}, nil
	}

	result, err := executor.Execute(ctx, CommandExecutionRequest{
		Command:       command,
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
				Evidence: map[string]any{
					"command": command,
				},
			}, nil
		default:
			errorClass := classifyCommandExecutionError(err)
			return VerificationResult{
				Name:       name,
				Status:     VerificationFail,
				Summary:    err.Error(),
				Reason:     "verification command execution failed",
				ErrorClass: errorClass,
				Retryable:  errorClass == ErrorClassTimeout,
				Evidence: map[string]any{
					"command": command,
				},
			}, nil
		}
	}

	if result.ExitCode == 0 {
		return VerificationResult{
			Name:     name,
			Status:   VerificationPass,
			Summary:  "verification command succeeded",
			Reason:   "command exit code is 0",
			Evidence: commandEvidence(command, result),
		}, nil
	}
	return VerificationResult{
		Name:       name,
		Status:     VerificationFail,
		Summary:    fmt.Sprintf("verification command failed with exit code %d", result.ExitCode),
		Reason:     "command exit code is non-zero",
		ErrorClass: classifyCommandFailure(name, result),
		Retryable:  true,
		Evidence:   commandEvidence(command, result),
	}, nil
}

// commandEvidence 将命令执行结果收敛为可观测证据。
func commandEvidence(command string, result CommandExecutionResult) map[string]any {
	return map[string]any{
		"command":      command,
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
	case strings.Contains(normalized, "permission"):
		return ErrorClassPermissionDenied
	default:
		return ErrorClassUnknown
	}
}

// classifyCommandFailure 将命令失败按 verifier 语义映射到稳定错误分类。
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
