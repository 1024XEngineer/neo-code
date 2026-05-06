package runtime

import (
	"context"
	"errors"
	"strings"

	agentcontext "neo-code/internal/context"
	"neo-code/internal/repository"
	providertypes "neo-code/internal/provider/types"
)

// buildRepositoryContext 返回最小 Git 摘要（迁移期保留），不再自动注入 changed-files 或 retrieval。
// 模型应通过 git_* / codebase_* 工具主动探索仓库。
func (s *Service) buildRepositoryContext(
	ctx context.Context,
	state *runState,
	activeWorkdir string,
) (*agentcontext.RepositorySummarySection, agentcontext.RepositoryContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, agentcontext.RepositoryContext{}, err
	}
	if strings.TrimSpace(activeWorkdir) == "" || state == nil {
		return nil, agentcontext.RepositoryContext{}, nil
	}

	repoService := s.repositoryFacts()
	inspectResult, inspectErr := repoService.Inspect(ctx, activeWorkdir, repository.InspectOptions{})
	if inspectErr != nil {
		if isRepositoryContextFatalError(inspectErr) {
			return nil, agentcontext.RepositoryContext{}, inspectErr
		}
		s.emitRepositoryContextUnavailable(ctx, state, "summary", "", inspectErr)
		return nil, agentcontext.RepositoryContext{}, nil
	}

	summarySection := projectRepositorySummary(inspectResult.Summary)
	return summarySection, agentcontext.RepositoryContext{}, nil
}

// repositoryFacts 返回 runtime 当前使用的 repository 事实服务，并在缺省时回落到默认实现。
func (s *Service) repositoryFacts() repositoryFactService {
	if s != nil && s.repositoryService != nil {
		return s.repositoryService
	}
	return repository.NewService()
}

func projectRepositorySummary(summary repository.Summary) *agentcontext.RepositorySummarySection {
	if !summary.InGitRepo {
		return nil
	}
	return &agentcontext.RepositorySummarySection{
		InGitRepo: true,
		Branch:    summary.Branch,
		Dirty:     summary.Dirty,
		Ahead:     summary.Ahead,
		Behind:    summary.Behind,
	}
}

// emitRepositoryContextUnavailable 记录 repository 事实获取失败但已降级为空上下文的可观测事件。
func (s *Service) emitRepositoryContextUnavailable(
	ctx context.Context,
	state *runState,
	stage string,
	mode string,
	err error,
) {
	if s == nil || s.events == nil || err == nil {
		return
	}
	s.emitRunScoped(ctx, EventRepositoryContextUnavailable, state, RepositoryContextUnavailablePayload{
		Stage:  strings.TrimSpace(stage),
		Mode:   strings.TrimSpace(mode),
		Reason: strings.TrimSpace(err.Error()),
	})
}

// latestUserText 提取最近一条用户消息中的纯文本内容，用于轻量触发判断。
func latestUserText(messages []providertypes.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != providertypes.RoleUser {
			continue
		}
		text := extractTextParts(message.Parts)
		if text != "" {
			return text
		}
	}
	return ""
}

// extractTextParts 聚合消息中的文本 part，忽略图片等非文本载荷。
func extractTextParts(parts []providertypes.ContentPart) string {
	fragments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartText {
			continue
		}
		if trimmed := strings.TrimSpace(part.Text); trimmed != "" {
			fragments = append(fragments, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(fragments, "\n"))
}

// isRepositoryContextFatalError 只把上下文取消类错误视作主链应立即返回的致命错误。
func isRepositoryContextFatalError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
