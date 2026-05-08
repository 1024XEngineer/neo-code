package acceptance

import (
	"fmt"
	"strings"

	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

// AcceptancePolicy 定义 final 验收时 verifier 选择策略。
type AcceptancePolicy interface {
	ResolveVerifiers(input verify.FinalVerifyInput) ([]verify.FinalVerifier, error)
}

// DefaultPolicy 按 session-owned verification profile 解析 verifier 列表。
type DefaultPolicy struct {
	Executor verify.CommandExecutor
}

// ResolveVerifiers 依据 verification profile 生成固定 verifier 执行列表。
func (p DefaultPolicy) ResolveVerifiers(input verify.FinalVerifyInput) ([]verify.FinalVerifier, error) {
	profile := agentsession.VerificationProfile(strings.TrimSpace(input.TaskState.VerificationProfile))
	if !profile.Valid() {
		return nil, fmt.Errorf("invalid verification profile %q", input.TaskState.VerificationProfile)
	}
	names := mappedVerifierNames(profile)
	if len(names) == 0 {
		return nil, fmt.Errorf("verification profile %q has no verifier mapping", profile)
	}
	verifiers := make([]verify.FinalVerifier, 0, len(names))
	for _, name := range names {
		if verifier := p.buildVerifier(name); verifier != nil {
			verifiers = append(verifiers, verifier)
		}
	}
	return verifiers, nil
}

// buildVerifier 基于名称构建 verifier 实例。
func (p DefaultPolicy) buildVerifier(name string) verify.FinalVerifier {
	switch strings.TrimSpace(name) {
	case "todo_convergence":
		return verify.TodoConvergenceVerifier{}
	case "file_exists":
		return verify.FileExistsVerifier{}
	case "content_match":
		return verify.ContentMatchVerifier{}
	case "command_success":
		return verify.CommandSuccessVerifier{VerifierName: "command_success", Executor: p.Executor}
	case "build":
		return verify.NewBuildVerifier(p.Executor)
	case "test":
		return verify.NewTestVerifier(p.Executor)
	case "lint":
		return verify.NewLintVerifier(p.Executor)
	case "typecheck":
		return verify.NewTypecheckVerifier(p.Executor)
	default:
		return nil
	}
}

// mappedVerifierNames 返回 verification profile 对应的 verifier 名称集合。
func mappedVerifierNames(profile agentsession.VerificationProfile) []string {
	switch profile {
	case agentsession.VerificationProfileTaskOnly:
		return []string{"todo_convergence"}
	case agentsession.VerificationProfileCreateFile, agentsession.VerificationProfileDocs:
		return []string{"todo_convergence", "file_exists", "content_match"}
	case agentsession.VerificationProfileConfig:
		return []string{"todo_convergence", "file_exists", "content_match", "command_success"}
	case agentsession.VerificationProfileEditCode:
		return []string{"todo_convergence", "build", "test", "typecheck"}
	case agentsession.VerificationProfileFixBug:
		return []string{"todo_convergence", "test", "build", "typecheck"}
	case agentsession.VerificationProfileRefactor:
		return []string{"todo_convergence", "build", "test", "lint", "typecheck"}
	default:
		return nil
	}
}
