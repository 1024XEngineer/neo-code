package tui

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/runtime/controlplane"
	tuiservices "neo-code/internal/tui/services"
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
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason(" success ")},
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
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason("success")},
	})
	if app.state.StatusText != statusReady {
		t.Fatalf("expected success with empty execution error to set ready status")
	}

	app.state.ExecutionError = "boom"
	app.state.StatusText = ""
	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason("success")},
	})
	if app.state.StatusText == statusReady {
		t.Fatalf("expected success branch to keep status unchanged when execution error exists")
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason("canceled")},
	})
	if app.state.ExecutionError != "" || app.state.StatusText != statusCanceled {
		t.Fatalf("expected canceled state to clear error and set canceled status")
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason("error"), Detail: "  "},
	})
	if app.state.StatusText != "runtime stopped" || app.state.ExecutionError != "runtime stopped" {
		t.Fatalf("expected default stop detail, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}

	runtimeEventStopReasonDecidedHandler(&app, agentruntime.RuntimeEvent{
		Payload: agentruntime.StopReasonDecidedPayload{Reason: controlplane.StopReason("error"), Detail: "explicit failure"},
	})
	if app.state.StatusText != "explicit failure" || app.state.ExecutionError != "explicit failure" {
		t.Fatalf("expected explicit stop detail to be surfaced")
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
		Type:      agentruntime.EventType(tuiservices.RuntimeEventRunContext),
		SessionID: "session-context",
		Payload: tuiservices.RuntimeRunContextPayload{
			Provider: "openai",
			Model:    "gpt-5.4",
		},
	})
	if app.state.ActiveSessionID != "session-context" {
		t.Fatalf("expected active session from run_context, got %q", app.state.ActiveSessionID)
	}
}
