package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	providertypes "neo-code/internal/provider/types"
	approvalflow "neo-code/internal/runtime/approval"
	"neo-code/internal/runtime/controlplane"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/subagent"
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
	if got := strings.TrimSpace(fmt.Sprintf("%v", metadata["workdir"])); got == "" {
		t.Fatalf("expected workdir metadata in after_tool_result hook, got %#v", metadata["workdir"])
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
		if payload.Source != string(runtimehooks.HookSourceInternal) {
			t.Fatalf("payload.Source = %q, want %q", payload.Source, runtimehooks.HookSourceInternal)
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
	if payload.Source != string(runtimehooks.HookSourceUser) {
		t.Fatalf("payload.Source = %q, want %q", payload.Source, runtimehooks.HookSourceUser)
	}
	if payload.Message != "user warning note" {
		t.Fatalf("payload.Message = %q, want %q", payload.Message, "user warning note")
	}
	if len(state.hookAnnotations) == 0 || state.hookAnnotations[0] != "user warning note" {
		t.Fatalf("hook annotations = %v, want contains user warning note", state.hookAnnotations)
	}
}

func TestHookIntegrationHelpersBranches(t *testing.T) {
	t.Parallel()

	if got := firstNonBlank(" ", "\n", "value", "ignored"); got != "value" {
		t.Fatalf("firstNonBlank() = %q, want value", got)
	}
	if got := firstNonBlank(" ", "\n"); got != "" {
		t.Fatalf("firstNonBlank() = %q, want empty", got)
	}

	if got := findHookBlockMessage(runtimehooks.RunOutput{}); got != "" {
		t.Fatalf("findHookBlockMessage() for non-blocked output = %q, want empty", got)
	}
	if got := findHookBlockMessage(runtimehooks.RunOutput{
		Blocked:   true,
		BlockedBy: "hook-1",
		Results:   []runtimehooks.HookResult{{HookID: "hook-1", Message: " denied "}},
	}); got != "denied" {
		t.Fatalf("findHookBlockMessage() from message = %q, want denied", got)
	}
	if got := findHookBlockMessage(runtimehooks.RunOutput{
		Blocked:   true,
		BlockedBy: "hook-2",
		Results:   []runtimehooks.HookResult{{HookID: "hook-2", Error: " failed "}},
	}); got != "failed" {
		t.Fatalf("findHookBlockMessage() from error = %q, want failed", got)
	}
	if got := findHookBlockMessage(runtimehooks.RunOutput{
		Blocked:   true,
		BlockedBy: "hook-3",
		Results:   []runtimehooks.HookResult{{HookID: "other", Message: "ignored"}},
	}); got != "hook blocked by hook-3" {
		t.Fatalf("findHookBlockMessage() fallback by hook id = %q", got)
	}
	if got := findHookBlockMessage(runtimehooks.RunOutput{
		Blocked: true,
		Results: []runtimehooks.HookResult{{HookID: "other", Message: "ignored"}},
	}); got != "hook blocked" {
		t.Fatalf("findHookBlockMessage() default fallback = %q", got)
	}
	if got := findHookBlockSource(runtimehooks.RunOutput{}); got != "" {
		t.Fatalf("findHookBlockSource() for non-blocked output = %q, want empty", got)
	}
	if got := findHookBlockSource(runtimehooks.RunOutput{
		Blocked:   true,
		BlockedBy: "hook-src-1",
		Results: []runtimehooks.HookResult{
			{HookID: "hook-src-1", Source: runtimehooks.HookSourceRepo},
		},
	}); got != runtimehooks.HookSourceRepo {
		t.Fatalf("findHookBlockSource() from result = %q, want %q", got, runtimehooks.HookSourceRepo)
	}
	if got := findHookBlockSource(runtimehooks.RunOutput{
		Blocked:       true,
		BlockedBy:     "hook-src-2",
		BlockedSource: runtimehooks.HookSourceUser,
		Results: []runtimehooks.HookResult{
			{HookID: "other", Source: runtimehooks.HookSourceRepo},
		},
	}); got != runtimehooks.HookSourceUser {
		t.Fatalf("findHookBlockSource() fallback = %q, want %q", got, runtimehooks.HookSourceUser)
	}
	if got := findHookBlockSource(runtimehooks.RunOutput{
		Blocked:       true,
		BlockedBy:     "hook-src-3",
		BlockedSource: runtimehooks.HookSourceInternal,
		Results: []runtimehooks.HookResult{
			{HookID: "hook-src-3", Source: ""},
		},
	}); got != runtimehooks.HookSourceInternal {
		t.Fatalf("findHookBlockSource() matched empty source fallback = %q, want %q", got, runtimehooks.HookSourceInternal)
	}

	wrapped := withRuntimeHookEnvelope(nil, hookRuntimeEnvelope{RunID: "run-1"})
	envelope, ok := runtimeHookEnvelopeFromContext(wrapped)
	if !ok || envelope.RunID != "run-1" {
		t.Fatalf("runtimeHookEnvelopeFromContext() = (%+v,%v), want run-1", envelope, ok)
	}
	if _, ok := runtimeHookEnvelopeFromContext(nil); ok {
		t.Fatalf("runtimeHookEnvelopeFromContext(nil) should return ok=false")
	}
	if _, ok := runtimeHookEnvelopeFromContext(context.Background()); ok {
		t.Fatalf("runtimeHookEnvelopeFromContext(background) should return ok=false")
	}

	state := newRunState(" run-id ", newRuntimeSession("session-x"))
	state.turn = 3
	if got := hookRunIDFromState(&state); got != "run-id" {
		t.Fatalf("hookRunIDFromState() = %q", got)
	}
	if got := hookSessionIDFromState(&state); got != "session-x" {
		t.Fatalf("hookSessionIDFromState() = %q", got)
	}
	if got := hookTurnFromState(&state); got != 3 {
		t.Fatalf("hookTurnFromState() = %d", got)
	}
	if got := hookPhaseFromState(&state); got != "" {
		t.Fatalf("hookPhaseFromState() without lifecycle = %q, want empty", got)
	}
	state.lifecycle = controlplane.RunStateExecute
	if got := hookPhaseFromState(&state); got != string(controlplane.RunStateExecute) {
		t.Fatalf("hookPhaseFromState() with lifecycle = %q", got)
	}
	if got := hookRunIDFromState(nil); got != "" {
		t.Fatalf("hookRunIDFromState(nil) = %q, want empty", got)
	}
	if got := hookSessionIDFromState(nil); got != "" {
		t.Fatalf("hookSessionIDFromState(nil) = %q, want empty", got)
	}
	if got := hookTurnFromState(nil); got != turnUnspecified {
		t.Fatalf("hookTurnFromState(nil) = %d, want %d", got, turnUnspecified)
	}
}

func TestRecordUserHookAnnotationsBranches(t *testing.T) {
	t.Parallel()

	service := &Service{}
	service.recordUserHookAnnotations(nil, runtimehooks.RunOutput{
		Results: []runtimehooks.HookResult{
			{Scope: runtimehooks.HookScopeUser, Message: "should ignore when state nil"},
		},
	})

	state := newRunState("run-anno", newRuntimeSession("session-anno"))
	service.recordUserHookAnnotations(&state, runtimehooks.RunOutput{})
	service.recordUserHookAnnotations(&state, runtimehooks.RunOutput{
		Results: []runtimehooks.HookResult{
			{Scope: runtimehooks.HookScopeInternal, Message: "ignore internal"},
			{Scope: runtimehooks.HookScopeUser, Message: "   "},
		},
	})
	if len(state.hookAnnotations) != 0 {
		t.Fatalf("unexpected annotations for non-user/repo or empty message: %v", state.hookAnnotations)
	}

	service.recordUserHookAnnotations(&state, runtimehooks.RunOutput{
		Results: []runtimehooks.HookResult{
			{Scope: runtimehooks.HookScopeUser, Message: "user note"},
			{Scope: runtimehooks.HookScopeRepo, Message: "repo note"},
		},
	})
	if len(state.hookAnnotations) != 2 {
		t.Fatalf("annotations len = %d, want 2", len(state.hookAnnotations))
	}
	if state.hookAnnotations[0] != "user note" || state.hookAnnotations[1] != "repo note" {
		t.Fatalf("annotations = %v, want [user note repo note]", state.hookAnnotations)
	}
}

func TestSummarizeHookResultContentTruncatesLongContent(t *testing.T) {
	t.Parallel()

	if got := summarizeHookResultContent(" short "); got != "short" {
		t.Fatalf("summarizeHookResultContent() short = %q", got)
	}
	long := strings.Repeat("x", 300)
	got := summarizeHookResultContent(long)
	if len(got) != 256 {
		t.Fatalf("summarizeHookResultContent() len = %d, want 256", len(got))
	}
}

func TestHookRuntimeEventEmitterBranches(t *testing.T) {
	t.Parallel()

	if err := (&hookRuntimeEventEmitter{}).EmitHookEvent(context.Background(), runtimehooks.HookEvent{
		Type: runtimehooks.HookEventStarted,
	}); err != nil {
		t.Fatalf("EmitHookEvent() with nil service error = %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 8)}
	emitter := newHookRuntimeEventEmitter(service)
	if err := emitter.EmitHookEvent(context.Background(), runtimehooks.HookEvent{}); err != nil {
		t.Fatalf("EmitHookEvent() blank type error = %v", err)
	}
	if got := len(collectRuntimeEvents(service.Events())); got != 0 {
		t.Fatalf("expected blank event type to be ignored, got %d events", got)
	}

	startedAt := time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC)
	ctx := withRuntimeHookEnvelope(context.Background(), hookRuntimeEnvelope{
		RunID:     "run-evt",
		SessionID: "session-evt",
		Turn:      2,
		Phase:     "execute",
	})
	if err := emitter.EmitHookEvent(ctx, runtimehooks.HookEvent{
		Type:       runtimehooks.HookEventFinished,
		HookID:     "hook-evt",
		Point:      runtimehooks.HookPointAfterToolResult,
		Scope:      runtimehooks.HookScopeInternal,
		Kind:       runtimehooks.HookKindFunction,
		Mode:       runtimehooks.HookModeSync,
		Status:     runtimehooks.HookResultPass,
		StartedAt:  startedAt,
		DurationMS: 12,
		Error:      "",
	}); err != nil {
		t.Fatalf("EmitHookEvent() finished error = %v", err)
	}
	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	evt := events[0]
	if evt.Type != EventHookFinished || evt.RunID != "run-evt" || evt.SessionID != "session-evt" || evt.Turn != 2 || evt.Phase != "execute" {
		t.Fatalf("unexpected runtime event envelope: %+v", evt)
	}
	payload, ok := evt.Payload.(HookEventPayload)
	if !ok {
		t.Fatalf("payload type = %T, want HookEventPayload", evt.Payload)
	}
	if payload.HookID != "hook-evt" || payload.Point != string(runtimehooks.HookPointAfterToolResult) || payload.DurationMS != 12 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunHookPointUsesInputEnvelopeWhenStateMissing(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 8),
	}
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-pre-compact",
		Point: runtimehooks.HookPointPreCompact,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register pre_compact hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	output := service.runHookPoint(context.Background(), nil, runtimehooks.HookPointPreCompact, runtimehooks.HookContext{
		RunID:     "run-envelope",
		SessionID: "session-envelope",
		Metadata: map[string]any{
			"workdir": t.TempDir(),
		},
	})
	if output.Blocked {
		t.Fatalf("runHookPoint() should not block, got %+v", output)
	}

	events := collectRuntimeEvents(service.Events())
	startedIndex := eventIndex(events, EventHookStarted)
	if startedIndex < 0 {
		t.Fatalf("expected hook_started event")
	}
	if got := events[startedIndex].RunID; got != "run-envelope" {
		t.Fatalf("hook_started run_id = %q, want %q", got, "run-envelope")
	}
	if got := events[startedIndex].SessionID; got != "session-envelope" {
		t.Fatalf("hook_started session_id = %q, want %q", got, "session-envelope")
	}
}

func TestRunHookPointReturnsEmptyWhenServiceOrExecutorMissing(t *testing.T) {
	t.Parallel()

	var nilService *Service
	if out := nilService.runHookPoint(
		context.Background(),
		nil,
		runtimehooks.HookPointBeforeToolCall,
		runtimehooks.HookContext{RunID: "run-x", SessionID: "session-x"},
	); out.Blocked || len(out.Results) != 0 {
		t.Fatalf("nil service should return empty output, got %+v", out)
	}

	service := &Service{events: make(chan RuntimeEvent, 1)}
	if out := service.runHookPoint(
		context.Background(),
		nil,
		runtimehooks.HookPointBeforeToolCall,
		runtimehooks.HookContext{RunID: "run-y", SessionID: "session-y"},
	); out.Blocked || len(out.Results) != 0 {
		t.Fatalf("service without executor should return empty output, got %+v", out)
	}
}

func TestRunTriggersSessionStartAndSessionEndHooks(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewTextDeltaStreamEvent("done"),
				providertypes.NewMessageDoneStreamEvent("", nil),
			},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})

	var (
		sessionStartCalled bool
		sessionEndCalled   bool
	)
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-session-start",
		Point: runtimehooks.HookPointSessionStart,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			sessionStartCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register session_start hook: %v", err)
	}
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-session-end",
		Point: runtimehooks.HookPointSessionEnd,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			sessionEndCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register session_end hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-hook-session-lifecycle",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !sessionStartCalled {
		t.Fatal("expected session_start hook to be triggered")
	}
	if !sessionEndCalled {
		t.Fatal("expected session_end hook to be triggered")
	}
}

