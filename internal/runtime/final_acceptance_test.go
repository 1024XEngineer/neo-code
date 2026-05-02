package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

func TestBeforeAcceptFinalDecisionPaths(t *testing.T) {
	t.Parallel()

	service := &Service{}
	baseCfg := config.StaticDefaults().Clone()
	snapshot := TurnBudgetSnapshot{
		Config:  baseCfg,
		Workdir: t.TempDir(),
	}

	t.Run("pending required todo -> continue", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-continue", agentsession.New("continue"))
		required := true
		state.session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "do work", Status: agentsession.TodoStatusPending, Required: &required},
		}
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{
			Role:  providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("done")},
		}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != acceptance.AcceptanceContinue {
			t.Fatalf("status = %q, want continue", decision.Status)
		}
	})

	t.Run("invalid profile -> failed", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-invalid-profile", agentsession.New("invalid-profile"))
		state.session.TaskState.VerificationProfile = "bad"
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != acceptance.AcceptanceFailed {
			t.Fatalf("status = %q, want failed", decision.Status)
		}
	})

	t.Run("continue carries pending final progress signal", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-progress", agentsession.New("progress"))
		required := true
		state.pendingFinalProgress = true
		state.session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "do work", Status: agentsession.TodoStatusPending, Required: &required},
		}
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if !decision.HasProgress {
			t.Fatal("expected continue decision to carry pending final progress")
		}
	})

	t.Run("all converged -> accepted", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-accepted", agentsession.New("accepted"))
		state.session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != acceptance.AcceptanceAccepted {
			t.Fatalf("status = %q, want accepted", decision.Status)
		}
	})

	t.Run("final intercept streak drives no-progress breaker", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-incomplete", agentsession.New("incomplete"))
		required := true
		state.finalInterceptStreak = snapshot.Config.Runtime.Verification.MaxNoProgress
		state.session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Content: "do work", Status: agentsession.TodoStatusPending, Required: &required},
		}
		decision, err := service.beforeAcceptFinal(context.Background(), &state, snapshot, providertypes.Message{}, true)
		if err != nil {
			t.Fatalf("beforeAcceptFinal() error = %v", err)
		}
		if decision.Status != acceptance.AcceptanceIncomplete {
			t.Fatalf("status = %q, want incomplete", decision.Status)
		}
	})
}

