package acceptance

import (
	"context"
	"time"
)

// Hook 表示 beforeAcceptFinal 流程中可插拔的内置钩子。
type Hook interface {
	Name() string
	Run(ctx context.Context, input FinalAcceptanceInput) error
}

// runHookWithTimeout 在限定超时下执行 hook，避免验收链路被单点阻塞。
func runHookWithTimeout(ctx context.Context, timeoutSec int, hook Hook, input FinalAcceptanceInput) error {
	if hook == nil {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 1
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	return hook.Run(runCtx, input)
}
