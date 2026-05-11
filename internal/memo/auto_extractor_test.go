package memo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

type stubDecisionMemoExtractor struct {
	mu             sync.Mutex
	callCount      int
	candidates     []ExtractionCandidate
	extractEntries []Entry
	decisions      []ExtractionDecision
	err            error
}

func (s *stubDecisionMemoExtractor) Extract(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.extractEntries...), nil
}

func (s *stubDecisionMemoExtractor) ResolveDecision(
	ctx context.Context,
	candidate Entry,
	existing []ExtractionCandidate,
) (ExtractionDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	s.candidates = append([]ExtractionCandidate(nil), existing...)
	if s.err != nil {
		return ExtractionDecision{}, s.err
	}
	if len(s.decisions) == 0 {
		return ExtractionDecision{Action: ExtractionActionCreate, Entry: candidate}, nil
	}
	decision := s.decisions[0]
	s.decisions = append([]ExtractionDecision(nil), s.decisions[1:]...)
	return decision, nil
}

func newAutoExtractorTestService(t *testing.T) *Service {
	t.Helper()
	store := NewFileStore(t.TempDir(), t.TempDir())
	return NewService(store, testMemoConfig(), nil)
}

func registerAutoExtractorCleanup(t *testing.T, auto *AutoExtractor) {
	t.Helper()
	auto.idleTTL = 20 * time.Millisecond
	t.Cleanup(func() {
		waitFor(t, time.Second, func() bool {
			auto.mu.Lock()
			defer auto.mu.Unlock()
			return len(auto.states) == 0
		})
	})
}

func TestAutoExtractorDebounceMergesRequests(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			last := renderMemoParts(messages[len(messages)-1].Parts)
			return []Entry{{Type: TypeProject, Title: last, Content: last, Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 20 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first")}}})
	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("second")}}})

	waitFor(t, time.Second, func() bool {
		recall, err := svc.Recall(context.Background(), "second", ScopeAll)
		return err == nil && len(recall) == 1 && strings.Contains(recall[0].Content, "second")
	})
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
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 15 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

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
		t.Fatal("second extraction did not start")
	}

	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background(), ScopeProject)
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
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 10 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("x")}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	entries, err := svc.List(context.Background(), ScopeAll)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty", entries)
	}
}

func TestAutoExtractorProtocolMismatchIsDowngraded(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return nil, ErrExtractionNoJSONArray
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 10 * time.Millisecond
	logMessages := make(chan string, 2)
	auto.logf = func(format string, args ...any) {
		logMessages <- fmt.Sprintf(format, args...)
	}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{
		providertypes.NewTextPart("x"),
	}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	select {
	case message := <-logMessages:
		if !strings.Contains(message, "skipped (protocol_mismatch)") {
			t.Fatalf("expected protocol mismatch downgrade log, got %q", message)
		}
		if strings.Contains(message, "auto extract failed") {
			t.Fatalf("unexpected failure-level log for protocol mismatch: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("expected protocol mismatch log")
	}
}

func TestAutoExtractorSuppressesExactDuplicates(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceAutoExtract,
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
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 10 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("dedupe")}}})
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background(), ScopeAll)
		return err == nil && len(entries) == 2
	})
}

