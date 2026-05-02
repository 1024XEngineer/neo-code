package decider

import (
	"encoding/json"
	"strings"
	"testing"

	runtimefacts "neo-code/internal/runtime/facts"
)

func assertDecisionStatus(t *testing.T, decision Decision, want DecisionStatus) {
	t.Helper()
	if decision.Status != want {
		t.Fatalf("status = %q, want %q", decision.Status, want)
	}
}

func TestDecideRequiredTodoFailedStopsImmediately(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind: TaskKindTodoState,
		Todos: TodoSnapshot{
			Summary: TodoSummary{RequiredFailed: 1},
		},
	})

	if decision.Status != DecisionFailed {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionFailed)
	}
	if decision.StopReason != "required_todo_failed" {
		t.Fatalf("stop_reason = %q, want required_todo_failed", decision.StopReason)
	}
}

func TestDecideUsesEffectiveTaskKindFromFacts(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindChatAnswer,
		CompletionPassed: true,
		UserGoal:         "你好",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", WorkspaceWrite: true}},
			},
		},
	})
	if decision.EffectiveTaskKind != TaskKindWorkspaceWrite {
		t.Fatalf("effective kind = %q, want %q", decision.EffectiveTaskKind, TaskKindWorkspaceWrite)
	}
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
}

func TestDecideNoProgressExceededReturnsIncomplete(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:           TaskKindWorkspaceWrite,
		NoProgressExceeded: true,
	})

	if decision.Status != DecisionIncomplete {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionIncomplete)
	}
	if decision.StopReason != "no_progress_after_final_intercept" {
		t.Fatalf("stop_reason = %q, want no_progress_after_final_intercept", decision.StopReason)
	}
}

func TestDecideCompletionBlockedReasonPendingTodo(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindTodoState,
		CompletionPassed: false,
		CompletionReason: "pending_todo",
		Todos: TodoSnapshot{
			Items: []TodoViewItem{
				{ID: "todo-1", Required: true, Status: "pending"},
			},
		},
	})

	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Kind != "required_todo_terminal" {
		t.Fatalf("missing facts = %+v", decision.MissingFacts)
	}
	if len(decision.RequiredNextActions) == 0 || decision.RequiredNextActions[0].Tool != "todo_write" {
		t.Fatalf("required actions = %+v", decision.RequiredNextActions)
	}
}

func TestDecideWorkspaceWriteNeedsVerificationThenAccepts(t *testing.T) {
	continueDecision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
		},
	})
	if continueDecision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", continueDecision.Status, DecisionContinue)
	}
	if len(continueDecision.MissingFacts) == 0 || continueDecision.MissingFacts[0].Kind != "file_exists" {
		t.Fatalf("missing facts = %+v, want file_exists", continueDecision.MissingFacts)
	}
	if len(continueDecision.RequiredNextActions) == 0 || continueDecision.RequiredNextActions[0].Tool != "filesystem_glob" {
		t.Fatalf("required actions = %+v, want filesystem_glob", continueDecision.RequiredNextActions)
	}

	acceptedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_read_file", Scope: "artifact:test.txt"}},
			},
		},
	})
	if acceptedDecision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", acceptedDecision.Status, DecisionAccepted)
	}
}

func TestDecideWorkspaceWriteNoopSatisfiedByVerificationFacts(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "创建 2.txt 内容为 2",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Exists: []runtimefacts.FileExistFact{{Path: "2.txt", Source: "filesystem_write_file_noop"}},
				ContentMatch: []runtimefacts.FileContentMatchFact{{
					Path:               "2.txt",
					Scope:              "artifact:2.txt",
					ExpectedContains:   []string{"2"},
					VerificationPassed: true,
				}},
			},
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_write_file", Scope: "artifact:2.txt"}},
			},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestDecideWorkspaceWriteRepeatedNoopShouldStayAccepted(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "创建 2.txt 内容为 2",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{
					{Path: "2.txt", Bytes: 1, WorkspaceWrite: true, ExpectedContent: "2"},
				},
				Exists: []runtimefacts.FileExistFact{
					{Path: "2.txt", Source: "filesystem_write_file"},
					{Path: "2.txt", Source: "filesystem_write_file_noop"},
				},
				ContentMatch: []runtimefacts.FileContentMatchFact{{
					Path:               "2.txt",
					Scope:              "artifact:2.txt",
					ExpectedContains:   []string{"2"},
					VerificationPassed: true,
				}},
			},
			Verification: runtimefacts.VerificationFacts{
				Performed: []runtimefacts.VerificationFact{{Tool: "filesystem_write_file", Scope: "artifact:2.txt"}},
				Passed:    []runtimefacts.VerificationFact{{Tool: "filesystem_write_file", Scope: "artifact:2.txt"}},
			},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestDecideWorkspaceWriteVerificationMustBindTarget(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_read_file", Scope: "artifact:other.txt"}},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Target != "test.txt" {
		t.Fatalf("missing facts = %+v, want target test.txt", decision.MissingFacts)
	}
}

