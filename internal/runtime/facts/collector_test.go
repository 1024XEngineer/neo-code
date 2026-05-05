package facts

import (
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestCollectorApplyToolResultTodoAndVerificationFacts(t *testing.T) {
	collector := NewCollector()

	collector.ApplyToolResult(tools.ToolNameTodoWrite, tools.ToolResult{
		Name:    tools.ToolNameTodoWrite,
		IsError: false,
		Metadata: map[string]any{
			"state_fact": "todo_created",
			"todo_ids":   []string{"todo-1"},
		},
	})

	collector.ApplyToolResult(tools.ToolNameFilesystemWriteFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemWriteFile,
		IsError: false,
		Metadata: map[string]any{
			"path":            "test.txt",
			"bytes":           1,
			"written_content": "1",
		},
		Facts: tools.ToolExecutionFacts{
			WorkspaceWrite: true,
		},
	})

	collector.ApplyToolResult(tools.ToolNameFilesystemReadFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemReadFile,
		IsError: false,
		Content: "1",
		Metadata: map[string]any{
			"path":                  "test.txt",
			"verification_expected": []string{"1"},
			"verification_reason":   "content_match",
		},
		Facts: tools.ToolExecutionFacts{
			VerificationPerformed: true,
			VerificationPassed:    true,
			VerificationScope:     "artifact:test.txt",
		},
	})
	collector.ApplyToolResult(tools.ToolNameFilesystemEdit, tools.ToolResult{
		Name:    tools.ToolNameFilesystemEdit,
		IsError: false,
		Metadata: map[string]any{
			"path":               "edit.go",
			"replacement_length": 12,
		},
	})
	collector.ApplyToolResult(tools.ToolNameFilesystemWriteFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemWriteFile,
		IsError: false,
		Metadata: map[string]any{
			"path":       "test.txt",
			"bytes":      1,
			"noop_write": true,
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Todos.CreatedIDs) != 1 || snapshot.Todos.CreatedIDs[0] != "todo-1" {
		t.Fatalf("todo created facts = %+v", snapshot.Todos.CreatedIDs)
	}
	if len(snapshot.Files.Written) != 2 || snapshot.Files.Written[0].Path != "test.txt" || snapshot.Files.Written[1].Path != "edit.go" {
		t.Fatalf("file written facts = %+v", snapshot.Files.Written)
	}
	if snapshot.Files.Written[0].ExpectedContent != "1" {
		t.Fatalf("expected content = %q, want 1", snapshot.Files.Written[0].ExpectedContent)
	}
	if len(snapshot.Files.Exists) == 0 || snapshot.Files.Exists[0].Path != "test.txt" {
		t.Fatalf("file exists facts = %+v", snapshot.Files.Exists)
	}
	if len(snapshot.Verification.Passed) != 1 {
		t.Fatalf("verification passed facts = %+v", snapshot.Verification.Passed)
	}
	if snapshot.Progress.ObservedFactCount < 3 {
		t.Fatalf("observed fact count = %d, want >= 3", snapshot.Progress.ObservedFactCount)
	}
}

func TestCollectorApplyTodoConflictAndSubAgentFacts(t *testing.T) {
	collector := NewCollector()
	collector.ApplyTodoSnapshot(TodoSummaryLike{
		RequiredOpen:      1,
		RequiredCompleted: 0,
		RequiredFailed:    0,
	})
	collector.ApplyTodoConflict([]string{"todo-1"})
	collector.ApplyTodoConflict([]string{"todo-1"}) // duplicate should be deduped

	collector.ApplyToolResult(tools.ToolNameSpawnSubAgent, tools.ToolResult{
		Name:    tools.ToolNameSpawnSubAgent,
		IsError: false,
		Content: "Summary: done",
		Metadata: map[string]any{
			"task_id": "sa-1",
			"role":    "reviewer",
			"state":   "succeeded",
		},
	})

	snapshot := collector.Snapshot()
	if snapshot.Todos.OpenRequiredCount != 1 {
		t.Fatalf("open required count = %d, want 1", snapshot.Todos.OpenRequiredCount)
	}
	if len(snapshot.Todos.ConflictIDs) != 1 || snapshot.Todos.ConflictIDs[0] != "todo-1" {
		t.Fatalf("todo conflict ids = %+v", snapshot.Todos.ConflictIDs)
	}
	if len(snapshot.SubAgents.Started) != 1 || len(snapshot.SubAgents.Completed) != 1 {
		t.Fatalf("subagent facts = %+v", snapshot.SubAgents)
	}
	if snapshot.SubAgents.Completed[0].TaskID != "sa-1" {
		t.Fatalf("subagent completed task_id = %q, want sa-1", snapshot.SubAgents.Completed[0].TaskID)
	}
	if len(snapshot.SubAgents.Failed) != 0 {
		t.Fatalf("failed subagent facts should be empty, got %+v", snapshot.SubAgents.Failed)
	}
}

