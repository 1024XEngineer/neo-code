package runtime

import (
	"context"

	providertypes "neo-code/internal/provider/types"
)

// triggerMemoExtraction 在 Run 结束后异步触发记忆提取，避免阻塞主闭环。
func (s *Service) triggerMemoExtraction(messages []providertypes.Message) {
	if s == nil || s.memoExtractor == nil || len(messages) == 0 {
		return
	}

	cloned := append([]providertypes.Message(nil), messages...)
	go s.memoExtractor.ExtractAndStore(context.Background(), cloned)
}
