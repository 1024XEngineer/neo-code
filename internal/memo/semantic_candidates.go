package memo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const (
	semanticCandidateShortlistLimit  = 5
	semanticCandidateContentMaxRunes = 240
)

type scoredExtractionCandidate struct {
	candidate ExtractionCandidate
	score     int
}

// semanticCandidateShortlist 为候选记忆检索相关 existing memo，并限制进入 LLM 的数量。
func (s *Service) semanticCandidateShortlist(
	ctx context.Context,
	entry Entry,
	limit int,
) ([]ExtractionCandidate, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = semanticCandidateShortlistLimit
	}
	if err := s.ensureSemanticCandidateIndex(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	scored := make([]scoredExtractionCandidate, 0, len(s.semanticCandidatesByRef))
	for _, candidate := range s.semanticCandidatesByRef {
		score := scoreExtractionCandidate(entry, candidate)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredExtractionCandidate{
			candidate: cloneExtractionCandidate(candidate),
			score:     score,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].candidate.Ref < scored[j].candidate.Ref
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]ExtractionCandidate, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.candidate)
	}
	return result, nil
}

// ensureSemanticCandidateIndex 懒加载语义去重候选索引，避免每轮 accepted run 都重扫 topic 文件。
func (s *Service) ensureSemanticCandidateIndex(ctx context.Context) error {
	s.mu.Lock()
	if s.semanticIndexReady {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.semanticIndexMu.Lock()
	defer s.semanticIndexMu.Unlock()

	s.mu.Lock()
	if s.semanticIndexReady {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	candidatesByRef := make(map[string]ExtractionCandidate)
	for _, scope := range supportedStorageScopes() {
		index, err := s.store.LoadIndex(ctx, scope)
		if err != nil {
			return fmt.Errorf("memo: load index: %w", err)
		}
		for _, entry := range index.Entries {
			topicFile := strings.TrimSpace(entry.TopicFile)
			if topicFile == "" {
				continue
			}
			topicContent, err := s.store.LoadTopic(ctx, scope, topicFile)
			if err != nil {
				continue
			}
			candidate := buildExtractionCandidateFromTopic(scope, entry, topicContent)
			if candidate.Ref == "" {
				continue
			}
			candidatesByRef[candidate.Ref] = candidate
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.semanticIndexReady {
		return nil
	}
	s.semanticCandidatesByRef = candidatesByRef
	s.semanticIndexReady = true
	return nil
}

// trackSemanticCandidateLocked 在语义索引已就绪时同步维护单条 memo 的 shortlist 快照。
func (s *Service) trackSemanticCandidateLocked(scope Scope, entry Entry) {
	if !s.semanticIndexReady {
		return
	}
	topicFile := strings.TrimSpace(entry.TopicFile)
	if topicFile == "" {
		return
	}
	candidate := buildExtractionCandidateFromEntry(scope, entry)
	if candidate.Ref == "" {
		return
	}
	if s.semanticCandidatesByRef == nil {
		s.semanticCandidatesByRef = make(map[string]ExtractionCandidate)
	}
	s.semanticCandidatesByRef[candidate.Ref] = candidate
}

// removeSemanticCandidateLocked 从语义索引中删除指定 topic 的候选快照。
func (s *Service) removeSemanticCandidateLocked(scope Scope, topicFile string) {
	if !s.semanticIndexReady {
		return
	}
	topicFile = strings.TrimSpace(topicFile)
	if topicFile == "" {
		return
	}
	delete(s.semanticCandidatesByRef, scopedTopicKey(scope, topicFile))
}

// buildExtractionCandidateFromEntry 将内存中的完整 Entry 收敛为 shortlist 快照。
func buildExtractionCandidateFromEntry(scope Scope, entry Entry) ExtractionCandidate {
	topicFile := strings.TrimSpace(entry.TopicFile)
	if topicFile == "" {
		return ExtractionCandidate{}
	}
	return ExtractionCandidate{
		Ref:      scopedTopicKey(scope, topicFile),
		Scope:    scope,
		Type:     entry.Type,
		Source:   strings.TrimSpace(entry.Source),
		Title:    NormalizeTitle(entry.Title),
		Keywords: normalizeKeywords(entry.Keywords),
		Content:  truncateSemanticContent(entry.Content),
	}
}

// buildExtractionCandidateFromTopic 将 topic 文件内容解析为 shortlist 快照。
func buildExtractionCandidateFromTopic(scope Scope, entry Entry, topicContent string) ExtractionCandidate {
	source, keywords, content := parseTopicSnapshot(topicContent)
	entry.Source = source
	entry.Keywords = keywords
	entry.Content = content
	return buildExtractionCandidateFromEntry(scope, entry)
}

// parseTopicSnapshot 解析 topic frontmatter 中的 source、keywords 与正文内容。
func parseTopicSnapshot(topic string) (string, []string, string) {
	parts := strings.Split(topic, "---")
	if len(parts) < 3 {
		return "", nil, strings.TrimSpace(topic)
	}

	var (
		source   string
		keywords []string
	)
	frontmatter := parts[1]
	body := strings.TrimSpace(strings.Join(parts[2:], "---"))
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "source:"):
			source = strings.TrimSpace(strings.TrimPrefix(line, "source:"))
		case strings.HasPrefix(line, "keywords:"):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "keywords:"))
			keywords = parseTopicKeywords(raw)
		}
	}
	return source, keywords, body
}