func TestCollectorCapturesErrorFactsForToolErrors(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameBash, tools.ToolResult{
		Name:       tools.ToolNameBash,
		IsError:    true,
		ErrorClass: "permission_denied",
		Content:    "permission denied",
		Metadata: map[string]any{
			"exit_code":         1,
			"normalized_intent": "cat README.md",
			"ok":                false,
		},
	})
	collector.ApplyToolResult(tools.ToolNameSpawnSubAgent, tools.ToolResult{
		Name:       tools.ToolNameSpawnSubAgent,
		IsError:    true,
		ErrorClass: "subagent_failed",
		Content:    "runtime: subagent output key \"findings\" must be []string",
		Metadata: map[string]any{
			"task_id":     "spawn-1",
			"role":        "reviewer",
			"state":       "failed",
			"stop_reason": "error",
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Errors.ToolErrors) != 2 {
		t.Fatalf("tool errors = %+v, want 2 entries", snapshot.Errors.ToolErrors)
	}
	if len(snapshot.Commands.Executed) != 1 || snapshot.Commands.Executed[0].Succeeded {
		t.Fatalf("command facts = %+v, want one failed command fact", snapshot.Commands.Executed)
	}
	if len(snapshot.SubAgents.Failed) != 1 || snapshot.SubAgents.Failed[0].TaskID != "spawn-1" {
		t.Fatalf("subagent failed facts = %+v", snapshot.SubAgents.Failed)
	}
	if len(snapshot.SubAgents.Completed) != 0 {
		t.Fatalf("completed subagent facts should be empty, got %+v", snapshot.SubAgents.Completed)
	}
}

func TestCollectorApplyWriteFileVerificationFacts(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameFilesystemWriteFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemWriteFile,
		IsError: false,
		Metadata: map[string]any{
			"path": "verified.txt",
		},
		Facts: tools.ToolExecutionFacts{
			WorkspaceWrite:        true,
			VerificationPerformed: true,
			VerificationPassed:    true,
			VerificationScope:     "artifact:verified.txt",
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Files.Written) != 1 || snapshot.Files.Written[0].Path != "verified.txt" {
		t.Fatalf("written facts = %+v", snapshot.Files.Written)
	}
	if len(snapshot.Files.Exists) != 1 || snapshot.Files.Exists[0].Path != "verified.txt" {
		t.Fatalf("exists facts = %+v", snapshot.Files.Exists)
	}
	if len(snapshot.Files.ContentMatch) != 1 {
		t.Fatalf("content_match facts = %+v", snapshot.Files.ContentMatch)
	}
	if !snapshot.Files.ContentMatch[0].VerificationPassed || snapshot.Files.ContentMatch[0].Scope != "artifact:verified.txt" {
		t.Fatalf("content_match[0] = %+v", snapshot.Files.ContentMatch[0])
	}
	if len(snapshot.Verification.Passed) != 1 {
		t.Fatalf("verification passed facts = %+v", snapshot.Verification.Passed)
	}
}

func TestCollectorApplyNoopWriteKeepsVerificationFacts(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameFilesystemWriteFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemWriteFile,
		IsError: false,
		Metadata: map[string]any{
			"path":                  "2.txt",
			"noop_write":            true,
			"verification_expected": []string{"2"},
		},
		Facts: tools.ToolExecutionFacts{
			VerificationPerformed: true,
			VerificationPassed:    true,
			VerificationScope:     "artifact:2.txt",
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Files.Written) != 0 {
		t.Fatalf("noop write should not append written fact, got %+v", snapshot.Files.Written)
	}
	if len(snapshot.Files.Exists) != 1 || snapshot.Files.Exists[0].Path != "2.txt" {
		t.Fatalf("exists facts = %+v, want path 2.txt", snapshot.Files.Exists)
	}
	if len(snapshot.Files.ContentMatch) != 1 {
		t.Fatalf("content match facts = %+v, want one noop verification content match", snapshot.Files.ContentMatch)
	}
	if !snapshot.Files.ContentMatch[0].VerificationPassed || snapshot.Files.ContentMatch[0].Path != "2.txt" {
		t.Fatalf("content match fact = %+v", snapshot.Files.ContentMatch[0])
	}
	if len(snapshot.Verification.Performed) != 1 || len(snapshot.Verification.Passed) != 1 {
		t.Fatalf("verification facts = %+v", snapshot.Verification)
	}
}

func TestCollectorApplyBashWorkspaceWritePathFacts(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameBash, tools.ToolResult{
		Name:    tools.ToolNameBash,
		IsError: false,
		Metadata: map[string]any{
			"workspace_write_paths": []any{" a.txt ", "a.txt", " b.txt "},
			"exit_code":             0,
			"ok":                    true,
		},
		Facts: tools.ToolExecutionFacts{
			WorkspaceWrite: true,
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Files.Written) != 2 {
		t.Fatalf("bash written facts = %+v, want 2", snapshot.Files.Written)
	}
	if snapshot.Files.Written[0].Path != "a.txt" || snapshot.Files.Written[1].Path != "b.txt" {
		t.Fatalf("bash written paths = %+v, want [a.txt b.txt]", snapshot.Files.Written)
	}
	if len(snapshot.Files.Exists) != 2 || snapshot.Files.Exists[0].Source != "bash" {
		t.Fatalf("bash exists facts = %+v", snapshot.Files.Exists)
	}
}

func TestCollectorApplyGlobAndStringMetadataFacts(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameFilesystemGlob, tools.ToolResult{
		Name:    tools.ToolNameFilesystemGlob,
		IsError: false,
		Content: " a.txt \n\nb.txt\na.txt",
	})
	collector.ApplyToolResult(tools.ToolNameFilesystemWriteFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemWriteFile,
		IsError: false,
		Metadata: map[string]any{
			"path":  "s.txt",
			"bytes": "7",
		},
	})
	collector.ApplyToolResult(tools.ToolNameSpawnSubAgent, tools.ToolResult{
		Name:    tools.ToolNameSpawnSubAgent,
		IsError: false,
		Content: "Summary: done",
		Metadata: map[string]any{
			"task_id":   "sa-1",
			"role":      "reviewer",
			"state":     "succeeded",
			"artifacts": []any{" x.md ", "x.md", " y.md "},
		},
	})
	snapshot := collector.Snapshot()
	if len(snapshot.Files.Exists) < 2 {
		t.Fatalf("glob exists facts = %+v", snapshot.Files.Exists)
	}
	if snapshot.Files.Written[0].Bytes != 7 {
		t.Fatalf("bytes = %d, want 7", snapshot.Files.Written[0].Bytes)
	}
	if len(snapshot.SubAgents.Completed) == 0 || len(snapshot.SubAgents.Completed[0].Artifacts) != 2 {
		t.Fatalf("subagent artifacts = %+v", snapshot.SubAgents.Completed)
	}
}