func TestRunUserPromptSubmitHookBlockEnforced(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{
				providertypes.NewTextDeltaStreamEvent("should-not-run"),
				providertypes.NewMessageDoneStreamEvent("", nil),
			},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: scripted}, &stubContextBuilder{})

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-user-submit",
		Point: runtimehooks.HookPointUserPromptSubmit,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked user prompt submit"}
		},
	}); err != nil {
		t.Fatalf("register user_prompt_submit hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	err := service.Run(context.Background(), UserInput{
		RunID: "run-hook-user-prompt-blocked",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
	})
	if err == nil {
		t.Fatal("expected Run() to be blocked by user_prompt_submit hook")
	}
	if !strings.Contains(err.Error(), "blocked user prompt submit") {
		t.Fatalf("Run() error = %q, want block reason", err.Error())
	}
	if scripted.callCount != 0 {
		t.Fatalf("provider call count = %d, want 0 when blocked before provider call", scripted.callCount)
	}

	events := collectRuntimeEvents(service.Events())
	blockedIndex := eventIndex(events, EventHookBlocked)
	if blockedIndex < 0 {
		t.Fatalf("expected hook_blocked event")
	}
	payload, ok := events[blockedIndex].Payload.(HookBlockedPayload)
	if !ok {
		t.Fatalf("hook_blocked payload type = %T, want HookBlockedPayload", events[blockedIndex].Payload)
	}
	if payload.Point != string(runtimehooks.HookPointUserPromptSubmit) {
		t.Fatalf("payload.Point = %q, want %q", payload.Point, runtimehooks.HookPointUserPromptSubmit)
	}
	if !payload.Enforced {
		t.Fatal("expected user_prompt_submit block to be enforced")
	}
}

