package facts

import (
	"testing"

	"neo-code/internal/tools"
)

func TestCollectorLowLevelHelpers(t *testing.T) {
	t.Parallel()

	if got := readInt(map[string]any{"v": int8(3)}, "v"); got != 3 {
		t.Fatalf("int8 readInt = %d, want 3", got)
	}
	if got := readInt(map[string]any{"v": int16(4)}, "v"); got != 4 {
		t.Fatalf("int16 readInt = %d, want 4", got)
	}
	if got := readInt(map[string]any{"v": int32(5)}, "v"); got != 5 {
		t.Fatalf("int32 readInt = %d, want 5", got)
	}
	if got := readInt(map[string]any{"v": int64(6)}, "v"); got != 6 {
		t.Fatalf("int64 readInt = %d, want 6", got)
	}
	if got := readInt(map[string]any{"v": float32(7)}, "v"); got != 7 {
		t.Fatalf("float32 readInt = %d, want 7", got)
	}
	if got := readInt(map[string]any{"v": float64(8)}, "v"); got != 8 {
		t.Fatalf("float64 readInt = %d, want 8", got)
	}
	if got := readInt(map[string]any{"v": "not-int"}, "v"); got != 0 {
		t.Fatalf("invalid string readInt = %d, want 0", got)
	}

	if readBool(map[string]any{"v": "TRUE"}, "v") != true {
		t.Fatal("readBool string true should be true")
	}
	if readBool(map[string]any{"v": 1}, "v") != false {
		t.Fatal("readBool non-bool/non-string should be false")
	}

	if values := readStringSlice(map[string]any{"v": []any{" a ", "", 2, "a"}}, "v"); len(values) != 2 || values[0] != "2" || values[1] != "a" {
		t.Fatalf("readStringSlice = %+v, want [2 a]", values)
	}
	if values := normalizeStringList([]string{" b ", "", "a", "a"}); len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("normalizeStringList = %+v", values)
	}

	c := &Collector{}
	if c.markSeen("  ") {
		t.Fatal("blank key must not be marked")
	}
	if !c.markSeen("k") || c.markSeen("k") {
		t.Fatal("markSeen dedupe failed")
	}
}

func TestCollectorBranchPaths(t *testing.T) {
	t.Parallel()

	collector := NewCollector()
	collector.ApplyToolResult("unknown_tool", tools.ToolResult{Name: "unknown_tool"})
	collector.ApplySubAgentStarted(SubAgentFact{})
	collector.ApplySubAgentFinished(SubAgentFact{}, true)
	collector.ApplySubAgentFinished(SubAgentFact{TaskID: "sa", StopReason: "err"}, false)
	collector.ApplySubAgentFinished(SubAgentFact{TaskID: "sa", StopReason: "err"}, false)

	snapshot := collector.Snapshot()
	if len(snapshot.SubAgents.Failed) != 1 {
		t.Fatalf("failed subagents = %+v, want deduped one", snapshot.SubAgents.Failed)
	}
}
