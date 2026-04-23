package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/repository"
)

const (
	maxAutoChangedFilesCount         = 20
	maxAutoSnippetChangedFilesCount  = 5
	defaultAutoChangedFilesLimit     = 10
	defaultAutoChangedFilesWithDiff  = 5
	defaultAutoPathRetrievalLimit    = 1
	defaultAutoSymbolRetrievalLimit  = 3
	defaultAutoTextRetrievalLimit    = 5
	defaultAutoRetrievalContextLines = 4
	defaultAutoTextRetrievalContext  = 3
)

var (
	pathAnchorPattern   = regexp.MustCompile(`(?i)([a-z0-9_.-]+[\\/])+[a-z0-9_.-]+\.(go|md|ya?ml|json|toml|txt|sh)`)
	symbolAnchorPattern = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]{2,}\b`)
	quotedTextPattern   = regexp.MustCompile("`([^`]+)`|\"([^\"]+)\"|'([^']+)'")
)

// buildRepositoryContext 按当前轮输入意图条件化构建 repository 上下文，避免默认膨胀 prompt。
func (s *Service) buildRepositoryContext(ctx context.Context, state *runState, activeWorkdir string) (agentcontext.RepositoryContext, error) {
	if err := ctx.Err(); err != nil {
		return agentcontext.RepositoryContext{}, err
	}
	if strings.TrimSpace(activeWorkdir) == "" || state == nil {
		return agentcontext.RepositoryContext{}, nil
	}

	latestUserText := latestUserText(state.session.Messages)
	if latestUserText == "" {
		return agentcontext.RepositoryContext{}, nil
	}

	repoService := s.repositoryFacts()
	repoContext := agentcontext.RepositoryContext{}

	changedFiles, err := s.maybeBuildChangedFilesContext(ctx, repoService, activeWorkdir, latestUserText)
	if err != nil {
		if isRepositoryContextFatalError(err) {
			return agentcontext.RepositoryContext{}, err
		}
		s.emitRepositoryContextUnavailable(ctx, state, "changed_files", "", err)
	} else {
		repoContext.ChangedFiles = changedFiles
	}

	if query, ok := autoRetrievalQueryFromUserText(latestUserText); ok {
		retrieval, retrievalErr := s.buildRetrievalContextForQuery(ctx, repoService, activeWorkdir, query)
		if retrievalErr != nil {
			if isRepositoryContextFatalError(retrievalErr) {
				return agentcontext.RepositoryContext{}, retrievalErr
			}
			s.emitRepositoryContextUnavailable(ctx, state, "retrieval", string(query.Mode), retrievalErr)
		} else {
			repoContext.Retrieval = retrieval
		}
	}

	return repoContext, nil
}

// repositoryFacts 返回 runtime 当前使用的 repository 事实服务，并在缺省时回落到默认实现。
func (s *Service) repositoryFacts() repositoryFactService {
	if s != nil && s.repositoryService != nil {
		return s.repositoryService
	}
	return repository.NewService()
}

// maybeBuildChangedFilesContext 仅在当前问题明显围绕改动集时提取 changed-files 上下文。
func (s *Service) maybeBuildChangedFilesContext(
	ctx context.Context,
	repoService repositoryFactService,
	workdir string,
	userText string,
) (*agentcontext.RepositoryChangedFilesSection, error) {
	explicitChangedFilesIntent := shouldAutoInjectChangedFiles(userText)
	includeSnippets := shouldAutoIncludeChangedFileSnippets(userText)
	if !explicitChangedFilesIntent && !mentionsFixOrReviewIntent(userText) {
		return nil, nil
	}

	limit := defaultAutoChangedFilesLimit
	if includeSnippets {
		limit = defaultAutoChangedFilesWithDiff
	}
	changed, err := repoService.ChangedFiles(ctx, workdir, repository.ChangedFilesOptions{
		Limit:                 limit,
		IncludeSnippets:       includeSnippets,
		SnippetFileCountLimit: maxAutoSnippetChangedFilesCount,
	})
	if err != nil {
		return nil, err
	}
	if len(changed.Files) == 0 {
		return nil, nil
	}
	if !explicitChangedFilesIntent && (changed.TotalCount <= 0 || changed.TotalCount > maxAutoChangedFilesCount) {
		return nil, nil
	}
	return &agentcontext.RepositoryChangedFilesSection{
		Files:         append([]repository.ChangedFile(nil), changed.Files...),
		Truncated:     changed.Truncated,
		ReturnedCount: changed.ReturnedCount,
		TotalCount:    changed.TotalCount,
	}, nil
}

