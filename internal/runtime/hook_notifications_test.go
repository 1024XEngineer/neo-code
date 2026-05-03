package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/tools"
)

func TestDrainHookNotificationsForTurnUsesLimitAndConsumesQueue(t *testing.T) {
	t.Parallel()

	service := &Service{}
	state := newRunState("run-drain", newRuntimeSession("session-drain"))
	now := time.Now()
	for i := 0; i < MaxInjectedNotificationsPerTurn+2; i++ {
		state.hookNotifications = append(state.hookNotifications, queuedHookNotification{
			Payload: HookNotificationPayload{
				HookID:  fmt.Sprintf("hook-%d", i),
				Point:   string(runtimehooks.HookPointBeforeToolCall),
				Status:  string(runtimehooks.HookResultPass),
				Summary: fmt.Sprintf("summary-%d", i),
			},
			CreatedAt: now,
		})
	}
	state.hookNotificationOmitted = 1

	hint := service.drainHookNotificationsForTurn(&state)
	if !strings.Contains(hint, "[runtime_async_notifications]") {
		t.Fatalf("hint = %q, want runtime_async_notifications header", hint)
	}
	if strings.Count(hint, "- hook=") != MaxInjectedNotificationsPerTurn {
		t.Fatalf("injected notification count mismatch, hint = %q", hint)
	}
	if !strings.Contains(hint, "3 more notifications omitted") {
		t.Fatalf("hint = %q, want omitted aggregation", hint)
	}

	second := service.drainHookNotificationsForTurn(&state)
	if strings.TrimSpace(second) != "" {
		t.Fatalf("second drain = %q, want empty after consume", second)
	}
}

func TestEnqueueHookNotificationDedupeTruncateAndLimit(t *testing.T) {
	t.Parallel()

	service := &Service{
		events:            make(chan RuntimeEvent, 128),
		activeRunCancels:  map[uint64]context.CancelFunc{},
		activeRunByID:     map[string]uint64{},
		activeRunTokenIDs: map[uint64]string{},
		activeRunStates:   map[uint64]*runState{},
	}
	runID := "run-enqueue"
	token := uint64(7)
	state := newRunState(runID, newRuntimeSession("session-enqueue"))
	state.runToken = token
	service.activeRunCancels[token] = func() {}
	service.activeRunByID[runID] = token
	service.activeRunTokenIDs[token] = runID
	service.activeRunStates[token] = &state

	longText := strings.Repeat("x", MaxNotificationChars+50)
	base := HookNotificationPayload{
		HookID:    "hook-1",
		Source:    "internal",
		Point:     string(runtimehooks.HookPointBeforeToolCall),
		Status:    string(runtimehooks.HookResultFailed),
		Reason:    longText,
		Summary:   longText,
		Message:   longText,
		DedupeKey: "dup-key",
	}
	service.enqueueHookNotification(context.Background(), runID, token, base)
	service.enqueueHookNotification(context.Background(), runID, token, base)

	state.mu.Lock()
	if got := len(state.hookNotifications); got != 1 {
		state.mu.Unlock()
		t.Fatalf("len(queue) = %d, want 1 after dedupe", got)
	}
	payload := state.hookNotifications[0].Payload
	if len([]rune(payload.Reason)) != MaxNotificationChars {
		state.mu.Unlock()
		t.Fatalf("reason rune len = %d, want %d", len([]rune(payload.Reason)), MaxNotificationChars)
	}
	if len([]rune(payload.Summary)) != MaxNotificationChars {
		state.mu.Unlock()
		t.Fatalf("summary rune len = %d, want %d", len([]rune(payload.Summary)), MaxNotificationChars)
	}
	if len([]rune(payload.Message)) != MaxNotificationChars {
		state.mu.Unlock()
		t.Fatalf("message rune len = %d, want %d", len([]rune(payload.Message)), MaxNotificationChars)
	}
	state.mu.Unlock()

	for i := 0; i < MaxNotificationsPerRun+10; i++ {
		service.enqueueHookNotification(context.Background(), runID, token, HookNotificationPayload{
			HookID:    "hook-limit",
			Source:    "internal",
			Point:     string(runtimehooks.HookPointBeforeToolCall),
			Status:    string(runtimehooks.HookResultFailed),
			Reason:    "r",
			Summary:   "s",
			DedupeKey: fmt.Sprintf("limit-%d", i+1),
		})
	}
	state.mu.Lock()
	if got := len(state.hookNotifications); got != MaxNotificationsPerRun {
		state.mu.Unlock()
		t.Fatalf("len(queue) = %d, want %d", got, MaxNotificationsPerRun)
	}
	if state.hookNotificationOmitted == 0 {
		state.mu.Unlock()
		t.Fatalf("hookNotificationOmitted = 0, want > 0 when over limit")
	}
	state.mu.Unlock()
}

func TestEnqueueHookNotificationDropsWhenRunInactive(t *testing.T) {
	t.Parallel()

	service := &Service{
		events:            make(chan RuntimeEvent, 16),
		activeRunCancels:  map[uint64]context.CancelFunc{},
		activeRunByID:     map[string]uint64{},
		activeRunTokenIDs: map[uint64]string{},
		activeRunStates:   map[uint64]*runState{},
	}
	runID := "run-inactive"
	token := uint64(9)
	state := newRunState(runID, newRuntimeSession("session-inactive"))
	state.runToken = token
	service.activeRunByID[runID] = token
	service.activeRunTokenIDs[token] = runID
	service.activeRunStates[token] = &state
	// 故意不写 activeRunCancels，模拟 run 已结束。

	service.enqueueHookNotification(context.Background(), runID, token, HookNotificationPayload{
		HookID:  "hook-x",
		Source:  "internal",
		Point:   string(runtimehooks.HookPointBeforeToolCall),
		Status:  string(runtimehooks.HookResultFailed),
		Summary: "should drop",
	})

	state.mu.Lock()
	got := len(state.hookNotifications)
	state.mu.Unlock()
	if got != 0 {
		t.Fatalf("len(queue) = %d, want 0 for inactive run", got)
	}
}

