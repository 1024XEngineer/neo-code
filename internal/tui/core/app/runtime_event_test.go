package tui

import (
	"testing"

	agentruntime "neo-code/internal/runtime"
)

func TestHandleRuntimeEventIgnoresStaleRunAndSession(t *testing.T) {
	app := App{}
	app.state.ActiveSessionID = "session-current"
	app.state.ActiveRunID = "run-current"

	dirty := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventAgentChunk,
		RunID:     "run-stale",
		SessionID: "session-stale",
		Payload:   "stale chunk",
	})
	if dirty {
		t.Fatalf("expected stale runtime event to be ignored")
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected stale runtime event to leave transcript untouched, got %+v", app.activeMessages)
	}
}
