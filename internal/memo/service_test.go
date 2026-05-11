package memo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"neo-code/internal/config"
)

func testMemoConfig() config.MemoConfig {
	return config.MemoConfig{
		MaxEntries:        200,
		MaxIndexBytes:     16 * 1024,
		ExtractTimeoutSec: 15,
	}
}

func TestServiceAddRoutesByScope(t *testing.T) {
	store := newMemoryTestStore()
	invalidateCalls := 0
	svc := NewService(store, testMemoConfig(), func() { invalidateCalls++ })

	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "user pref",
		Content: "user pref",
		Source:  SourceUserManual,
	}); err != nil {
		t.Fatalf("Add(user) error = %v", err)
	}
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeProject,
		Title:   "project fact",
		Content: "project fact",
		Source:  SourceUserManual,
	}); err != nil {
		t.Fatalf("Add(project) error = %v", err)
	}

	userEntries, err := svc.List(context.Background(), ScopeUser)
	if err != nil {
		t.Fatalf("List(user) error = %v", err)
	}
	projectEntries, err := svc.List(context.Background(), ScopeProject)
	if err != nil {
		t.Fatalf("List(project) error = %v", err)
	}
	if len(userEntries) != 1 || userEntries[0].Type != TypeUser {
		t.Fatalf("unexpected user entries: %#v", userEntries)
	}
	if len(projectEntries) != 1 || projectEntries[0].Type != TypeProject {
		t.Fatalf("unexpected project entries: %#v", projectEntries)
	}
	if invalidateCalls != 2 {
		t.Fatalf("invalidate calls = %d, want 2", invalidateCalls)
	}
}

func TestServiceAddValidatesEntry(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)

	tests := []Entry{
		{Type: "invalid", Title: "x", Content: "x"},
		{Type: TypeUser, Title: "", Content: "x"},
		{Type: TypeUser, Title: "x", Content: ""},
	}
	for _, entry := range tests {
		if err := svc.Add(context.Background(), entry); err == nil {
			t.Fatalf("expected Add(%+v) to fail", entry)
		}
	}
}

func TestServiceAddNormalizesTitleAndKeywords(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)

	err := svc.Add(context.Background(), Entry{
		Type:     TypeUser,
		Title:    "  # heading\n(with suffix)  ",
		Content:  "content",
		Keywords: []string{" Tabs ", "tabs", "", "Style"},
		Source:   SourceUserManual,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	entries, err := svc.List(context.Background(), ScopeUser)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if entries[0].Title != "# heading {with suffix}" {
		t.Fatalf("normalized title = %q", entries[0].Title)
	}
	if len(entries[0].Keywords) != 2 || entries[0].Keywords[0] != "Tabs" || entries[0].Keywords[1] != "Style" {
		t.Fatalf("normalized keywords = %#v", entries[0].Keywords)
	}
}

func TestServiceSearchMatchesContentAndScope(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)
	_ = svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "style",
		Content: "please use tabs",
		Source:  SourceUserManual,
	})
	_ = svc.Add(context.Background(), Entry{
		Type:    TypeProject,
		Title:   "plan",
		Content: "ship in april",
		Source:  SourceUserManual,
	})

	results, err := svc.Search(context.Background(), "tabs", ScopeUser)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Type != TypeUser {
		t.Fatalf("unexpected search results: %#v", results)
	}

	results, err = svc.Search(context.Background(), "ship", ScopeProject)
	if err != nil {
		t.Fatalf("Search() project error = %v", err)
	}
	if len(results) != 1 || results[0].Type != TypeProject {
		t.Fatalf("unexpected project search results: %#v", results)
	}
}

func TestServiceRecallReturnsScopedEntries(t *testing.T) {
	store := newMemoryTestStore()
	svc := NewService(store, testMemoConfig(), nil)
	_ = svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceUserManual,
	})

	results, err := svc.Recall(context.Background(), "chinese", ScopeAll)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Scope != ScopeUser {
		t.Fatalf("Scope = %q, want %q", results[0].Scope, ScopeUser)
	}
	if !strings.Contains(results[0].Content, "reply in chinese") {
		t.Fatalf("content = %q", results[0].Content)
	}
}

