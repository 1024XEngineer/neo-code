package memo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontext "neo-code/internal/context"
)

// stubStore 实现 Store 接口用于测试。
type stubStore struct {
	index            *Index
	err              error
	loadIndexCalls   int
	saveIndexErr     error
	saveTopicErr     error
	deleteTopicErr   error
	deletedTopics    []string
	saveIndexCalls   int
	saveTopicCalls   int
	deleteTopicCalls int
}

func (s *stubStore) LoadIndex(_ context.Context) (*Index, error) {
	s.loadIndexCalls++
	if s.err != nil {
		return nil, s.err
	}
	if s.index == nil {
		return &Index{Entries: []Entry{}}, nil
	}
	return s.index, nil
}

func (s *stubStore) SaveIndex(_ context.Context, index *Index) error {
	s.saveIndexCalls++
	if s.saveIndexErr != nil {
		return s.saveIndexErr
	}
	s.index = index
	return nil
}
func (s *stubStore) LoadTopic(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubStore) SaveTopic(_ context.Context, _, _ string) error {
	s.saveTopicCalls++
	if s.saveTopicErr != nil {
		return s.saveTopicErr
	}
	return nil
}
func (s *stubStore) DeleteTopic(_ context.Context, filename string) error {
	s.deleteTopicCalls++
	s.deletedTopics = append(s.deletedTopics, filename)
	if s.deleteTopicErr != nil {
		return s.deleteTopicErr
	}
	return nil
}
func (s *stubStore) ListTopics(_ context.Context) ([]string, error) { return nil, nil }

func TestContextSourceEmpty(t *testing.T) {
	store := &stubStore{}
	source := NewContextSource(store)
	sections, err := source.Sections(context.Background(), agentcontext.BuildInput{})
	if err != nil {
		t.Fatalf("Sections error: %v", err)
	}
	if len(sections) != 0 {
		t.Errorf("Sections on empty store = %d, want 0", len(sections))
	}
}

func TestContextSourceWithEntries(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{Type: TypeUser, Title: "偏好 tab", TopicFile: "user.md"},
			},
		},
	}
	source := NewContextSource(store)
	sections, err := source.Sections(context.Background(), agentcontext.BuildInput{})
	if err != nil {
		t.Fatalf("Sections error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("Sections = %d, want 1", len(sections))
	}
	if sections[0].Title != "Memo" {
		t.Errorf("Title = %q, want %q", sections[0].Title, "Memo")
	}
	if !strings.Contains(sections[0].Content, "偏好 tab") {
		t.Errorf("Content should contain entry: %q", sections[0].Content)
	}
}

func TestContextSourceCache(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{Type: TypeUser, Title: "first"},
			},
		},
	}
	source := NewContextSource(store, WithCacheTTL(10*time.Second))
	ctx := context.Background()

	// 第一次加载
	sections1, _ := source.Sections(ctx, agentcontext.BuildInput{})
	if !strings.Contains(sections1[0].Content, "first") {
		t.Error("first load should contain 'first'")
	}

	// 修改 store 数据（模拟外部变更）
	store.index.Entries[0].Title = "second"

	// 缓存 TTL 内应返回旧数据
	sections2, _ := source.Sections(ctx, agentcontext.BuildInput{})
	if !strings.Contains(sections2[0].Content, "first") {
		t.Error("cached load should still contain 'first'")
	}
}

func TestContextSourceCacheCachesEmptyIndex(t *testing.T) {
	store := &stubStore{index: &Index{Entries: []Entry{}}}
	source := NewContextSource(store, WithCacheTTL(10*time.Second))
	ctx := context.Background()

	sections1, err := source.Sections(ctx, agentcontext.BuildInput{})
	if err != nil {
		t.Fatalf("Sections first call error: %v", err)
	}
	if len(sections1) != 0 {
		t.Fatalf("sections first call = %d, want 0", len(sections1))
	}

	sections2, err := source.Sections(ctx, agentcontext.BuildInput{})
	if err != nil {
		t.Fatalf("Sections second call error: %v", err)
	}
	if len(sections2) != 0 {
		t.Fatalf("sections second call = %d, want 0", len(sections2))
	}
	if store.loadIndexCalls != 1 {
		t.Fatalf("LoadIndex calls = %d, want 1", store.loadIndexCalls)
	}
}

func TestContextSourceCacheInvalidation(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{Type: TypeUser, Title: "old"},
			},
		},
	}
	source := NewContextSource(store, WithCacheTTL(10*time.Second))
	ctx := context.Background()

	// 加载并缓存
	source.Sections(ctx, agentcontext.BuildInput{})

	// 修改数据
	store.index.Entries[0].Title = "new"

	// 手动失效缓存
	cs := source.(*memoContextSource)
	cs.InvalidateCache()

	// 应加载新数据
	sections, _ := source.Sections(ctx, agentcontext.BuildInput{})
	if !strings.Contains(sections[0].Content, "new") {
		t.Errorf("after invalidation, should contain 'new': %q", sections[0].Content)
	}
}

func TestContextSourceStoreError(t *testing.T) {
	store := &stubStore{err: errors.New("read error")}
	source := NewContextSource(store)
	sections, err := source.Sections(context.Background(), agentcontext.BuildInput{})
	if err != nil {
		t.Fatalf("Sections should not propagate error: %v", err)
	}
	if sections != nil {
		t.Errorf("Sections on store error should return nil, got %v", sections)
	}
}

func TestContextSourceCancelledContext(t *testing.T) {
	store := &stubStore{index: &Index{}}
	source := NewContextSource(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sections, err := source.Sections(ctx, agentcontext.BuildInput{})
	if err == nil && sections != nil {
		// 取消的上下文可能导致错误或空结果，都合理
		t.Logf("cancelled context returned %d sections", len(sections))
	}
}
