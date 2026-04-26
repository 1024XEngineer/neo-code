package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"

	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/streaming"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

type callbackToolManager struct {
	executeFn   func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error)
	rememberErr error
}

func (m *callbackToolManager) ListAvailableSpecs(ctx context.Context, input tools.SpecListInput) ([]providertypes.ToolSpec, error) {
	return nil, ctx.Err()
}

func (m *callbackToolManager) MicroCompactPolicy(name string) tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (m *callbackToolManager) MicroCompactSummarizer(name string) tools.ContentSummarizer {
	return nil
}

func (m *callbackToolManager) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, input)
	}
	return tools.ToolResult{Name: input.Name}, nil
}

func (m *callbackToolManager) RememberSessionDecision(sessionID string, action security.Action, scope tools.SessionPermissionScope) error {
	return m.rememberErr
}

type saveHookStore struct {
	base     *memoryStore
	saveHook func()
}

func (s *saveHookStore) beforeSave(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.saveHook != nil {
		s.saveHook()
	}
	return nil
}

type postSaveHookStore struct {
	base     *memoryStore
	saveHook func()
	once     sync.Once
}

func (s *postSaveHookStore) afterSave(err error) error {
	if err == nil && s.saveHook != nil {
		s.once.Do(s.saveHook)
	}
	return err
}

func (s *saveHookStore) CreateSession(ctx context.Context, input agentsession.CreateSessionInput) (agentsession.Session, error) {
	if err := s.beforeSave(ctx); err != nil {
		return agentsession.Session{}, err
	}
	return s.base.CreateSession(ctx, input)
}

func (s *saveHookStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return s.base.LoadSession(ctx, id)
}

func (s *saveHookStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	return s.base.ListSummaries(ctx)
}

func (s *saveHookStore) AppendMessages(ctx context.Context, input agentsession.AppendMessagesInput) error {
	if err := s.beforeSave(ctx); err != nil {
		return err
	}
	return s.base.AppendMessages(ctx, input)
}

// UpdateSessionWorkdir 在写前注入回调，再转发给底层内存 store。
func (s *saveHookStore) UpdateSessionWorkdir(ctx context.Context, input agentsession.UpdateSessionWorkdirInput) error {
	if err := s.beforeSave(ctx); err != nil {
		return err
	}
	return s.base.UpdateSessionWorkdir(ctx, input)
}

func (s *saveHookStore) UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error {
	if err := s.beforeSave(ctx); err != nil {
		return err
	}
	return s.base.UpdateSessionState(ctx, input)
}

func (s *saveHookStore) ReplaceTranscript(ctx context.Context, input agentsession.ReplaceTranscriptInput) error {
	if err := s.beforeSave(ctx); err != nil {
		return err
	}
	return s.base.ReplaceTranscript(ctx, input)
}

func (s *postSaveHookStore) CreateSession(ctx context.Context, input agentsession.CreateSessionInput) (agentsession.Session, error) {
	session, err := s.base.CreateSession(ctx, input)
	return session, s.afterSave(err)
}

func (s *postSaveHookStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return s.base.LoadSession(ctx, id)
}

func (s *postSaveHookStore) ListSummaries(ctx context.Context) ([]agentsession.Summary, error) {
	return s.base.ListSummaries(ctx)
}

func (s *postSaveHookStore) AppendMessages(ctx context.Context, input agentsession.AppendMessagesInput) error {
	return s.afterSave(s.base.AppendMessages(ctx, input))
}

// UpdateSessionWorkdir 在底层写入完成后执行一次 post-save 钩子。
func (s *postSaveHookStore) UpdateSessionWorkdir(ctx context.Context, input agentsession.UpdateSessionWorkdirInput) error {
	return s.afterSave(s.base.UpdateSessionWorkdir(ctx, input))
}

func (s *postSaveHookStore) UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error {
	return s.afterSave(s.base.UpdateSessionState(ctx, input))
}

func (s *postSaveHookStore) ReplaceTranscript(ctx context.Context, input agentsession.ReplaceTranscriptInput) error {
	return s.afterSave(s.base.ReplaceTranscript(ctx, input))
}