func TestServiceRemoveRespectsScope(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)
	_ = svc.Add(context.Background(), Entry{Type: TypeUser, Title: "same", Content: "user content", Source: SourceUserManual})
	_ = svc.Add(context.Background(), Entry{Type: TypeProject, Title: "same", Content: "project content", Source: SourceUserManual})

	removed, err := svc.Remove(context.Background(), "same", ScopeUser)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	userEntries, _ := svc.List(context.Background(), ScopeUser)
	projectEntries, _ := svc.List(context.Background(), ScopeProject)
	if len(userEntries) != 0 {
		t.Fatalf("expected user scope to be empty, got %#v", userEntries)
	}
	if len(projectEntries) != 1 {
		t.Fatalf("expected project scope to remain, got %#v", projectEntries)
	}
}

func TestServiceRemoveRejectsInvalidScope(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)
	if _, err := svc.Remove(context.Background(), "x", Scope("bad")); err == nil {
		t.Fatal("expected invalid scope error")
	}
}

func TestServiceMaxEntriesTrim(t *testing.T) {
	cfg := testMemoConfig()
	cfg.MaxEntries = 2
	svc := NewService(newMemoryTestStore(), cfg, nil)

	for _, title := range []string{"A", "B", "C"} {
		if err := svc.Add(context.Background(), Entry{
			Type:    TypeUser,
			Title:   title,
			Content: title,
			Source:  SourceUserManual,
		}); err != nil {
			t.Fatalf("Add(%s) error = %v", title, err)
		}
	}

	entries, err := svc.List(context.Background(), ScopeUser)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 || entries[0].Title != "B" || entries[1].Title != "C" {
		t.Fatalf("entries after trim = %#v", entries)
	}
}

func TestServiceMaxIndexBytesTrim(t *testing.T) {
	cfg := testMemoConfig()
	cfg.MaxEntries = 10
	cfg.MaxIndexBytes = 40
	svc := NewService(newMemoryTestStore(), cfg, nil)

	for _, title := range []string{"one", "two", "three"} {
		if err := svc.Add(context.Background(), Entry{
			Type:    TypeProject,
			Title:   title,
			Content: title,
			Source:  SourceUserManual,
		}); err != nil {
			t.Fatalf("Add(%s) error = %v", title, err)
		}
	}

	entries, err := svc.List(context.Background(), ScopeProject)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) >= 3 {
		t.Fatalf("expected byte trimming to remove oldest entry, got %#v", entries)
	}
}

func TestServiceSaveTopicFailureDoesNotPersistIndex(t *testing.T) {
	store := newMemoryTestStore()
	store.saveTopicErr = errors.New("save topic failed")
	svc := NewService(store, testMemoConfig(), nil)

	err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "new",
		Content: "new",
		Source:  SourceUserManual,
	})
	if err == nil || !strings.Contains(err.Error(), "save topic") {
		t.Fatalf("expected save topic error, got %v", err)
	}
	if store.saveIndexCalls != 0 {
		t.Fatalf("SaveIndex() calls = %d, want 0", store.saveIndexCalls)
	}
}

func TestServiceSaveIndexFailureDoesNotDeleteTopic(t *testing.T) {
	store := newMemoryTestStore()
	store.saveIndexErr = errors.New("save index failed")
	svc := NewService(store, testMemoConfig(), nil)

	err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "new",
		Content: "new",
		Source:  SourceUserManual,
	})
	if err == nil || !strings.Contains(err.Error(), "save index") {
		t.Fatalf("expected save index error, got %v", err)
	}
	if store.deleteTopicCalls != 1 {
		t.Fatalf("DeleteTopic() calls = %d, want 1 rollback delete", store.deleteTopicCalls)
	}
}

func TestServiceAutoExtractDedupAcrossScopes(t *testing.T) {
	svc := NewService(newMemoryTestStore(), testMemoConfig(), nil)
	entry := Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceAutoExtract,
	}

	added, err := svc.addAutoExtractIfAbsent(context.Background(), entry)
	if err != nil || !added {
		t.Fatalf("first addAutoExtractIfAbsent() = (%v, %v), want (true, nil)", added, err)
	}
	added, err = svc.addAutoExtractIfAbsent(context.Background(), entry)
	if err != nil {
		t.Fatalf("second addAutoExtractIfAbsent() error = %v", err)
	}
	if added {
		t.Fatal("expected duplicate auto extract to be skipped")
	}
}

