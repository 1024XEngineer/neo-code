package memo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
)

type stubMemoExtractor struct {
	mu        sync.Mutex
	callCount int
	calls     [][]providertypes.Message
	extractFn func(ctx context.Context, messages []providertypes.Message) ([]Entry, error)
}

func (s *stubMemoExtractor) Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
	s.mu.Lock()
	s.callCount++
	s.calls = append(s.calls, cloneProviderMessages(messages))
	extractFn := s.extractFn
	s.mu.Unlock()

	if extractFn != nil {
		return extractFn(ctx, messages)
	}
	return nil, nil
}

func (s *stubMemoExtractor) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

func newAutoExtractorTestService(t *testing.T) *Service {
	t.Helper()
	store := NewFileStore(t.TempDir(), t.TempDir())
	return NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, nil)
}

func TestAutoExtractorDebounceMergesRequests(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			last := renderMemoParts(messages[len(messages)-1].Parts)
			return []Entry{{Type: TypeProject, Title: last, Content: last, Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 20 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first")}}})
	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("second")}}})

	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })
	time.Sleep(60 * time.Millisecond)

	if extractor.Calls() != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractor.Calls())
	}

	recall, err := svc.Recall(context.Background(), "second")
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(recall) != 1 {
		t.Fatalf("recall = %#v", recall)
	}
	for _, content := range recall {
		if !strings.Contains(content, "second") {
			t.Fatalf("recall content = %q", content)
		}
	}
}

func TestAutoExtractorTrailingRun(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	firstStarted := make(chan struct{}, 1)
	secondStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})

	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			switch renderMemoParts(messages[len(messages)-1].Parts) {
			case "first":
				firstStarted <- struct{}{}
				<-releaseFirst
			case "second":
				secondStarted <- struct{}{}
			}
			last := renderMemoParts(messages[len(messages)-1].Parts)
			return []Entry{{Type: TypeProject, Title: last, Content: last, Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 15 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first")}}})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first extraction did not start")
	}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("second")}}})
	time.Sleep(40 * time.Millisecond)
	close(releaseFirst)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second trailing extraction did not start")
	}

	waitFor(t, time.Second, func() bool { return extractor.Calls() == 2 })
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background())
		return err == nil && len(entries) == 2
	})
}

func TestAutoExtractorErrorsAreSilent(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return nil, errors.New("boom")
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 10 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("x")}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	entries, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty", entries)
	}
}

func TestAutoExtractorSuppressesExactDuplicates(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	if err := svc.Add(context.Background(), Entry{
		Type:  TypeUser,
		Title: "reply in chinese", Content: "reply in chinese",
		Source: SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed Add() error = %v", err)
	}

	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{
				{Type: TypeUser, Title: "reply in chinese", Content: "reply in chinese", Source: SourceAutoExtract},
				{Type: TypeFeedback, Title: "run tests first", Content: "run tests first", Source: SourceAutoExtract},
				{Type: TypeFeedback, Title: "run tests first", Content: "run tests first", Source: SourceAutoExtract},
			}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 10 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("dedupe")}}})
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background())
		return err == nil && len(entries) == 2
	})

	entries, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestAutoExtractorSuppressesExactDuplicatesAcrossSessions(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			started <- struct{}{}
			<-release
			return []Entry{
				{Type: TypeProject, Title: "same title", Content: "same content", Source: SourceAutoExtract},
			}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 0
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("one")}}})
	auto.Schedule("session-2", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("two")}}})

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("concurrent extraction did not start")
		}
	}
	close(release)

	waitFor(t, time.Second, func() bool { return extractor.Calls() == 2 })
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background())
		return err == nil && len(entries) == 1
	})

	entries, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

func TestAutoExtractorRemovesIdleState(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "done", Content: "done", Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 5 * time.Millisecond
	auto.idleTTL = 20 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("cleanup")}}})

	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })
	waitFor(t, time.Second, func() bool {
		auto.mu.Lock()
		defer auto.mu.Unlock()
		return len(auto.states) == 0
	})
}

func TestAutoExtractorHandleIdleKeepsActiveState(t *testing.T) {
	auto := NewAutoExtractor(&stubMemoExtractor{}, newAutoExtractorTestService(t))
	auto.logf = func(string, ...any) {}

	state := &autoExtractState{
		idleSeq: 2,
		pending: &autoExtractRequest{
			messages: []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("keep")}}},
		},
	}

	auto.mu.Lock()
	auto.states["session-1"] = state
	auto.mu.Unlock()

	auto.handleIdle("session-1", state, 2)

	auto.mu.Lock()
	defer auto.mu.Unlock()
	if _, ok := auto.states["session-1"]; !ok {
		t.Fatal("active state should not be removed by idle callback")
	}
}

