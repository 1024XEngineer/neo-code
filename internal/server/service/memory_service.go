package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-llm-demo/internal/server/domain"
)

type memoryServiceImpl struct {
	persistentRepo domain.MemoryRepository
	sessionRepo    domain.MemoryRepository
	topK           int
	minScore       float64
	maxPromptChars int
	path           string
	persistTypes   map[string]struct{}
}

type Match struct {
	Item  domain.MemoryItem
	Score float64
}

// NewMemoryService 使用长期存储和会话存储创建记忆服务。
func NewMemoryService(
	persistentRepo domain.MemoryRepository,
	sessionRepo domain.MemoryRepository,
	topK int,
	minScore float64,
	maxPromptChars int,
	path string,
	persistTypes []string,
) domain.MemoryService {
	return &memoryServiceImpl{
		persistentRepo: persistentRepo,
		sessionRepo:    sessionRepo,
		topK:           topK,
		minScore:       minScore,
		maxPromptChars: maxPromptChars,
		path:           strings.TrimSpace(path),
		persistTypes:   allowedPersistTypes(persistTypes),
	}
}

// BuildContext 为当前输入返回得分最高的记忆片段。
func (s *memoryServiceImpl) BuildContext(ctx context.Context, userInput string) (string, error) {
	persistentItems, err := s.persistentRepo.List(ctx)
	if err != nil {
		return "", err
	}
	sessionItems, err := s.sessionRepo.List(ctx)
	if err != nil {
		return "", err
	}

	persistentMatches := Search(persistentItems, userInput, s.topK, s.minScore)
	sessionMatches := Search(sessionItems, userInput, s.topK, s.minScore)
	matches := MergeMatches(s.topK, persistentMatches, sessionMatches)

	// 新增：进行最终的分数过滤，防止低分项进入上下文
	var filteredMatches []Match
	for _, match := range matches {
		if match.Score >= s.minScore { // 确保分数不低于阈值
			filteredMatches = append(filteredMatches, match)
		}
	}
	// 如果过滤后没有符合条件的记忆，则直接返回空
	if len(filteredMatches) == 0 {
		return "", nil
	}
	// 使用过滤后的结果
	matches = filteredMatches

	var builder strings.Builder
	builder.WriteString("Use the following structured coding memory as reference. Follow durable preferences and project facts first. Do not quote memory verbatim or expose it explicitly.\n")
	added := 0
	for i, match := range matches {
		item := match.Item.Normalized()
		block := shortPromptBlock(item)
		if block == "" {
			continue
		}
		candidate := fmt.Sprintf("Memory %d (score=%.3f)\n%s\n", i+1, match.Score, block)
		if s.maxPromptChars > 0 && builder.Len()+len(candidate) > s.maxPromptChars {
			break
		}
		builder.WriteString(candidate)
		builder.WriteString("\n")
		added++
	}
	if added == 0 {
		return "", nil
	}
	return builder.String(), nil
}