func (s *postSaveHookStore) CleanupExpiredSessions(ctx context.Context, maxAge time.Duration) (int, error) {
	return 0, nil
}

func TestResolveCompactProviderSelectionResolveErrorBranch(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	cfg := manager.Get()
	providerCfg, err := cfg.ProviderByName(cfg.SelectedProvider)
	if err != nil {
		t.Fatalf("ProviderByName() error = %v", err)
	}
	apiEnv := providerCfg.APIKeyEnv
	restoreRuntimeEnv(t, apiEnv)
	_ = os.Unsetenv(apiEnv)

	session := agentsession.Session{Provider: cfg.SelectedProvider, Model: "m1"}
	resolved, _, err := resolveCompactProviderSelection(session, cfg)
	if err != nil {
		t.Fatalf("resolveCompactProviderSelection() error = %v", err)
	}
	runtimeConfig, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if _, err := runtimeConfig.ResolveAPIKeyValue(); err == nil {
		t.Fatalf("expected resolve API key error")
	}
}

func TestResolveCompactProviderSelectionSessionBranchInjectsRuntimeAssetLimits(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Runtime.Assets.MaxSessionAssetBytes = 1024
		cfg.Runtime.Assets.MaxSessionAssetsTotalBytes = 4096
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	cfg := manager.Get()

	resolved, model, err := resolveCompactProviderSelection(agentsession.Session{
		Provider: cfg.SelectedProvider,
		Model:    "session-model",
	}, cfg)
	if err != nil {
		t.Fatalf("resolveCompactProviderSelection() error = %v", err)
	}
	if model != "session-model" {
		t.Fatalf("expected model=session-model, got %q", model)
	}

	expectedPolicy := cfg.Runtime.ResolveSessionAssetPolicy()
	expectedBudget := cfg.Runtime.ResolveRequestAssetBudget()
	if resolved.SessionAssetPolicy.MaxSessionAssetBytes != expectedPolicy.MaxSessionAssetBytes {
		t.Fatalf(
			"expected MaxSessionAssetBytes=%d, got %d",
			expectedPolicy.MaxSessionAssetBytes,
			resolved.SessionAssetPolicy.MaxSessionAssetBytes,
		)
	}
	if resolved.RequestAssetBudget.MaxSessionAssetsTotalBytes != expectedBudget.MaxSessionAssetsTotalBytes {
		t.Fatalf(
			"expected MaxSessionAssetsTotalBytes=%d, got %d",
			expectedBudget.MaxSessionAssetsTotalBytes,
			resolved.RequestAssetBudget.MaxSessionAssetsTotalBytes,
		)
	}
}

func TestGenerateStreamingMessageHooksAndContextDoneBranches(t *testing.T) {
	t.Parallel()

	onDoneCalled := false
	providerOK := &scriptedProvider{streams: [][]providertypes.StreamEvent{{providertypes.NewTextDeltaStreamEvent("ok")}}}
	outcome := generateStreamingMessage(context.Background(), providerOK, providertypes.GenerateRequest{}, streaming.Hooks{
		OnMessageDone: func(payload providertypes.MessageDonePayload) {
			onDoneCalled = true
		},
	})
	if outcome.err != nil {
		t.Fatalf("generateStreamingMessage() error = %v", outcome.err)
	}
	if !onDoneCalled {
		t.Fatalf("expected OnMessageDone hook to be invoked")
	}

	ctx, cancel := context.WithCancel(context.Background())
	providerWait := &scriptedProvider{chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	outcome = generateStreamingMessage(ctx, providerWait, providertypes.GenerateRequest{}, streaming.Hooks{})
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", outcome.err)
	}
}

func TestGenerateStreamingMessageDrainEventsAfterContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	providerIgnoreCancel := &scriptedProvider{chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
		<-ctx.Done()
		for i := 0; i < 64; i++ {
			events <- providertypes.NewTextDeltaStreamEvent(fmt.Sprintf("delta-%d", i))
		}
		return context.Canceled
	}}

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	done := make(chan streamGenerateResult, 1)
	go func() {
		done <- generateStreamingMessage(ctx, providerIgnoreCancel, providertypes.GenerateRequest{}, streaming.Hooks{})
	}()

	select {
	case outcome := <-done:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("generateStreamingMessage blocked after context cancellation")
	}
}