func TestAutoExtractorLoadsDedupIndexOutsideCurrentProcessState(t *testing.T) {
	baseDir := t.TempDir()
	workspace := t.TempDir()
	store := NewFileStore(baseDir, workspace)
	svc := NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, nil)
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed Add() error = %v", err)
	}

	reloaded := NewService(NewFileStore(baseDir, workspace), nil, config.MemoConfig{MaxIndexLines: 200}, nil)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{
				{Type: TypeUser, Title: "reply in chinese", Content: "reply in chinese", Source: SourceAutoExtract},
			}, nil
		},
	}
	auto := NewAutoExtractor(extractor, reloaded)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("dedupe after reload")}}})

	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })
	entries, err := reloaded.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

func TestAutoExtractorScheduleWithExtractorUsesBoundExtractor(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	defaultExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "default", Content: "default", Source: SourceAutoExtract}}, nil
		},
	}
	boundExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "bound", Content: "bound", Source: SourceAutoExtract}}, nil
		},
	}

	auto := NewAutoExtractor(defaultExtractor, svc)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.ScheduleWithExtractor("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("use bound")}}}, boundExtractor)

	waitFor(t, time.Second, func() bool { return boundExtractor.Calls() == 1 })
	if defaultExtractor.Calls() != 0 {
		t.Fatalf("default extractor calls = %d, want 0", defaultExtractor.Calls())
	}
}

func TestAutoExtractorScheduleGuardClauses(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{}
	auto := NewAutoExtractor(extractor, svc)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}

	auto.Schedule("", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("skip")}}})
	auto.ScheduleWithExtractor("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("skip")}}}, nil)

	waitFor(t, 150*time.Millisecond, func() bool { return true })
	if extractor.Calls() != 0 {
		t.Fatalf("extractor calls = %d, want 0", extractor.Calls())
	}
}

func TestAutoExtractDedupKeyAndTopicParsing(t *testing.T) {
	if key := autoExtractDedupKey(Entry{Type: TypeProject, Title: "  demo  ", Content: "  value  "}); key != "project\x1fdemo\x1fvalue" {
		t.Fatalf("autoExtractDedupKey() = %q", key)
	}
	if key := autoExtractDedupKey(Entry{Type: "invalid", Title: "demo", Content: "value"}); key != "" {
		t.Fatalf("autoExtractDedupKey() invalid type = %q, want empty", key)
	}

	source, body := parseTopicSourceAndContent("plain body")
	if source != "" || body != "plain body" {
		t.Fatalf("parse plain topic = (%q,%q)", source, body)
	}

	source, body = parseTopicSourceAndContent("---\nsource: extractor_auto\n---\n\n正文")
	if source != SourceAutoExtract || body != "正文" {
		t.Fatalf("parse frontmatter topic = (%q,%q)", source, body)
	}
}

func TestCloneProviderMessagesDeepCopyAndStopTimer(t *testing.T) {
	original := []providertypes.Message{
		{
			Role:  providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("msg")},
			ToolCalls: []providertypes.ToolCall{
				{ID: "c1", Name: "tool", Arguments: "{}"},
			},
			ToolMetadata: map[string]string{"k": "v"},
		},
	}
	cloned := cloneProviderMessages(original)
	if len(cloned) != 1 || len(cloned[0].ToolCalls) != 1 || cloned[0].ToolMetadata["k"] != "v" {
		t.Fatalf("cloneProviderMessages() = %#v", cloned)
	}

	original[0].ToolCalls[0].Name = "changed"
	original[0].ToolMetadata["k"] = "changed"
	if cloned[0].ToolCalls[0].Name != "tool" || cloned[0].ToolMetadata["k"] != "v" {
		t.Fatalf("clone should be isolated, got %#v", cloned[0])
	}

	stopTimer(nil)
	timer := time.NewTimer(5 * time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	stopTimer(timer)
}

func TestIsIdleStateLocked(t *testing.T) {
	state := &autoExtractState{idleSeq: 3}
	if !isIdleStateLocked(state, 3) {
		t.Fatal("expected idle state to be recyclable")
	}

	state.pending = &autoExtractRequest{}
	if isIdleStateLocked(state, 3) {
		t.Fatal("state with pending request should not be recyclable")
	}

	state.pending = nil
	state.running = true
	if isIdleStateLocked(state, 3) {
		t.Fatal("running state should not be recyclable")
	}
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
