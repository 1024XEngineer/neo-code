package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go-llm-demo/internal/server/domain"
)

var fileRefPattern = regexp.MustCompile(`(?i)(?:[a-z]:\\|\./|\.\\|/)?[a-z0-9_./\\-]+\.[a-z0-9]+`)

type workingMemoryServiceImpl struct {
	repo             domain.WorkingMemoryRepository
	maxRecentTurns   int
	maxOpenQuestions int
	maxRecentFiles   int
}

// NewWorkingMemoryService 创建第一阶段的工作记忆服务。
// 目标是把“最近几轮 + 当前任务 + 文件线索”整理成稳定的短期上下文。
func NewWorkingMemoryService(repo domain.WorkingMemoryRepository, maxRecentTurns int) domain.WorkingMemoryService {
	if maxRecentTurns <= 0 {
		maxRecentTurns = 6
	}
	return &workingMemoryServiceImpl{
		repo:             repo,
		maxRecentTurns:   maxRecentTurns,
		maxOpenQuestions: 3,
		maxRecentFiles:   6,
	}
}

// BuildContext 刷新工作记忆并格式化为提示上下文。
func (s *workingMemoryServiceImpl) BuildContext(ctx context.Context, messages []domain.Message) (string, error) {
	if err := s.Refresh(ctx, messages); err != nil {
		return "", err
	}
	state, err := s.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	return formatWorkingMemoryContext(state), nil
}

// Refresh 根据当前消息重建工作记忆快照。
func (s *workingMemoryServiceImpl) Refresh(ctx context.Context, messages []domain.Message) error {
	state := s.buildState(messages)
	return s.repo.Save(ctx, state)
}

// Clear 清空当前工作记忆快照。
func (s *workingMemoryServiceImpl) Clear(ctx context.Context) error {
	return s.repo.Clear(ctx)
}

func (s *workingMemoryServiceImpl) buildState(messages []domain.Message) *domain.WorkingMemoryState {
	turns := collectRecentTurns(messages)
	if len(turns) > s.maxRecentTurns {
		turns = turns[len(turns)-s.maxRecentTurns:]
	}

	state := &domain.WorkingMemoryState{
		RecentTurns: turns,
	}
	state.CurrentTask = latestUserMessage(messages)
	state.TaskSummary = buildTaskSummary(turns)
	state.OpenQuestions = collectOpenQuestions(messages, s.maxOpenQuestions)
	state.RecentFiles = collectRecentFiles(messages, s.maxRecentFiles)
	return state
}

func collectRecentTurns(messages []domain.Message) []domain.WorkingMemoryTurn {
	turns := make([]domain.WorkingMemoryTurn, 0)
	var pendingUser string
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			pendingUser = strings.TrimSpace(msg.Content)
		case "assistant":
			assistant := strings.TrimSpace(msg.Content)
			if pendingUser == "" && assistant == "" {
				continue
			}
			turns = append(turns, domain.WorkingMemoryTurn{
				User:      pendingUser,
				Assistant: assistant,
			})
			pendingUser = ""
		}
	}
	if pendingUser != "" {
		turns = append(turns, domain.WorkingMemoryTurn{User: pendingUser})
	}
	return turns
}

func latestUserMessage(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func buildTaskSummary(turns []domain.WorkingMemoryTurn) string {
	if len(turns) == 0 {
		return ""
	}
	latest := turns[len(turns)-1]
	if latest.User != "" {
		return domain.SummarizeText(latest.User, 160)
	}
	if latest.Assistant != "" {
		return domain.SummarizeText(latest.Assistant, 160)
	}
	return ""
}

func collectOpenQuestions(messages []domain.Message, limit int) []string {
	questions := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for i := len(messages) - 1; i >= 0 && len(questions) < limit; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || !looksLikeOpenQuestion(content) {
			continue
		}
		key := strings.ToLower(content)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		questions = append(questions, domain.SummarizeText(content, 120))
	}
	return reverseStrings(questions)
}

func collectRecentFiles(messages []domain.Message, limit int) []string {
	files := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for i := len(messages) - 1; i >= 0 && len(files) < limit; i-- {
		matches := fileRefPattern.FindAllString(messages[i].Content, -1)
		for _, match := range matches {
			normalized := strings.TrimSpace(strings.ReplaceAll(match, "\\", "/"))
			if normalized == "" {
				continue
			}
			lowered := strings.ToLower(normalized)
			if _, ok := seen[lowered]; ok {
				continue
			}
			seen[lowered] = struct{}{}
			files = append(files, normalized)
			if len(files) >= limit {
				break
			}
		}
	}
	return reverseStrings(files)
}

func looksLikeOpenQuestion(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "?？") {
		return true
	}
	return containsAnyFold(trimmed, "怎么", "如何", "为什么", "是否", "在哪", "what", "how", "why", "where", "which")
}

func formatWorkingMemoryContext(state *domain.WorkingMemoryState) string {
	if state == nil {
		return ""
	}
	parts := make([]string, 0, 5)
	if state.CurrentTask != "" {
		parts = append(parts, "Current task: "+domain.SummarizeText(state.CurrentTask, 180))
	}
	if state.TaskSummary != "" {
		parts = append(parts, "Task summary: "+state.TaskSummary)
	}
	if len(state.OpenQuestions) > 0 {
		parts = append(parts, "Open questions: "+strings.Join(state.OpenQuestions, " | "))
	}
	if len(state.RecentFiles) > 0 {
		parts = append(parts, "Recent file refs: "+strings.Join(state.RecentFiles, ", "))
	}
	if len(state.RecentTurns) > 0 {
		turnLines := make([]string, 0, len(state.RecentTurns))
		for idx, turn := range state.RecentTurns {
			user := domain.SummarizeText(turn.User, 100)
			assistant := domain.SummarizeText(turn.Assistant, 100)
			turnLines = append(turnLines, fmt.Sprintf("Turn %d user=%q assistant=%q", idx+1, user, assistant))
		}
		parts = append(parts, "Recent turns:\n"+strings.Join(turnLines, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Use the following working memory to stay consistent with the active task and recent context. Prefer it over reconstructing context from scratch.\n" + strings.Join(parts, "\n")
}

func reverseStrings(values []string) []string {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
	return values
}
