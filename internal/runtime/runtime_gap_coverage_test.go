package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func TestCompactValidationAndLoadErrorBranches(t *testing.T) {
	t.Parallel()

	service := &Service{
		configManager: newRuntimeConfigManager(t),
		sessionStore:  newMemoryStore(),
		events:        make(chan RuntimeEvent, 16),
		sessionLocks:  make(map[string]*sessionLockEntry),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Compact(ctx, CompactInput{SessionID: "s-1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	if _, err := service.Compact(context.Background(), CompactInput{}); err == nil {
		t.Fatalf("expected empty session id error")
	}

	if _, err := service.Compact(context.Background(), CompactInput{SessionID: "missing"}); err == nil {
		t.Fatalf("expected load error for missing session")
	}
}

func TestRunCompactForSessionPolicyBranches(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-compact-policy")
	session.Messages = []providertypes.Message{{Role: providertypes.RoleUser, Content: "before"}}

	service := &Service{
		events: make(chan RuntimeEvent, 32),
	}

	// default runner init failure: strict returns error
	if _, _, err := service.runCompactForSession(context.Background(), "run-compact", session, config.Config{}, contextcompact.ModeManual, compactErrorStrict); err == nil {
		t.Fatalf("expected strict mode to return default runner error")
	}

	// default runner init failure: best effort swallows error
	if gotSession, gotResult, err := service.runCompactForSession(context.Background(), "run-compact", session, config.Config{}, contextcompact.ModeManual, compactErrorBestEffort); err != nil {
		t.Fatalf("expected best effort to swallow runner init error, got %v", err)
	} else if gotResult.Applied || len(gotSession.Messages) != len(session.Messages) {
		t.Fatalf("expected noop result in best effort mode")
	}

	service.compactRunner = &stubCompactRunner{err: errors.New("runner failed")}
	if _, _, err := service.runCompactForSession(context.Background(), "run-compact", session, config.Config{}, contextcompact.ModeManual, compactErrorStrict); err == nil {
		t.Fatalf("expected strict mode to return runner error")
	}
	if _, _, err := service.runCompactForSession(context.Background(), "run-compact", session, config.Config{}, contextcompact.ModeManual, compactErrorBestEffort); err != nil {
		t.Fatalf("expected best effort to swallow runner error, got %v", err)
	}
}

func TestRunCompactForSessionSaveErrorPolicyBranches(t *testing.T) {
	t.Parallel()

	baseStore := newMemoryStore()
	session := newRuntimeSession("session-compact-save-error")
	session.Messages = []providertypes.Message{{Role: providertypes.RoleUser, Content: "before"}}
	baseStore.sessions[session.ID] = cloneSession(session)

	store := &failingStore{Store: baseStore, saveErr: errors.New("save failed"), failOnSave: 1, ignoreContextErr: true}
	service := &Service{
		sessionStore: store,
		events:       make(chan RuntimeEvent, 16),
		compactRunner: &stubCompactRunner{result: contextcompact.Result{
			Applied:  true,
			Messages: []providertypes.Message{{Role: providertypes.RoleAssistant, Content: "after"}},
		}},
	}

	strictSession, _, err := service.runCompactForSession(context.Background(), "run-compact-save", session, config.Config{}, contextcompact.ModeManual, compactErrorStrict)
	if err == nil {
		t.Fatalf("expected strict mode save error")
	}
	if strictSession.Messages[0].Content != "before" {
		t.Fatalf("expected strict mode to rollback messages, got %+v", strictSession.Messages)
	}

	store.saveCalls = 0
	bestEffortSession, bestEffortResult, err := service.runCompactForSession(context.Background(), "run-compact-save", session, config.Config{}, contextcompact.ModeManual, compactErrorBestEffort)
	if err != nil {
		t.Fatalf("expected best effort to swallow save error, got %v", err)
	}
	if bestEffortResult.Applied {
		t.Fatalf("expected empty compact result on best effort save failure")
	}
	if bestEffortSession.Messages[0].Content != "before" {
		t.Fatalf("expected best effort rollback messages, got %+v", bestEffortSession.Messages)
	}
}

func TestCompactProviderSelectionErrorBranches(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	cfg := manager.Get()
	session := newRuntimeSession("session-provider-select")
	session.Provider = "missing-provider"
	session.Model = "m1"
	if _, _, err := resolveCompactProviderSelection(session, cfg); err == nil {
		t.Fatalf("expected provider not found error")
	}

	cfg.SelectedProvider = ""
	if _, _, err := resolveCompactProviderSelection(agentsession.Session{}, cfg); err == nil {
		t.Fatalf("expected selected provider empty error")
	}

	service := &Service{providerFactory: &scriptedProviderFactory{provider: &scriptedProvider{}}}
	if _, err := service.defaultCompactRunner(agentsession.Session{}, config.Config{}); err == nil {
		t.Fatalf("expected defaultCompactRunner to fail on invalid config")
	}
}

func TestCompactSummaryGeneratorErrorBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &compactSummaryGenerator{}
	if _, err := g.Generate(ctx, contextcompact.SummaryInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}

	if _, err := (&compactSummaryGenerator{}).Generate(context.Background(), contextcompact.SummaryInput{}); err == nil {
		t.Fatalf("expected nil provider factory error")
	}

	if _, err := (&compactSummaryGenerator{providerFactory: &scriptedProviderFactory{provider: &scriptedProvider{}}}).Generate(context.Background(), contextcompact.SummaryInput{}); err == nil {
		t.Fatalf("expected incomplete provider config error")
	}

	g = &compactSummaryGenerator{
		providerFactory: &scriptedProviderFactory{err: errors.New("build failed")},
		providerConfig:  provider.RuntimeConfig{Name: "openai", Driver: "openai", BaseURL: "https://example.com", APIKey: "k"},
	}
	if _, err := g.Generate(context.Background(), contextcompact.SummaryInput{}); err == nil {
		t.Fatalf("expected provider build error")
	}

	g = &compactSummaryGenerator{
		providerFactory: &scriptedProviderFactory{provider: &scriptedProvider{streams: [][]providertypes.StreamEvent{{providertypes.NewTextDeltaStreamEvent("   ")}}}},
		providerConfig:  provider.RuntimeConfig{Name: "openai", Driver: "openai", BaseURL: "https://example.com", APIKey: "k"},
	}
	if _, err := g.Generate(context.Background(), contextcompact.SummaryInput{}); err == nil {
		t.Fatalf("expected empty summary error")
	}
}

func TestRuntimeLoadOrCreateAndEmitBranches(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	service := &Service{configManager: manager, sessionStore: newMemoryStore(), events: make(chan RuntimeEvent, 1), sessionLocks: map[string]*sessionLockEntry{}}

	if _, err := service.loadOrCreateSession(context.Background(), "", "title", t.TempDir(), "missing/subdir"); err == nil {
		t.Fatalf("expected new session workdir resolve error")
	}

	if _, err := service.loadOrCreateSession(context.Background(), "not-found", "title", t.TempDir(), ""); err == nil {
		t.Fatalf("expected load existing session error")
	}

	store := newMemoryStore()
	session := newRuntimeSession("session-load-update")
	session.Workdir = t.TempDir()
	store.sessions[session.ID] = cloneSession(session)
	service.sessionStore = store
	if _, err := service.loadOrCreateSession(context.Background(), session.ID, "title", t.TempDir(), "missing/child"); err == nil {
		t.Fatalf("expected resolve error for requested workdir")
	}

	store.saves = 0
	if got, err := service.loadOrCreateSession(context.Background(), session.ID, "title", t.TempDir(), "."); err != nil {
		t.Fatalf("loadOrCreateSession same-workdir error = %v", err)
	} else if got.Workdir != session.Workdir || store.saves != 0 {
		t.Fatalf("expected same workdir path to skip save, got workdir=%q saves=%d", got.Workdir, store.saves)
	}

	fStore := &failingStore{Store: store, saveErr: errors.New("save failed"), failOnSave: 1, ignoreContextErr: true}
	service.sessionStore = fStore
	if _, err := service.loadOrCreateSession(context.Background(), session.ID, "title", t.TempDir(), ".."); err == nil {
		t.Fatalf("expected save error when updating session workdir")
	}

	service.events <- RuntimeEvent{Type: EventAgentChunk}
	done := make(chan struct{})
	go func() {
		<-time.After(10 * time.Millisecond)
		<-service.events
		close(done)
	}()
	if err := service.emit(context.Background(), EventAgentDone, "run", "session", "ok"); err != nil {
		t.Fatalf("emit() error = %v", err)
	}
	<-done

	canceled := make([]string, 0, 2)
	token1 := service.startRun(func() { canceled = append(canceled, "run-1") })
	token2 := service.startRun(func() { canceled = append(canceled, "run-2") })
	service.finishRun(token1)
	if service.activeRunToken != token2 {
		t.Fatalf("expected finishRun with stale token to keep active run")
	}

	if !service.CancelActiveRun() {
		t.Fatalf("expected cancel latest active run")
	}
	if len(canceled) != 1 || canceled[0] != "run-2" {
		t.Fatalf("expected latest run canceled first, got %v", canceled)
	}

	service.finishRun(token2)
	if service.CancelActiveRun() {
		t.Fatalf("expected no active run after latest run finished")
	}
	if len(canceled) != 1 || canceled[0] != "run-2" {
		t.Fatalf("expected only run-2 canceled, got %v", canceled)
	}

	service.finishRun(token1)
	if service.CancelActiveRun() {
		t.Fatalf("expected no active run after all runs finished")
	}
}

func TestPermissionHelperAndAwaitBranches(t *testing.T) {
	t.Parallel()

	if status := permissionResolutionStatus(string(security.DecisionAsk)); status != permissionResolvedRejected {
		t.Fatalf("expected ask to map rejected, got %q", status)
	}
	if category := permissionToolCategory(security.Action{Type: security.ActionTypeWrite, Payload: security.ActionPayload{Resource: "filesystem_write"}}); category != permissionToolCategoryFilesystemWrite {
		t.Fatalf("expected filesystem write category, got %q", category)
	}
	if category := permissionToolCategory(security.Action{Payload: security.ActionPayload{ToolName: "fallback_tool"}}); category != "fallback_tool" {
		t.Fatalf("expected tool_name fallback, got %q", category)
	}

	service := &Service{approvalBroker: nil, events: make(chan RuntimeEvent, 8)}
	permissionErr := permissionDecisionAskError(t)
	_, _, err := service.awaitPermissionDecision(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}}, permissionErr)
	if err == nil {
		t.Fatalf("expected open error from nil approval broker")
	}

	service.approvalBroker = approvalflow.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = service.awaitPermissionDecision(ctx, permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}}, permissionErr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled while waiting permission, got %v", err)
	}
}

