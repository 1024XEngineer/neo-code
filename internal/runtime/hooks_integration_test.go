package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/tools"
)

func TestExecuteOneToolCallBlocksWhenBeforeToolHookReturnsBlock(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-hook-before-tool-block")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		result: tools.ToolResult{Name: "filesystem_read_file", Content: "should not execute"},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    toolManager,
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-hook-before-tool-block", session)

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-before-tool",
		Point: runtimehooks.HookPointBeforeToolCall,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked by test hook"}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	result, wrote, err := service.executeOneToolCall(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second},
		providertypes.ToolCall{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
		&sync.Mutex{},
		func() bool { return false },
	)
	if err != nil {
		t.Fatalf("executeOneToolCall() error = %v", err)
	}
	if wrote {
		t.Fatalf("executeOneToolCall() wrote = true, want false")
	}
	if !result.IsError {
		t.Fatalf("tool result should be error when blocked by hook")
	}
	if result.ErrorClass != hookErrorClassBlocked {
		t.Fatalf("result.ErrorClass = %q, want %q", result.ErrorClass, hookErrorClassBlocked)
	}

	toolManager.mu.Lock()
	executeCalls := toolManager.executeCalls
	toolManager.mu.Unlock()
	if executeCalls != 0 {
		t.Fatalf("tool manager execute calls = %d, want 0", executeCalls)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventHookStarted)
	assertEventContains(t, events, EventHookFinished)
	assertEventContains(t, events, EventHookBlocked)
	assertEventContains(t, events, EventToolResult)
	assertNoEventType(t, events, EventToolStart)
	if eventIndex(events, EventHookBlocked) > eventIndex(events, EventToolResult) {
		t.Fatalf("hook_blocked should be emitted before tool_result")
	}

	hookStartedIndex := eventIndex(events, EventHookStarted)
	if hookStartedIndex >= 0 {
		started := events[hookStartedIndex]
		if started.RunID != state.runID {
			t.Fatalf("hook_started run id = %q, want %q", started.RunID, state.runID)
		}
		if started.SessionID != state.session.ID {
			t.Fatalf("hook_started session id = %q, want %q", started.SessionID, state.session.ID)
		}
	}
}

func TestExecuteOneToolCallTriggersAfterToolResultHookWithoutMutatingResult(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-hook-after-tool-result")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		result: tools.ToolResult{Name: "filesystem_read_file", Content: "ok"},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    toolManager,
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-hook-after-tool-result", session)

	var (
		called   bool
		metadata map[string]any
	)
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-after-tool",
		Point: runtimehooks.HookPointAfterToolResult,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			called = true
			metadata = input.Metadata
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	result, _, err := service.executeOneToolCall(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second},
		providertypes.ToolCall{ID: "call-2", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
		&sync.Mutex{},
		func() bool { return false },
	)
	if err != nil {
		t.Fatalf("executeOneToolCall() error = %v", err)
	}
	if !called {
		t.Fatalf("after_tool_result hook should be called")
	}
	if got := result.Content; got != "ok" {
		t.Fatalf("tool result content = %q, want %q", got, "ok")
	}
	if got := metadata["result_content_preview"]; got != "ok" {
		t.Fatalf("result_content_preview = %#v, want %q", got, "ok")
	}
}

func TestExecuteOneToolCallCanceledStillTriggersAfterToolResultHook(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-hook-after-tool-result-canceled")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name}, context.Canceled
		},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    toolManager,
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-hook-after-tool-result-canceled", session)

	var (
		called bool
		errMsg string
	)
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-after-tool-canceled",
		Point: runtimehooks.HookPointAfterToolResult,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			called = true
			if raw, ok := input.Metadata["execution_error"]; ok {
				if text, ok := raw.(string); ok {
					errMsg = text
				}
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, _, err := service.executeOneToolCall(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second},
		providertypes.ToolCall{ID: "call-3", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
		&sync.Mutex{},
		func() bool { return false },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("executeOneToolCall() error = %v, want context.Canceled", err)
	}
	if !called {
		t.Fatalf("after_tool_result hook should be called when tool execution is canceled")
	}
	if errMsg == "" {
		t.Fatalf("expected execution_error metadata for canceled execution")
	}
}

func TestRunBeforeCompletionDecisionHookBlockIsObservedOnly(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewTextDeltaStreamEvent("final answer"),
				providertypes.NewMessageDoneStreamEvent("", nil),
			},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})

	var capturedWorkdir string
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-before-completion",
		Point: runtimehooks.HookPointBeforeCompletionDecision,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			if raw, ok := input.Metadata["workdir"]; ok {
				if text, ok := raw.(string); ok {
					capturedWorkdir = strings.TrimSpace(text)
				}
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked but non-authoritative"}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-hook-before-completion",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventHookBlocked)
	assertEventContains(t, events, EventAgentDone)
	if eventIndex(events, EventHookBlocked) > eventIndex(events, EventVerificationStarted) {
		t.Fatalf("before_completion_decision hook_blocked should be emitted before verification_started")
	}

	blockedIndex := eventIndex(events, EventHookBlocked)
	if blockedIndex >= 0 {
		payload, ok := events[blockedIndex].Payload.(HookBlockedPayload)
		if !ok {
			t.Fatalf("hook_blocked payload type = %T, want HookBlockedPayload", events[blockedIndex].Payload)
		}
		if payload.Enforced {
			t.Fatalf("before_completion_decision block should be observed only, got enforced=true")
		}
		if payload.Point != string(runtimehooks.HookPointBeforeCompletionDecision) {
			t.Fatalf("payload.Point = %q, want %q", payload.Point, runtimehooks.HookPointBeforeCompletionDecision)
		}
	}
	if capturedWorkdir == "" {
		t.Fatalf("expected before_completion_decision hook metadata to include workdir")
	}
}

func TestUserHookEventCarriesScopeAndMessage(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-user-hook-message")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		result: tools.ToolResult{Name: "bash", Content: "ok"},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    toolManager,
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-user-hook-message", session)

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "user-note-hook",
		Point: runtimehooks.HookPointBeforeToolCall,
		Scope: runtimehooks.HookScopeUser,
		Handler: func(_ context.Context, _ runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{
				Status:  runtimehooks.HookResultPass,
				Message: "user warning note",
			}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, _, err := service.executeOneToolCall(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second},
		providertypes.ToolCall{ID: "call-user-hook", Name: "bash", Arguments: `{"command":"echo hi"}`},
		&sync.Mutex{},
		func() bool { return false },
	)
	if err != nil {
		t.Fatalf("executeOneToolCall() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	finishedIndex := eventIndex(events, EventHookFinished)
	if finishedIndex < 0 {
		t.Fatalf("expected hook_finished event")
	}
	payload, ok := events[finishedIndex].Payload.(HookEventPayload)
	if !ok {
		t.Fatalf("payload type = %T, want HookEventPayload", events[finishedIndex].Payload)
	}
	if payload.Scope != string(runtimehooks.HookScopeUser) {
		t.Fatalf("payload.Scope = %q, want %q", payload.Scope, runtimehooks.HookScopeUser)
	}
	if payload.Message != "user warning note" {
		t.Fatalf("payload.Message = %q, want %q", payload.Message, "user warning note")
	}
	if len(state.hookAnnotations) == 0 || state.hookAnnotations[0] != "user warning note" {
		t.Fatalf("hook annotations = %v, want contains user warning note", state.hookAnnotations)
	}
}
