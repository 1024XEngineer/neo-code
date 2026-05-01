package tui

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/tui/services"
)

func TestRuntimeEventPhaseChangedHandlerBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if handled := runtimeEventPhaseChangedHandler(&app, agentruntime.RuntimeEvent{Payload: "invalid"}); handled {
		t.Fatalf("expected invalid payload to return false")
	}

	cases := []struct {
		to        string
		wantValue float64
		wantLabel string
	}{
		{to: " plan ", wantValue: 0.3, wantLabel: "Planning"},
		{to: "execute", wantValue: 0.6, wantLabel: "Running tools"},
		{to: "VERIFY", wantValue: 0.82, wantLabel: "Verifying"},
	}
	for _, tc := range cases {
		app.clearRunProgress()
		handled := runtimeEventPhaseChangedHandler(&app, agentruntime.RuntimeEvent{
			Payload: agentruntime.PhaseChangedPayload{To: tc.to},
		})
		if handled {
			t.Fatalf("expected phase handler to return false")
		}
		if !app.runProgressKnown || app.runProgressValue != tc.wantValue || app.runProgressLabel != tc.wantLabel {
			t.Fatalf("unexpected progress for %q: known=%v value=%v label=%q", tc.to, app.runProgressKnown, app.runProgressValue, app.runProgressLabel)
		}
	}

	app.clearRunProgress()
	runtimeEventPhaseChangedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.PhaseChangedPayload{To: "compacting"},
	})
	if app.runProgressKnown {
		t.Fatalf("expected non-plan/execute/verify phase to keep progress unchanged")
	}
}

func TestRuntimeEventStopReasonDecidedHandlerBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-1"},
	}
	app.state.IsAgentRunning = true
	app.state.StreamingReply = true
	app.state.CurrentTool = "bash"
	app.state.ActiveRunID = "run-1"
	app.state.ExecutionError = "should-clear"
	app.setRunProgress(0.8, "running")

	if handled := runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{Payload: 123}); handled {
		t.Fatalf("expected invalid payload to return false")
	}

	handled := runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReason(" STOP_COMPLETED ")},
	})
	if handled {
		t.Fatalf("expected handler to return false")
	}
	if app.state.IsAgentRunning || app.state.StreamingReply || app.state.CurrentTool != "" || app.state.ActiveRunID != "" {
		t.Fatalf("expected run flags to be reset")
	}
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared")
	}
	if app.runProgressKnown {
		t.Fatalf("expected run progress to be cleared")
	}
	if app.state.StatusText != statusReady {
		t.Fatalf("expected success status %q, got %q", statusReady, app.state.StatusText)
	}

	app.state.ExecutionError = ""
	app.state.StatusText = "not-ready"
	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonAccepted},
	})
	if app.state.StatusText != statusReady {
		t.Fatalf("expected completed with empty execution error to set ready status")
	}

	app.state.ExecutionError = "boom"
	app.state.StatusText = ""
	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonAccepted},
	})
	if app.state.StatusText == statusReady {
		t.Fatalf("expected completed branch to keep status unchanged when execution error exists")
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonUserInterrupt},
	})
	if app.state.ExecutionError != "" || app.state.StatusText != statusCanceled {
		t.Fatalf("expected canceled state to clear error and set canceled status")
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonBudgetExceeded},
	})
	if app.state.ExecutionError != "" || app.state.StatusText != "Context budget exceeded" {
		t.Fatalf("expected budget stop without execution error, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}
	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{
			Reason: agentruntime.StopReasonMaxTurnExceeded,
			Detail: "runtime: max turn limit reached (40)",
		},
	})
	if app.state.ExecutionError != "" || app.state.StatusText != "runtime: max turn limit reached (40)" {
		t.Fatalf("expected max turns stop without execution error, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonFatalError, Detail: "  "},
	})
	if app.state.StatusText != "runtime stopped" || app.state.ExecutionError != "runtime stopped" {
		t.Fatalf("expected fatal stop default detail, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonFatalError, Detail: "explicit failure"},
	})
	if app.state.StatusText != "explicit failure" || app.state.ExecutionError != "explicit failure" {
		t.Fatalf("expected explicit fatal stop detail to be surfaced")
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReason("STOP_UNKNOWN")},
	})
	if !strings.Contains(app.state.ExecutionError, "unknown stop reason") {
		t.Fatalf("expected unknown stop reason error, got %q", app.state.ExecutionError)
	}
}

