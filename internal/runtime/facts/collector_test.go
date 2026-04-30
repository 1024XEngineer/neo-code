package facts

import (
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
			"path":  "test.txt",
			"bytes": 1,
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

	snapshot := collector.Snapshot()
	if len(snapshot.Todos.CreatedIDs) != 1 || snapshot.Todos.CreatedIDs[0] != "todo-1" {
		t.Fatalf("todo created facts = %+v", snapshot.Todos.CreatedIDs)
	}
	if len(snapshot.Files.Written) != 1 || snapshot.Files.Written[0].Path != "test.txt" {
		t.Fatalf("file written facts = %+v", snapshot.Files.Written)
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
}