func TestPrepareTurnBudgetSnapshotErrorBranches(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	service := &Service{
		configManager: manager,
		contextBuilder: &stubContextBuilder{buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{}, errors.New("build failed")
		}},
		toolManager: &stubToolManager{},
	}
	state := newRunState("run-snapshot", newRuntimeSession("session-snapshot"))
	if _, _, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err == nil {
		t.Fatalf("expected build error")
	}

	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = ""
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	service.contextBuilder = &stubContextBuilder{buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
		return agentcontext.BuildResult{Messages: input.Messages}, nil
	}}
	if _, _, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err == nil {
		t.Fatalf("expected resolve selected provider error")
	}
}

func TestApplyCompactForStateStrictErrorBranch(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-apply-compact", newRuntimeSession("session-apply-compact"))
	if _, err := service.applyCompactForState(context.Background(), &state, config.Config{}, contextcompact.ModeManual, compactErrorStrict); err == nil {
		t.Fatalf("expected strict compact error")
	}
}

func TestApplyCompactForStateDoesNotIncreaseCompactCountWhenNotApplied(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 8),
		compactRunner: &stubCompactRunner{
			result: contextcompact.Result{
				Applied: false,
			},
		},
	}
	state := newRunState("run-apply-compact-not-applied", newRuntimeSession("session-apply-compact-not-applied"))
	state.compactCount = 1
	if err := service.setBaseRunState(context.Background(), &state, controlplane.RunStatePlan); err != nil {
		t.Fatalf("set base run state: %v", err)
	}

	applied, err := service.applyCompactForState(context.Background(), &state, config.Config{}, contextcompact.ModeProactive, compactErrorStrict)
	if err != nil {
		t.Fatalf("applyCompactForState() error = %v", err)
	}
	if applied {
		t.Fatalf("expected applied=false when compact runner result is not applied")
	}
	if state.compactCount != 1 {
		t.Fatalf("expected compactCount to stay 1 when compact not applied, got %d", state.compactCount)
	}
}

func TestExecuteToolCallWithPermissionRemainingBranches(t *testing.T) {
	t.Parallel()

	askErr := permissionDecisionAskError(t)

	t.Run("emit chunk reads canceled context", func(t *testing.T) {
		t.Parallel()
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			if err := input.EmitChunk([]byte("x")); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected canceled emit chunk, got %v", err)
			}
			return tools.ToolResult{Name: input.Name}, context.Canceled
		}}
		service := &Service{toolManager: manager, approvalBroker: approvalflow.NewBroker(), events: make(chan RuntimeEvent, 8)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _ = service.executeToolCallWithPermission(ctx, permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
	})

	t.Run("await permission decision error path", func(t *testing.T) {
		t.Parallel()
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name}, askErr
		}}
		service := &Service{toolManager: manager, approvalBroker: nil, events: make(chan RuntimeEvent, 8)}
		_, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
		if err == nil {
			t.Fatalf("expected await permission error")
		}
	})

	t.Run("invalid decision remember scope error", func(t *testing.T) {
		t.Parallel()
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name}, askErr
		}}
		service := &Service{toolManager: manager, approvalBroker: approvalflow.NewBroker(), events: make(chan RuntimeEvent, 16)}
		go func() {
			for evt := range service.events {
				if !isPermissionRequestEvent(evt.Type) {
					continue
				}
				payload := evt.Payload.(PermissionRequestPayload)
				_ = service.approvalBroker.Resolve(payload.RequestID, approvalflow.Decision("invalid"))
				return
			}
		}()
		_, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
		if err == nil {
			t.Fatalf("expected invalid decision error")
		}
	})

	t.Run("reject remember decision error", func(t *testing.T) {
		t.Parallel()
		manager := &callbackToolManager{
			executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
				return tools.ToolResult{Name: input.Name}, askErr
			},
			rememberErr: errors.New("remember failed"),
		}
		service := &Service{toolManager: manager, approvalBroker: approvalflow.NewBroker(), events: make(chan RuntimeEvent, 16)}
		go resolveDecisionFromEvent(service, approvalflow.DecisionReject)
		_, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
		if err == nil {
			t.Fatalf("expected remember reject error")
		}
	})

	t.Run("allow remember decision error", func(t *testing.T) {
		t.Parallel()
		manager := &callbackToolManager{
			executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
				return tools.ToolResult{Name: input.Name}, askErr
			},
			rememberErr: errors.New("remember failed"),
		}
		service := &Service{toolManager: manager, approvalBroker: approvalflow.NewBroker(), events: make(chan RuntimeEvent, 16)}
		go resolveDecisionFromEvent(service, approvalflow.DecisionAllowSession)
		_, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
		if err == nil {
			t.Fatalf("expected remember allow error")
		}
	})

	t.Run("non-ask deny emits fallback reason", func(t *testing.T) {
		t.Parallel()
		denyErr := permissionDecisionDenyError(t)
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name}, denyErr
		}}
		service := &Service{toolManager: manager, approvalBroker: approvalflow.NewBroker(), events: make(chan RuntimeEvent, 16)}
		_, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{RunID: "r", SessionID: "s", Call: providertypes.ToolCall{ID: "c", Name: "filesystem_read_file"}, ToolTimeout: time.Second})
		if err == nil {
			t.Fatalf("expected deny error")
		}
		events := collectRuntimeEvents(service.events)
		found := false
		for _, evt := range events {
			if evt.Type != EventPermissionResolved {
				continue
			}
			payload := evt.Payload.(PermissionResolvedPayload)
			if payload.Reason != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected permission resolved event with fallback reason")
		}
	})
}