func TestRuntimeEventHandlerRegistryContainsRenamedEvents(t *testing.T) {
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventPhaseChanged]; !ok {
		t.Fatalf("expected phase_changed handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventStopReasonDecided]; !ok {
		t.Fatalf("expected stop_reason_decided handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventPermissionRequested]; !ok {
		t.Fatalf("expected permission_requested handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventCompactApplied]; !ok {
		t.Fatalf("expected compact_applied handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSkillActivated]; !ok {
		t.Fatalf("expected skill_activated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSkillDeactivated]; !ok {
		t.Fatalf("expected skill_deactivated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSkillMissing]; !ok {
		t.Fatalf("expected skill_missing handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventVerificationStarted]; !ok {
		t.Fatalf("expected verification_started handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventVerificationStageFinished]; !ok {
		t.Fatalf("expected verification_stage_finished handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventVerificationFinished]; !ok {
		t.Fatalf("expected verification_finished handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventVerificationCompleted]; !ok {
		t.Fatalf("expected verification_completed handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventVerificationFailed]; !ok {
		t.Fatalf("expected verification_failed handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventAcceptanceDecided]; !ok {
		t.Fatalf("expected acceptance_decided handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventHookStarted]; !ok {
		t.Fatalf("expected hook_started handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventHookFinished]; !ok {
		t.Fatalf("expected hook_finished handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventHookFailed]; !ok {
		t.Fatalf("expected hook_failed handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventHookBlocked]; !ok {
		t.Fatalf("expected hook_blocked handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventRepoHooksDiscovered]; !ok {
		t.Fatalf("expected repo_hooks_discovered handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventRepoHooksLoaded]; !ok {
		t.Fatalf("expected repo_hooks_loaded handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventRepoHooksSkippedUntrusted]; !ok {
		t.Fatalf("expected repo_hooks_skipped_untrusted handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventRepoHooksTrustStoreInvalid]; !ok {
		t.Fatalf("expected repo_hooks_trust_store_invalid handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSubAgentStarted]; !ok {
		t.Fatalf("expected subagent_started handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSubAgentFailed]; !ok {
		t.Fatalf("expected subagent_failed handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSubAgentToolCallResult]; !ok {
		t.Fatalf("expected subagent_tool_call_result handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventRuntimeSnapshotUpdated]; !ok {
		t.Fatalf("expected runtime_snapshot_updated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventFactsUpdated]; !ok {
		t.Fatalf("expected facts_updated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventDecisionMade]; !ok {
		t.Fatalf("expected decision_made handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventSubAgentSnapshotUpdated]; !ok {
		t.Fatalf("expected subagent_snapshot_updated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventTodoSnapshotUpdated]; !ok {
		t.Fatalf("expected todo_snapshot_updated handler to be registered")
	}
}

func TestRuntimeSnapshotAndFactsHandlers(t *testing.T) {
	app, _ := newTestApp(t)

	if runtimeEventRuntimeSnapshotUpdatedHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}) {
		t.Fatalf("expected invalid runtime snapshot payload to return false")
	}
	if runtimeEventFactsUpdatedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}) {
		t.Fatalf("expected invalid facts payload to return false")
	}
	if runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{Payload: true}) {
		t.Fatalf("expected invalid decision payload to return false")
	}
	if runtimeEventSubAgentSnapshotUpdatedHandler(&app, agentruntime.RuntimeEvent{Payload: []string{"bad"}}) {
		t.Fatalf("expected invalid subagent snapshot payload to return false")
	}

	runtimeEventRuntimeSnapshotUpdatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.RuntimeSnapshotUpdatedPayload{
			Reason: "tool_result",
			Snapshot: agentruntime.RuntimeSnapshot{
				Todos: agentruntime.TodoSnapshot{
					Items: []agentruntime.TodoViewItem{
						{ID: "t1", Content: "demo", Status: "pending"},
					},
					Summary: agentruntime.TodoSummary{Total: 1, RequiredTotal: 1, RequiredOpen: 1},
				},
			},
		},
	})
	if len(app.todoItems) != 1 || app.todoItems[0].ID != "t1" {
		t.Fatalf("expected todo panel synced from runtime snapshot, got %+v", app.todoItems)
	}

	runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.DecisionMadePayload{
			Status:             "continue",
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "need verification facts",
			MissingFacts: []map[string]any{
				{"kind": "verification_passed", "target": "test.txt"},
			},
			RequiredNextActions: []map[string]any{
				{"tool": "filesystem_read_file"},
			},
		},
	})
	last := app.activities[len(app.activities)-1]
	if last.Title != "Final decision (continue)" {
		t.Fatalf("unexpected decision activity: %+v", last)
	}
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected decision block inline message")
	}
	decisionText := renderMessagePartsForDisplay(app.activeMessages[len(app.activeMessages)-1].Parts)
	if !strings.Contains(decisionText, "正在验收中") || !strings.Contains(decisionText, "缺少事实") {
		t.Fatalf("expected friendly runtime decision hint, got %q", decisionText)
	}
}

func TestRuntimeEventHookHandlers(t *testing.T) {
	app, _ := newTestApp(t)
	if runtimeEventHookStartedHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}) {
		t.Fatalf("expected invalid hook_started payload to return false")
	}
	if runtimeEventHookFinishedHandler(&app, agentruntime.RuntimeEvent{Payload: 123}) {
		t.Fatalf("expected invalid hook_finished payload to return false")
	}
	if runtimeEventHookFailedHandler(&app, agentruntime.RuntimeEvent{Payload: true}) {
		t.Fatalf("expected invalid hook_failed payload to return false")
	}
	if runtimeEventHookBlockedHandler(&app, agentruntime.RuntimeEvent{Payload: 1.23}) {
		t.Fatalf("expected invalid hook_blocked payload to return false")
	}

	runtimeEventHookStartedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookEventPayload{HookID: "  ", Point: " "},
	})
	last := app.activities[len(app.activities)-1]
	if last.Title != "Hook started" || !strings.Contains(last.Detail, "unknown_hook @ unknown_point") {
		t.Fatalf("unexpected hook started activity: %+v", last)
	}

	runtimeEventHookFinishedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookEventPayload{
			HookID:     "h1",
			Source:     "user",
			Status:     "pass",
			DurationMS: 12,
			Message:    "note",
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook finished: user:h1" || !strings.Contains(last.Detail, "pass (12ms) · note") {
		t.Fatalf("unexpected hook finished activity: %+v", last)
	}
	runtimeEventHookFinishedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookEventPayload{HookID: " ", Status: " ", DurationMS: 0},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook finished: unknown_hook" || !strings.Contains(last.Detail, "pass (0ms)") {
		t.Fatalf("unexpected hook finished default activity: %+v", last)
	}

	runtimeEventHookFailedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookEventPayload{HookID: "hook-1", Source: "repo", Error: "boom", Message: "warn"},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook failed: repo:hook-1" || !strings.Contains(last.Detail, "warn · boom") || !last.IsError {
		t.Fatalf("unexpected hook failed activity: %+v", last)
	}
	runtimeEventHookFailedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookEventPayload{HookID: " ", Error: " ", Message: " "},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook failed: unknown_hook" || !strings.Contains(last.Detail, "hook execution failed") || !last.IsError {
		t.Fatalf("unexpected hook failed default activity: %+v", last)
	}

	runtimeEventHookBlockedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookBlockedPayload{
			HookID:   "h2",
			Source:   "repo",
			Point:    "before_tool_call",
			Reason:   "blocked",
			Enforced: true,
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook blocked: repo:h2 @ before_tool_call" || last.Detail != "blocked" || !last.IsError {
		t.Fatalf("unexpected hook blocked activity: %+v", last)
	}
	runtimeEventHookBlockedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.HookBlockedPayload{HookID: " ", Point: " ", Reason: " ", Enforced: false},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Hook block observed: unknown_hook @ unknown_point" || last.Detail != "hook returned block" || last.IsError {
		t.Fatalf("unexpected hook blocked default activity: %+v", last)
	}
}

func TestRuntimeEventRepoHookLifecycleHandlers(t *testing.T) {
	app, _ := newTestApp(t)

	if runtimeEventRepoHooksDiscoveredHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}) {
		t.Fatalf("expected invalid discovered payload to return false")
	}
	runtimeEventRepoHooksDiscoveredHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.RepoHooksLifecyclePayload{Workspace: "/ws/a", HooksPath: "/ws/a/.neocode/hooks.yaml"},
	})
	last := app.activities[len(app.activities)-1]
	if last.Title != "Repo hooks discovered" || !strings.Contains(last.Detail, "hooks.yaml") {
		t.Fatalf("unexpected discovered activity: %+v", last)
	}

	if runtimeEventRepoHooksLoadedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}) {
		t.Fatalf("expected invalid loaded payload to return false")
	}
	runtimeEventRepoHooksLoadedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.RepoHooksLifecyclePayload{Workspace: "/ws/a", HookCount: 2},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Repo hooks loaded" || !strings.Contains(last.Detail, "hooks=2") {
		t.Fatalf("unexpected loaded activity: %+v", last)
	}

	if runtimeEventRepoHooksSkippedUntrustedHandler(&app, agentruntime.RuntimeEvent{Payload: true}) {
		t.Fatalf("expected invalid skipped payload to return false")
	}
	runtimeEventRepoHooksSkippedUntrustedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.RepoHooksLifecyclePayload{Reason: "workspace is not trusted"},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Repo hooks skipped (untrusted)" || last.Detail != "workspace is not trusted" {
		t.Fatalf("unexpected skipped activity: %+v", last)
	}

	if runtimeEventRepoHooksTrustStoreInvalidHandler(&app, agentruntime.RuntimeEvent{Payload: 3.14}) {
		t.Fatalf("expected invalid trust_store_invalid payload to return false")
	}
	runtimeEventRepoHooksTrustStoreInvalidHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.RepoHooksTrustStoreInvalidPayload{Reason: "trust store is missing"},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "Repo hooks trust store invalid" || last.Detail != "trust store is missing" {
		t.Fatalf("unexpected trust_store_invalid activity: %+v", last)
	}
}

func TestRuntimeEventSubAgentHandlers(t *testing.T) {
	app, _ := newTestApp(t)
	if runtimeEventSubAgentLifecycleHandler(&app, agentruntime.RuntimeEvent{
		Type:    agentruntime.EventSubAgentStarted,
		Payload: "bad",
	}) {
		t.Fatalf("expected invalid subagent lifecycle payload to return false")
	}
	runtimeEventSubAgentLifecycleHandler(&app, agentruntime.RuntimeEvent{
		Type: agentruntime.EventSubAgentProgress,
		Payload: agentruntime.SubAgentEventPayload{
			Role:   "coder",
			TaskID: "task-1",
			State:  "running",
			Step:   2,
			Delta:  "working",
		},
	})
	last := app.activities[len(app.activities)-1]
	if last.Title != "SubAgent progress" || !strings.Contains(last.Detail, "task=task-1") || last.IsError {
		t.Fatalf("unexpected subagent progress activity: %+v", last)
	}

	runtimeEventSubAgentLifecycleHandler(&app, agentruntime.RuntimeEvent{
		Type: agentruntime.EventSubAgentFailed,
		Payload: agentruntime.SubAgentEventPayload{
			Role:  "reviewer",
			State: "failed",
			Error: "boom",
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "SubAgent failed" || !strings.Contains(last.Detail, "error=boom") || !last.IsError {
		t.Fatalf("unexpected subagent failed activity: %+v", last)
	}

	if runtimeEventSubAgentToolCallHandler(&app, agentruntime.RuntimeEvent{
		Type:    agentruntime.EventSubAgentToolCallStarted,
		Payload: 1,
	}) {
		t.Fatalf("expected invalid subagent tool call payload to return false")
	}
	runtimeEventSubAgentToolCallHandler(&app, agentruntime.RuntimeEvent{
		Type: agentruntime.EventSubAgentToolCallResult,
		Payload: agentruntime.SubAgentToolCallEventPayload{
			Role:      "coder",
			TaskID:    "task-2",
			ToolName:  "bash",
			Decision:  "allow",
			ElapsedMS: 88,
		},
	})
	last = app.activities[len(app.activities)-1]
	if last.Title != "SubAgent tool call result" || !strings.Contains(last.Detail, "tool=bash") || last.IsError {
		t.Fatalf("unexpected subagent tool call result activity: %+v", last)
	}
}

func TestShouldHandleRuntimeEventFiltersBySessionAndRun(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-active"
	app.state.ActiveRunID = "run-active"

	if app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		SessionID: "session-other",
		RunID:     "run-active",
	}) {
		t.Fatalf("expected mismatched session event to be ignored")
	}
	if app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		SessionID: "session-active",
		RunID:     "run-other",
	}) {
		t.Fatalf("expected mismatched run event to be ignored")
	}
	if !app.shouldHandleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		SessionID: "session-active",
		RunID:     "run-active",
	}) {
		t.Fatalf("expected matched event to be handled")
	}
}

