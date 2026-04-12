package memo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
)

// Service 编排记忆的存储、检索、提取和删除，是 memo 子系统对外的统一入口。
type Service struct {
	store      Store
	extractor  Extractor
	config     config.MemoConfig
	mu         sync.Mutex
	sourceInvl func() // 可选的缓存失效回调
}

// NewService 创建 memo Service 实例。
// extractor 可以为 nil（禁用自动提取时不需要）。
func NewService(store Store, extractor Extractor, cfg config.MemoConfig, sourceInvl func()) *Service {
	return &Service{
		store:      store,
		extractor:  extractor,
		config:     cfg,
		sourceInvl: sourceInvl,
	}
}

// Add 添加一条记忆并持久化索引和 topic 文件。
func (s *Service) Add(ctx context.Context, entry Entry) error {
	if !IsValidType(entry.Type) {
		return fmt.Errorf("memo: invalid type %q", entry.Type)
	}
	entry.Title = NormalizeTitle(entry.Title)
	if entry.Title == "" {
		return fmt.Errorf("memo: title is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if entry.ID == "" {
		entry.ID = newEntryID(entry.Type)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	if entry.TopicFile == "" {
		entry.TopicFile = fmt.Sprintf("%s_%s.md", entry.Type, entry.ID)
	}

	index, err := s.store.LoadIndex(ctx)
	if err != nil {
		return fmt.Errorf("memo: load index: %w", err)
	}
	working := cloneIndex(index)

	// 检查是否已存在相同 ID（更新场景）
	replaced := false
	for i, existing := range working.Entries {
		if existing.ID == entry.ID {
			working.Entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		working.Entries = append(working.Entries, entry)
	}
	working.UpdatedAt = now

	// 截断索引到最大行数
	var topicsToDelete []string
	if s.config.MaxIndexLines > 0 && len(working.Entries) > s.config.MaxIndexLines {
		excess := len(working.Entries) - s.config.MaxIndexLines
		// 记录最旧条目对应的 topic 文件，待索引写入成功后再删除。
		for i := 0; i < excess; i++ {
			topicFile := strings.TrimSpace(working.Entries[i].TopicFile)
			if topicFile != "" && topicFile != entry.TopicFile {
				topicsToDelete = append(topicsToDelete, topicFile)
			}
		}
		working.Entries = working.Entries[excess:]
	}

	if err := s.store.SaveTopic(ctx, entry.TopicFile, RenderTopic(&entry)); err != nil {
		return fmt.Errorf("memo: save topic: %w", err)
	}
	if err := s.store.SaveIndex(ctx, working); err != nil {
		if !replaced {
			_ = s.store.DeleteTopic(ctx, entry.TopicFile)
		}
		return fmt.Errorf("memo: save index: %w", err)
	}
	for _, topicFile := range topicsToDelete {
		_ = s.store.DeleteTopic(ctx, topicFile)
	}

	s.invalidateCache()
	return nil
}

// loadIndexLocked 在持有锁的状态下加载索引，供多个 Service 方法复用。
// 调用方须持有 s.mu 锁。
func (s *Service) loadIndexLocked(ctx context.Context) (*Index, error) {
	index, err := s.store.LoadIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("memo: load index: %w", err)
	}
	return index, nil
}

// Remove 按关键词搜索并删除匹配的记忆条目。
// 返回被删除的条目数量。
func (s *Service) Remove(ctx context.Context, keyword string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked(ctx)
	if err != nil {
		return 0, err
	}
	working := cloneIndex(index)

	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, fmt.Errorf("memo: keyword is empty")
	}

	var remaining []Entry
	removed := 0
	topicsToDelete := make([]string, 0, len(working.Entries))
	for _, entry := range working.Entries {
		if matchesKeyword(entry, keyword) {
			if topicFile := strings.TrimSpace(entry.TopicFile); topicFile != "" {
				topicsToDelete = append(topicsToDelete, topicFile)
			}
			removed++
		} else {
			remaining = append(remaining, entry)
		}
	}

	if removed == 0 {
		return 0, nil
	}

	working.Entries = remaining
	working.UpdatedAt = time.Now()
	if err := s.store.SaveIndex(ctx, working); err != nil {
		return 0, fmt.Errorf("memo: save index: %w", err)
	}
	for _, topicFile := range topicsToDelete {
		_ = s.store.DeleteTopic(ctx, topicFile)
	}

	s.invalidateCache()
	return removed, nil
}

// List 返回所有记忆条目的浅拷贝。
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Entry, len(index.Entries))
	copy(result, index.Entries)
	return result, nil
}

// Search 按关键词搜索记忆条目，返回匹配结果。
func (s *Service) Search(ctx context.Context, keyword string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked(ctx)
	if err != nil {
		return nil, err
	}

	keyword = strings.ToLower(strings.TrimSpace(keyword))
	var results []Entry
	for _, entry := range index.Entries {
		if matchesKeyword(entry, keyword) {
			results = append(results, entry)
		}
	}
	return results, nil
}

// Recall 加载匹配关键词的 topic 文件内容。
func (s *Service) Recall(ctx context.Context, keyword string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexLocked(ctx)
	if err != nil {
		return nil, err
	}

	keyword = strings.ToLower(strings.TrimSpace(keyword))
	results := make(map[string]string)
	for _, entry := range index.Entries {
		if !matchesKeyword(entry, keyword) {
			continue
		}
		if entry.TopicFile == "" {
			continue
		}
		content, err := s.store.LoadTopic(ctx, entry.TopicFile)
		if err != nil {
			continue
		}
		results[entry.TopicFile] = content
	}
	return results, nil
}

// invalidateCache 触发上下文源的缓存失效回调。
func (s *Service) invalidateCache() {
	if s.sourceInvl != nil {
		s.sourceInvl()
	}
}

// matchesKeyword 检查条目是否匹配关键词（标题、关键词列表、类型）。
// 调用方须确保 keyword 已转换为小写。
func matchesKeyword(entry Entry, keyword string) bool {
	if strings.Contains(strings.ToLower(entry.Title), keyword) {
		return true
	}
	if strings.Contains(strings.ToLower(string(entry.Type)), keyword) {
		return true
	}
	for _, kw := range entry.Keywords {
		if strings.Contains(strings.ToLower(kw), keyword) {
			return true
		}
	}
	return false
}

// newEntryID 生成格式为 <type>_<timestamp_hex>_<random_hex> 的唯一 ID。
func newEntryID(t Type) string {
	ts := fmt.Sprintf("%x", time.Now().Unix())
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	randHex := hex.EncodeToString(buf)
	return fmt.Sprintf("%s_%s_%s", t, ts, randHex)
}

// cloneIndex 复制索引结构，避免在持久化失败时污染内存中的原始数据引用。
func cloneIndex(index *Index) *Index {
	if index == nil {
		return &Index{}
	}
	cloned := &Index{
		Entries:   make([]Entry, len(index.Entries)),
		UpdatedAt: index.UpdatedAt,
	}
	copy(cloned.Entries, index.Entries)
	return cloned
}