func TestAutoExtractorAppliesSemanticUpdateOnlyForAutoExtractedMemory(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeFeedback,
		Title:   "测试策略",
		Content: "用户要求修改后先跑测试。",
		Source:  SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed auto Add() error = %v", err)
	}
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "语言偏好",
		Content: "用户手动保存偏好中文回复。",
		Source:  SourceUserManual,
	}); err != nil {
		t.Fatalf("seed manual Add() error = %v", err)
	}

	candidates, err := svc.autoExtractionCandidates(context.Background())
	if err != nil {
		t.Fatalf("autoExtractionCandidates() error = %v", err)
	}
	var autoRef, manualRef string
	for _, candidate := range candidates {
		switch candidate.Source {
		case SourceAutoExtract:
			autoRef = candidate.Ref
		case SourceUserManual:
			manualRef = candidate.Ref
		}
	}
	if autoRef == "" || manualRef == "" {
		t.Fatalf("expected refs, auto=%q manual=%q candidates=%+v", autoRef, manualRef, candidates)
	}

	extractor := &stubDecisionMemoExtractor{
		extractEntries: []Entry{
			{Type: TypeFeedback, Title: "测试策略", Content: "用户要求修改后先跑相关测试。"},
		},
		decisions: []ExtractionDecision{
			{
				Action: ExtractionActionUpdate,
				Ref:    autoRef,
				Entry: Entry{
					Title:    "测试策略",
					Content:  "用户要求修改后先跑相关测试。",
					Keywords: []string{"test"},
				},
			},
			{
				Action: ExtractionActionUpdate,
				Ref:    manualRef,
				Entry: Entry{
					Title:   "语言偏好",
					Content: "不应覆盖手动记忆。",
				},
			},
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("更新测试策略")}}})
	waitFor(t, time.Second, func() bool {
		extractor.mu.Lock()
		defer extractor.mu.Unlock()
		return extractor.callCount == 1
	})

	recall, err := svc.Recall(context.Background(), "测试策略", ScopeProject)
	if err != nil {
		t.Fatalf("Recall(auto) error = %v", err)
	}
	if len(recall) != 1 || !strings.Contains(recall[0].Content, "相关测试") {
		t.Fatalf("expected auto memory to be updated, got %+v", recall)
	}
	manualRecall, err := svc.Recall(context.Background(), "语言偏好", ScopeUser)
	if err != nil {
		t.Fatalf("Recall(manual) error = %v", err)
	}
	if len(manualRecall) != 1 || strings.Contains(manualRecall[0].Content, "不应覆盖") {
		t.Fatalf("manual memory should not be overwritten, got %+v", manualRecall)
	}
	if len(extractor.candidates) != 1 || extractor.candidates[0].Ref != autoRef {
		t.Fatalf("expected shortlist to target the auto-extracted memory, got %+v", extractor.candidates)
	}
}

func TestAutoExtractorSemanticCreateStillUsesExactDedup(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "中文回复",
		Content: "用户偏好中文回复。",
		Source:  SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed Add() error = %v", err)
	}

	extractor := &stubDecisionMemoExtractor{
		extractEntries: []Entry{
			{Type: TypeUser, Title: "中文回复", Content: "用户偏好中文回复。"},
			{Type: TypeProject, Title: "新事实", Content: "项目需要语义去重。"},
		},
		decisions: []ExtractionDecision{
			{Action: ExtractionActionCreate, Entry: Entry{Type: TypeUser, Title: "中文回复", Content: "用户偏好中文回复。"}},
			{Action: ExtractionActionCreate, Entry: Entry{Type: TypeProject, Title: "新事实", Content: "项目需要语义去重。"}},
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("dedupe")}}})
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background(), ScopeAll)
		return err == nil && len(entries) == 2
	})
}

func TestAutoExtractorUsesTimeoutContext(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	auto := NewAutoExtractor(extractor, svc, 20*time.Millisecond)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("timeout")}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	entries, err := svc.List(context.Background(), ScopeAll)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after timeout = %#v, want empty", entries)
	}
}

func TestAutoExtractorRemovesIdleState(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "done", Content: "done", Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.idleTTL = 20 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("cleanup")}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })
	waitFor(t, time.Second, func() bool {
		auto.mu.Lock()
		defer auto.mu.Unlock()
		return len(auto.states) == 0
	})
}

func TestAutoExtractorLoadsDedupIndexOutsideCurrentProcessState(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileStore(baseDir, "/workspace/a")
	svc := NewService(store, testMemoConfig(), nil)
	if err := svc.Add(context.Background(), Entry{
		Type:    TypeUser,
		Title:   "reply in chinese",
		Content: "reply in chinese",
		Source:  SourceAutoExtract,
	}); err != nil {
		t.Fatalf("seed Add() error = %v", err)
	}

	reloaded := NewService(NewFileStore(baseDir, "/workspace/b"), testMemoConfig(), nil)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeUser, Title: "reply in chinese", Content: "reply in chinese", Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, reloaded, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	auto.Schedule("session-1", []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("dedupe after reload")}}})
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	entries, err := reloaded.List(context.Background(), ScopeAll)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

func TestAutoExtractorFailedRunDoesNotAdvanceFingerprint(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	var attemptMu sync.Mutex
	attempt := 0
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			attemptMu.Lock()
			defer attemptMu.Unlock()
			attempt++
			if attempt == 1 {
				return nil, errors.New("boom")
			}
			return []Entry{{Type: TypeProject, Title: "retry", Content: "retry", Source: SourceAutoExtract}}, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	messages := []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("same payload")}}}
	auto.Schedule("session-1", messages)
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 1 })

	auto.Schedule("session-1", messages)
	waitFor(t, time.Second, func() bool { return extractor.Calls() == 2 })
	waitFor(t, time.Second, func() bool {
		entries, err := svc.List(context.Background(), ScopeProject)
		return err == nil && len(entries) == 1
	})
}