// buildRetrievalContextForQuery 基于已解析出的显式锚点执行单次定向检索并投影为 context 结构。
func (s *Service) buildRetrievalContextForQuery(
	ctx context.Context,
	repoService repositoryFactService,
	workdir string,
	query repository.RetrievalQuery,
) (*agentcontext.RepositoryRetrievalSection, error) {
	hits, err := repoService.Retrieve(ctx, workdir, query)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	return &agentcontext.RepositoryRetrievalSection{
		Hits:      append([]repository.RetrievalHit(nil), hits...),
		Truncated: false,
		Mode:      string(query.Mode),
		Query:     query.Value,
	}, nil
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
	_ = s.emitRunScoped(ctx, EventRepositoryContextUnavailable, state, RepositoryContextUnavailablePayload{
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

// shouldAutoInjectChangedFiles 判断本轮是否应优先注入 changed-files 摘要。
func shouldAutoInjectChangedFiles(userText string) bool {
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return false
	}
	keywords := []string{
		"当前改动",
		"这次修改",
		"changed files",
		"current diff",
		"git diff",
		"review 我的改动",
		"review my changes",
		"我的改动",
		"本次改动",
		"未提交",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// shouldAutoIncludeChangedFileSnippets 仅在小变更集的 review/fix 语义下升级为 snippet 注入。
func shouldAutoIncludeChangedFileSnippets(userText string) bool {
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return false
	}
	keywords := []string{
		"review",
		"diff",
		"patch",
		"解释改动",
		"explain changes",
		"fix",
		"修复",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// mentionsFixOrReviewIntent 判断问题是否属于更依赖当前工作树状态的 fix/review 类型任务。
func mentionsFixOrReviewIntent(userText string) bool {
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return false
	}
	keywords := []string{
		"fix",
		"debug",
		"review",
		"修复",
		"排查",
		"debugging",
		"bug",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// autoRetrievalQueryFromUserText 基于显式锚点抽取本轮至多一组自动 retrieval 请求。
func autoRetrievalQueryFromUserText(userText string) (repository.RetrievalQuery, bool) {
	if pathQuery, ok := autoPathRetrievalQuery(userText); ok {
		return pathQuery, true
	}
	if symbolQuery, ok := autoSymbolRetrievalQuery(userText); ok {
		return symbolQuery, true
	}
	if textQuery, ok := autoTextRetrievalQuery(userText); ok {
		return textQuery, true
	}
	return repository.RetrievalQuery{}, false
}

// autoPathRetrievalQuery 从文本中提取最明确的路径锚点，并映射为 path 模式检索。
func autoPathRetrievalQuery(userText string) (repository.RetrievalQuery, bool) {
	match := pathAnchorPattern.FindString(strings.TrimSpace(userText))
	if strings.TrimSpace(match) == "" {
		return repository.RetrievalQuery{}, false
	}
	normalized := filepath.ToSlash(strings.Trim(match, "`\"'"))
	return repository.RetrievalQuery{
		Mode:         repository.RetrievalModePath,
		Value:        normalized,
		Limit:        defaultAutoPathRetrievalLimit,
		ContextLines: defaultAutoRetrievalContextLines,
	}, true
}

// autoSymbolRetrievalQuery 仅在句式明显指向符号定义/实现时抽取 Go-first 符号检索。
func autoSymbolRetrievalQuery(userText string) (repository.RetrievalQuery, bool) {
	lower := strings.ToLower(userText)
	if !(strings.Contains(lower, "定义") ||
		strings.Contains(lower, "实现") ||
		strings.Contains(lower, "在哪") ||
		strings.Contains(lower, "where is") ||
		strings.Contains(lower, "explain") ||
		strings.Contains(lower, "look at")) {
		return repository.RetrievalQuery{}, false
	}

	symbol := symbolAnchorPattern.FindString(userText)
	if strings.TrimSpace(symbol) == "" {
		return repository.RetrievalQuery{}, false
	}
	return repository.RetrievalQuery{
		Mode:         repository.RetrievalModeSymbol,
		Value:        symbol,
		Limit:        defaultAutoSymbolRetrievalLimit,
		ContextLines: defaultAutoRetrievalContextLines,
	}, true
}

// autoTextRetrievalQuery 只对显式包裹的关键字做一次有限文本检索，避免宽泛问题误触发。
func autoTextRetrievalQuery(userText string) (repository.RetrievalQuery, bool) {
	matches := quotedTextPattern.FindAllStringSubmatch(userText, -1)
	for _, match := range matches {
		candidate := ""
		for _, group := range match[1:] {
			if strings.TrimSpace(group) != "" {
				candidate = strings.TrimSpace(group)
				break
			}
		}
		if candidate == "" || strings.Contains(candidate, "/") || strings.Contains(candidate, "\\") {
			continue
		}
		return repository.RetrievalQuery{
			Mode:         repository.RetrievalModeText,
			Value:        candidate,
			Limit:        defaultAutoTextRetrievalLimit,
			ContextLines: defaultAutoTextRetrievalContext,
		}, true
	}
	return repository.RetrievalQuery{}, false
}

// isRepositoryContextFatalError 只把上下文取消类错误视作主链应立即返回的致命错误。
func isRepositoryContextFatalError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