func TestRuntimeEventMultimodalHandlers(t *testing.T) {
	app, _ := newTestApp(t)

	if handled := runtimeEventInputNormalizedHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}); handled {
		t.Fatalf("expected invalid normalized payload to return false")
	}
	runtimeEventInputNormalizedHandler(&app, agentruntime.RuntimeEvent{
		RunID: "run-1",
		Payload: agentruntime.InputNormalizedPayload{
			TextLength: 12,
			ImageCount: 2,
		},
	})
	if app.state.ActiveRunID != "run-1" {
		t.Fatalf("expected active run id to be updated, got %q", app.state.ActiveRunID)
	}
	if len(app.activities) == 0 {
		t.Fatalf("expected input normalized activity to be appended")
	}
	last := app.activities[len(app.activities)-1]
	if last.Title != "Input normalized" || !strings.Contains(last.Detail, "images=2") {
		t.Fatalf("unexpected normalized activity: %+v", last)
	}

	before := len(app.activities)
	runtimeEventAssetSavedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.AssetSavedPayload{
			AssetID: "asset-1",
			Path:    "/tmp/chart.png",
		},
	})
	if len(app.activities) != before+1 {
		t.Fatalf("expected saved attachment activity appended")
	}
	last = app.activities[len(app.activities)-1]
	if last.Title != "Saved attachment" || !strings.Contains(last.Detail, "chart.png") {
		t.Fatalf("unexpected asset saved activity: %+v", last)
	}
	if handled := runtimeEventAssetSavedHandler(&app, agentruntime.RuntimeEvent{Payload: 123}); handled {
		t.Fatalf("expected invalid asset_saved payload to return false")
	}

	runtimeEventAssetSaveFailedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.AssetSaveFailedPayload{Message: " failed "},
	})
	if app.state.ExecutionError != "failed" || app.state.StatusText != "failed" {
		t.Fatalf("expected failed status to be surfaced, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}
	last = app.activities[len(app.activities)-1]
	if !last.IsError || last.Title != "Failed to save attachment" {
		t.Fatalf("unexpected asset save failed activity: %+v", last)
	}
	runtimeEventAssetSaveFailedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.AssetSaveFailedPayload{},
	})
	if app.state.ExecutionError != "failed to save attachment" || app.state.StatusText != "failed to save attachment" {
		t.Fatalf("expected default failed message, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}
	if handled := runtimeEventAssetSaveFailedHandler(&app, agentruntime.RuntimeEvent{Payload: true}); handled {
		t.Fatalf("expected invalid asset_save_failed payload to return false")
	}
}

func TestRuntimeEventVerificationAndAcceptanceHandlers(t *testing.T) {
	app, _ := newTestApp(t)

	if handled := runtimeEventVerificationStartedHandler(&app, agentruntime.RuntimeEvent{Payload: "bad"}); handled {
		t.Fatalf("expected invalid verification_started payload to return false")
	}
	runtimeEventVerificationStartedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationStartedPayload{CompletionPassed: false},
	})
	if !app.runProgressKnown || app.runProgressValue != 0.84 || app.runProgressLabel != "Verifying acceptance" {
		t.Fatalf("unexpected progress after verification_started: known=%v value=%v label=%q", app.runProgressKnown, app.runProgressValue, app.runProgressLabel)
	}
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Verification started" {
		t.Fatalf("expected verification started activity, got %+v", app.activities)
	}

	runtimeEventVerificationStartedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationStartedPayload{CompletionPassed: true},
	})
	if app.runProgressValue != 0.88 {
		t.Fatalf("progress value = %v, want 0.88 when completion passed", app.runProgressValue)
	}
	runtimeEventVerificationStartedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationStartedPayload{
			CompletionPassed:        false,
			CompletionBlockedReason: "pending_todo",
		},
	})
	verificationStarted := app.activities[len(app.activities)-1]
	if !strings.Contains(verificationStarted.Detail, "reason=pending_todo") {
		t.Fatalf("expected verification started detail to include blocked reason, got %+v", verificationStarted)
	}

	if handled := runtimeEventVerificationStageFinishedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}); handled {
		t.Fatalf("expected invalid stage payload to return false")
	}
	runtimeEventVerificationStageFinishedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationStageFinishedPayload{
			Name:    "git_diff",
			Status:  "fail",
			Summary: "",
			Reason:  "",
		},
	})
	stage := app.activities[len(app.activities)-1]
	if stage.Title != "Verifier stage: git_diff" || stage.Detail != "no summary" || !stage.IsError {
		t.Fatalf("unexpected stage activity: %+v", stage)
	}

	if handled := runtimeEventVerificationFinishedHandler(&app, agentruntime.RuntimeEvent{Payload: true}); handled {
		t.Fatalf("expected invalid verification_finished payload to return false")
	}
	runtimeEventVerificationFinishedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationFinishedPayload{
			AcceptanceStatus: "accepted",
			StopReason:       agentruntime.StopReasonAccepted,
		},
	})
	if app.runProgressValue != 0.92 || app.runProgressLabel != "Verification finished" {
		t.Fatalf("unexpected progress after verification_finished: value=%v label=%q", app.runProgressValue, app.runProgressLabel)
	}

	if handled := runtimeEventVerificationCompletedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}); handled {
		t.Fatalf("expected invalid verification_completed payload to return false")
	}
	runtimeEventVerificationCompletedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationCompletedPayload{},
	})
	completed := app.activities[len(app.activities)-1]
	if completed.Title != "Verification completed" || completed.Detail != "accepted" || completed.IsError {
		t.Fatalf("unexpected completed activity: %+v", completed)
	}

	if handled := runtimeEventVerificationFailedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}); handled {
		t.Fatalf("expected invalid verification_failed payload to return false")
	}
	runtimeEventVerificationFailedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.VerificationFailedPayload{
			StopReason: agentruntime.StopReasonVerificationFailed,
			ErrorClass: "test_failure",
		},
	})
	failed := app.activities[len(app.activities)-1]
	if failed.Title != "Verification failed" || !strings.Contains(failed.Detail, "test_failure") || !failed.IsError {
		t.Fatalf("unexpected failed activity: %+v", failed)
	}

	if handled := runtimeEventAcceptanceDecidedHandler(&app, agentruntime.RuntimeEvent{Payload: nil}); handled {
		t.Fatalf("expected invalid acceptance payload to return false")
	}
	runtimeEventAcceptanceDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.AcceptanceDecidedPayload{
			Status:                  "failed",
			UserVisibleSummary:      "",
			InternalSummary:         "",
			ContinueHint:            "provide missing files",
			CompletionBlockedReason: "unverified_write",
		},
	})
	acceptance := app.activities[len(app.activities)-1]
	if acceptance.Title != "Acceptance decided (failed)" || !strings.Contains(acceptance.Detail, "reason=unverified_write") || !acceptance.IsError {
		t.Fatalf("unexpected acceptance activity: %+v", acceptance)
	}
}