func TestServiceAutoExtractTrimmedEntryDoesNotPolluteDedupIndex(t *testing.T) {
	svc := NewService(newMemoryTestStore(), config.MemoConfig{
		MaxEntries:        10,
		MaxIndexBytes:     1,
		ExtractTimeoutSec: 15,
	}, nil)
	entry := Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceAutoExtract,
	}

	added, err := svc.addAutoExtractIfAbsent(context.Background(), entry)
	if err != nil || !added {
		t.Fatalf("first addAutoExtractIfAbsent() = (%v, %v), want (true, nil)", added, err)
	}
	added, err = svc.addAutoExtractIfAbsent(context.Background(), entry)
	if err != nil || !added {
		t.Fatalf("second addAutoExtractIfAbsent() = (%v, %v), want (true, nil)", added, err)
	}

	entries, err := svc.List(context.Background(), ScopeUser)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0 after byte trim", len(entries))
	}

	key := autoExtractDedupKey(entry)
	if refs := svc.autoExtractKeyRefs[key]; refs != 0 {
		t.Fatalf("autoExtractKeyRefs[%q] = %d, want 0", key, refs)
	}
}

func TestServiceEnsureAutoExtractIndexLoadsExistingEntries(t *testing.T) {
	store := newMemoryTestStore()
	store.indexes[ScopeUser] = &Index{Entries: []Entry{{Type: TypeUser, Title: "A", TopicFile: "a.md"}}}
	store.topics[ScopeUser]["a.md"] = "---\nsource: extractor_auto\n---\n\na content"
	svc := NewService(store, testMemoConfig(), nil)

	if err := svc.ensureAutoExtractIndex(context.Background()); err != nil {
		t.Fatalf("ensureAutoExtractIndex() error = %v", err)
	}
	if !svc.autoExtractIndexReady {
		t.Fatal("autoExtractIndexReady = false")
	}
	if svc.autoExtractKeyRefs[autoExtractDedupKey(Entry{
		Type:    TypeUser,
		Title:   "A",
		Content: "a content",
		Source:  SourceAutoExtract,
	})] != 1 {
		t.Fatalf("autoExtractKeyRefs = %#v", svc.autoExtractKeyRefs)
	}
}

func TestMatchesKeywordIncludesContent(t *testing.T) {
	entry := Entry{
		Type:     TypeUser,
		Title:    "style",
		Content:  "please use tabs",
		Keywords: []string{"indentation"},
	}
	if !matchesKeyword(entry, "tabs") {
		t.Fatal("expected content match")
	}
	if !matchesKeyword(entry, "indent") {
		t.Fatal("expected keyword match")
	}
	if matchesKeyword(entry, "missing") {
		t.Fatal("unexpected match for missing keyword")
	}
}

func TestServiceSemanticCandidateShortlistMatchesRelevantMemo(t *testing.T) {
	store := NewFileStore(t.TempDir(), t.TempDir())
	svc := NewService(store, testMemoConfig(), nil)

	if err := svc.Add(context.Background(), Entry{
		Type:    TypeFeedback,
		Title:   "测试策略",
		Content: "用户要求修改后先跑测试。",
		Source:  SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed relevant Add() error = %v", err)
	}
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "语言偏好",
		Content: "用户偏好中文回复。",
		Source:  SourceUserManual,
	}); err != nil {
		t.Fatalf("seed unrelated Add() error = %v", err)
	}

	shortlist, err := svc.semanticCandidateShortlist(context.Background(), Entry{
		Type:    TypeFeedback,
		Title:   "测试策略",
		Content: "用户要求修改后先跑相关测试。",
	}, semanticCandidateShortlistLimit)
	if err != nil {
		t.Fatalf("semanticCandidateShortlist() error = %v", err)
	}
	if len(shortlist) != 1 {
		t.Fatalf("len(shortlist) = %d, want 1", len(shortlist))
	}
	if shortlist[0].Title != "测试策略" || shortlist[0].Source != SourceAutoExtract {
		t.Fatalf("unexpected shortlist candidate: %+v", shortlist[0])
	}
}