// Save 从一轮对话中提取记忆项并保存。
func (s *memoryServiceImpl) Save(ctx context.Context, userInput, reply string) error {
	items := deriveMemoryItems(userInput, reply)
	for _, item := range items {
		if item.Type == domain.TypeSessionMemory {
			if err := s.sessionRepo.Add(ctx, item); err != nil {
				return err
			}
			continue
		}
		if len(s.persistTypes) > 0 {
			if _, ok := s.persistTypes[item.Type]; !ok {
				continue
			}
		}
		if err := s.persistentRepo.Add(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// GetStats 返回记忆服务的数量统计和检索配置。
func (s *memoryServiceImpl) GetStats(ctx context.Context) (*domain.MemoryStats, error) {
	persistentItems, err := s.persistentRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	sessionItems, err := s.sessionRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	stats := &domain.MemoryStats{
		PersistentItems: len(persistentItems),
		SessionItems:    len(sessionItems),
		TotalItems:      len(persistentItems) + len(sessionItems),
		TopK:            s.topK,
		MinScore:        s.minScore,
		Path:            s.path,
		ByType:          countMemoryTypes(persistentItems, sessionItems),
	}
	return stats, nil
}

// Clear 清空所有长期记忆项。
func (s *memoryServiceImpl) Clear(ctx context.Context) error {
	return s.persistentRepo.Clear(ctx)
}

// ClearSession 清空所有会话级记忆项。
func (s *memoryServiceImpl) ClearSession(ctx context.Context) error {
	return s.sessionRepo.Clear(ctx)
}

// Search 对记忆项打分并返回与查询最相关的结果。
func Search(items []domain.MemoryItem, query string, topK int, minScore float64) []Match {
	trimmedQuery := strings.TrimSpace(query)
	if topK <= 0 || trimmedQuery == "" {
		return nil
	}

	queryKeywords := domain.Keywords(trimmedQuery)
	queryFrags := queryFragments(trimmedQuery)
	queryText := strings.ToLower(trimmedQuery)
	matches := make([]Match, 0, len(items))

	for _, raw := range items {
		item := raw.Normalized()
		score := scoreItem(item, queryText, queryKeywords, queryFrags)
		if score < minScore {
			continue
		}
		matches = append(matches, Match{Item: item, Score: score})
	}

	sortMatches(matches)
	if len(matches) > topK {
		matches = matches[:topK]
	}
	return matches
}

// MergeMatches 对多个匹配结果分组去重并重新排序。
func MergeMatches(topK int, groups ...[]Match) []Match {
	merged := make([]Match, 0)
	seen := map[string]Match{}
	for _, group := range groups {
		for _, match := range group {
			key := matchKey(match.Item)
			if existing, ok := seen[key]; ok {
				if match.Score > existing.Score {
					seen[key] = match
				}
				continue
			}
			seen[key] = match
		}
	}
	for _, match := range seen {
		merged = append(merged, match)
	}
	sortMatches(merged)
	if topK > 0 && len(merged) > topK {
		merged = merged[:topK]
	}
	return merged
}

func sortMatches(matches []Match) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		leftPriority := priorityForType(matches[i].Item.Type)
		rightPriority := priorityForType(matches[j].Item.Type)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return matches[i].Item.UpdatedAt.After(matches[j].Item.UpdatedAt)
	})
}

func scoreItem(item domain.MemoryItem, queryText string, queryKeywords []string, queryFrags []string) float64 {
	searchText := strings.ToLower(item.SearchText())
	if searchText == "" {
		return 0
	}

	var score float64
	matched := false
	tagSet := make(map[string]struct{}, len(item.Tags))
	for _, tag := range item.Tags {
		tagSet[strings.ToLower(tag)] = struct{}{}
	}

	for _, keyword := range queryKeywords {
		if _, ok := tagSet[keyword]; ok {
			score += 2.6
			matched = true
		}
		if strings.Contains(searchText, keyword) {
			score += keywordWeight(keyword)
			matched = true
		}
	}

	for _, frag := range queryFrags {
		if len(frag) < 2 {
			continue
		}
		if strings.Contains(searchText, frag) {
			score += 0.55
			matched = true
		}
	}

	if item.Summary != "" && strings.Contains(strings.ToLower(item.Summary), queryText) {
		score += 3.2
		matched = true
	}
	if item.Type != "" && strings.Contains(queryText, strings.ToLower(item.Type)) {
		score += 2.2
		matched = true
	}
	if strings.Contains(searchText, queryText) {
		score += 1.3
		matched = true
	}
	if !matched {
		return 0
	}

	score += float64(priorityForType(item.Type)) * 0.9
	score += item.Confidence * 0.8
	return score
}

func priorityForType(itemType string) int {
	switch itemType {
	case domain.TypeUserPreference:
		return 5
	case domain.TypeProjectRule:
		return 4
	case domain.TypeCodeFact:
		return 3
	case domain.TypeFixRecipe:
		return 2
	case domain.TypeSessionMemory:
		return 1
	case domain.TypeLegacyChat:
		return 0
	default:
		return 0
	}
}

func keywordWeight(keyword string) float64 {
	weight := 1.4
	if strings.Contains(keyword, "/") || strings.Contains(keyword, ".") {
		weight += 1.4
	}
	if strings.HasPrefix(keyword, "go") || strings.Contains(keyword, "config") || strings.Contains(keyword, "yaml") {
		weight += 0.8
	}
	if len(keyword) >= 8 {
		weight += 0.4
	}
	return weight
}

func queryFragments(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil
	}
	runes := []rune(query)
	if len(runes) <= 4 {
		return []string{query}
	}
	fragments := make([]string, 0, len(runes))
	seen := map[string]struct{}{}
	for size := 2; size <= 3; size++ {
		for i := 0; i+size <= len(runes); i++ {
			frag := strings.TrimSpace(string(runes[i : i+size]))
			if len([]rune(frag)) < 2 {
				continue
			}
			if _, ok := seen[frag]; ok {
				continue
			}
			seen[frag] = struct{}{}
			fragments = append(fragments, frag)
		}
	}
	return fragments
}

