package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type memoryServiceImpl struct {
	persistentRepo domain.MemoryRepository
	sessionRepo    domain.MemoryRepository
	extractor      domain.MemoryExtractor
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

// NewMemoryService creates a memory service with the default rule-based extractor.
func NewMemoryService(
	persistentRepo domain.MemoryRepository,
	sessionRepo domain.MemoryRepository,
	topK int,
	minScore float64,
	maxPromptChars int,
	path string,
	persistTypes []string,
) domain.MemoryService {
	return NewMemoryServiceWithExtractor(
		persistentRepo,
		sessionRepo,
		NewRuleBasedMemoryExtractor(),
		topK,
		minScore,
		maxPromptChars,
		path,
		persistTypes,
	)
}

// NewMemoryServiceWithExtractor creates a memory service with a pluggable extractor.
func NewMemoryServiceWithExtractor(
	persistentRepo domain.MemoryRepository,
	sessionRepo domain.MemoryRepository,
	extractor domain.MemoryExtractor,
	topK int,
	minScore float64,
	maxPromptChars int,
	path string,
	persistTypes []string,
) domain.MemoryService {
	if extractor == nil {
		extractor = NewRuleBasedMemoryExtractor()
	}

	return &memoryServiceImpl{
		persistentRepo: persistentRepo,
		sessionRepo:    sessionRepo,
		extractor:      extractor,
		topK:           topK,
		minScore:       minScore,
		maxPromptChars: maxPromptChars,
		path:           strings.TrimSpace(path),
		persistTypes:   allowedPersistTypes(persistTypes),
	}
}

// BuildContext returns the most relevant memory snippets for the current input.
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

	var filteredMatches []Match
	for _, match := range matches {
		if match.Score >= s.minScore {
			filteredMatches = append(filteredMatches, match)
		}
	}
	if len(filteredMatches) == 0 {
		return "", nil
	}
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

// Save extracts memory items from a conversation turn and persists them.
func (s *memoryServiceImpl) Save(ctx context.Context, userInput, reply string) error {
	items, err := s.extractor.Extract(ctx, userInput, reply)
	if err != nil {
		return err
	}

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

// GetStats returns memory counts and retrieval settings.
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

// Clear removes all persistent memory items.
func (s *memoryServiceImpl) Clear(ctx context.Context) error {
	return s.persistentRepo.Clear(ctx)
}

// ClearSession removes all session-scoped memory items.
func (s *memoryServiceImpl) ClearSession(ctx context.Context) error {
	return s.sessionRepo.Clear(ctx)
}

// Search scores memory items and returns the most relevant matches.
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

// MergeMatches merges and resorts multiple match groups.
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

func countMemoryTypes(groups ...[]domain.MemoryItem) map[string]int {
	counts := map[string]int{}
	for _, group := range groups {
		for _, item := range group {
			counts[item.Normalized().Type]++
		}
	}
	return counts
}

// ListMemoryItems returns both persistent and session memory items for inspection.
func (s *memoryServiceImpl) ListMemoryItems(ctx context.Context) ([]domain.MemoryItem, error) {
	persistentItems, err := s.persistentRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	sessionItems, err := s.sessionRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]domain.MemoryItem, 0, len(persistentItems)+len(sessionItems))
	for _, item := range persistentItems {
		items = append(items, item.Normalized())
	}
	for _, item := range sessionItems {
		items = append(items, item.Normalized())
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].Summary < items[j].Summary
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}
