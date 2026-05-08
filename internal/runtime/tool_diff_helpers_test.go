package runtime

import (
	"context"
	"testing"

	"neo-code/internal/repository"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func TestBuildToolDiffPayload(t *testing.T) {
	t.Run("single file payload", func(t *testing.T) {
		result := tools.ToolResult{
			Name:       tools.ToolNameFilesystemWriteFile,
			ToolCallID: "call-1",
			Metadata: map[string]any{
				"path":          "main.go",
				"tool_diff":     "@@ -1 +1 @@",
				"tool_diff_new": true,
			},
		}

		payload, ok := buildToolDiffPayload(result)
		if !ok {
			t.Fatal("expected payload")
		}
		if payload.FilePath != "main.go" || payload.Diff != "@@ -1 +1 @@" || !payload.WasNew {
			t.Fatalf("unexpected single payload: %#v", payload)
		}
	})

	t.Run("multi file payload", func(t *testing.T) {
		result := tools.ToolResult{
			Name:       tools.ToolNameFilesystemMoveFile,
			ToolCallID: "call-2",
			Metadata: map[string]any{
				"tool_diffs": []map[string]any{
					{"path": "old.txt", "diff": "@@ -1 +0 @@", "was_new": false},
					{"path": "new.txt", "diff": "@@ -0 +1 @@", "was_new": true},
					{"path": " ", "diff": "ignored", "was_new": true},
				},
			},
		}

		payload, ok := buildToolDiffPayload(result)
		if !ok {
			t.Fatal("expected payload")
		}
		if len(payload.Files) != 2 || len(payload.Diffs) != 2 {
			t.Fatalf("unexpected multi payload lengths: %#v", payload)
		}
		if payload.Files[0].Kind != "modified" || payload.Files[1].Kind != "added" {
			t.Fatalf("unexpected file kinds: %#v", payload.Files)
		}
		if payload.FilePath != "old.txt" {
			t.Fatalf("first file path = %q, want old.txt", payload.FilePath)
		}
	})

	t.Run("multi file kind from metadata wins over WasNew fallback", func(t *testing.T) {
		result := tools.ToolResult{
			Name:       tools.ToolNameFilesystemMoveFile,
			ToolCallID: "call-move",
			Metadata: map[string]any{
				"tool_diffs": []map[string]any{
					{"path": "src/a.txt", "diff": "@@ -1 +0 @@", "was_new": false, "kind": FileChangeKindDeleted},
					{"path": "dst/a.txt", "diff": "@@ -0 +1 @@", "was_new": true, "kind": FileChangeKindAdded},
				},
			},
		}

		payload, ok := buildToolDiffPayload(result)
		if !ok {
			t.Fatal("expected payload")
		}
		if len(payload.Files) != 2 {
			t.Fatalf("unexpected length: %#v", payload.Files)
		}
		if payload.Files[0].Kind != FileChangeKindDeleted {
			t.Fatalf("Files[0].Kind = %q, want deleted", payload.Files[0].Kind)
		}
		if payload.Files[1].Kind != FileChangeKindAdded {
			t.Fatalf("Files[1].Kind = %q, want added", payload.Files[1].Kind)
		}
	})

	t.Run("multi file filters unchanged copy source", func(t *testing.T) {
		result := tools.ToolResult{
			Name:       tools.ToolNameFilesystemCopyFile,
			ToolCallID: "call-copy",
			Metadata: map[string]any{
				"tool_diffs": []map[string]any{
					{"path": "src/a.txt", "diff": "", "was_new": false, "kind": FileChangeKindUnchanged},
					{"path": "dst/a.txt", "diff": "@@ -0 +1 @@", "was_new": true, "kind": FileChangeKindAdded},
				},
			},
		}

		payload, ok := buildToolDiffPayload(result)
		if !ok {
			t.Fatal("expected payload")
		}
		if len(payload.Files) != 1 {
			t.Fatalf("expected unchanged source filtered, got %#v", payload.Files)
		}
		if payload.Files[0].Path != "dst/a.txt" || payload.Files[0].Kind != FileChangeKindAdded {
			t.Fatalf("unexpected surviving file: %#v", payload.Files[0])
		}
	})

	t.Run("multi file delete metadata preserves deleted kind", func(t *testing.T) {
		result := tools.ToolResult{
			Name:       tools.ToolNameFilesystemRemoveDir,
			ToolCallID: "call-rm",
			Metadata: map[string]any{
				"tool_diffs": []map[string]any{
					{"path": "old/a.txt", "diff": "@@ -1 +0 @@", "was_new": false, "kind": FileChangeKindDeleted},
					{"path": "old/b.txt", "diff": "@@ -1 +0 @@", "was_new": false, "kind": FileChangeKindDeleted},
				},
			},
		}

		payload, ok := buildToolDiffPayload(result)
		if !ok {
			t.Fatal("expected payload")
		}
		if len(payload.Files) != 2 {
			t.Fatalf("unexpected length: %#v", payload.Files)
		}
		for idx, f := range payload.Files {
			if f.Kind != FileChangeKindDeleted {
				t.Fatalf("Files[%d].Kind = %q, want deleted", idx, f.Kind)
			}
		}
	})

	t.Run("missing file path returns false", func(t *testing.T) {
		if _, ok := buildToolDiffPayload(tools.ToolResult{Name: tools.ToolNameFilesystemWriteFile}); ok {
			t.Fatal("expected no payload when metadata has no path")
		}
	})
}

func TestToolExecutionHelperFunctions(t *testing.T) {
	t.Run("toolCallTouchedPaths covers write and move payloads", func(t *testing.T) {
		writePaths := toolCallTouchedPaths(providertypes.ToolCall{
			Name:      tools.ToolNameFilesystemWriteFile,
			Arguments: `{"path":" docs/readme.md "}`,
		}, "/repo")
		if len(writePaths) != 1 || writePaths[0] != "/repo/docs/readme.md" {
			t.Fatalf("write toolCallTouchedPaths() = %#v", writePaths)
		}

		movePaths := toolCallTouchedPaths(providertypes.ToolCall{
			Name:      tools.ToolNameFilesystemMoveFile,
			Arguments: `{"source_path":"src/a.txt","destination_path":" /tmp/b.txt "}`,
		}, "/repo")
		if len(movePaths) != 2 || movePaths[0] != "/repo/src/a.txt" || movePaths[1] != "/tmp/b.txt" {
			t.Fatalf("move toolCallTouchedPaths() = %#v", movePaths)
		}

		if got := toolCallTouchedPaths(providertypes.ToolCall{
			Name:      tools.ToolNameFilesystemCopyFile,
			Arguments: `{invalid`,
		}, "/repo"); got != nil {
			t.Fatalf("malformed toolCallTouchedPaths() = %#v, want nil", got)
		}
	})

	t.Run("toolResultMultiDiffs parses valid entries", func(t *testing.T) {
		entries, ok := toolResultMultiDiffs(map[string]any{
			"tool_diffs": []map[string]any{
				{"path": "a.txt", "diff": "a", "was_new": true},
				{"path": " ", "diff": "ignored", "was_new": false},
			},
		})
		if !ok || len(entries) != 1 {
			t.Fatalf("entries=%#v ok=%v", entries, ok)
		}
		if entries[0].Path != "a.txt" || !entries[0].WasNew {
			t.Fatalf("unexpected entry: %#v", entries[0])
		}
	})

	t.Run("toolResultMultiDiffs filters unchanged kind and surfaces explicit kind", func(t *testing.T) {
		entries, ok := toolResultMultiDiffs(map[string]any{
			"tool_diffs": []map[string]any{
				{"path": "src/a.txt", "diff": "", "was_new": false, "kind": FileChangeKindUnchanged},
				{"path": "dst/a.txt", "diff": "@@", "was_new": true, "kind": FileChangeKindAdded},
				{"path": "old.txt", "diff": "@@", "was_new": false, "kind": FileChangeKindDeleted},
			},
		})
		if !ok || len(entries) != 2 {
			t.Fatalf("entries=%#v ok=%v", entries, ok)
		}
		if entries[0].Path != "dst/a.txt" || entries[0].Kind != FileChangeKindAdded {
			t.Fatalf("unexpected entries[0]: %#v", entries[0])
		}
		if entries[1].Path != "old.txt" || entries[1].Kind != FileChangeKindDeleted {
			t.Fatalf("unexpected entries[1]: %#v", entries[1])
		}
	})

	t.Run("toolResultFilePath trims metadata", func(t *testing.T) {
		if got := toolResultFilePath(map[string]any{"path": "  demo.txt  "}); got != "demo.txt" {
			t.Fatalf("toolResultFilePath() = %q, want demo.txt", got)
		}
		if got := toolResultFilePath(nil); got != "" {
			t.Fatalf("toolResultFilePath(nil) = %q, want empty", got)
		}
	})

	t.Run("resolveWorkdirPaths normalizes relative and absolute values", func(t *testing.T) {
		paths := resolveWorkdirPaths("/repo", " a.txt ", "/tmp/demo.txt", "")
		if len(paths) != 2 || paths[0] != "/repo/a.txt" || paths[1] != "/tmp/demo.txt" {
			t.Fatalf("resolveWorkdirPaths() = %#v", paths)
		}
	})

	t.Run("bashCommandFromCall prefers command then cmd alias", func(t *testing.T) {
		if got := bashCommandFromCall(providertypes.ToolCall{Arguments: `{"command":" echo hi "}`}); got != "echo hi" {
			t.Fatalf("command field = %q", got)
		}
		if got := bashCommandFromCall(providertypes.ToolCall{Arguments: `{"cmd":" pwd "}`}); got != "pwd" {
			t.Fatalf("cmd alias = %q", got)
		}
		if got := bashCommandFromCall(providertypes.ToolCall{Arguments: `{invalid`}); got != "" {
			t.Fatalf("invalid json should return empty command, got %q", got)
		}
	})

	t.Run("collectUncoveredBashPaths removes covered and duplicate entries", func(t *testing.T) {
		diff := repository.FingerprintDiff{
			Added:    []string{"new.txt", "new.txt"},
			Modified: []string{"tracked.txt", "covered.txt"},
		}
		covered := map[string]struct{}{
			"/repo/covered.txt": {},
		}
		got := collectUncoveredBashPaths("/repo", diff, covered)
		if len(got) != 2 || got[0] != "tracked.txt" || got[1] != "new.txt" {
			t.Fatalf("collectUncoveredBashPaths() = %#v", got)
		}
	})

	t.Run("collectBashWriteFactPaths includes only added and modified paths", func(t *testing.T) {
		got := collectBashWriteFactPaths(repository.FingerprintDiff{
			Added:    []string{"b.txt", "a.txt"},
			Modified: []string{"a.txt", "c.txt"},
			Deleted:  []string{"old.txt"},
		})
		want := []string{"a.txt", "c.txt", "b.txt"}
		if len(got) != len(want) {
			t.Fatalf("collectBashWriteFactPaths() = %v, want %v", got, want)
		}
		for idx := range want {
			if got[idx] != want[idx] {
				t.Fatalf("collectBashWriteFactPaths() = %v, want %v", got, want)
			}
		}
	})
}

func TestEmitHelpersPublishExpectedEvents(t *testing.T) {
	service := &Service{events: make(chan RuntimeEvent, 8)}
	state := &runState{
		runID:   "run-1",
		session: agentsession.Session{ID: "session-1"},
	}

	service.emitBashSideEffectEvent(
		context.Background(),
		state,
		providertypes.ToolCall{ID: "tool-1"},
		"touch x",
		repository.FingerprintDiff{
			Added:    []string{"new.txt"},
			Modified: []string{"edit.txt"},
			Deleted:  []string{"old.txt"},
		},
		[]string{"/repo/edit.txt"},
		[]string{"new.txt"},
	)

	evt := <-service.events
	if evt.Type != EventBashSideEffect {
		t.Fatalf("event type = %q, want %q", evt.Type, EventBashSideEffect)
	}
	payload, ok := evt.Payload.(BashSideEffectPayload)
	if !ok {
		t.Fatalf("payload type = %T", evt.Payload)
	}
	if len(payload.Changes) != 3 || payload.UncoveredPaths[0] != "new.txt" {
		t.Fatalf("unexpected bash payload: %#v", payload)
	}

	service.emitBashSideEffectEvent(
		context.Background(),
		state,
		providertypes.ToolCall{ID: "tool-2"},
		"touch noop",
		repository.FingerprintDiff{},
		nil,
		nil,
	)
	select {
	case extra := <-service.events:
		t.Fatalf("unexpected empty bash side effect event: %#v", extra)
	default:
	}

	service.emitToolDiffs(context.Background(), state, toolExecutionSummary{
		Results: []tools.ToolResult{
			{
				Name: tools.ToolNameFilesystemWriteFile,
				Facts: tools.ToolExecutionFacts{
					WorkspaceWrite: true,
				},
				Metadata: map[string]any{
					"path":          "main.go",
					"tool_diff":     "@@ -1 +1 @@",
					"tool_diff_new": true,
				},
			},
			{
				Name: tools.ToolNameFilesystemWriteFile,
				Facts: tools.ToolExecutionFacts{
					WorkspaceWrite: true,
				},
				Metadata: map[string]any{
					"path":       "noop.go",
					"noop_write": true,
				},
			},
		},
	})

	evt = <-service.events
	if evt.Type != EventToolDiff {
		t.Fatalf("event type = %q, want %q", evt.Type, EventToolDiff)
	}
	diffPayload, ok := evt.Payload.(ToolDiffPayload)
	if !ok {
		t.Fatalf("diff payload type = %T", evt.Payload)
	}
	if diffPayload.FilePath != "main.go" || !diffPayload.WasNew {
		t.Fatalf("unexpected tool diff payload: %#v", diffPayload)
	}
	select {
	case extra := <-service.events:
		t.Fatalf("unexpected extra event: %#v", extra)
	default:
	}
}