func TestAutoExtractorHandleDebounceSkipsDuplicateFingerprint(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	extractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			t.Fatal("duplicate fingerprint should skip extractor invocation")
			return nil, nil
		},
	}
	auto := NewAutoExtractor(extractor, svc, time.Second)
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("same payload")}},
	}
	state := auto.ensureState("session-1")
	state.mu.Lock()
	state.pending = &autoExtractRequest{
		messages:  cloneProviderMessages(messages),
		dueAt:     time.Now().Add(-time.Millisecond),
		extractor: extractor,
	}
	state.scheduleSeq = 1
	state.lastFingerprint = computeMessageFingerprint(messages)
	state.mu.Unlock()

	auto.handleDebounce("session-1", state, 1)

	waitFor(t, time.Second, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return !state.running && state.pending == nil && state.idleTimer != nil
	})
	if extractor.Calls() != 0 {
		t.Fatalf("extractor call count = %d, want 0", extractor.Calls())
	}
}

func TestAutoExtractorScheduleWithExtractorUsesCallScopedExtractorAndSkipsSuccessfulDuplicates(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	defaultExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			t.Fatal("default extractor should not be used for call-scoped scheduling")
			return nil, nil
		},
	}
	firstExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "deduped", Content: "deduped", Source: SourceAutoExtract}}, nil
		},
	}
	secondExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			t.Fatal("duplicate fingerprint should short-circuit before second extractor runs")
			return nil, nil
		},
	}

	auto := NewAutoExtractor(defaultExtractor, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("same payload")}},
	}
	fingerprint := computeMessageFingerprint(messages)

	auto.ScheduleWithExtractor("session-1", messages, firstExtractor)
	waitFor(t, time.Second, func() bool { return firstExtractor.Calls() == 1 })
	waitFor(t, time.Second, func() bool {
		auto.mu.Lock()
		state := auto.states["session-1"]
		auto.mu.Unlock()
		if state == nil {
			return false
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		return !state.running && state.pending == nil && state.lastFingerprint == fingerprint
	})

	auto.ScheduleWithExtractor("session-1", messages, secondExtractor)
	waitFor(t, time.Second, func() bool {
		auto.mu.Lock()
		state := auto.states["session-1"]
		auto.mu.Unlock()
		if state == nil {
			return false
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		return !state.running && state.pending == nil && state.lastFingerprint == fingerprint && state.idleTimer != nil
	})

	if defaultExtractor.Calls() != 0 {
		t.Fatalf("default extractor call count = %d, want 0", defaultExtractor.Calls())
	}
	if secondExtractor.Calls() != 0 {
		t.Fatalf("second extractor call count = %d, want 0", secondExtractor.Calls())
	}

	entries, err := svc.List(context.Background(), ScopeProject)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
}

func TestAutoExtractorFingerprintIncludesNonTextParts(t *testing.T) {
	svc := newAutoExtractorTestService(t)
	firstExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "first-image", Content: "first-image", Source: SourceAutoExtract}}, nil
		},
	}
	secondExtractor := &stubMemoExtractor{
		extractFn: func(ctx context.Context, messages []providertypes.Message) ([]Entry, error) {
			return []Entry{{Type: TypeProject, Title: "second-image", Content: "second-image", Source: SourceAutoExtract}}, nil
		},
	}

	auto := NewAutoExtractor(nil, svc, time.Second)
	auto.debounce = 5 * time.Millisecond
	auto.logf = func(string, ...any) {}
	registerAutoExtractorCleanup(t, auto)

	firstMessages := []providertypes.Message{{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("same text"),
			providertypes.NewRemoteImagePart("https://example.com/first.png"),
		},
	}}
	secondMessages := []providertypes.Message{{
		Role: providertypes.RoleUser,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("same text"),
			providertypes.NewRemoteImagePart("https://example.com/second.png"),
		},
	}}

	auto.ScheduleWithExtractor("session-1", firstMessages, firstExtractor)
	waitFor(t, time.Second, func() bool { return firstExtractor.Calls() == 1 })

	auto.ScheduleWithExtractor("session-1", secondMessages, secondExtractor)
	waitFor(t, time.Second, func() bool { return secondExtractor.Calls() == 1 })

	entries, err := svc.List(context.Background(), ScopeProject)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestStopTimerDrainsExpiredTimer(t *testing.T) {
	timer := time.NewTimer(5 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	stopTimer(timer)

	select {
	case <-timer.C:
		t.Fatal("expected stopTimer to drain expired timer channel")
	default:
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
