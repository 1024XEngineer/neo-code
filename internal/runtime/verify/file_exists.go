package verify

import (
	"context"
	"os"
)

const fileExistsVerifierName = "file_exists"

// FileExistsVerifier 校验结构化声明的交付文件是否存在且可用。
type FileExistsVerifier struct{}

// Name 返回 verifier 名称。
func (FileExistsVerifier) Name() string {
	return fileExistsVerifierName
}

// VerifyFinal 校验 required todo artifacts 与 task_state.key_artifacts。
func (FileExistsVerifier) VerifyFinal(_ context.Context, input FinalVerifyInput) (VerificationResult, error) {
	paths := collectArtifactTargets(input)
	if len(paths) == 0 {
		return VerificationResult{
			Name:    fileExistsVerifierName,
			Status:  VerificationFail,
			Summary: "no artifact targets declared",
			Reason:  "file existence targets are missing",
		}, nil
	}

	missing := make([]string, 0)
	empty := make([]string, 0)
	dirs := make([]string, 0)
	for _, path := range paths {
		absPath, err := resolvePathWithinWorkdir(input.Workdir, path)
		if err != nil {
			return verificationDeniedResult(
				fileExistsVerifierName,
				"artifact path is outside workdir",
				"artifact path denied by workdir policy",
				map[string]any{"artifact": path},
			), nil
		}
		info, err := os.Stat(absPath)
		if err != nil {
			missing = append(missing, path)
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, path)
			continue
		}
		if info.Size() <= 0 {
			empty = append(empty, path)
		}
	}

	evidence := map[string]any{
		"artifact_targets": paths,
		"missing_files":    missing,
		"empty_files":      empty,
		"directory_paths":  dirs,
	}
	if len(missing) == 0 && len(empty) == 0 && len(dirs) == 0 {
		return VerificationResult{
			Name:     fileExistsVerifierName,
			Status:   VerificationPass,
			Summary:  "all artifact targets exist and are non-empty",
			Reason:   "file existence check passed",
			Evidence: evidence,
		}, nil
	}
	return VerificationResult{
		Name:     fileExistsVerifierName,
		Status:   VerificationFail,
		Summary:  "artifact targets are missing or invalid",
		Reason:   "file existence check did not pass",
		Evidence: evidence,
	}, nil
}
