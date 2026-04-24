package verify

const (
	testVerifierName = "test"
)

// TestVerifier 复用命令执行 verifier，承载测试验证语义。
type TestVerifier struct {
	CommandSuccessVerifier
}

// NewTestVerifier 创建 test verifier。
func NewTestVerifier(executor CommandExecutor) TestVerifier {
	return TestVerifier{
		CommandSuccessVerifier: CommandSuccessVerifier{
			VerifierName: testVerifierName,
			Executor:     executor,
		},
	}
}
