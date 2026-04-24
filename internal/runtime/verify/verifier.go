package verify

import "context"

// FinalVerifier 定义 final 验收阶段 verifier 的统一契约。
type FinalVerifier interface {
	Name() string
	VerifyFinal(ctx context.Context, input FinalVerifyInput) (VerificationResult, error)
}
