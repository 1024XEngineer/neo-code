package runtime

import (
	"context"
	"strings"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptance"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/decider"
	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

func TestFinalAcceptanceMappingAndLegacyPaths(t *testing.T) {
	t.Parallel()

	t.Run("map decider statuses", func(t *testing.T) {
		t.Parallel()
		got := mapDeciderDecisionToAcceptance(decider.Decision{Status: decider.DecisionBlocked})
		if got.Status != acceptance.AcceptanceFailed || got.StopReason != controlplane.StopReasonVerificationFailed {
			t.Fatalf("blocked mapping = %+v", got)
		}
		got = mapDeciderDecisionToAcceptance(decider.Decision{Status: decider.DecisionContinue})
		if got.Status != acceptance.AcceptanceContinue || got.StopReason != controlplane.StopReasonTodoNotConverged {
			t.Fatalf("continue mapping = %+v", got)
		}
	})

	t.Run("projection keeps required input and task kinds", func(t *testing.T) {
		t.Parallel()
		required := &decider.RequiredInput{
			Kind:    "missing_file_target_or_content",
			Message: "need target path",
			Details: map[string]any{"path": "test.txt"},
		}
		projected := toDeciderDecisionFromAcceptance(acceptance.AcceptanceDecision{
			Status:            acceptance.AcceptanceContinue,
			StopReason:        controlplane.StopReasonTodoNotConverged,
			RequiredInput:     required,
			IntentHint:        decider.TaskKindWorkspaceWrite,
			EffectiveTaskKind: decider.TaskKindWorkspaceWrite,
		})
		if projected.RequiredInput == nil || projected.RequiredInput.Kind != "missing_file_target_or_content" {
			t.Fatalf("required input lost in projection: %+v", projected)
		}
		if projected.IntentHint != decider.TaskKindWorkspaceWrite || projected.EffectiveTaskKind != decider.TaskKindWorkspaceWrite {
			t.Fatalf("task kind hints lost in projection: %+v", projected)
		}
	})

	t.Run("legacy path adds continue hint", func(t *testing.T) {
		t.Parallel()
		service := &Service{}
		state := newRunState("run-legacy", agentsession.New("legacy"))
		required := true
		state.session.Todos = []agentsession.TodoItem{
			{ID: "todo-1", Status: agentsession.TodoStatusPending, Required: &required},
		}
		snapshot := TurnBudgetSnapshot{Config: config.StaticDefaults().Clone(), Workdir: t.TempDir()}
		decision, err := service.beforeAcceptFinalLegacy(context.Background(), &state, snapshot, providertypes.Message{}, false)
		if err != nil {
			t.Fatalf("beforeAcceptFinalLegacy() error = %v", err)
		}
		if decision.Status != acceptance.AcceptanceContinue {
			t.Fatalf("legacy status = %q, want continue", decision.Status)
		}
		if !strings.Contains(decision.ContinueHint, "<acceptance_continue>") {
			t.Fatalf("continue hint = %q", decision.ContinueHint)
		}
	})
}

func TestFinalAcceptanceHelperBranches(t *testing.T) {
	t.Parallel()

	if got := buildAcceptanceContinueHint(acceptance.AcceptanceDecision{
		Status:          acceptance.AcceptanceContinue,
		ContinueHint:    "base",
		VerifierResults: nil,
	}); !strings.Contains(got, "base") {
		t.Fatalf("continue hint fallback = %q", got)
	}

	if got := renderCompletionBlockedReasonHintSection("pending_todo", nil); !strings.Contains(got, "required_action") {
		t.Fatalf("pending_todo fallback hint = %q", got)
	}
	if got := renderCompletionBlockedReasonHintSection("unverified_write", nil); !strings.Contains(got, "VerificationPerformed") {
		t.Fatalf("unverified_write hint = %q", got)
	}

	results := []verify.VerificationResult{
		{Name: "z", Status: verify.VerificationSoftBlock},
		{Name: "a", Status: verify.VerificationHardBlock},
		{Name: "ok", Status: verify.VerificationPass},
	}
	section := renderVerifierFailureHintSection(results)
	if !strings.Contains(section, "name=\"a\"") || !strings.Contains(section, "name=\"z\"") {
		t.Fatalf("verifier section = %q", section)
	}

	if xmlEscape(`<a&"' >`) == `<a&"' >` {
		t.Fatal("xmlEscape should escape special chars")
	}
}
