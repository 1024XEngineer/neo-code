package tui

import (
	"testing"

	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/runtime/controlplane"
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
