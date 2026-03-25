package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"go-llm-demo/internal/server/domain"
)

type ruleBasedMemoryExtractor struct{}

var (
	durablePreferenceCues = []string{
		"默认", "始终", "以后", "后续", "统一", "记住", "长期",
		"always", "from now on", "by default", "going forward", "remember",
	}
	durablePreferenceTargets = []string{
		"中文", "英文", "命令", "说明", "提交", "commit", "command", "style", "配置", "config",
	}
	projectRuleAnchors = []string{
		"config.yaml", "readme", "go test", "go build",
		"cmd/", "internal/", "configs/", "services/", "memory/", "main.go",
		"data/", "workspace", "工作区", "根目录", "主配置文件", "文件", "路径",
	}
	projectRuleSignals = []string{
		"约定", "规则", "配置", "结构", "目录", "命令",
		"默认", "统一", "必须", "需要", "测试命令", "构建命令",
		"convention", "rule", "config", "structure", "directory", "command", "default", "must", "should",
	}
	codeAnchors = []string{
		".go", "config.yaml", "main.go", "services/", "memory/", "json", "yaml",
		"function", "func", "struct", "interface", "method", "package", "import",
		"函数", "文件", "模块", "包", "结构体", "接口", "方法", "字段", "参数", "路径", "目录",
	}
	codeQuestionCues = []string{
		"什么", "干啥", "作用", "怎么", "如何", "为什么", "在哪", "含义", "区别",
		"what", "why", "where", "which",
	}
	codeExplanationCues = []string{
		"用于", "负责", "位于", "表示", "通过", "调用", "读取", "写入", "返回", "实现", "处理", "对应", "配置", "路径", "字段", "参数",
		"used for", "responsible for", "located in", "reads", "writes", "returns", "defines", "implements",
	}
	codingSignals = []string{
		"function", "file", "repo", "project", "build", "test", "config", "bug", "error", "fix",
		"golang", "go ", "yaml", "json", "memory", "prompt", "cli", "agent",
		"编码", "项目", "配置", "测试", "构建", "报错", "修复", "代码",
	}
	problemCues     = []string{"error", "failed", "panic", "bug", "报错", "失败", "异常"}
	fixCues         = []string{"修复", "已通过", "解决", "fixed", "use", "改为", "增加", "remove", "replace"}
	taskRequestCues = []string{"帮我", "请你", "写一个", "实现一个"}
	factAnswerCues  = []string{"在", "位于", "负责", "调用", "使用", "路径", "文件", "函数", "模块", "返回", "读取", "写入", "responsible", "located", "returns", "reads", "writes"}
)

// NewRuleBasedMemoryExtractor returns the default deterministic extractor.
func NewRuleBasedMemoryExtractor() domain.MemoryExtractor {
	return &ruleBasedMemoryExtractor{}
}

// Extract converts a conversation turn into structured memory items.
func (e *ruleBasedMemoryExtractor) Extract(_ context.Context, userInput, assistantReply string) ([]domain.MemoryItem, error) {
	if shouldSkipConversationMemory(userInput, assistantReply) {
		return nil, nil
	}

	now := time.Now().UTC()
	items := make([]domain.MemoryItem, 0, 5)

	if item, ok := e.extractUserPreference(userInput, assistantReply, now); ok {
		items = append(items, item)
	}
	if item, ok := e.extractProjectRule(userInput, assistantReply, now); ok {
		items = append(items, item)
	}
	if item, ok := e.extractCodeFact(userInput, assistantReply, now); ok {
		items = append(items, item)
	}
	if item, ok := e.extractFixRecipe(userInput, assistantReply, now); ok {
		items = append(items, item)
	}
	if item, ok := e.extractSessionMemory(userInput, assistantReply, now); ok {
		items = append(items, item)
	}

	return dedupeMemoryItems(items), nil
}

func (e *ruleBasedMemoryExtractor) extractSessionMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := conversationText(userInput, assistantReply)
	if !isCodingRelevant(userInput, assistantReply) || hasDurablePreferenceIntent(userInput) || looksLikeProjectRuleEvidence(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(userInput, 140)
	if summary == "" {
		summary = domain.SummarizeText(assistantReply, 140)
	}

	return newConversationMemoryItem(now, domain.TypeSessionMemory, domain.ScopeSession, summary, assistantReply, userInput, assistantReply, combined, 0.66), true
}

func (e *ruleBasedMemoryExtractor) extractUserPreference(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	trimmed := strings.TrimSpace(userInput)
	if trimmed == "" || !hasDurablePreferenceIntent(trimmed) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(trimmed, 140)
	return newConversationMemoryItem(now, domain.TypeUserPreference, domain.ScopeUser, summary, assistantReply, userInput, assistantReply, conversationText(userInput, assistantReply), 0.95), true
}

