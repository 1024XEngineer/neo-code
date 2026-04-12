package memo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"neo-code/internal/config"
)

func TestServiceAdd(t *testing.T) {
	store := &stubStore{}
	var invCalled bool
	svc := NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, func() { invCalled = true })

	entry := Entry{
		Type:    TypeUser,
		Title:   "偏好 tab 缩进",
		Content: "用户偏好使用 tab 缩进",
		Source:  SourceUserManual,
	}
	if err := svc.Add(context.Background(), entry); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if !invCalled {
		t.Error("cache invalidation callback should have been called")
	}

	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("List entries = %d, want 1", len(entries))
	}
	if entries[0].Title != "偏好 tab 缩进" {
		t.Errorf("Title = %q, want %q", entries[0].Title, "偏好 tab 缩进")
	}
	if entries[0].ID == "" {
		t.Error("ID should be auto-generated")
	}
	if entries[0].TopicFile == "" {
		t.Error("TopicFile should be auto-generated")
	}
}

func TestServiceAddInvalidType(t *testing.T) {
	svc := NewService(&stubStore{}, nil, config.MemoConfig{}, nil)
	err := svc.Add(context.Background(), Entry{Type: "invalid", Title: "test"})
	if err == nil {
		t.Error("Add with invalid type should return error")
	}
}

func TestServiceAddEmptyTitle(t *testing.T) {
	svc := NewService(&stubStore{}, nil, config.MemoConfig{}, nil)
	err := svc.Add(context.Background(), Entry{Type: TypeUser, Title: ""})
	if err == nil {
		t.Error("Add with empty title should return error")
	}
}

func TestServiceAddNormalizesTitle(t *testing.T) {
	store := &stubStore{}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	err := svc.Add(context.Background(), Entry{
		Type:   TypeUser,
		Title:  "  # heading\n(with suffix)  ",
		Source: SourceUserManual,
	})
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Title != "# heading {with suffix}" {
		t.Fatalf("normalized title = %q", entries[0].Title)
	}
}

func TestServiceRemove(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "1", Type: TypeUser, Title: "偏好 tab", TopicFile: "a.md", Keywords: []string{"tabs"}},
				{ID: "2", Type: TypeProject, Title: "使用 Go", TopicFile: "b.md"},
			},
		},
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	removed, err := svc.Remove(context.Background(), "tab")
	if err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if removed != 1 {
		t.Errorf("Remove returned %d, want 1", removed)
	}

	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("after remove, entries = %d, want 1", len(entries))
	}
	if entries[0].Title != "使用 Go" {
		t.Errorf("remaining Title = %q, want %q", entries[0].Title, "使用 Go")
	}
}

func TestServiceRemoveNoMatch(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{{ID: "1", Type: TypeUser, Title: "test"}},
		},
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	removed, err := svc.Remove(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if removed != 0 {
		t.Errorf("Remove returned %d, want 0", removed)
	}
}

func TestServiceRemoveEmptyKeyword(t *testing.T) {
	svc := NewService(&stubStore{}, nil, config.MemoConfig{}, nil)
	_, err := svc.Remove(context.Background(), "")
	if err == nil {
		t.Error("Remove with empty keyword should return error")
	}
}

func TestServiceSearch(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "1", Type: TypeUser, Title: "偏好 tab", Keywords: []string{"indentation"}},
				{ID: "2", Type: TypeProject, Title: "使用 Go"},
				{ID: "3", Type: TypeUser, Title: "偏好中文注释"},
			},
		},
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	results, err := svc.Search(context.Background(), "偏好")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Search '偏好' = %d results, want 2", len(results))
	}
}

func TestServiceSearchByKeyword(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "1", Type: TypeUser, Title: "style", Keywords: []string{"tabs", "indentation"}},
			},
		},
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	results, err := svc.Search(context.Background(), "indent")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search by keyword = %d results, want 1", len(results))
	}
}

func TestServiceRecall(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "1", Type: TypeUser, Title: "偏好 tab", TopicFile: "a.md"},
				{ID: "2", Type: TypeProject, Title: "其他", TopicFile: "b.md"},
			},
		},
	}
	// 为 stubStore 添加 topic 加载能力
	storeWithTopics := &stubStoreWithTopics{
		stubStore: store,
		topics: map[string]string{
			"a.md": "---\ntype: user\n---\n\n详细内容A",
			"b.md": "---\ntype: project\n---\n\n详细内容B",
		},
	}
	svc := NewService(storeWithTopics, nil, config.MemoConfig{}, nil)

	results, err := svc.Recall(context.Background(), "tab")
	if err != nil {
		t.Fatalf("Recall error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Recall = %d results, want 1", len(results))
	}
	if !strings.Contains(results["a.md"], "详细内容A") {
		t.Errorf("Recall content = %q, should contain 详细内容A", results["a.md"])
	}
}