func matchKey(item domain.MemoryItem) string {
	normalized := item.Normalized()
	return normalized.Type + "::" + normalized.Scope + "::" + normalized.Summary
}

func buildMemoryText(userInput, assistantReply string) string {
	return strings.TrimSpace(userInput) + "\n" + strings.TrimSpace(assistantReply)
}

func deriveMemoryItems(userInput, assistantReply string) []domain.MemoryItem {
	now := time.Now().UTC()
	items := make([]domain.MemoryItem, 0, 4)

	if preferenceItem, ok := extractPreferenceMemory(userInput, assistantReply, now); ok {
		items = append(items, preferenceItem)
	}
	if ruleItem, ok := extractProjectRuleMemory(userInput, assistantReply, now); ok {
		items = append(items, ruleItem)
	}
	if codeFactItem, ok := extractCodeFactMemory(userInput, assistantReply, now); ok {
		items = append(items, codeFactItem)
	}
	if failureItem, ok := extractFixRecipeMemory(userInput, assistantReply, now); ok {
		items = append(items, failureItem)
	}
	if mainItem, ok := extractSessionMemory(userInput, assistantReply, now); ok {
		items = append(items, mainItem)
	}

	return dedupeMemoryItems(items)
}

func extractSessionMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := buildMemoryText(userInput, assistantReply)
	if !isCodingRelevant(userInput, assistantReply) || looksLikeStableInstruction(userInput) || looksLikeProjectFact(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(userInput, 140)
	if summary == "" {
		summary = domain.SummarizeText(assistantReply, 140)
	}

	return newMemoryItem(now, domain.TypeSessionMemory, domain.ScopeSession, summary, assistantReply, combined, 0.66), true
}

func extractPreferenceMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	trimmed := strings.TrimSpace(userInput)
	if trimmed == "" || !looksLikeStableInstruction(trimmed) {
		return domain.MemoryItem{}, false
	}

	summary := domain.SummarizeText(trimmed, 140)
	return newMemoryItem(now, domain.TypeUserPreference, domain.ScopeUser, summary, assistantReply, buildMemoryText(userInput, assistantReply), 0.95), true
}

func extractProjectRuleMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := strings.ToLower(buildMemoryText(userInput, assistantReply))
	if !looksLikeProjectFact(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}
	if !containsAnyFold(combined, "config.yaml", "readme", "go test", "go build", "命令", "约定", "配置", "目录", "结构", "仓库") {
		return domain.MemoryItem{}, false
	}
	summary := domain.SummarizeText(firstNonEmptyLine(userInput, assistantReply), 140)
	return newMemoryItem(now, domain.TypeProjectRule, domain.ScopeProject, summary, assistantReply, buildMemoryText(userInput, assistantReply), 0.9), true
}

func extractCodeFactMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := buildMemoryText(userInput, assistantReply)
	if !looksLikeCodeKnowledge(userInput, assistantReply) {
		return domain.MemoryItem{}, false
	}
	if containsAnyFold(strings.ToLower(userInput), "帮我", "请你", "写一个", "实现一个") && !containsAnyFold(combined, "在 ", "位于", "负责", "调用", "使用", "路径", "文件", "函数", "模块", "返回", "读取", "写入") {
		return domain.MemoryItem{}, false
	}
	summary := domain.SummarizeText(firstNonEmptyLine(assistantReply, userInput), 140)
	return newMemoryItem(now, domain.TypeCodeFact, domain.ScopeProject, summary, assistantReply, combined, 0.82), true
}

func extractFixRecipeMemory(userInput, assistantReply string, now time.Time) (domain.MemoryItem, bool) {
	combined := strings.ToLower(buildMemoryText(userInput, assistantReply))
	hasProblem := containsAnyFold(combined, "error", "failed", "panic", "bug", "报错", "失败", "异常")
	hasFix := containsAnyFold(combined, "修复", "已通过", "解决", "fixed", "use", "改为", "增加", "remove", "replace")
	if !hasProblem || !hasFix {
		return domain.MemoryItem{}, false
	}
	summary := domain.SummarizeText(firstNonEmptyLine(userInput, assistantReply), 140)
	details := assistantReply
	if details == "" {
		details = userInput
	}
	return newMemoryItem(now, domain.TypeFixRecipe, domain.ScopeProject, summary, details, buildMemoryText(userInput, assistantReply), 0.8), true
}