func TestCollectorTodoStateFallbackAndErrorDedup(t *testing.T) {
	collector := NewCollector()
	collector.ApplyToolResult(tools.ToolNameTodoWrite, tools.ToolResult{
		Name:    tools.ToolNameTodoWrite,
		IsError: false,
		Metadata: map[string]any{
			"state_fact": "todo_updated",
			"id":         "todo-single",
		},
	})
	longErr := ""
	for i := 0; i < 300; i++ {
		longErr += "x"
	}
	collector.ApplyToolResult(tools.ToolNameBash, tools.ToolResult{
		Name:       tools.ToolNameBash,
		IsError:    true,
		ErrorClass: "timeout",
		Content:    longErr,
	})
	collector.ApplyToolResult(tools.ToolNameBash, tools.ToolResult{
		Name:       tools.ToolNameBash,
		IsError:    true,
		ErrorClass: "timeout",
		Content:    "duplicate should dedupe by class",
	})
	snapshot := collector.Snapshot()
	if len(snapshot.Todos.UpdatedIDs) != 1 || snapshot.Todos.UpdatedIDs[0] != "todo-single" {
		t.Fatalf("todo updated ids = %+v", snapshot.Todos.UpdatedIDs)
	}
	if len(snapshot.Errors.ToolErrors) != 1 {
		t.Fatalf("tool errors = %+v", snapshot.Errors.ToolErrors)
	}
	if len(snapshot.Errors.ToolErrors[0].Content) != 256 {
		t.Fatalf("error content length = %d, want 256", len(snapshot.Errors.ToolErrors[0].Content))
	}
}