func resolveDecisionFromEvent(service *Service, decision approvalflow.Decision) {
	for evt := range service.events {
		if !isPermissionRequestEvent(evt.Type) {
			continue
		}
		payload := evt.Payload.(PermissionRequestPayload)
		_ = service.approvalBroker.Resolve(payload.RequestID, decision)
		return
	}
}

func permissionDecisionDenyError(t *testing.T) *tools.PermissionDecisionError {
	t.Helper()

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "ok"})
	engine, err := security.NewStaticGateway(security.DecisionDeny, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	manager, err := tools.NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new tool manager: %v", err)
	}
	_, execErr := manager.Execute(context.Background(), tools.ToolCallInput{
		ID:        "call-deny",
		Name:      "filesystem_read_file",
		Arguments: []byte(`{"path":"README.md"}`),
		Workdir:   t.TempDir(),
		SessionID: "session-deny",
	})
	var permissionErr *tools.PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		t.Fatalf("expected PermissionDecisionError, got %v", execErr)
	}
	return permissionErr
}

func TestExecuteAssistantToolCallsRemainingBranches(t *testing.T) {
	t.Parallel()

	t.Run("returns ctx error before tool start", func(t *testing.T) {
		t.Parallel()
		service := &Service{events: make(chan RuntimeEvent, 8), approvalBroker: approvalflow.NewBroker()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		state := newRunState("run", newRuntimeSession("session-top-cancel"))
		_, err := service.executeAssistantToolCalls(ctx, &state, TurnBudgetSnapshot{}, providertypes.Message{ToolCalls: []providertypes.ToolCall{{ID: "c", Name: "filesystem_read_file"}}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("successful tool but ctx canceled right after execution", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			cancel()
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		}}
		store := newMemoryStore()
		session := newRuntimeSession("session-mid-cancel")
		store.sessions[session.ID] = cloneSession(session)
		service := &Service{events: make(chan RuntimeEvent, 8), approvalBroker: approvalflow.NewBroker(), toolManager: manager, sessionStore: store}
		state := newRunState("run", session)
		_, err := service.executeAssistantToolCalls(ctx, &state, TurnBudgetSnapshot{ToolTimeout: time.Second}, providertypes.Message{ToolCalls: []providertypes.ToolCall{{ID: "c", Name: "filesystem_read_file"}}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("ctx canceled after save when execErr nil", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name, Content: "ok"}, nil
		}}
		base := newMemoryStore()
		session := newRuntimeSession("session-after-save-cancel")
		base.sessions[session.ID] = cloneSession(session)
		store := &postSaveHookStore{base: base, saveHook: cancel}
		service := &Service{events: make(chan RuntimeEvent, 8), approvalBroker: approvalflow.NewBroker(), toolManager: manager, sessionStore: store}

		state := newRunState("run", session)
		_, err := service.executeAssistantToolCalls(ctx, &state, TurnBudgetSnapshot{ToolTimeout: time.Second}, providertypes.Message{ToolCalls: []providertypes.ToolCall{{ID: "c", Name: "filesystem_read_file"}}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("ctx canceled after emit when execErr not nil", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		manager := &callbackToolManager{executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name}, errors.New("tool failed")
		}}
		base := newMemoryStore()
		session := newRuntimeSession("session-after-emit-cancel")
		base.sessions[session.ID] = cloneSession(session)
		store := &postSaveHookStore{base: base, saveHook: cancel}
		service := &Service{events: make(chan RuntimeEvent, 8), approvalBroker: approvalflow.NewBroker(), toolManager: manager, sessionStore: store}

		state := newRunState("run", session)
		_, err := service.executeAssistantToolCalls(ctx, &state, TurnBudgetSnapshot{ToolTimeout: time.Second}, providertypes.Message{ToolCalls: []providertypes.ToolCall{{ID: "c", Name: "filesystem_read_file"}}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestRunAndProviderRetryRemainingBranches(t *testing.T) {
	t.Parallel()

	t.Run("run exits when context canceled before snapshot", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		manager := newRuntimeConfigManager(t)
		base := newMemoryStore()
		existing := newRuntimeSession("session-cancel-before-snapshot")
		existing.Workdir = manager.Get().Workdir
		base.sessions[existing.ID] = cloneSession(existing)
		store := &postSaveHookStore{base: base, saveHook: cancel}
		service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

		err := service.Run(ctx, UserInput{RunID: "run-cancel-before-snapshot", SessionID: existing.ID, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("run returns assistant save error", func(t *testing.T) {
		t.Parallel()
		manager := newRuntimeConfigManager(t)
		base := newMemoryStore()
		existing := newRuntimeSession("session-assistant-save-fail")
		existing.Workdir = manager.Get().Workdir
		base.sessions[existing.ID] = cloneSession(existing)
		store := &failingStore{Store: base, saveErr: errors.New("assistant save failed"), failOnSave: 2}

		service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{streams: [][]providertypes.StreamEvent{{providertypes.NewTextDeltaStreamEvent("answer")}}}}, &stubContextBuilder{})

		err := service.Run(context.Background(), UserInput{RunID: "run-assistant-save-fail", SessionID: existing.ID, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}})
		if err == nil || !containsError(err, "assistant save failed") {
			t.Fatalf("expected assistant save error, got %v", err)
		}
	})

	t.Run("callProvider returns context error from provider", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		providerRetry := &scriptedProvider{chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			cancel()
			return ctx.Err()
		}}
		service := &Service{providerFactory: &scriptedProviderFactory{provider: providerRetry}, events: make(chan RuntimeEvent, 8)}
		state := newRunState("run-retry-backoff", newRuntimeSession("session-retry-backoff"))
		_, err := service.callProvider(
			ctx,
			&state,
			TurnBudgetSnapshot{ProviderConfig: provider.RuntimeConfig{Name: "x"}},
			providerRetry,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("callProvider returns retryable provider error without retry", func(t *testing.T) {
		t.Parallel()
		providerRetry := &scriptedProvider{chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			return &provider.ProviderError{StatusCode: 500, Code: provider.ErrorCodeServer, Message: "retry", Retryable: true}
		}}
		service := &Service{providerFactory: &scriptedProviderFactory{provider: providerRetry}, events: make(chan RuntimeEvent, 8)}
		state := newRunState("run-retry-ctx-check", newRuntimeSession("session-retry-ctx-check"))
		_, err := service.callProvider(
			context.Background(),
			&state,
			TurnBudgetSnapshot{ProviderConfig: provider.RuntimeConfig{Name: "x"}},
			providerRetry,
		)
		if err == nil || !containsError(err, "retry") {
			t.Fatalf("expected retryable provider error, got %v", err)
		}
		if providerRetry.callCount != 1 {
			t.Fatalf("expected provider to be called once, got %d", providerRetry.callCount)
		}
	})
}