func TestDecideWorkspaceWriteVerificationShouldNotMatchByBasenameOnly(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "src/readme.md", Bytes: 1, WorkspaceWrite: true}},
			},
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_read_file", Scope: "artifact:docs/readme.md"}},
			},
		},
	})
	assertDecisionStatus(t, decision, DecisionContinue)
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Target != "src/readme.md" {
		t.Fatalf("missing facts = %+v, want target src/readme.md", decision.MissingFacts)
	}
}

func TestDecideCompletionBlockedUnverifiedWriteUsesExpectedContentWhenAvailable(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: false,
		CompletionReason: "unverified_write",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{
					{Path: "2.txt", Bytes: 1, WorkspaceWrite: true, ExpectedContent: "2"},
				},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.RequiredNextActions) == 0 || decision.RequiredNextActions[0].Tool != "filesystem_read_file" {
		t.Fatalf("required actions = %+v, want filesystem_read_file", decision.RequiredNextActions)
	}
	expectContains, _ := decision.RequiredNextActions[0].ArgsHint["expect_contains"].([]string)
	if len(expectContains) != 1 || expectContains[0] != "2" {
		t.Fatalf("expect_contains = %#v, want [\"2\"]", decision.RequiredNextActions[0].ArgsHint["expect_contains"])
	}
}

func TestDecideCompletionBlockedUnverifiedWriteFallsBackToExistsWhenContentUnknown(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: false,
		CompletionReason: "unverified_write",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{
					{Path: "2.txt", Bytes: 1, WorkspaceWrite: true},
				},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Kind != "file_exists" {
		t.Fatalf("missing facts = %+v, want file_exists", decision.MissingFacts)
	}
	if len(decision.RequiredNextActions) == 0 || decision.RequiredNextActions[0].Tool != "filesystem_glob" {
		t.Fatalf("required actions = %+v, want filesystem_glob", decision.RequiredNextActions)
	}
}

func TestDecideWorkspaceWriteHardFailureStops(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "Z:/not-exist/test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
			Errors: runtimefacts.ErrorFacts{
				ToolErrors: []runtimefacts.ToolErrorFact{
					{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for Z:/not-exist/test.txt"},
					{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for Z:/not-exist/test.txt"},
				},
			},
		},
	})
	if decision.Status != DecisionFailed {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionFailed)
	}
}

func TestDecideWorkspaceWriteHardFailureRequiresTargetCorrelation(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "target/test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
			Errors: runtimefacts.ErrorFacts{
				ToolErrors: []runtimefacts.ToolErrorFact{
					{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for other/path.txt"},
					{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for another/path.txt"},
				},
			},
		},
	})
	assertDecisionStatus(t, decision, DecisionContinue)
}

func TestDecideWorkspaceWriteWithoutExplicitTargetFallsBackToAccepted(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "edit file",
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestLatestWriteVerificationHintBranches(t *testing.T) {
	facts := runtimefacts.RuntimeFacts{
		Files: runtimefacts.FileFacts{
			Written: []runtimefacts.FileWriteFact{
				{Path: "a.txt", ExpectedContent: "A"},
				{Path: "b.txt", ExpectedContent: "B"},
			},
		},
	}
	path, expected := latestWriteVerificationHint(facts, "b.txt")
	if path != "b.txt" || expected != "B" {
		t.Fatalf("hint for preferred path = (%q,%q), want (b.txt,B)", path, expected)
	}

	path, expected = latestWriteVerificationHint(facts, "missing.txt")
	if path != "missing.txt" || expected != "" {
		t.Fatalf("fallback preferred hint = (%q,%q), want (missing.txt,\"\")", path, expected)
	}

	path, expected = latestWriteVerificationHint(facts, "")
	if path != "b.txt" || expected != "B" {
		t.Fatalf("latest hint = (%q,%q), want (b.txt,B)", path, expected)
	}
}

func TestDecideWorkspaceWriteMissingFactsShouldRequestInputNotPlaceholderAction(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "请帮我修一下",
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}

	decision = Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: false,
		CompletionReason: "unverified_write",
		UserGoal:         "请帮我修一下",
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if decision.RequiredInput == nil {
		t.Fatalf("required_input is nil")
	}
	if len(decision.RequiredNextActions) != 0 {
		t.Fatalf("required actions = %+v, want empty", decision.RequiredNextActions)
	}
}

func TestDecideWorkspaceWriteSelectsLatestMentionedTarget(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "创建 2.txt 内容为 2",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{
					{Path: "1.txt", WorkspaceWrite: true, ExpectedContent: "1"},
					{Path: "2.txt", WorkspaceWrite: true, ExpectedContent: "2"},
				},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Target != "2.txt" {
		t.Fatalf("missing facts = %+v, want target 2.txt", decision.MissingFacts)
	}
}

