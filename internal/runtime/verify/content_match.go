package verify

import (
	"context"
	"os"
	"strings"
)

const (
	contentMatchVerifierName = "content_match"
)

// ContentMatchVerifier 校验预期文件内容是否命中要求。
type ContentMatchVerifier struct{}

// Name 返回 verifier 名称。
func (ContentMatchVerifier) Name() string {
	return contentMatchVerifierName
}

// VerifyFinal 校验 metadata.content_match 声明的路径与关键内容约束。
func (ContentMatchVerifier) VerifyFinal(_ context.Context, input FinalVerifyInput) (VerificationResult, error) {
	cfg, exists := input.VerificationConfig.Verifiers[contentMatchVerifierName]
	required := exists && cfg.Required
	rules := metadataStringMapSlice(input.Metadata, "content_match")
	if len(rules) == 0 {
		return verifierMetadataResult(
			contentMatchVerifierName,
			required,
			"content_match",
			"no content_match rule configured, skip content check",
		), nil
	}

	missingFiles := make([]string, 0)
	deniedPaths := make([]string, 0)
	missingTokens := make(map[string][]string)
	for rawPath, expectedTokens := range rules {
		absPath, err := resolvePathWithinWorkdir(input.Workdir, rawPath)
		if err != nil {
			deniedPaths = append(deniedPaths, rawPath)
			continue
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
		"denied_paths":   deniedPaths,
	}
	if len(deniedPaths) > 0 {
		return verificationDeniedResult(
			contentMatchVerifierName,
			"content rules contain paths outside workdir",
			"content path denied by workdir policy",
			evidence,
		), nil
	}
	if len(missingFiles) == 0 && len(missingTokens) == 0 {
		return VerificationResult{
			Name:     contentMatchVerifierName,
			Status:   VerificationPass,
			Summary:  "all expected content rules matched",
			Reason:   "content match check passed",
			Evidence: evidence,
		}, nil
	}
	return VerificationResult{
		Name:       contentMatchVerifierName,
		Status:     VerificationFail,
		Summary:    "content rule mismatch detected",
		Reason:     "content match check failed",
		ErrorClass: ErrorClassUnknown,
		Evidence:   evidence,
	}, nil
}

// collectMissingTokens 收敛文件缺失关键字，保持 token 去空白后的匹配语义不变。
func collectMissingTokens(missingTokens map[string][]string, path string, content string, expectedTokens []string) {
	for _, token := range compactStrings(expectedTokens) {
		if !strings.Contains(content, token) {
			missingTokens[path] = append(missingTokens[path], token)
		}
	}
}