func TestDecisionContinueSuppressesAssistantFinalMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.activeMessages = append(app.activeMessages, providertypes.Message{
		Role:  roleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("任务已完成")},
	})

	runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{
		RunID: "run-1",
		Payload: agentruntime.DecisionMadePayload{
			Status:             "continue",
			StopReason:         "todo_not_converged",
			UserVisibleSummary: "need verification",
		},
	})

	if len(app.activeMessages) == 0 {
		t.Fatalf("expected runtime decision block message")
	}
	last := app.activeMessages[len(app.activeMessages)-1]
	if last.Role != roleSystem || !strings.Contains(renderMessagePartsForDisplay(last.Parts), "正在验收中") {
		t.Fatalf("expected runtime decision block as last message, got %+v", last)
	}

	runtimeEventAgentDoneHandler(&app, agentruntime.RuntimeEvent{
		RunID: "run-1",
		Payload: providertypes.Message{
			Role:  roleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("任务已完成")},
		},
	})

	for _, message := range app.activeMessages {
		if message.Role != roleAssistant {
			continue
		}
		if strings.Contains(renderMessagePartsForDisplay(message.Parts), "任务已完成") {
			t.Fatalf("unexpected assistant final message kept after continue decision")
		}
	}
}

func TestRuntimeEventDecisionMadeHandlerAcceptedDoesNotRenderDecisionBlock(t *testing.T) {
	app, _ := newTestApp(t)
	initialMessages := len(app.activeMessages)

	runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.DecisionMadePayload{
			Status: "accepted",
		},
	})

	if len(app.activeMessages) == 0 {
		t.Fatalf("expected at least one message after accepted decision activity")
	}
	lastText := renderMessagePartsForDisplay(app.activeMessages[len(app.activeMessages)-1].Parts)
	if strings.Contains(lastText, "正在验收中") {
		t.Fatalf("accepted decision should not render runtime decision block, got %q", lastText)
	}
	if len(app.activeMessages) < initialMessages {
		t.Fatalf("message count should not shrink for accepted decision")
	}
	if len(app.activities) == 0 {
		t.Fatalf("expected decision activity to be appended")
	}
	last := app.activities[len(app.activities)-1]
	if last.Title != "Final decision (accepted)" {
		t.Fatalf("unexpected decision activity title: %+v", last)
	}
}

