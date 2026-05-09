package verify

import (
	"context"
	"os"
	"strings"
)

const contentMatchVerifierName = "content_match"

// ContentMatchVerifier 校验结构化声明的文件内容是否命中要求 token。
type ContentMatchVerifier struct{}

// Name 返回 verifier 名称。
func (ContentMatchVerifier) Name() string {
	return contentMatchVerifierName
}

// VerifyFinal 校验 todo.content_checks 声明的路径与 token 约束。
func (ContentMatchVerifier) VerifyFinal(_ context.Context, input FinalVerifyInput) (VerificationResult, error) {
	rules, err := collectContentCheckRules(input)
	if err != nil {
		return VerificationResult{
			Name:       contentMatchVerifierName,
			Status:     VerificationFail,
			Summary:    err.Error(),
			Reason:     "content check rules are invalid",
			ErrorClass: ErrorClassUnknown,
		}, nil
	}
	if len(rules) == 0 {
		return VerificationResult{
			Name:    contentMatchVerifierName,
			Status:  VerificationPass,
			Summary: "no content checks declared",
			Reason:  "content match check skipped without rules",
		}, nil
	}

	missingFiles := make([]string, 0)
	missingTokens := make(map[string][]string)
	for rawPath, expectedTokens := range rules {
		absPath, err := resolvePathWithinWorkdir(input.Workdir, rawPath)
		if err != nil {
			return verificationDeniedResult(
				contentMatchVerifierName,
				"content rule path is outside workdir",
				"content path denied by workdir policy",
				map[string]any{"artifact": rawPath},
			), nil
		}
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			missingFiles = append(missingFiles, rawPath)
			continue
		}
		collectMissingTokens(missingTokens, rawPath, string(contentBytes), expectedTokens)
	}

	evidence := map[string]any{
		"rules":          rules,
		"missing_files":  missingFiles,
		"missing_tokens": missingTokens,
	}
	if len(missingFiles) == 0 && len(missingTokens) == 0 {
		return VerificationResult{
			Name:     contentMatchVerifierName,
			Status:   VerificationPass,
			Summary:  "all content checks matched",
			Reason:   "content match check passed",
			Evidence: evidence,
		}, nil
	}
	return VerificationResult{
		Name:     contentMatchVerifierName,
		Status:   VerificationFail,
		Summary:  "content rule mismatch detected",
		Reason:   "content match check did not pass",
		Evidence: evidence,
	}, nil
}

// collectMissingTokens 收集文件缺失 token，保持“包含全部 token”语义不变。
func collectMissingTokens(missingTokens map[string][]string, path string, content string, expectedTokens []string) {
	for _, token := range compactStrings(expectedTokens) {
		if !strings.Contains(content, token) {
			missingTokens[path] = append(missingTokens[path], token)
		}
	}
}
