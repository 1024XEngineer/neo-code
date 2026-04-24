package verify

const (
	typecheckVerifierName = "typecheck"
)

// TypecheckVerifier 复用命令执行 verifier，承载类型检查验证语义。
type TypecheckVerifier struct {
	CommandSuccessVerifier
}

// NewTypecheckVerifier 创建 typecheck verifier。
func NewTypecheckVerifier(executor CommandExecutor) TypecheckVerifier {
	return TypecheckVerifier{
		CommandSuccessVerifier: CommandSuccessVerifier{
			VerifierName: typecheckVerifierName,
			Executor:     executor,
		},
	}
}