func TestDecideRequiredNextActionsShouldNotContainPlaceholders(t *testing.T) {
	decisions := []Decision{
		Decide(DecisionInput{
			TaskKind:         TaskKindSubAgent,
			CompletionPassed: true,
			UserGoal:         "用 subagent 创建 test1.txt 内容为 1",
		}),
		Decide(DecisionInput{
			TaskKind:         TaskKindWorkspaceWrite,
			CompletionPassed: false,
			CompletionReason: "unverified_write",
			UserGoal:         "请继续",
		}),
		Decide(DecisionInput{
			TaskKind:         TaskKindTodoState,
			CompletionPassed: true,
			UserGoal:         "创建 todo",
		}),
	}
	for i, decision := range decisions {
		payload, err := json.Marshal(decision.RequiredNextActions)
		if err != nil {
			t.Fatalf("marshal required_next_actions[%d] failed: %v", i, err)
		}
		serialized := string(payload)
		if strings.Contains(serialized, "<") || strings.Contains(serialized, ">") {
			t.Fatalf("required_next_actions[%d] contains placeholder: %s", i, serialized)
		}
	}
}

func TestDecideSubAgentRequiresCompletedFact(t *testing.T) {
	continueDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
			},
		},
	})
	if continueDecision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", continueDecision.Status, DecisionContinue)
	}

	failedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
				Failed:  []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
			},
		},
	})
	if failedDecision.Status != DecisionFailed {
		t.Fatalf("status = %q, want %q", failedDecision.Status, DecisionFailed)
	}

	acceptedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started:   []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
				Completed: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
			},
		},
	})
	if acceptedDecision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", acceptedDecision.Status, DecisionAccepted)
	}
}

func TestDecideSubAgentWriteIntentNeedsArtifactEvidence(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		UserGoal:         "用 subagent 创建 test1.txt，内容为 1",
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started:   []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "coder"}},
				Completed: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "coder"}},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Kind != "subagent_artifact_or_file_fact" {
		t.Fatalf("missing facts = %+v", decision.MissingFacts)
	}

	accepted := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		UserGoal:         "用 subagent 创建 test1.txt，内容为 1",
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "coder"}},
				Completed: []runtimefacts.SubAgentFact{{
					TaskID: "sa-1", Role: "coder", Artifacts: []string{"test1.txt"},
				}},
			},
		},
	})
	if accepted.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", accepted.Status, DecisionAccepted)
	}
}

func TestDecideTodoStateAcceptsWithoutFileVerification(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindTodoState,
		CompletionPassed: true,
		Todos: TodoSnapshot{
			Items: []TodoViewItem{
				{ID: "todo-1", Content: "创建 Todo", Status: "pending", Required: false},
			},
			Summary: TodoSummary{Total: 1, RequiredTotal: 0},
		},
		Facts: runtimefacts.RuntimeFacts{
			Todos: runtimefacts.TodoFacts{CreatedIDs: []string{"todo-1"}},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestDecideReadOnlyBranches(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindReadOnly,
		CompletionPassed: true,
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}

	decision = Decide(DecisionInput{
		TaskKind:          TaskKindReadOnly,
		CompletionPassed:  true,
		LastAssistantText: "analysis done",
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}

	decision = Decide(DecisionInput{
		TaskKind:         TaskKindReadOnly,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Commands: runtimefacts.CommandFacts{
				Executed: []runtimefacts.CommandFact{{Tool: "bash", Command: "ls", ExitCode: 0, Succeeded: true}},
			},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestDecideMixedBranches(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindMixed,
		CompletionPassed: true,
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}

	decision = Decide(DecisionInput{
		TaskKind:         TaskKindMixed,
		CompletionPassed: true,
		Todos: TodoSnapshot{
			Summary: TodoSummary{RequiredOpen: 1},
		},
		Facts: runtimefacts.RuntimeFacts{
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_glob", Scope: "artifact:test.txt"}},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}

	decision = Decide(DecisionInput{
		TaskKind:          TaskKindMixed,
		CompletionPassed:  true,
		LastAssistantText: "analysis done",
		Facts: runtimefacts.RuntimeFacts{
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_glob", Scope: "artifact:test.txt"}},
			},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}

func TestDecideWorkspaceWriteInjectsLatestToolErrorIntoMissingFact(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		UserGoal:         "please update README.md",
		Facts: runtimefacts.RuntimeFacts{
			Errors: runtimefacts.ErrorFacts{
				ToolErrors: []runtimefacts.ToolErrorFact{
					{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "first error"},
					{Tool: "filesystem_write_file", ErrorClass: "generic_error", Content: ""},
				},
			},
		},
	})
	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 {
		t.Fatalf("missing facts = %+v", decision.MissingFacts)
	}
	details := decision.MissingFacts[0].Details
	if details["last_write_error"] != "generic_error" {
		t.Fatalf("last_write_error = %#v, want generic_error", details["last_write_error"])
	}
}