func (e *ruleBasedMemoryExtractor) extractProjectRule(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	if hasDurablePreferenceIntent(userInput) {
		return domain.MemoryItem{}, false
	}
	if !looksLikeProjectRuleEvidence(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(firstNonEmptyLine(userInput, assistantReply), 140)
	return newConversationMemoryItem(now, domain.TypeProjectRule, domain.ScopeProject, summary, assistantReply, userInput, assistantReply, conversationText(userInput, assistantReply), 0.9), true
}

func (e *ruleBasedMemoryExtractor) extractCodeFact(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := conversationText(userInput, assistantReply)
	if !looksLikeCodeFactEvidence(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}
	if containsAnyFold(strings.ToLower(userInput), taskRequestCues...) && !containsAnyFold(combined, factAnswerCues...) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(firstNonEmptyLine(assistantReply, userInput), 140)
	return newConversationMemoryItem(now, domain.TypeCodeFact, domain.ScopeProject, summary, assistantReply, userInput, assistantReply, combined, 0.82), true
}

func (e *ruleBasedMemoryExtractor) extractFixRecipe(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := strings.ToLower(conversationText(userInput, assistantReply))
	if !containsAnyFold(combined, problemCues...) || !containsAnyFold(combined, fixCues...) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(firstNonEmptyLine(userInput, assistantReply), 140)
	details := assistantReply
	if details == "" {
		details = userInput
	}
	return newConversationMemoryItem(now, domain.TypeFixRecipe, domain.ScopeProject, summary, details, userInput, assistantReply, conversationText(userInput, assistantReply), 0.8), true
}

func newConversationMemoryItem(now time.Time, itemType, scope, summary, details, userInput, assistantReply, text string, confidence float64) domain.MemoryItem {
	item := domain.MemoryItem{
		ID:             strconv.FormatInt(now.UnixNano(), 10) + "-" + itemType,
		Type:           itemType,
		Summary:        strings.TrimSpace(summary),
		Details:        domain.SummarizeText(details, 220),
		Scope:          scope,
		Tags:           domain.InferTags(summary + "\n" + details),
		Source:         "conversation",
		Confidence:     confidence,
		Text:           strings.TrimSpace(text),
		CreatedAt:      now,
		UpdatedAt:      now,
		UserInput:      strings.TrimSpace(userInput),
		AssistantReply: strings.TrimSpace(assistantReply),
	}
	return item.Normalized()
}

func conversationText(userInput, assistantReply string) string {
	return strings.TrimSpace(userInput) + "\n" + strings.TrimSpace(assistantReply)
}

func isCodingRelevant(userInput, assistantReply string) bool {
	combined := strings.ToLower(conversationText(userInput, assistantReply))
	if containsAnyFold(combined, codingSignals...) {
		return true
	}
	trimmedUser := strings.TrimSpace(strings.ToLower(userInput))
	return len(trimmedUser) > 20 && containsAnyFold(trimmedUser, "code", "代码")
}

func looksLikeProjectRuleEvidence(userInput, assistantReply string) bool {
	combined := strings.ToLower(conversationText(userInput, assistantReply))
	return containsAnyFold(combined, projectRuleAnchors...) && containsAnyFold(combined, projectRuleSignals...)
}

func looksLikeCodeFactEvidence(userInput, assistantReply string) bool {
	combined := conversationText(userInput, assistantReply)
	if !isCodingRelevant(userInput, assistantReply) {
		return false
	}
	if !containsAnyFold(combined, codeAnchors...) {
		return false
	}

	trimmedUser := strings.ToLower(strings.TrimSpace(userInput))
	trimmedReply := strings.ToLower(strings.TrimSpace(assistantReply))
	hasQuestionIntent := containsAnyFold(trimmedUser, codeQuestionCues...)
	hasExplanation := containsAnyFold(trimmedReply, codeExplanationCues...)
	return hasQuestionIntent || hasExplanation
}

func hasDurablePreferenceIntent(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	return containsAnyFold(trimmed, durablePreferenceCues...) && containsAnyFold(trimmed, durablePreferenceTargets...)
}

func shouldSkipConversationMemory(userInput, assistantReply string) bool {
	trimmedUser := strings.TrimSpace(userInput)
	trimmedReply := strings.TrimSpace(assistantReply)
	if trimmedUser == "" || trimmedReply == "" {
		return true
	}
	return looksLikeToolCallPayload(trimmedReply)
}

func looksLikeToolCallPayload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return false
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}

	toolValue, hasTool := payload["tool"]
	paramsValue, hasParams := payload["params"]
	if !hasTool || !hasParams {
		return false
	}

	var toolName string
	if err := json.Unmarshal(toolValue, &toolName); err != nil {
		return false
	}

	var params map[string]interface{}
	return strings.TrimSpace(toolName) != "" && json.Unmarshal(paramsValue, &params) == nil
}

func containsAnyFold(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(strings.ToLower(text), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(values ...string) string {
	for _, value := range values {
		for _, line := range strings.Split(value, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func dedupeMemoryItems(items []domain.MemoryItem) []domain.MemoryItem {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]domain.MemoryItem{}
	for _, item := range items {
		key := item.Type + "::" + item.Scope + "::" + strings.ToLower(strings.TrimSpace(item.Summary))
		seen[key] = item
	}
	result := make([]domain.MemoryItem, 0, len(seen))
	for _, item := range seen {
		result = append(result, item)
	}
	return result
}