func TestRuntimeEventDecisionMadeHandlerFallsBackForEmptyStatusAndSummary(t *testing.T) {
	app, _ := newTestApp(t)

	runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.DecisionMadePayload{},
	})

	if len(app.activities) == 0 {
		t.Fatalf("expected fallback decision activity")
	}
	last := app.activities[len(app.activities)-1]
	if last.Title != "Final decision (unknown)" {
		t.Fatalf("unexpected fallback title: %+v", last)
	}
	if !strings.Contains(last.Detail, "decision generated") {
		t.Fatalf("expected fallback detail, got %+v", last)
	}
}

func TestRuntimeEventDecisionMadeHandlerUsesInternalSummaryFallback(t *testing.T) {
	app, _ := newTestApp(t)

	runtimeEventDecisionMadeHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.DecisionMadePayload{
			Status:          "blocked",
			InternalSummary: "blocked by policy",
		},
	})

	last := app.activities[len(app.activities)-1]
	if last.Title != "Final decision (blocked)" {
		t.Fatalf("unexpected decision title: %+v", last)
	}
	if !strings.Contains(last.Detail, "blocked by policy") {
		t.Fatalf("expected internal summary fallback, got %+v", last)
	}
}

func TestDecisionHelperBranches(t *testing.T) {
	if shouldSuppressAssistantFinalMessage(nil, "run-1") {
		t.Fatalf("nil app should never suppress assistant final message")
	}

	app, _ := newTestApp(t)
	if shouldSuppressAssistantFinalMessage(&app, "run-1") {
		t.Fatalf("empty suppression run id should return false")
	}

	app.suppressAssistantForRun = "run-2"
	if shouldSuppressAssistantFinalMessage(&app, "run-1") {
		t.Fatalf("mismatched run id should not suppress")
	}

	app.suppressAssistantForRun = "RUN-3"
	if !shouldSuppressAssistantFinalMessage(&app, "run-3") {
		t.Fatalf("case-insensitive run id match should suppress")
	}
	if app.suppressAssistantForRun != "" {
		t.Fatalf("suppression marker should be cleared after suppress")
	}

	discardTrailingAssistantMessage(nil)

	discardTrailingAssistantMessage(&app)
	if len(app.activeMessages) != 0 {
		t.Fatalf("empty messages should remain unchanged")
	}

	app.activeMessages = append(app.activeMessages, providertypes.Message{Role: roleSystem})
	discardTrailingAssistantMessage(&app)
	if len(app.activeMessages) != 1 {
		t.Fatalf("non-assistant tail should not be removed")
	}

	app.activeMessages = append(app.activeMessages, providertypes.Message{
		Role:  roleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
	})
	discardTrailingAssistantMessage(&app)
	if len(app.activeMessages) != 1 {
		t.Fatalf("assistant tail should be removed")
	}
}

