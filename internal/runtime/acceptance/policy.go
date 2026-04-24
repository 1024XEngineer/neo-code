package acceptance

import (
	"fmt"
	"strings"

	"neo-code/internal/runtime/verify"
)

type taskType string

const (
	taskTypeUnknown    taskType = "unknown"
	taskTypeCreateFile taskType = "create_file"
	taskTypeEditCode   taskType = "edit_code"
	taskTypeFixBug     taskType = "fix_bug"
	taskTypeRefactor   taskType = "refactor"
	taskTypeDocs       taskType = "docs"
	taskTypeConfig     taskType = "config"
)

// AcceptancePolicy 定义 final 验收时 verifier 选择策略。
type AcceptancePolicy interface {
	ResolveVerifiers(input verify.FinalVerifyInput) []verify.FinalVerifier
}

// DefaultPolicy 按 task_type 与 verification config 解析 verifier 列表。
type DefaultPolicy struct {
	Executor verify.CommandExecutor
}

// ResolveVerifiers 依据 task policy 与配置启停生成 verifier 执行列表。
func (p DefaultPolicy) ResolveVerifiers(input verify.FinalVerifyInput) []verify.FinalVerifier {
	task := resolveTaskType(input.Metadata, input.VerificationConfig.DefaultTaskPolicy)
	names := mappedVerifierNames(task)

	verifiers := make([]verify.FinalVerifier, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if !isVerifierEnabled(name, input) {
			continue
		}
		if verifier := p.buildVerifier(name); verifier != nil {
			verifiers = append(verifiers, verifier)
		}
	}
	return verifiers
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
	case "git_diff":
		return verify.GitDiffVerifier{Executor: p.Executor}
	case "build":
		v := verify.NewBuildVerifier(p.Executor)
		return v
	case "test":
		v := verify.NewTestVerifier(p.Executor)
		return v
	case "lint":
		v := verify.NewLintVerifier(p.Executor)
		return v
	case "typecheck":
		v := verify.NewTypecheckVerifier(p.Executor)
		return v
	default:
		return nil
	}
}

// isVerifierEnabled 根据 verification config 判断 verifier 是否生效。
func isVerifierEnabled(name string, input verify.FinalVerifyInput) bool {
	cfg, exists := input.VerificationConfig.Verifiers[strings.TrimSpace(name)]
	if !exists {
		return name == "todo_convergence"
	}
	return cfg.Enabled
}

// resolveTaskType 根据 metadata 与默认策略解析任务类型。
func resolveTaskType(metadata map[string]any, fallback string) taskType {
	raw := strings.ToLower(strings.TrimSpace(fallback))
	if len(metadata) > 0 {
		if value, ok := metadata["task_type"]; ok && value != nil {
			raw = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
		}
	}
	switch taskType(raw) {
	case taskTypeCreateFile, taskTypeEditCode, taskTypeFixBug, taskTypeRefactor, taskTypeDocs, taskTypeConfig:
		return taskType(raw)
	default:
		return taskTypeUnknown
	}
}

// mappedVerifierNames 返回任务类型对应的 verifier 名称集合。
func mappedVerifierNames(task taskType) []string {
	switch task {
	case taskTypeCreateFile, taskTypeDocs:
		return []string{"todo_convergence", "file_exists", "content_match"}
	case taskTypeConfig:
		return []string{"todo_convergence", "file_exists", "content_match", "command_success"}
	case taskTypeEditCode:
		return []string{"todo_convergence", "git_diff", "build", "test", "typecheck"}
	case taskTypeFixBug:
		return []string{"todo_convergence", "git_diff", "test", "build", "typecheck"}
	case taskTypeRefactor:
		return []string{"todo_convergence", "git_diff", "build", "test", "lint", "typecheck"}
	default:
		return []string{"todo_convergence"}
	}
}
