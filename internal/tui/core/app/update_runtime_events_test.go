package tui

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/tui/services"
)

func TestRuntimeEventPhaseChangedHandlerBranches(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonCompleted},
	})
	if app.state.StatusText != statusReady {
		t.Fatalf("expected completed with empty execution error to set ready status")
	}

	app.state.ExecutionError = "boom"
	app.state.StatusText = ""
	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonCompleted},
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
			Reason: agentruntime.StopReasonMaxTurnsReached,
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
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReasonCompatibilityFallback},
	})
	if app.state.ExecutionError != "" || app.state.StatusText != "Completed via compatibility fallback" {
		t.Fatalf("expected compatibility fallback to be non-error, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: agentruntime.StopReason("STOP_UNKNOWN")},
	})
	if !strings.Contains(app.state.ExecutionError, "unknown stop reason") {
		t.Fatalf("expected unknown stop reason error, got %q", app.state.ExecutionError)
	}
}

func TestRuntimeEventHandlerRegistryContainsRenamedEvents(t *testing.T) {
	t.Parallel()

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
}

func TestShouldHandleRuntimeEventFiltersBySessionAndRun(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
			Status:             "failed",
			UserVisibleSummary: "",
			InternalSummary:    "",
			ContinueHint:       "provide missing files",
		},
	})
	acceptance := app.activities[len(app.activities)-1]
	if acceptance.Title != "Acceptance decided (failed)" || acceptance.Detail != "provide missing files" || !acceptance.IsError {
		t.Fatalf("unexpected acceptance activity: %+v", acceptance)
	}
}

func TestHandleRuntimeEventRoutesByRegistryWithoutBindingTransientSession(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