func TestFormattingHelperBranches(t *testing.T) {
	if firstNonBlank(" ", "\t", "") != "" {
		t.Fatalf("all blank values should produce empty result")
	}
	if got := firstNonBlank(" ", "ok", "fallback"); got != "ok" {
		t.Fatalf("first non-blank value mismatch: %q", got)
	}

	if got := jsonCompactOrFallback(map[string]any{"k": "v"}); !strings.Contains(got, "\"k\":\"v\"") {
		t.Fatalf("json compact output mismatch: %q", got)
	}
	if got := jsonCompactOrFallback(map[string]any{"bad": func() {}}); !strings.Contains(got, "map[bad:") {
		t.Fatalf("fallback output mismatch for marshal error: %q", got)
	}
}

func TestHandleRuntimeEventRoutesByRegistryWithoutBindingTransientSession(t *testing.T) {
	app, _ := newTestApp(t)
	handled := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAssetSaved,
		SessionID: "session-1",
		Payload:   agentruntime.AssetSavedPayload{AssetID: "asset-1"},
	})
	if handled {
		t.Fatalf("expected asset_saved handler to return false")
	}
	if app.state.ActiveSessionID != "" {
		t.Fatalf("expected active session to stay empty for non-stable event, got %q", app.state.ActiveSessionID)
	}
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Saved attachment" {
		t.Fatalf("expected saved attachment activity")
	}

	if app.handleRuntimeEvent(agentruntime.RuntimeEvent{Type: "unknown_event", SessionID: "session-1"}) {
		t.Fatalf("expected unknown event handler result to be false")
	}
}