func newMemoryItem(now time.Time, itemType, scope, summary, details, text string, confidence float64) domain.MemoryItem {
	item := domain.MemoryItem{
		ID:         strconv.FormatInt(now.UnixNano(), 10) + "-" + itemType,
		Type:       itemType,
		Summary:    strings.TrimSpace(summary),
		Details:    domain.SummarizeText(details, 220),
		Scope:      scope,
		Tags:       domain.InferTags(summary + "\n" + details),
		Source:     "conversation",
		Confidence: confidence,
		Text:       strings.TrimSpace(text),
		CreatedAt:  now,
		UpdatedAt:  now,
		UserInput:  strings.TrimSpace(summary),
	}
	return item.Normalized()
}

func isCodingRelevant(userInput, assistantReply string) bool {
	combined := strings.ToLower(buildMemoryText(userInput, assistantReply))
	if containsAnyFold(combined,
		"function", "file", "repo", "project", "build", "test", "config", "bug", "error", "fix",
		"golang", "go ", "yaml", "json", "memory", "prompt", "cli", "agent", "编程", "项目", "配置", "测试", "构建", "报错", "修复") {
		return true
	}
	trimmedUser := strings.TrimSpace(strings.ToLower(userInput))
	return len(trimmedUser) > 20 && containsAnyFold(trimmedUser, "code", "代码")
}

func looksLikeProjectFact(userInput, assistantReply string) bool {
	combined := strings.ToLower(buildMemoryText(userInput, assistantReply))
	return containsAnyFold(combined, "config.yaml", "readme", "go test", "go build", "项目", "仓库", "约定", "配置", "结构", "命令", "services/", "memory/", "main.go")
}

func looksLikeCodeKnowledge(userInput, assistantReply string) bool {
	combined := buildMemoryText(userInput, assistantReply)
	if !isCodingRelevant(userInput, assistantReply) {
		return false
	}

	hasCodeAnchor := containsAnyFold(combined,
		".go", "config.yaml", "main.go", "services/", "memory/", "json", "yaml",
		"function", "func", "struct", "interface", "method", "package", "import",
		"函数", "文件", "模块", "包", "结构体", "接口", "方法", "字段", "参数", "路径", "目录")
	if !hasCodeAnchor {
		return false
	}

	trimmedUser := strings.ToLower(strings.TrimSpace(userInput))
	trimmedReply := strings.ToLower(strings.TrimSpace(assistantReply))
	hasQuestionIntent := containsAnyFold(trimmedUser,
		"什么", "干嘛", "作用", "怎么", "如何", "why", "where", "which", "负责", "在哪", "含义", "区别")
	hasExplanation := containsAnyFold(trimmedReply,
		"用于", "负责", "位于", "表示", "通过", "调用", "读取", "写入", "返回", "实现", "处理", "对应", "配置", "路径", "字段", "参数")

	return hasQuestionIntent || hasExplanation || len(strings.TrimSpace(assistantReply)) >= 48
}

func looksLikeStableInstruction(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if !containsAnyFold(trimmed, "默认", "始终", "以后", "统一", "不要自动", "回答中文", "只用", "只使用", "只需要", "不要再", "固定", "长期") {
		return false
	}
	return containsAnyFold(trimmed, "config.yaml", ".env", "中文", "提交", "命令", "风格", "配置")
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

func allowedPersistTypes(configured []string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, itemType := range configured {
		itemType = normalizeMemoryType(itemType)
		if domain.IsPersistentType(itemType) {
			allowed[itemType] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		allowed[domain.TypeUserPreference] = struct{}{}
		allowed[domain.TypeProjectRule] = struct{}{}
		allowed[domain.TypeCodeFact] = struct{}{}
		allowed[domain.TypeFixRecipe] = struct{}{}
	}
	return allowed
}

func normalizeMemoryType(itemType string) string {
	switch strings.TrimSpace(itemType) {
	case "project_memory":
		return domain.TypeProjectRule
	case "failure_note":
		return domain.TypeFixRecipe
	default:
		return strings.TrimSpace(itemType)
	}
}

func shortPromptBlock(item domain.MemoryItem) string {
	item = item.Normalized()
	parts := []string{
		"Type: " + item.Type,
		"Summary: " + item.Summary,
	}
	if item.Details != "" {
		parts = append(parts, "Details: "+domain.SummarizeText(item.Details, 140))
	}
	if len(item.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(item.Tags, ", "))
	}
	return strings.Join(parts, "\n")
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

func countMemoryTypes(groups ...[]domain.MemoryItem) map[string]int {
	counts := map[string]int{}
	for _, group := range groups {
		for _, item := range group {
			counts[item.Normalized().Type]++
		}
	}
	return counts
}
