package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	runtimehooks "neo-code/internal/runtime/hooks"
)

func TestShouldQueueAsyncRewakeBranches(t *testing.T) {
	t.Parallel()

	if !shouldQueueAsyncRewake(runtimehooks.HookResult{Status: runtimehooks.HookResultFailed}) {
		t.Fatal("failed status should queue rewake notification")
	}
	if !shouldQueueAsyncRewake(runtimehooks.HookResult{Status: runtimehooks.HookResultBlock}) {
		t.Fatal("block status should queue rewake notification")
	}
	if !shouldQueueAsyncRewake(runtimehooks.HookResult{
		Status: runtimehooks.HookResultPass,
		Metadata: runtimehooks.HookResultMetadata{
			Rewake: true,
		},
	}) {
		t.Fatal("metadata.rewake=true should queue rewake notification")
	}
	if shouldQueueAsyncRewake(runtimehooks.HookResult{Status: runtimehooks.HookResultPass}) {
		t.Fatal("plain pass status should not queue rewake notification")
	}
}

func TestReadHookRunTokenBranches(t *testing.T) {
	t.Parallel()

	if got := readHookRunToken(nil); got != 0 {
		t.Fatalf("readHookRunToken(nil) = %d, want 0", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": uint64(7)}); got != 7 {
		t.Fatalf("uint64 token = %d, want 7", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": int(8)}); got != 8 {
		t.Fatalf("int token = %d, want 8", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": int64(9)}); got != 9 {
		t.Fatalf("int64 token = %d, want 9", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": float64(10)}); got != 10 {
		t.Fatalf("float64 token = %d, want 10", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": "11"}); got != 11 {
		t.Fatalf("string token = %d, want 11", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": "invalid"}); got != 0 {
		t.Fatalf("invalid string token = %d, want 0", got)
	}
	if got := readHookRunToken(map[string]any{"runtime_run_token": int(-1)}); got != 0 {
		t.Fatalf("negative int token = %d, want 0", got)
	}
}

func TestResolveActiveRunStateBranches(t *testing.T) {
	t.Parallel()

	if got := (*Service)(nil).resolveActiveRunState("run", 1); got != nil {
		t.Fatal("nil service should return nil run state")
	}

	service := &Service{
		activeRunCancels:  map[uint64]context.CancelFunc{},
		activeRunByID:     map[string]uint64{},
		activeRunTokenIDs: map[uint64]string{},
		activeRunStates:   map[uint64]*runState{},
	}
	runID := "run-active"
	token := uint64(33)
	state := newRunState(runID, newRuntimeSession("session-active"))

	// token 未注册取消句柄 -> 非活跃
	service.activeRunStates[token] = &state
	if got := service.resolveActiveRunState(runID, token); got != nil {
		t.Fatal("missing activeRunCancels should be treated as inactive")
	}

	// 注册活跃 token + run id 映射
	service.activeRunCancels[token] = func() {}
	service.activeRunByID[runID] = token
	if got := service.resolveActiveRunState("other-run", token); got != nil {
		t.Fatal("run id mismatch should return nil")
	}
	if got := service.resolveActiveRunState(runID, token); got == nil {
		t.Fatal("expected active run state by token + run id")
	}

	// token=0 路径：根据 run id 回查
	if got := service.resolveActiveRunState("", 0); got != nil {
		t.Fatal("blank run id with token=0 should return nil")
	}
	if got := service.resolveActiveRunState(runID, 0); got == nil {
		t.Fatal("expected active run state by run id lookup")
	}

	// 移除 cancel 后应判定为 inactive
	delete(service.activeRunCancels, token)
	if got := service.resolveActiveRunState(runID, 0); got != nil {
		t.Fatal("inactive run should not be resolved by run id")
	}
}

func TestPurgeExpiredHookNotificationsLockedAndTruncateBranches(t *testing.T) {
	t.Parallel()

	state := newRunState("run-purge", newRuntimeSession("session-purge"))
	now := time.Now()
	old := now.Add(-NotificationTTL - time.Second)
	fresh := now.Add(-time.Second)
	state.hookNotifications = []queuedHookNotification{
		{
			Payload:   HookNotificationPayload{HookID: "old"},
			CreatedAt: old,
		},
		{
			Payload:   HookNotificationPayload{HookID: "fresh"},
			CreatedAt: fresh,
		},
	}
	state.hookNotificationSeen["old"] = old
	state.hookNotificationSeen["fresh"] = fresh

	purgeExpiredHookNotificationsLocked(&state, now)
	if len(state.hookNotifications) != 1 || state.hookNotifications[0].Payload.HookID != "fresh" {
		t.Fatalf("unexpected queue after purge: %+v", state.hookNotifications)
	}
	if _, ok := state.hookNotificationSeen["old"]; ok {
		t.Fatal("expired dedupe key should be removed")
	}
	if _, ok := state.hookNotificationSeen["fresh"]; !ok {
		t.Fatal("fresh dedupe key should remain")
	}

	if got := truncateNotificationText("abc", 0); got != "" {
		t.Fatalf("truncateNotificationText max<=0 = %q, want empty", got)
	}
	if got := truncateNotificationText("  abc  ", 3); got != "abc" {
		t.Fatalf("truncateNotificationText exact length = %q, want abc", got)
	}
	if got := truncateNotificationText("  abcd  ", 3); got != "abc" {
		t.Fatalf("truncateNotificationText trimmed truncation = %q, want abc", got)
	}
}

func TestBuildRuntimeHookNotificationDedupeKeyAndSinkFallbacks(t *testing.T) {
	t.Parallel()

	spec := runtimehooks.HookSpec{
		ID:     "HOOK-ID",
		Point:  runtimehooks.HookPointBeforeToolCall,
		Source: runtimehooks.HookSourceInternal,
		Mode:   runtimehooks.HookModeAsyncRewake,
	}
	message := strings.Repeat("M", 300)
	key := buildRuntimeHookNotificationDedupeKey(spec, runtimehooks.HookResult{
		Status:  runtimehooks.HookResultPass,
		Message: message,
		Metadata: runtimehooks.HookResultMetadata{
			RewakeReason:  "Reason",
			RewakeSummary: "Summary",
		},
	})
	if strings.Contains(key, strings.Repeat("m", 129)) {
		t.Fatalf("dedupe key should truncate message preview, got %q", key)
	}
	if key != strings.ToLower(key) {
		t.Fatalf("dedupe key should be normalized to lower-case, got %q", key)
	}

	service := &Service{
		activeRunCancels:  map[uint64]context.CancelFunc{},
		activeRunByID:     map[string]uint64{},
		activeRunTokenIDs: map[uint64]string{},
		activeRunStates:   map[uint64]*runState{},
		events:            make(chan RuntimeEvent, 8),
	}
	runID := "run-sink-fallback"
	token := uint64(44)
	state := newRunState(runID, newRuntimeSession("session-sink-fallback"))
	state.runToken = token
	service.activeRunCancels[token] = func() {}
	service.activeRunByID[runID] = token
	service.activeRunTokenIDs[token] = runID
	service.activeRunStates[token] = &state

	sink := newHookAsyncResultSink(service)
	// 非 async_rewake 模式不入队
	sink.HandleAsyncHookResult(context.Background(), runtimehooks.HookSpec{
		ID:   "skip-mode",
		Mode: runtimehooks.HookModeSync,
	}, runtimehooks.HookContext{RunID: runID, Metadata: map[string]any{"runtime_run_token": token}}, runtimehooks.HookResult{
		Status: runtimehooks.HookResultFailed,
	})
	state.mu.Lock()
	skipped := len(state.hookNotifications)
	state.mu.Unlock()
	if skipped != 0 {
		t.Fatalf("non async_rewake should not enqueue, got %d", skipped)
	}

	// async_rewake + block，验证 reason/summary 回退逻辑
	sink.HandleAsyncHookResult(context.Background(), spec, runtimehooks.HookContext{
		RunID: runID,
		Metadata: map[string]any{
			"runtime_run_token": token,
		},
	}, runtimehooks.HookResult{
		Status:  runtimehooks.HookResultBlock,
		Message: "need follow up",
		Error:   "fallback error",
	})
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.hookNotifications) != 1 {
		t.Fatalf("queue len = %d, want 1", len(state.hookNotifications))
	}
	payload := state.hookNotifications[0].Payload
	if payload.Summary != "need follow up" {
		t.Fatalf("summary fallback = %q, want message", payload.Summary)
	}
	if payload.Reason != "fallback error" {
		t.Fatalf("reason fallback = %q, want error", payload.Reason)
	}
}

func TestDrainHookNotificationsForTurnNilGuards(t *testing.T) {
	t.Parallel()

	if got := (*Service)(nil).drainHookNotificationsForTurn(nil); got != "" {
		t.Fatalf("nil service drain = %q, want empty", got)
	}
	service := &Service{}
	if got := service.drainHookNotificationsForTurn(nil); got != "" {
		t.Fatalf("nil state drain = %q, want empty", got)
	}
}