func TestExecuteOneToolCallTriggersAfterToolFailureHook(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-hook-after-tool-failure")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{Name: input.Name, IsError: true, ErrorClass: "tool_failed"}, errors.New("tool failed")
		},
	}
	service := &Service{
		sessionStore:   store,
		toolManager:    toolManager,
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-hook-after-tool-failure", session)

	var called bool
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-after-tool-failure",
		Point: runtimehooks.HookPointAfterToolFailure,
		Handler: func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			called = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register after_tool_failure hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, _, err := service.executeOneToolCall(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Workdir: t.TempDir(), ToolTimeout: time.Second},
		providertypes.ToolCall{ID: "call-4", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
		&sync.Mutex{},
		func() bool { return false },
	)
	if err != nil {
		t.Fatalf("executeOneToolCall() error = %v, want nil for non-cancel tool failure", err)
	}
	if !called {
		t.Fatal("expected after_tool_failure hook to be triggered")
	}
}

func TestBeforePermissionDecisionHookBlockEnforced(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := newRuntimeSession("session-hook-before-permission")
	store.sessions[session.ID] = cloneSession(session)
	service := &Service{
		sessionStore:   store,
		toolManager:    &stubToolManager{},
		approvalBroker: approvalflow.NewBroker(),
		events:         make(chan RuntimeEvent, 32),
	}
	state := newRunState("run-hook-before-permission", session)
	permissionErr := permissionDecisionAskError(t)

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-before-permission",
		Point: runtimehooks.HookPointBeforePermissionDecision,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked before permission decision"}
		},
	}); err != nil {
		t.Fatalf("register before_permission_decision hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))
	service.toolManager = &stubToolManager{
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{
				ToolCallID: input.ID,
				Name:       input.Name,
				IsError:    true,
			}, permissionErr
		},
	}

	result, err := service.executeToolCallWithPermission(context.Background(), permissionExecutionInput{
		RunID:       state.runID,
		SessionID:   state.session.ID,
		State:       &state,
		Call:        providertypes.ToolCall{ID: "call-permission", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
		Workdir:     t.TempDir(),
		ToolTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected permission chain to be blocked")
	}
	if result.ErrorClass != hookErrorClassBlocked {
		t.Fatalf("result.ErrorClass = %q, want %q", result.ErrorClass, hookErrorClassBlocked)
	}
	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventHookBlocked)
	assertNoEventType(t, events, EventPermissionRequested)
}

func TestRunCompactTriggersPreAndPostCompactHooks(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, &stubToolManager{}, newMemoryStore(), nil, nil)
	session := newRuntimeSession("session-hook-compact")
	cfg := *config.StaticDefaults()
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Applied: false,
			Metrics: contextcompact.Metrics{TriggerMode: string(contextcompact.ModeManual)},
		},
	}

	var preCalled, postCalled bool
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "pre-compact-observe",
		Point: runtimehooks.HookPointPreCompact,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			preCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register pre_compact hook: %v", err)
	}
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "post-compact-observe",
		Point: runtimehooks.HookPointPostCompact,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			postCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register post_compact hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, _, err := service.runCompactForSession(
		context.Background(),
		"run-hook-compact",
		session,
		cfg,
		contextcompact.ModeManual,
		compactErrorStrict,
	)
	if err != nil {
		t.Fatalf("runCompactForSession() error = %v", err)
	}
	if !preCalled {
		t.Fatal("expected pre_compact hook to be triggered")
	}
	if !postCalled {
		t.Fatal("expected post_compact hook to be triggered")
	}
}