func TestSubAgentFactJSONDoesNotContainStateField(t *testing.T) {
	payload, err := json.Marshal(SubAgentFact{
		TaskID:     "sa-1",
		Role:       "reviewer",
		StopReason: "completed",
		Summary:    "done",
		Artifacts:  []string{"a.md"},
	})
	if err != nil {
		t.Fatalf("marshal subagent fact failed: %v", err)
	}
	if strings.Contains(string(payload), "\"state\"") {
		t.Fatalf("subagent fact payload should not contain state field, got %s", string(payload))
	}
}

func TestVerificationFactJSONDoesNotContainPassedField(t *testing.T) {
	payload, err := json.Marshal(VerificationFact{
		Tool:   "filesystem_read_file",
		Scope:  "artifact:test.txt",
		Reason: "content_match",
	})
	if err != nil {
		t.Fatalf("marshal verification fact failed: %v", err)
	}
	if strings.Contains(string(payload), "\"passed\"") {
		t.Fatalf("verification fact payload should not contain passed field, got %s", string(payload))
	}
}

func TestCollectorVerificationFactsGroupedByCollections(t *testing.T) {
	collector := NewCollector()

	collector.ApplyToolResult(tools.ToolNameFilesystemReadFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemReadFile,
		IsError: false,
		Metadata: map[string]any{
			"path":                "pass.txt",
			"verification_reason": "content_match",
		},
		Facts: tools.ToolExecutionFacts{
			VerificationPerformed: true,
			VerificationPassed:    true,
			VerificationScope:     "artifact:pass.txt",
		},
	})
	collector.ApplyToolResult(tools.ToolNameFilesystemReadFile, tools.ToolResult{
		Name:    tools.ToolNameFilesystemReadFile,
		IsError: false,
		Metadata: map[string]any{
			"path":                "fail.txt",
			"verification_reason": "content_mismatch",
		},
		Facts: tools.ToolExecutionFacts{
			VerificationPerformed: true,
			VerificationPassed:    false,
			VerificationScope:     "artifact:fail.txt",
		},
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Verification.Performed) != 2 {
		t.Fatalf("verification performed facts = %+v, want 2 entries", snapshot.Verification.Performed)
	}
	if len(snapshot.Verification.Passed) != 1 || snapshot.Verification.Passed[0].Scope != "artifact:pass.txt" {
		t.Fatalf("verification passed facts = %+v", snapshot.Verification.Passed)
	}
	if len(snapshot.Verification.Failed) != 1 || snapshot.Verification.Failed[0].Scope != "artifact:fail.txt" {
		t.Fatalf("verification failed facts = %+v", snapshot.Verification.Failed)
	}
}