func TestServiceSemanticCandidateShortlistLimitsAndTruncates(t *testing.T) {
	store := NewFileStore(t.TempDir(), t.TempDir())
	svc := NewService(store, testMemoConfig(), nil)

	longContent := strings.Repeat("release-note ", 40)
	for index := 0; index < 6; index++ {
		if err := svc.Add(context.Background(), Entry{
			Type:     TypeProject,
			Title:    fmt.Sprintf("发布计划 %d", index),
			Content:  longContent + fmt.Sprintf(" milestone-%d", index),
			Keywords: []string{"release"},
			Source:   SourceAutoExtract,
		}); err != nil {
			t.Fatalf("seed Add(%d) error = %v", index, err)
		}
	}

	shortlist, err := svc.semanticCandidateShortlist(context.Background(), Entry{
		Type:     TypeProject,
		Title:    "发布计划",
		Content:  "release milestone",
		Keywords: []string{"release"},
	}, semanticCandidateShortlistLimit)
	if err != nil {
		t.Fatalf("semanticCandidateShortlist() error = %v", err)
	}
	if len(shortlist) != semanticCandidateShortlistLimit {
		t.Fatalf("len(shortlist) = %d, want %d", len(shortlist), semanticCandidateShortlistLimit)
	}
	for _, candidate := range shortlist {
		if len([]rune(candidate.Content)) > semanticCandidateContentMaxRunes+3 {
			t.Fatalf("candidate content should be truncated, got %q", candidate.Content)
		}
		if len(candidate.Keywords) != 1 || candidate.Keywords[0] != "release" {
			t.Fatalf("candidate keywords should be preserved, got %+v", candidate)
		}
	}
}

func TestTrimIndexEntriesByBytesRemovesMinimalPrefix(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "one", TopicFile: "one.md"},
			{Type: TypeUser, Title: "two", TopicFile: "two.md"},
			{Type: TypeUser, Title: "three", TopicFile: "three.md"},
		},
	}

	target := &Index{Entries: append([]Entry(nil), index.Entries[1:]...)}
	maxIndexBytes := len(RenderIndex(target))
	removed := trimIndexEntries(index, 10, maxIndexBytes)

	if len(removed) != 1 || removed[0].Title != "one" {
		t.Fatalf("removed = %#v, want only first entry", removed)
	}
	if len(index.Entries) != 2 || index.Entries[0].Title != "two" || index.Entries[1].Title != "three" {
		t.Fatalf("remaining entries = %#v", index.Entries)
	}
}

func TestTrimIndexEntriesPrefersLowestPriorityEntries(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "user", Source: SourceAutoExtract},
			{Type: TypeReference, Title: "reference", Source: SourceAutoExtract},
			{Type: TypeProject, Title: "project", Source: SourceUserManual},
		},
	}

	removed := trimIndexEntries(index, 2, 0)
	if len(removed) != 1 || removed[0].Title != "reference" {
		t.Fatalf("removed = %#v, want reference entry", removed)
	}
	if len(index.Entries) != 2 {
		t.Fatalf("len(index.Entries) = %d, want 2", len(index.Entries))
	}
}

func TestTrimIndexEntriesByBytesPrefersLowestPriorityEntries(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "keep-user", Content: "u", Source: SourceUserManual},
			{Type: TypeReference, Title: strings.Repeat("ref", 32), Content: strings.Repeat("payload", 64), Source: SourceAutoExtract},
			{Type: TypeProject, Title: "keep-project", Content: "p", Source: SourceAutoExtract},
		},
	}

	target := &Index{
		Entries: []Entry{
			index.Entries[0],
			index.Entries[2],
		},
	}
	maxIndexBytes := len(RenderIndex(target))
	removed := trimIndexEntries(index, 10, maxIndexBytes)

	if len(removed) != 1 || removed[0].Type != TypeReference {
		t.Fatalf("removed = %#v, want only low-priority reference entry", removed)
	}
	if len(index.Entries) != 2 {
		t.Fatalf("len(index.Entries) = %d, want 2", len(index.Entries))
	}
	if index.Entries[0].Title != "keep-user" || index.Entries[1].Title != "keep-project" {
		t.Fatalf("remaining entries = %#v, want keep-user and keep-project", index.Entries)
	}
	if got := len(RenderIndex(index)); got > maxIndexBytes {
		t.Fatalf("rendered index bytes = %d, want <= %d", got, maxIndexBytes)
	}
}

func TestFindEvictionVictimBranches(t *testing.T) {
	if got := findEvictionVictim(nil); got != -1 {
		t.Fatalf("findEvictionVictim(nil) = %d, want -1", got)
	}

	entries := []Entry{
		{Type: TypeReference, Title: "first", Source: SourceAutoExtract},
		{Type: TypeReference, Title: "second", Source: SourceAutoExtract},
		{Type: TypeProject, Title: "third", Source: SourceAutoExtract},
	}
	if got := findEvictionVictim(entries); got != 0 {
		t.Fatalf("findEvictionVictim() = %d, want 0 for first lowest-priority entry", got)
	}
}
