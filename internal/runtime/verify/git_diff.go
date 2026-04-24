package verify

import (
	"context"
	"strings"
)

const (
	gitDiffVerifierName = "git_diff"
)

// GitDiffVerifier 校验工作区是否存在有效 diff。
type GitDiffVerifier struct {
	Executor CommandExecutor
}

// Name 返回 verifier 名称。
func (v GitDiffVerifier) Name() string {
	return gitDiffVerifierName
}

// VerifyFinal 执行 git diff 检查，确保 edit/fix/refactor 任务有真实改动。
func (v GitDiffVerifier) VerifyFinal(ctx context.Context, input FinalVerifyInput) (VerificationResult, error) {
	executor := v.Executor
	if executor == nil {
		executor = PolicyCommandExecutor{}
	}
	cfg := input.VerificationConfig.Verifiers[gitDiffVerifierName]
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		command = "git diff --name-only"
	}

	result, err := executor.Execute(ctx, CommandExecutionRequest{
		Command:       command,
		Workdir:       input.Workdir,
		TimeoutSec:    cfg.TimeoutSec,
		OutputCapByte: cfg.OutputCapBytes,
		Policy:        input.VerificationConfig.ExecutionPolicy,
	})
	if err != nil {
		return VerificationResult{
			Name:       gitDiffVerifierName,
			Status:     VerificationFail,
			Summary:    err.Error(),
			Reason:     "git diff command execution failed",
			ErrorClass: classifyCommandExecutionError(err),
			Evidence: map[string]any{
				"command": command,
			},
		}, nil
	}
	if result.ExitCode != 0 {
		return VerificationResult{
			Name:       gitDiffVerifierName,
			Status:     VerificationFail,
			Summary:    "git diff command returned non-zero",
			Reason:     "git diff command failed",
			ErrorClass: ErrorClassUnknown,
			Evidence:   commandEvidence(command, result),
		}, nil
	}

	lines := nonEmptyLines(result.Stdout)
	if len(lines) == 0 {
		return VerificationResult{
			Name:     gitDiffVerifierName,
			Status:   VerificationFail,
			Summary:  "git diff is empty",
			Reason:   "no changed files detected",
			Evidence: commandEvidence(command, result),
		}, nil
	}
	evidence := commandEvidence(command, result)
	evidence["changed_files"] = lines
	evidence["changed_files_count"] = len(lines)
	return VerificationResult{
		Name:     gitDiffVerifierName,
		Status:   VerificationPass,
		Summary:  "git diff contains changed files",
		Reason:   "workspace change detected",
		Evidence: evidence,
	}, nil
}

// nonEmptyLines 返回文本中的非空行列表。
func nonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, item)
	}
	return lines
}