func TestServiceMaxIndexLines(t *testing.T) {
	store := &stubStore{}
	svc := NewService(store, nil, config.MemoConfig{MaxIndexLines: 2}, nil)

	for i := 0; i < 4; i++ {
		entry := Entry{
			Type:    TypeUser,
			Title:   string(rune('A' + i)),
			Content: string(rune('A' + i)),
			Source:  SourceUserManual,
		}
		if err := svc.Add(context.Background(), entry); err != nil {
			t.Fatalf("Add %d error: %v", i, err)
		}
	}

	entries, _ := svc.List(context.Background())
	if len(entries) != 2 {
		t.Errorf("after overflow, entries = %d, want 2", len(entries))
	}
	// 应保留最新的两条
	if entries[0].Title != "C" || entries[1].Title != "D" {
		t.Errorf("expected [C, D], got [%s, %s]", entries[0].Title, entries[1].Title)
	}
}

func TestServiceAddUpdate(t *testing.T) {
	store := &stubStore{}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	entry := Entry{
		ID:     "fixed_id",
		Type:   TypeUser,
		Title:  "旧标题",
		Source: SourceUserManual,
	}
	_ = svc.Add(context.Background(), entry)

	entry.Title = "新标题"
	_ = svc.Add(context.Background(), entry)

	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("after update, entries = %d, want 1", len(entries))
	}
	if entries[0].Title != "新标题" {
		t.Errorf("Title = %q, want %q", entries[0].Title, "新标题")
	}
}

func TestServiceAddSaveTopicFailureDoesNotPersistIndex(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "existing", Type: TypeUser, Title: "existing", TopicFile: "existing.md"},
			},
		},
		saveTopicErr: errors.New("save topic failed"),
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	err := svc.Add(context.Background(), Entry{
		ID:        "new-id",
		Type:      TypeUser,
		Title:     "new entry",
		Source:    SourceUserManual,
		TopicFile: "new.md",
	})
	if err == nil || !strings.Contains(err.Error(), "save topic") {
		t.Fatalf("expected save topic error, got %v", err)
	}
	if len(store.index.Entries) != 1 {
		t.Fatalf("index should stay unchanged on topic failure, entries=%d", len(store.index.Entries))
	}
	if store.saveIndexCalls != 0 {
		t.Fatalf("SaveIndex should not run when SaveTopic fails, calls=%d", store.saveIndexCalls)
	}
}

func TestServiceRemoveSaveIndexFailureDoesNotDeleteTopics(t *testing.T) {
	store := &stubStore{
		index: &Index{
			Entries: []Entry{
				{ID: "1", Type: TypeUser, Title: "match", TopicFile: "a.md"},
				{ID: "2", Type: TypeUser, Title: "other", TopicFile: "b.md"},
			},
		},
		saveIndexErr: errors.New("save index failed"),
	}
	svc := NewService(store, nil, config.MemoConfig{}, nil)

	_, err := svc.Remove(context.Background(), "match")
	if err == nil || !strings.Contains(err.Error(), "save index") {
		t.Fatalf("expected save index error, got %v", err)
	}
	if store.deleteTopicCalls != 0 {
		t.Fatalf("DeleteTopic should not run when SaveIndex fails, calls=%d", store.deleteTopicCalls)
	}
	if len(store.index.Entries) != 2 {
		t.Fatalf("index should stay unchanged on save failure, entries=%d", len(store.index.Entries))
	}
}

func TestNewEntryID(t *testing.T) {
	id := newEntryID(TypeUser)
	if !strings.HasPrefix(id, "user_") {
		t.Errorf("ID = %q, should start with 'user_'", id)
	}
	// 确保唯一
	id2 := newEntryID(TypeUser)
	if id == id2 {
		t.Error("consecutive IDs should be unique")
	}
}

func TestMatchesKeyword(t *testing.T) {
	entry := Entry{
		Type:     TypeUser,
		Title:    "偏好 tab 缩进",
		Keywords: []string{"indentation", "style"},
	}
	tests := []struct {
		kw   string
		want bool
	}{
		{"tab", true},
		{"偏好", true},
		{"indent", true},
		{"user", true},
		{"nonexistent", false},
	}
	for _, tt := range tests {
		got := matchesKeyword(entry, strings.ToLower(tt.kw))
		if got != tt.want {
			t.Errorf("matchesKeyword(%q) = %v, want %v", tt.kw, got, tt.want)
		}
	}
}

// stubStoreWithTopics 扩展 stubStore 支持 topic 加载。
type stubStoreWithTopics struct {
	*stubStore
	topics map[string]string
	mu     sync.Mutex
}

func (s *stubStoreWithTopics) LoadTopic(_ context.Context, filename string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.topics[filename]
	if !ok {
		return "", errors.New("not found")
	}
	return content, nil
}

func (s *stubStoreWithTopics) SaveTopic(_ context.Context, filename string, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.topics == nil {
		s.topics = make(map[string]string)
	}
	s.topics[filename] = content
	return nil
}

func (s *stubStoreWithTopics) DeleteTopic(_ context.Context, filename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.topics, filename)
	return nil
}