func TestRunCompactPreCompactHookBlockEnforced(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, &stubToolManager{}, newMemoryStore(), nil, nil)
	session := newRuntimeSession("session-hook-compact-block")
	cfg := *config.StaticDefaults()
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Applied: false,
			Metrics: contextcompact.Metrics{TriggerMode: string(contextcompact.ModeManual)},
		},
	}

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-pre-compact",
		Point: runtimehooks.HookPointPreCompact,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked pre compact"}
		},
	}); err != nil {
		t.Fatalf("register pre_compact block hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, _, err := service.runCompactForSession(
		context.Background(),
		"run-hook-compact-block",
		session,
		cfg,
		contextcompact.ModeManual,
		compactErrorStrict,
	)
	if err == nil {
		t.Fatal("expected runCompactForSession to be blocked")
	}
	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventHookBlocked)
	assertNoEventType(t, events, EventCompactStart)
}

func TestRunSubAgentTaskTriggersSubagentStartAndStopHooks(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)
	service.SetSubAgentFactory(subagent.NewWorkerFactory(func(role subagent.Role, policy subagent.RolePolicy) subagent.Engine {
		return subagent.EngineFunc(func(ctx context.Context, input subagent.StepInput) (subagent.StepOutput, error) {
			return subagent.StepOutput{
				Delta: "done",
				Done:  true,
				Output: subagent.Output{
					Summary:     "ok",
					Findings:    []string{"f1"},
					Patches:     []string{"p1"},
					Risks:       []string{"r1"},
					NextActions: []string{"n1"},
					Artifacts:   []string{"a1"},
				},
			}, nil
		})
	}))

	var startCalled, stopCalled bool
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-subagent-start",
		Point: runtimehooks.HookPointSubAgentStart,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			startCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register subagent_start hook: %v", err)
	}
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-subagent-stop",
		Point: runtimehooks.HookPointSubAgentStop,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			stopCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register subagent_stop hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID:     "sub-run-hook-lifecycle",
		SessionID: "session-hook-lifecycle",
		Role:      subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-hook-lifecycle",
			Goal: "goal",
		},
		Budget: subagent.Budget{MaxSteps: 2},
	})
	if err != nil {
		t.Fatalf("RunSubAgentTask() error = %v", err)
	}
	if !startCalled {
		t.Fatal("expected subagent_start hook to be triggered")
	}
	if !stopCalled {
		t.Fatal("expected subagent_stop hook to be triggered")
	}
}

