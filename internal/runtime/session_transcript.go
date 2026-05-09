package runtime

import (
	"context"
	"time"

	agentsession "neo-code/internal/session"
)

// repairSessionTranscriptIfNeeded 检查会话 transcript 是否存在未闭合的 tool_calls 尾部，
// 若存在则截断残缺尾巴并原子回写，避免后续继续对话时向 provider 发送非法消息链。
func (s *Service) repairSessionTranscriptIfNeeded(ctx context.Context, session *agentsession.Session) error {
	if s == nil || session == nil {
		return nil
	}

	repairedMessages, repaired := agentsession.RepairIncompleteToolCallTail(session.Messages)
	if !repaired {
		return nil
	}

	session.Messages = repairedMessages
	session.UpdatedAt = time.Now()
	return s.sessionStore.ReplaceTranscript(ctx, replaceTranscriptInputFromSession(*session))
}