func TestHookAsyncResultSinkHonorsRunLifecycle(t *testing.T) {
	t.Parallel()

	service := &Service{
		events: make(chan RuntimeEvent, 16),
	}
	runID := "run-sink"
	token := service.startRun(func() {}, runID)
	state := newRunState(runID, newRuntimeSession("session-sink"))
	state.runToken = token
	service.bindRunState(token, &state)
	sink := newHookAsyncResultSink(service)
	spec := runtimehooks.HookSpec{
		ID:     "hook-rewake",
		Point:  runtimehooks.HookPointBeforeToolCall,
		Source: runtimehooks.HookSourceInternal,
		Mode:   runtimehooks.HookModeAsyncRewake,
	}
	input := runtimehooks.HookContext{
		RunID: runID,
		Metadata: map[string]any{
			"runtime_run_token": token,
		},
	}
	result := runtimehooks.HookResult{
		Status: runtimehooks.HookResultFailed,
		Metadata: runtimehooks.HookResultMetadata{
			RewakeReason:  "failed",
			RewakeSummary: "needs follow up",
		},
	}

	sink.HandleAsyncHookResult(context.Background(), spec, input, result)
	state.mu.Lock()
	firstLen := len(state.hookNotifications)
	state.mu.Unlock()
	if firstLen != 1 {
		t.Fatalf("first queue len = %d, want 1", firstLen)
	}
	firstEvents := collectRuntimeEvents(service.Events())
	if got := countEventType(firstEvents, EventHookNotification); got != 1 {
		t.Fatalf("hook_notification events after first enqueue = %d, want 1", got)
	}

	service.finishRun(token)
	sink.HandleAsyncHookResult(context.Background(), spec, input, result)
	state.mu.Lock()
	secondLen := len(state.hookNotifications)
	state.mu.Unlock()
	if secondLen != 1 {
		t.Fatalf("second queue len = %d, want unchanged 1 after run finished", secondLen)
	}
	secondEvents := collectRuntimeEvents(service.Events())
	if got := countEventType(secondEvents, EventHookNotification); got != 0 {
		t.Fatalf("hook_notification events after finished run = %d, want 0", got)
	}
}

func TestRunInjectsEphemeralHookNotificationWithoutPersistingHistory(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	toolManager := &stubToolManager{
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return tools.ToolResult{Name: "filesystem_read_file", Content: "ok"}, nil
		},
	}
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
				},
				FinishReason: "stop",
			},
		},
	}
	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: providerImpl}, &stubContextBuilder{})

	registry := runtimehooks.NewRegistry()
	if err := registry.Register(runtimehooks.HookSpec{
		ID:    "async-rewake-before-tool",
		Point: runtimehooks.HookPointBeforeToolCall,
		Mode:  runtimehooks.HookModeAsyncRewake,
		Handler: func(context.Context, runtimehooks.HookContext) runtimehooks.HookResult {
			return runtimehooks.HookResult{
				Status: runtimehooks.HookResultPass,
				Metadata: runtimehooks.HookResultMetadata{
					Rewake:        true,
					RewakeReason:  "tool_follow_up",
					RewakeSummary: "verify tool side effects",
				},
			}
		},
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	service.SetHookExecutor(runtimehooks.NewExecutor(registry, newHookRuntimeEventEmitter(service), time.Second))

	if err := service.Run(context.Background(), UserInput{
		RunID: "run-ephemeral-inject",
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("read file then finish")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(providerImpl.requests) < 2 {
		t.Fatalf("provider requests = %d, want >=2", len(providerImpl.requests))
	}
	second := providerImpl.requests[1]
	if len(second.Messages) == 0 || second.Messages[0].Role != providertypes.RoleUser {
		t.Fatalf("second request messages = %+v, want original conversation messages", second.Messages)
	}
	injected := strings.TrimSpace(second.SystemPrompt)
	if !strings.Contains(injected, "[runtime_async_notifications]") {
		t.Fatalf("system prompt = %q, want runtime_async_notifications marker", injected)
	}

	saved := onlySession(t, store)
	for _, message := range saved.Messages {
		if strings.Contains(renderPartsForTest(message.Parts), "[runtime_async_notifications]") {
			t.Fatalf("ephemeral hint should not persist in session history: %+v", saved.Messages)
		}
	}

	events := collectRuntimeEvents(service.Events())
	if got := countEventType(events, EventHookNotification); got != 1 {
		t.Fatalf("hook_notification events = %d, want exactly 1", got)
	}
}

func TestMergeEphemeralHookNotificationIntoSystemPrompt(t *testing.T) {
	t.Parallel()

	if got := mergeEphemeralHookNotificationIntoSystemPrompt("base", "hint"); got != "base\n\nhint" {
		t.Fatalf("merge result = %q, want base + blank line + hint", got)
	}
	if got := mergeEphemeralHookNotificationIntoSystemPrompt("  ", "hint"); got != "hint" {
		t.Fatalf("merge blank base = %q, want hint", got)
	}
	if got := mergeEphemeralHookNotificationIntoSystemPrompt("base", "   "); got != "base" {
		t.Fatalf("merge blank hint = %q, want base", got)
	}
}

func countEventType(events []RuntimeEvent, target EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == target {
			count++
		}
	}
	return count
}