func TestRunSubAgentTaskSubagentStartHookBlockEnforced(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "block-subagent-start",
		Point: runtimehooks.HookPointSubAgentStart,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{Status: runtimehooks.HookResultBlock, Message: "blocked subagent start"}
		},
	}); err != nil {
		t.Fatalf("register subagent_start block hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	_, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID:     "sub-run-hook-blocked",
		SessionID: "session-hook-blocked",
		Role:      subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-hook-blocked",
			Goal: "goal",
		},
	})
	if err == nil {
		t.Fatal("expected RunSubAgentTask() to be blocked by subagent_start hook")
	}

	events := collectRuntimeEvents(service.Events())
	assertEventContains(t, events, EventHookBlocked)
	assertNoEventType(t, events, EventSubAgentStarted)
}

func TestRunSubAgentTaskEmitsStopHookWhenResultUnavailableAfterStepError(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)
	service.SetSubAgentFactory(stubSubAgentFactory{
		create: func(role subagent.Role) (subagent.WorkerRuntime, error) {
			return &stubSubAgentWorker{
				stepResult: subagent.StepResult{
					State: subagent.StateRunning,
					Done:  false,
				},
				stepErr:   errors.New("step failed"),
				resultErr: errors.New("result unavailable"),
			}, nil
		},
	})

	var stopCalled bool
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-subagent-stop-on-step-err",
		Point: runtimehooks.HookPointSubAgentStop,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			stopCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register subagent_stop hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	result, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID:     "sub-run-stop-on-step-err",
		SessionID: "session-stop-on-step-err",
		Role:      subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-stop-on-step-err",
			Goal: "goal",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "step failed") {
		t.Fatalf("RunSubAgentTask() error = %v, want step failed", err)
	}
	if result.State != subagent.StateFailed {
		t.Fatalf("result.State = %q, want %q", result.State, subagent.StateFailed)
	}
	if result.StopReason != subagent.StopReasonError {
		t.Fatalf("result.StopReason = %q, want %q", result.StopReason, subagent.StopReasonError)
	}
	if !stopCalled {
		t.Fatal("expected subagent_stop hook to be triggered on step error + result unavailable")
	}
}