func TestFinalAcceptanceHelpers(t *testing.T) {
	t.Parallel()

	t.Run("buildVerifyTaskState includes profile", func(t *testing.T) {
		t.Parallel()
		got := buildVerifyTaskState(agentsession.TaskState{
			VerificationProfile: agentsession.VerificationProfileDocs,
			KeyArtifacts:        []string{"README.md"},
		})
		if got.VerificationProfile != "docs" || len(got.KeyArtifacts) != 1 {
			t.Fatalf("unexpected task state snapshot: %+v", got)
		}
	})

	t.Run("applyAcceptanceResultProgress uses pending final progress", func(t *testing.T) {
		t.Parallel()
		state := newRunState("run-progress", agentsession.New("progress"))
		state.finalInterceptStreak = 2
		state.pendingFinalProgress = true
		applyAcceptanceResultProgress(&state, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceContinue})
		if state.finalInterceptStreak != 0 || state.pendingFinalProgress {
			t.Fatalf("unexpected state after progress reset: %+v", state)
		}

		applyAcceptanceResultProgress(&state, acceptance.AcceptanceDecision{Status: acceptance.AcceptanceContinue})
		if state.finalInterceptStreak != 1 {
			t.Fatalf("streak = %d, want 1", state.finalInterceptStreak)
		}
	})

	t.Run("buildAcceptanceContinueHint includes actionable evidence and tool requirement", func(t *testing.T) {
		t.Parallel()
		decision := acceptance.AcceptanceDecision{
			Status:                  acceptance.AcceptanceContinue,
			CompletionBlockedReason: "pending_todo",
			VerifierResults: []verify.VerificationResult{
				{
					Name:    "todo_convergence",
					Status:  verify.VerificationSoftBlock,
					Summary: "required todos are not converged",
					Reason:  "required todos are still pending, in progress, or internally blocked",
					Evidence: map[string]any{
						"pending_ids":     []string{"todo-2", "todo-1"},
						"in_progress_ids": []string{"todo-3"},
						"blocked_ids":     []string{"todo-4"},
					},
				},
			},
		}
		hint := buildAcceptanceContinueHint(decision)
		if !strings.Contains(hint, "<acceptance_continue>") {
			t.Fatalf("hint should contain acceptance xml envelope, got %q", hint)
		}
		if !strings.Contains(hint, "MUST call todo_write") {
			t.Fatalf("hint should force tool-based facts, got %q", hint)
		}
		if !strings.Contains(hint, "<pending_ids>todo-1,todo-2</pending_ids>") {
			t.Fatalf("hint should include sorted pending ids, got %q", hint)
		}
		if !strings.Contains(hint, "<completion_blocked_reason>pending_todo</completion_blocked_reason>") {
			t.Fatalf("hint should include completion blocked reason, got %q", hint)
		}
	})

	t.Run("buildAcceptanceContinueHint emits unverified_write guidance", func(t *testing.T) {
		t.Parallel()
		hint := buildAcceptanceContinueHint(acceptance.AcceptanceDecision{
			Status:                  acceptance.AcceptanceContinue,
			CompletionBlockedReason: "unverified_write",
		})
		if !strings.Contains(hint, "<completion_blocked_reason>unverified_write</completion_blocked_reason>") {
			t.Fatalf("hint should include unverified_write reason, got %q", hint)
		}
		if !strings.Contains(hint, "VerificationPerformed") || !strings.Contains(hint, "VerificationPassed") {
			t.Fatalf("hint should require verification facts, got %q", hint)
		}
	})

	t.Run("synthesizeTodoConvergenceEvidence projects required todos", func(t *testing.T) {
		t.Parallel()
		required := true
		result := synthesizeTodoConvergenceEvidence([]agentsession.TodoItem{
			{ID: "todo-1", Content: "a", Status: agentsession.TodoStatusPending, Required: &required},
			{ID: "todo-2", Content: "b", Status: agentsession.TodoStatusInProgress, Required: &required},
			{ID: "todo-3", Content: "c", Status: agentsession.TodoStatusCompleted, Required: &required},
		})
		if result == nil {
			t.Fatal("expected synthetic verifier result")
		}
		if result.Name != "todo_convergence" || result.Status != verify.VerificationSoftBlock {
			t.Fatalf("unexpected synthetic result: %+v", *result)
		}
		pending, _ := result.Evidence["pending_ids"].([]string)
		if len(pending) != 1 || pending[0] != "todo-1" {
			t.Fatalf("pending ids = %+v, want [todo-1]", pending)
		}
	})

	t.Run("buildVerifyTodos and content checks keep normalized values", func(t *testing.T) {
		t.Parallel()
		required := true
		todos := buildVerifyTodos([]agentsession.TodoItem{
			{
				ID:         " todo-1 ",
				Content:    " do work ",
				Status:     agentsession.TodoStatusInProgress,
				Required:   &required,
				Acceptance: []string{"a"},
				Artifacts:  []string{"x.md"},
				Supersedes: []string{"todo-0"},
				ContentChecks: []agentsession.TodoContentCheck{
					{Artifact: " README.md ", Contains: []string{"done"}},
				},
				RetryCount:    1,
				RetryLimit:    2,
				FailureReason: " none ",
			},
		})
		if len(todos) != 1 {
			t.Fatalf("todos len = %d, want 1", len(todos))
		}
		if todos[0].ID != "todo-1" || todos[0].Content != "do work" || todos[0].Status != "in_progress" {
			t.Fatalf("unexpected todo snapshot: %+v", todos[0])
		}
		if len(todos[0].ContentChecks) != 1 || todos[0].ContentChecks[0].Artifact != "README.md" {
			t.Fatalf("unexpected content checks: %+v", todos[0].ContentChecks)
		}
	})

	t.Run("buildVerifyMessages ignores non-text and trims content", func(t *testing.T) {
		t.Parallel()
		messages := buildVerifyMessages([]providertypes.Message{
			{
				Role: " assistant ",
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("  first "),
					{Kind: "tool_call", Text: "ignored"},
					providertypes.NewTextPart("second"),
				},
			},
		})
		if len(messages) != 1 {
			t.Fatalf("messages len = %d, want 1", len(messages))
		}
		if messages[0].Role != "assistant" || messages[0].Content != "first\nsecond" {
			t.Fatalf("unexpected message snapshot: %+v", messages[0])
		}
	})

	t.Run("evidence helpers normalize and serialize", func(t *testing.T) {
		t.Parallel()
		items := evidenceStringList([]any{" b ", map[string]any{"k": 1}, "a", "a"})
		if len(items) != 3 || items[0] != "a" || items[1] != "b" {
			t.Fatalf("unexpected evidence list: %+v", items)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(items[2]), &m); err != nil {
			t.Fatalf("expected JSON encoded item, got %q", items[2])
		}
		preview := evidenceJSONPreview(map[string]any{"x": 1})
		if preview == "" || !strings.Contains(preview, "\"x\":1") {
			t.Fatalf("unexpected preview: %q", preview)
		}
	})
}