// parseTopicKeywords 解析 frontmatter 中的单行 keywords 列表。
func parseTopicKeywords(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	keywords := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, `"'`))
		if part == "" {
			continue
		}
		keywords = append(keywords, part)
	}
	return normalizeKeywords(keywords)
}

// truncateSemanticContent 对候选正文做固定上限截断，避免把全量 memo 内容送入 prompt。
func truncateSemanticContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= semanticCandidateContentMaxRunes {
		return content
	}
	return string(runes[:semanticCandidateContentMaxRunes]) + "..."
}

// scoreExtractionCandidate 为 shortlist 候选打分，优先保留最相关的 memo。
func scoreExtractionCandidate(target Entry, existing ExtractionCandidate) int {
	targetTitle := strings.ToLower(NormalizeTitle(target.Title))
	targetContent := strings.ToLower(strings.TrimSpace(target.Content))
	existingTitle := strings.ToLower(NormalizeTitle(existing.Title))
	existingContent := strings.ToLower(strings.TrimSpace(existing.Content))

	score := 0
	if target.Type == existing.Type {
		score += 80
	}
	if targetTitle != "" && targetTitle == existingTitle {
		score += 1000
	}
	if targetContent != "" && targetContent == existingContent {
		score += 900
	}
	if targetTitle != "" && strings.Contains(existingContent, targetTitle) {
		score += 120
	}
	if existingTitle != "" && strings.Contains(targetContent, existingTitle) {
		score += 120
	}

	targetKeywordSet := tokenSet(append([]string{target.Title, target.Content}, target.Keywords...)...)
	existingKeywordSet := tokenSet(append([]string{existing.Title, existing.Content}, existing.Keywords...)...)
	for token := range targetKeywordSet {
		if _, ok := existingKeywordSet[token]; ok {
			score += 10
		}
	}
	for _, keyword := range normalizeKeywords(target.Keywords) {
		normalized := strings.ToLower(strings.TrimSpace(keyword))
		if normalized == "" {
			continue
		}
		for _, existingKeyword := range existing.Keywords {
			if normalized == strings.ToLower(strings.TrimSpace(existingKeyword)) {
				score += 40
				break
			}
		}
	}
	return score
}

// tokenSet 将多段文本规整为去重后的 token 集合，供 shortlist 相关性排序使用。
func tokenSet(parts ...string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, part := range parts {
		for _, token := range tokenizeSemanticText(part) {
			set[token] = struct{}{}
		}
	}
	return set
}

// tokenizeSemanticText 按字母数字边界切分文本，生成 shortlist 排序所需的归一化 token。
func tokenizeSemanticText(text string) []string {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens = append(tokens, part)
	}
	return tokens
}

// cloneExtractionCandidate 深拷贝 shortlist 候选，避免切片字段共享底层数组。
func cloneExtractionCandidate(candidate ExtractionCandidate) ExtractionCandidate {
	cloned := candidate
	if len(candidate.Keywords) > 0 {
		cloned.Keywords = append([]string(nil), candidate.Keywords...)
	}
	return cloned
}
