package status

import (
	"testing"

	tuistate "neo-code/internal/tui/state"
)

func TestBuildFromUIState(t *testing.T) {
	state := tuistate.UIState{
		ActiveSessionID:    "session-1",
		ActiveSessionTitle: "Test Session",
		ActiveRunID:        "run-1",
		IsAgentRunning:     true,
		IsCompacting:       false,
		CurrentProvider:    "openai",
		CurrentModel:       "gpt-4",
		CurrentWorkdir:     "/home/user",
		CurrentTool:        "bash",
		ToolStates:         []tuistate.ToolState{},
		TokenUsage: tuistate.TokenUsageState{
			RunTotalTokens:     100,
			SessionTotalTokens: 500,
		},
		ExecutionError: "",
	}

	snapshot := BuildFromUIState(state, 10, "composer", "none")

	if snapshot.ActiveSessionID != "session-1" {
		t.Errorf("expected session ID session-1, got %s", snapshot.ActiveSessionID)
	}
	if snapshot.ActiveSessionTitle != "Test Session" {
		t.Errorf("expected session title 'Test Session', got %s", snapshot.ActiveSessionTitle)
	}
	if snapshot.IsAgentRunning != true {
		t.Errorf("expected IsAgentRunning true, got %v", snapshot.IsAgentRunning)
	}
	if snapshot.CurrentProvider != "openai" {
		t.Errorf("expected provider openai, got %s", snapshot.CurrentProvider)
	}
	if snapshot.MessageCount != 10 {
		t.Errorf("expected message count 10, got %d", snapshot.MessageCount)
	}
	if snapshot.ToolStateCount != 0 {
		t.Errorf("expected tool state count 0, got %d", snapshot.ToolStateCount)
	}
}

func TestBuildFromUIStateWithEmptyValues(t *testing.T) {
	state := tuistate.UIState{
		ActiveSessionID:    "",
		ActiveSessionTitle: "",
		ActiveRunID:        "",
		IsAgentRunning:     false,
		IsCompacting:       false,
		CurrentProvider:    "",
		CurrentModel:       "",
		CurrentWorkdir:     "",
		CurrentTool:        "",
		ToolStates:         nil,
		TokenUsage:         tuistate.TokenUsageState{},
		ExecutionError:     "",
	}

	snapshot := BuildFromUIState(state, 0, "", "")

	if snapshot.ActiveSessionID != "" {
		t.Errorf("expected empty session ID, got %s", snapshot.ActiveSessionID)
	}
	if snapshot.CurrentTool != "" {
		t.Errorf("expected empty current tool, got %s", snapshot.CurrentTool)
	}
}

func TestFormat(t *testing.T) {
	snapshot := Snapshot{
		ActiveSessionID:    "session-123",
		ActiveSessionTitle: "My Session",
		ActiveRunID:        "run-456",
		IsAgentRunning:     true,
		IsCompacting:       false,
		CurrentProvider:    "openai",
		CurrentModel:       "gpt-4",
		CurrentWorkdir:     "/home/user",
		CurrentTool:        "bash",
		ToolStateCount:     3,
		RunTotalTokens:     150,
		SessionTotalTokens: 1000,
		ExecutionError:     "",
		FocusLabel:         "composer",
		PickerLabel:        "none",
		MessageCount:       5,
	}

	output := Format(snapshot, "Draft Session")

	if !contains(output, "Session: My Session") {
		t.Errorf("expected output to contain 'Session: My Session', got %s", output)
	}
	if !contains(output, "Running: yes") {
		t.Errorf("expected output to contain 'Running: yes', got %s", output)
	}
	if !contains(output, "Provider: openai") {
		t.Errorf("expected output to contain 'Provider: openai', got %s", output)
	}
	if !contains(output, "Current Tool: bash") {
		t.Errorf("expected output to contain 'Current Tool: bash', got %s", output)
	}
}

func TestFormatWithEmptySession(t *testing.T) {
	snapshot := Snapshot{
		ActiveSessionID:    "",
		ActiveSessionTitle: "",
		ActiveRunID:        "",
		IsAgentRunning:     false,
		IsCompacting:       false,
		CurrentProvider:    "openai",
		CurrentModel:       "gpt-4",
		CurrentWorkdir:     "/home/user",
		CurrentTool:        "",
		ToolStateCount:     0,
		RunTotalTokens:     0,
		SessionTotalTokens: 0,
		ExecutionError:     "some error",
		FocusLabel:         "composer",
		PickerLabel:        "none",
		MessageCount:       0,
	}

	output := Format(snapshot, "Default Draft")

	if !contains(output, "Session: Default Draft") {
		t.Errorf("expected output to contain 'Session: Default Draft', got %s", output)
	}
	if !contains(output, "Session ID: <draft>") {
		t.Errorf("expected output to contain 'Session ID: <draft>', got %s", output)
	}
	if !contains(output, "Running: no") {
		t.Errorf("expected output to contain 'Running: no', got %s", output)
	}
	if !contains(output, "Current Tool: <none>") {
		t.Errorf("expected output to contain 'Current Tool: <none>', got %s", output)
	}
	if !contains(output, "Error: some error") {
		t.Errorf("expected output to contain 'Error: some error', got %s", output)
	}
}

func TestFormatWithCompacting(t *testing.T) {
	snapshot := Snapshot{
		ActiveSessionID:    "session-1",
		ActiveSessionTitle: "Test",
		IsAgentRunning:     false,
		IsCompacting:       true,
		CurrentProvider:    "openai",
		CurrentModel:       "gpt-4",
		CurrentWorkdir:     "/home",
		CurrentTool:        "",
		ToolStateCount:     0,
		RunTotalTokens:     0,
		SessionTotalTokens: 0,
		ExecutionError:     "",
		FocusLabel:         "",
		PickerLabel:        "",
		MessageCount:       0,
	}

	output := Format(snapshot, "")

	if !contains(output, "Running: yes") {
		t.Errorf("expected output to contain 'Running: yes' when compacting, got %s", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