func TestHandleRuntimeEventBindsSessionFromStableEvents(t *testing.T) {
	app, _ := newTestApp(t)

	app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventUserMessage,
		SessionID: "session-user",
		RunID:     "run-1",
		Payload: providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		},
	})
	if app.state.ActiveSessionID != "session-user" {
		t.Fatalf("expected active session from user_message, got %q", app.state.ActiveSessionID)
	}

	app.state.ActiveSessionID = ""
	app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventType(agentruntime.RuntimeEventRunContext),
		SessionID: "session-context",
		Payload: agentruntime.RuntimeRunContextPayload{
			Provider: "openai",
			Model:    "gpt-5.4",
		},
	})
	if app.state.ActiveSessionID != "session-context" {
		t.Fatalf("expected active session from run_context, got %q", app.state.ActiveSessionID)
	}
}

func TestRuntimeSkillEventHandlers(t *testing.T) {
	app, _ := newTestApp(t)

	if handled := runtimeEventSkillActivatedHandler(&app, agentruntime.RuntimeEvent{Payload: 1}); handled {
		t.Fatalf("expected invalid payload to return false")
	}
	runtimeEventSkillActivatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.SessionSkillEventPayload{SkillID: "go-review"},
	})
	if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Skill activated" {
		t.Fatalf("expected skill activated activity")
	}

	runtimeEventSkillDeactivatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: map[string]any{"skill_id": "go-review"},
	})
	if app.activities[len(app.activities)-1].Title != "Skill deactivated" {
		t.Fatalf("expected skill deactivated activity")
	}

	runtimeEventSkillMissingHandler(&app, agentruntime.RuntimeEvent{
		Payload: map[string]any{"SkillID": "missing-skill"},
	})
	last := app.activities[len(app.activities)-1]
	if !last.IsError || last.Title != "Skill missing in registry" {
		t.Fatalf("expected skill missing error activity, got %+v", last)
	}

	runtimeEventSkillActivatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: &agentruntime.SessionSkillEventPayload{SkillID: " "},
	})
	last = app.activities[len(app.activities)-1]
	if !strings.Contains(last.Detail, "(unknown)") {
		t.Fatalf("expected unknown fallback for blank skill id, got %+v", last)
	}

	if handled := runtimeEventSkillDeactivatedHandler(&app, agentruntime.RuntimeEvent{Payload: map[string]any{}}); handled {
		t.Fatalf("expected empty map payload to be rejected")
	}
	if handled := runtimeEventSkillMissingHandler(&app, agentruntime.RuntimeEvent{Payload: (*agentruntime.SessionSkillEventPayload)(nil)}); handled {
		t.Fatalf("expected nil pointer payload to be rejected")
	}

	runtimeEventSkillDeactivatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.SessionSkillEventPayload{SkillID: " "},
	})
	last = app.activities[len(app.activities)-1]
	if !strings.Contains(last.Detail, "(unknown)") {
		t.Fatalf("expected unknown fallback for deactivated event, got %+v", last)
	}

	runtimeEventSkillMissingHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.SessionSkillEventPayload{SkillID: ""},
	})
	last = app.activities[len(app.activities)-1]
	if !last.IsError || !strings.Contains(last.Detail, "(unknown)") {
		t.Fatalf("expected unknown fallback for missing event, got %+v", last)
	}

	runtimeEventSkillActivatedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.SessionSkillEventPayload{SkillID: "go\x1b[31m-review"},
	})
	last = app.activities[len(app.activities)-1]
	if strings.Contains(last.Detail, "\x1b") {
		t.Fatalf("expected sanitized skill id in activity detail, got %+v", last)
	}
}

func TestParseSessionSkillEventPayloadBranches(t *testing.T) {
	if payload, ok := parseSessionSkillEventPayload(map[string]any{"skill_id": 42}); !ok || payload.SkillID != "42" {
		t.Fatalf("expected snake-case skill_id to be parsed, got payload=%+v ok=%v", payload, ok)
	}
	if payload, ok := parseSessionSkillEventPayload(map[string]any{"SkillID": " go-review "}); !ok || payload.SkillID != "go-review" {
		t.Fatalf("expected camel-case SkillID to be parsed, got payload=%+v ok=%v", payload, ok)
	}
	if _, ok := parseSessionSkillEventPayload(map[string]any{"unexpected": "value"}); ok {
		t.Fatalf("expected unknown map keys to be rejected")
	}
	if _, ok := parseSessionSkillEventPayload(nil); ok {
		t.Fatalf("expected nil payload to be rejected")
	}
}