func TestRunSubAgentTaskEmitsStopHookWhenResultUnavailableAfterDone(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)
	service.SetSubAgentFactory(stubSubAgentFactory{
		create: func(role subagent.Role) (subagent.WorkerRuntime, error) {
			return &stubSubAgentWorker{
				stepResult: subagent.StepResult{
					State: subagent.StateRunning,
					Done:  true,
				},
				resultErr: errors.New("result unavailable after done"),
			}, nil
		},
	})

	var stopCalled bool
	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "observe-subagent-stop-on-done-result-err",
		Point: runtimehooks.HookPointSubAgentStop,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			stopCalled = true
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		},
	}); err != nil {
		t.Fatalf("register subagent_stop hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	result, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID:     "sub-run-stop-on-done-result-err",
		SessionID: "session-stop-on-done-result-err",
		Role:      subagent.RoleCoder,
		Task: subagent.Task{
			ID:   "task-stop-on-done-result-err",
			Goal: "goal",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "result unavailable after done") {
		t.Fatalf("RunSubAgentTask() error = %v, want result unavailable after done", err)
	}
	if result.State != subagent.StateFailed {
		t.Fatalf("result.State = %q, want %q", result.State, subagent.StateFailed)
	}
	if result.StopReason != subagent.StopReasonError {
		t.Fatalf("result.StopReason = %q, want %q", result.StopReason, subagent.StopReasonError)
	}
	if !stopCalled {
		t.Fatal("expected subagent_stop hook to be triggered on done+result unavailable path")
	}
}

func TestEmitSubAgentStopHookNilServiceNoop(t *testing.T) {
	t.Parallel()

	emitSubAgentStopHook(nil, context.Background(), SubAgentTaskInput{
		RunID:     "run-nil-stop-hook",
		SessionID: "session-nil-stop-hook",
	}, subagent.Result{
		Role:       subagent.RoleCoder,
		TaskID:     "task-nil-stop-hook",
		State:      subagent.StateFailed,
		StopReason: subagent.StopReasonError,
		Error:      "noop",
	})
}