func TestAcquireSessionLockReleasesAndCleansUp(t *testing.T) {
	t.Parallel()

	service := &Service{}
	lock1, release1 := service.acquireSessionLock("session-1")
	lock2, release2 := service.acquireSessionLock("session-1")
	if lock1 != lock2 {
		t.Fatalf("expected same mutex instance for same session")
	}
	if got := service.sessionLocks["session-1"].refs; got != 2 {
		t.Fatalf("expected refs=2, got %d", got)
	}

	lock1.Lock()
	lock1.Unlock()
	release1()
	if got := service.sessionLocks["session-1"].refs; got != 1 {
		t.Fatalf("expected refs=1 after first release, got %d", got)
	}

	release2()
	if len(service.sessionLocks) != 0 {
		t.Fatalf("expected session lock entry to be cleaned up")
	}
}

func permissionDecisionAskError(t *testing.T) *tools.PermissionDecisionError {
	t.Helper()

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})
	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{{
		ID:       "ask-read",
		Type:     security.ActionTypeRead,
		Resource: "filesystem_read_file",
		Decision: security.DecisionAsk,
	}})
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	manager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}
	_, execErr := manager.Execute(context.Background(), tools.ToolCallInput{
		ID:        "call-ask",
		Name:      "filesystem_read_file",
		Arguments: []byte(`{"path":"README.md"}`),
		Workdir:   t.TempDir(),
		SessionID: "session-ask",
	})
	var permissionErr *tools.PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		t.Fatalf("expected PermissionDecisionError, got %v", execErr)
	}
	return permissionErr
}
