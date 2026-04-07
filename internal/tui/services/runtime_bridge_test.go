package services

import (
	"fmt"
	"testing"
	"time"

	tuistate "neo-code/internal/tui/state"
)

func TestRuntimeBridgeMappings(t *testing.T) {
	context := MapRunContextPayload("run-1", "session-1", RuntimeRunContextPayload{
		Provider: "openai",
		Model:    "gpt-5.4",
		Workdir:  "/repo",
		Mode:     "act",
	})
	if context.RunID != "run-1" || context.Provider != "openai" {
		t.Fatalf("unexpected context mapping: %+v", context)
	}

	tool := MapToolStatusPayload(RuntimeToolStatusPayload{
		ToolCallID: "call-1",
		ToolName:   "filesystem_edit",
		Status:     "succeeded",
		Message:    "ok",
		DurationMS: 120,
	})
	if tool.Status != tuistate.ToolLifecycleSucceeded || tool.DurationMS != 120 {
		t.Fatalf("unexpected tool mapping: %+v", tool)
	}

	usage := MapUsagePayload(RuntimeUsagePayload{
		Run:     RuntimeUsageSnapshot{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		Session: RuntimeUsageSnapshot{InputTokens: 40, OutputTokens: 50, TotalTokens: 90},
	})
	if usage.RunTotalTokens != 30 || usage.SessionTotalTokens != 90 {
		t.Fatalf("unexpected usage mapping: %+v", usage)
	}
}

func TestRuntimeBridgeParsers(t *testing.T) {
	ctx, ok := ParseRunContextPayload(map[string]any{
		"Provider": "openai",
		"Model":    "gpt-5.4",
		"Workdir":  "/repo",
		"Mode":     "act",
	})
	if !ok || ctx.Provider != "openai" {
		t.Fatalf("expected run_context payload to parse, got %+v", ctx)
	}

	tool, ok := ParseToolStatusPayload(map[string]any{
		"ToolCallID": "call-1",
		"ToolName":   "filesystem_edit",
		"Status":     "running",
		"DurationMS": int64(99),
	})
	if !ok || tool.ToolCallID != "call-1" || tool.DurationMS != 99 {
		t.Fatalf("expected tool_status payload to parse, got %+v", tool)
	}

	usage, ok := ParseUsagePayload(map[string]any{
		"Run": map[string]any{
			"InputTokens":  1,
			"OutputTokens": 2,
			"TotalTokens":  3,
		},
		"Session": map[string]any{
			"InputTokens":  10,
			"OutputTokens": 20,
			"TotalTokens":  30,
		},
	})
	if !ok || usage.Run.TotalTokens != 3 || usage.Session.TotalTokens != 30 {
		t.Fatalf("expected usage payload to parse, got %+v", usage)
	}
}

func TestMergeToolStatesHandlesDuplicateAndLimit(t *testing.T) {
	now := time.Now()
	states := []ToolStateVM{
		{
			ToolCallID: "call-1",
			ToolName:   "tool-a",
			Status:     tuistate.ToolLifecycleRunning,
			UpdatedAt:  now,
		},
	}

	updated := MergeToolStates(states, ToolStateVM{
		ToolCallID: "call-1",
		ToolName:   "tool-a",
		Status:     tuistate.ToolLifecycleSucceeded,
		UpdatedAt:  now.Add(time.Second),
	}, 2)
	if len(updated) != 1 || updated[0].Status != tuistate.ToolLifecycleSucceeded {
		t.Fatalf("expected duplicate to be replaced, got %+v", updated)
	}

	updated = MergeToolStates(updated, ToolStateVM{
		ToolCallID: "call-2",
		ToolName:   "tool-b",
		Status:     tuistate.ToolLifecycleRunning,
		UpdatedAt:  now.Add(2 * time.Second),
	}, 1)
	if len(updated) != 1 || updated[0].ToolCallID != "call-2" {
		t.Fatalf("expected limit to keep newest item, got %+v", updated)
	}
}

func TestMapRunSnapshot(t *testing.T) {
	now := time.Now()
	context, tools, usage := MapRunSnapshot(RuntimeRunSnapshot{
		RunID:     "run-2",
		SessionID: "session-2",
		Context: RuntimeRunContextSnapshot{
			RunID:     "run-2",
			SessionID: "session-2",
			Provider:  "openai",
			Model:     "gpt-5.4-mini",
			Workdir:   "/repo",
			Mode:      "act",
		},
		ToolStates: []RuntimeToolStateSnapshot{
			{
				ToolCallID: "call-1",
				ToolName:   "filesystem_read_file",
				Status:     "succeeded",
				Message:    "ok",
				DurationMS: 88,
				UpdatedAt:  now,
			},
		},
		Usage:        RuntimeUsageSnapshot{InputTokens: 3, OutputTokens: 7, TotalTokens: 10},
		SessionUsage: RuntimeUsageSnapshot{InputTokens: 30, OutputTokens: 70, TotalTokens: 100},
	})

	if context.RunID != "run-2" || len(tools) != 1 || usage.SessionTotalTokens != 100 {
		t.Fatalf("unexpected run snapshot mapping: context=%+v tools=%+v usage=%+v", context, tools, usage)
	}
}

func TestRuntimeBridgeParsersTypedAndNilInputs(t *testing.T) {
	ctx, ok := ParseRunContextPayload(RuntimeRunContextPayload{
		Provider: " openai ",
		Model:    " gpt-5.4 ",
	})
	if !ok || ctx.Provider != "openai" || ctx.Model != "gpt-5.4" {
		t.Fatalf("expected typed run context payload to parse, got %+v ok=%v", ctx, ok)
	}

	var nilRunContext *RuntimeRunContextPayload
	if _, ok := ParseRunContextPayload(nilRunContext); ok {
		t.Fatalf("expected nil run context pointer to fail parsing")
	}
	if _, ok := ParseRunContextPayload(map[string]any{"Provider": " ", "Model": " "}); ok {
		t.Fatalf("expected empty run context map to fail parsing")
	}

	tool, ok := ParseToolStatusPayload(map[string]any{
		"ToolCallID": 12345,
		"ToolName":   "  filesystem  ",
		"Status":     " failed ",
		"Message":    " boom ",
		"DurationMS": "88",
	})
	if !ok || tool.ToolCallID != "12345" || tool.ToolName != "filesystem" || tool.DurationMS != 88 {
		t.Fatalf("unexpected tool status parse result: %+v ok=%v", tool, ok)
	}

	var nilToolStatus *RuntimeToolStatusPayload
	if _, ok := ParseToolStatusPayload(nilToolStatus); ok {
		t.Fatalf("expected nil tool status pointer to fail parsing")
	}
	if _, ok := ParseToolStatusPayload(map[string]any{"ToolCallID": " ", "ToolName": ""}); ok {
		t.Fatalf("expected empty tool status payload to fail parsing")
	}

	usage, ok := ParseUsagePayload(map[string]any{
		"Delta": map[string]any{
			"InputTokens":  "1",
			"OutputTokens": float64(2),
			"TotalTokens":  int64(3),
		},
		"Run": &RuntimeUsageSnapshot{
			InputTokens:  4,
			OutputTokens: 5,
			TotalTokens:  9,
		},
		"Session": RuntimeUsageSnapshot{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})
	if !ok || usage.Delta.TotalTokens != 3 || usage.Run.TotalTokens != 9 || usage.Session.TotalTokens != 30 {
		t.Fatalf("unexpected usage payload parse result: %+v ok=%v", usage, ok)
	}

	var nilUsage *RuntimeUsagePayload
	if _, ok := ParseUsagePayload(nilUsage); ok {
		t.Fatalf("expected nil usage pointer to fail parsing")
	}
	if _, ok := ParseUsagePayload(RuntimeUsagePayload{}); ok {
		t.Fatalf("expected zero usage payload to fail parsing")
	}
}

func TestRuntimeBridgeSnapshotParsers(t *testing.T) {
	session, ok := ParseSessionContextSnapshot(map[string]any{
		"SessionID": " session-1 ",
		"Provider":  " openai ",
		"Model":     " gpt-5.4 ",
		"Workdir":   " /repo ",
		"Mode":      " act ",
	})
	if !ok || session.SessionID != "session-1" || session.Workdir != "/repo" {
		t.Fatalf("unexpected session snapshot parse result: %+v ok=%v", session, ok)
	}

	var nilSession *RuntimeSessionContextSnapshot
	if _, ok := ParseSessionContextSnapshot(nilSession); ok {
		t.Fatalf("expected nil session snapshot pointer to fail parsing")
	}
	if _, ok := ParseSessionContextSnapshot(RuntimeSessionContextSnapshot{}); ok {
		t.Fatalf("expected empty session snapshot to fail parsing")
	}

	usage, ok := ParseUsageSnapshot(map[string]any{
		"InputTokens":  "11",
		"OutputTokens": float64(22),
		"TotalTokens":  int32(33),
	})
	if !ok || usage.TotalTokens != 33 {
		t.Fatalf("unexpected usage snapshot parse result: %+v ok=%v", usage, ok)
	}
	if _, ok := ParseUsageSnapshot(RuntimeUsageSnapshot{}); ok {
		t.Fatalf("expected empty usage snapshot to fail parsing")
	}
}

func TestRuntimeBridgeParseRunSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	existing := RuntimeToolStateSnapshot{
		ToolCallID: "call-2",
		ToolName:   "tool-b",
		Status:     "failed",
	}
	snapshot, ok := ParseRunSnapshot(map[string]any{
		"RunID":     " run-9 ",
		"SessionID": " session-9 ",
		"Context": map[string]any{
			"RunID":     "run-9",
			"SessionID": "session-9",
			"Provider":  " openai ",
			"Model":     " gpt-5.4 ",
			"Workdir":   " /repo ",
			"Mode":      " act ",
		},
		"ToolStates": []any{
			map[string]any{
				"ToolCallID": "call-1",
				"ToolName":   "tool-a",
				"Status":     "succeeded",
				"Message":    "ok",
				"DurationMS": "41",
				"UpdatedAt":  now,
			},
			&existing,
			"ignored",
			(*RuntimeToolStateSnapshot)(nil),
		},
		"Usage": map[string]any{
			"InputTokens":  1,
			"OutputTokens": 2,
			"TotalTokens":  3,
		},
		"SessionUsage": RuntimeUsageSnapshot{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})
	if !ok {
		t.Fatalf("expected run snapshot to parse")
	}
	if snapshot.RunID != "run-9" || snapshot.SessionID != "session-9" {
		t.Fatalf("unexpected run/session ids: %+v", snapshot)
	}
	if len(snapshot.ToolStates) != 2 || snapshot.ToolStates[0].DurationMS != 41 {
		t.Fatalf("unexpected parsed tool states: %+v", snapshot.ToolStates)
	}
	if !snapshot.ToolStates[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected parsed updated-at timestamp, got %v", snapshot.ToolStates[0].UpdatedAt)
	}
	if snapshot.Context.Provider != "openai" {
		t.Fatalf("expected context provider from map, got %q", snapshot.Context.Provider)
	}

	var nilSnapshot *RuntimeRunSnapshot
	if _, ok := ParseRunSnapshot(nilSnapshot); ok {
		t.Fatalf("expected nil run snapshot pointer to fail parsing")
	}
	if _, ok := ParseRunSnapshot(map[string]any{"RunID": " ", "SessionID": ""}); ok {
		t.Fatalf("expected empty run snapshot ids to fail parsing")
	}
	if _, ok := ParseRunSnapshot(42); ok {
		t.Fatalf("expected unsupported run snapshot type to fail parsing")
	}
}

func TestRuntimeBridgeMapSnapshotHelpers(t *testing.T) {
	context := MapSessionContextSnapshot(RuntimeSessionContextSnapshot{
		SessionID: " session-2 ",
		Provider:  " openai ",
		Model:     " gpt-5.4-mini ",
		Workdir:   " /workspace ",
		Mode:      " plan ",
	})
	if context.SessionID != "session-2" || context.Provider != "openai" || context.Workdir != "/workspace" {
		t.Fatalf("unexpected mapped session context: %+v", context)
	}

	current := TokenUsageVM{
		RunInputTokens:      1,
		RunOutputTokens:     2,
		RunTotalTokens:      3,
		SessionInputTokens:  4,
		SessionOutputTokens: 5,
		SessionTotalTokens:  9,
	}
	mapped := MapUsageSnapshot(RuntimeUsageSnapshot{
		InputTokens:  100,
		OutputTokens: 200,
		TotalTokens:  300,
	}, current)
	if mapped.RunTotalTokens != 3 || mapped.SessionTotalTokens != 300 {
		t.Fatalf("unexpected mapped usage snapshot: %+v", mapped)
	}
}

func TestRuntimeBridgeMergeToolStatesCaseInsensitiveAndDefaultLimit(t *testing.T) {
	now := time.Now()
	replaced := MergeToolStates([]ToolStateVM{
		{
			ToolCallID: "Call-1",
			ToolName:   "Tool-A",
			Status:     tuistate.ToolLifecycleRunning,
			UpdatedAt:  now,
		},
	}, ToolStateVM{
		ToolCallID: "call-1",
		ToolName:   "tool-a",
		Status:     tuistate.ToolLifecycleSucceeded,
		UpdatedAt:  now.Add(time.Second),
	}, 0)
	if len(replaced) != 1 || replaced[0].Status != tuistate.ToolLifecycleSucceeded {
		t.Fatalf("expected case-insensitive duplicate replacement, got %+v", replaced)
	}

	var states []ToolStateVM
	for i := 0; i < 16; i++ {
		states = append(states, ToolStateVM{
			ToolCallID: fmt.Sprintf("call-%d", i),
			ToolName:   "tool",
			Status:     tuistate.ToolLifecycleRunning,
		})
	}
	states = MergeToolStates(states, ToolStateVM{
		ToolCallID: "call-16",
		ToolName:   "tool",
		Status:     tuistate.ToolLifecycleRunning,
	}, 0)
	if len(states) != 16 {
		t.Fatalf("expected default limit to keep 16 states, got %d", len(states))
	}
	if states[0].ToolCallID != "call-1" || states[len(states)-1].ToolCallID != "call-16" {
		t.Fatalf("expected oldest state to be evicted with default limit, got %+v", states)
	}
}

func TestRuntimeBridgeInternalHelpers(t *testing.T) {
	if got := mapToolLifecycleStatus("planned"); got != tuistate.ToolLifecyclePlanned {
		t.Fatalf("expected planned status, got %q", got)
	}
	if got := mapToolLifecycleStatus(" RUNNING "); got != tuistate.ToolLifecycleRunning {
		t.Fatalf("expected running status, got %q", got)
	}
	if got := mapToolLifecycleStatus("succeeded"); got != tuistate.ToolLifecycleSucceeded {
		t.Fatalf("expected succeeded status, got %q", got)
	}
	if got := mapToolLifecycleStatus("failed"); got != tuistate.ToolLifecycleFailed {
		t.Fatalf("expected failed status, got %q", got)
	}
	if got := mapToolLifecycleStatus("unknown"); got != tuistate.ToolLifecycleRunning {
		t.Fatalf("expected unknown status fallback to running, got %q", got)
	}

	if got := parseRunContextSnapshotFromAny(RuntimeRunContextSnapshot{RunID: "r1"}); got.RunID != "r1" {
		t.Fatalf("unexpected direct run context snapshot parse result: %+v", got)
	}
	if got := parseRunContextSnapshotFromAny((*RuntimeRunContextSnapshot)(nil)); got != (RuntimeRunContextSnapshot{}) {
		t.Fatalf("expected nil run context pointer to produce zero value, got %+v", got)
	}
	if got := parseRunContextSnapshotFromAny(map[string]any{"RunID": "r2", "Provider": "openai"}); got.RunID != "r2" {
		t.Fatalf("unexpected map run context snapshot parse result: %+v", got)
	}

	original := []RuntimeToolStateSnapshot{{ToolCallID: "call-1"}}
	cloned := parseToolStatesFromAny(original)
	if len(cloned) != 1 || cloned[0].ToolCallID != "call-1" {
		t.Fatalf("unexpected direct tool state parse result: %+v", cloned)
	}
	cloned[0].ToolCallID = "modified"
	if original[0].ToolCallID != "call-1" {
		t.Fatalf("expected parseToolStatesFromAny to return a copied slice")
	}

	parsedStates := parseToolStatesFromAny([]any{
		map[string]any{"ToolCallID": "call-2", "ToolName": "tool"},
		RuntimeToolStateSnapshot{ToolCallID: "call-3"},
		(*RuntimeToolStateSnapshot)(nil),
		"ignored",
	})
	if len(parsedStates) != 2 {
		t.Fatalf("expected two parsed tool states from mixed input, got %+v", parsedStates)
	}
	if got := parseToolStatesFromAny("unsupported"); got != nil {
		t.Fatalf("expected unsupported tool state input to return nil, got %+v", got)
	}

	if _, ok := parseToolStateFromAny((*RuntimeToolStateSnapshot)(nil)); ok {
		t.Fatalf("expected nil tool state pointer to fail parsing")
	}
	if _, ok := parseToolStateFromAny(false); ok {
		t.Fatalf("expected unsupported tool state type to fail parsing")
	}
}

func TestRuntimeBridgeReadMapHelpers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	m := map[string]any{
		"string":     " value ",
		"int":        12,
		"int64":      int64(13),
		"int32":      int32(14),
		"float64":    float64(15.9),
		"float32":    float32(16.7),
		"numericStr": " 17 ",
		"badStr":     "x",
		"time":       now,
		"nil":        nil,
	}

	if got := readMapString(m, "string"); got != "value" {
		t.Fatalf("unexpected readMapString result: %q", got)
	}
	if got := readMapString(m, "int"); got != "12" {
		t.Fatalf("expected non-string value to be stringified, got %q", got)
	}
	if got := readMapString(m, "missing"); got != "" {
		t.Fatalf("expected missing key to return empty string, got %q", got)
	}

	if got := readMapInt(m, "int"); got != 12 {
		t.Fatalf("unexpected readMapInt int value: %d", got)
	}
	if got := readMapInt(m, "int64"); got != 13 {
		t.Fatalf("unexpected readMapInt int64 value: %d", got)
	}
	if got := readMapInt(m, "int32"); got != 14 {
		t.Fatalf("unexpected readMapInt int32 value: %d", got)
	}
	if got := readMapInt(m, "float64"); got != 15 {
		t.Fatalf("unexpected readMapInt float64 value: %d", got)
	}
	if got := readMapInt(m, "float32"); got != 16 {
		t.Fatalf("unexpected readMapInt float32 value: %d", got)
	}
	if got := readMapInt(m, "numericStr"); got != 17 {
		t.Fatalf("unexpected readMapInt string value: %d", got)
	}
	if got := readMapInt(m, "badStr"); got != 0 {
		t.Fatalf("expected invalid numeric string to return 0, got %d", got)
	}
	if got := readMapInt(m, "nil"); got != 0 {
		t.Fatalf("expected nil value to return 0, got %d", got)
	}

	if got := readMapInt64(m, "int"); got != 12 {
		t.Fatalf("unexpected readMapInt64 int value: %d", got)
	}
	if got := readMapInt64(m, "int64"); got != 13 {
		t.Fatalf("unexpected readMapInt64 int64 value: %d", got)
	}
	if got := readMapInt64(m, "int32"); got != 14 {
		t.Fatalf("unexpected readMapInt64 int32 value: %d", got)
	}
	if got := readMapInt64(m, "float64"); got != 15 {
		t.Fatalf("unexpected readMapInt64 float64 value: %d", got)
	}
	if got := readMapInt64(m, "float32"); got != 16 {
		t.Fatalf("unexpected readMapInt64 float32 value: %d", got)
	}
	if got := readMapInt64(m, "numericStr"); got != 17 {
		t.Fatalf("unexpected readMapInt64 string value: %d", got)
	}
	if got := readMapInt64(m, "badStr"); got != 0 {
		t.Fatalf("expected invalid int64 string to return 0, got %d", got)
	}

	if got := readMapTime(m, "time"); !got.Equal(now) {
		t.Fatalf("unexpected readMapTime value: %v", got)
	}
	if got := readMapTime(m, "string"); !got.IsZero() {
		t.Fatalf("expected non-time value to return zero time, got %v", got)
	}
}
