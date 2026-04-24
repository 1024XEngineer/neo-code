package verify

const (
	lintVerifierName = "lint"
)

// LintVerifier 复用命令执行 verifier，承载 lint 验证语义。
type LintVerifier struct {
	CommandSuccessVerifier
}

// NewLintVerifier 创建 lint verifier。
func NewLintVerifier(executor CommandExecutor) LintVerifier {
	return LintVerifier{
		CommandSuccessVerifier: CommandSuccessVerifier{
			VerifierName: lintVerifierName,
			Executor:     executor,
		},
	}
}
