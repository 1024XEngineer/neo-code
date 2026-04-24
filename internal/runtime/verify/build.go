package verify

const (
	buildVerifierName = "build"
)

// BuildVerifier 复用命令执行 verifier，承载 build 验证语义。
type BuildVerifier struct {
	CommandSuccessVerifier
}

// NewBuildVerifier 创建 build verifier。
func NewBuildVerifier(executor CommandExecutor) BuildVerifier {
	return BuildVerifier{
		CommandSuccessVerifier: CommandSuccessVerifier{
			VerifierName: buildVerifierName,
			Executor:     executor,
		},
	}
}
