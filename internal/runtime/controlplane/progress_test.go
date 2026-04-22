package controlplane

import "testing"

func TestEvaluateProgressBusinessProgressResetsStreaks(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore: ProgressScore{
			ExplorationStreak: 2,
			NoProgressStreak:  3,
			RepeatCycleStreak: 1,
		},
	}

	got := EvaluateProgress(state, ProgressInput{
		RunState: RunStateExecute,
		Evidence: []ProgressEvidenceRecord{
			{Kind: EvidenceTodoStateChanged},
		},
		NoProgressLimit:  3,
		RepeatCycleLimit: 3,
	})

	if !got.LastScore.HasBusinessProgress {
		t.Fatalf("expected business progress")
	}
	if got.LastScore.NoProgressStreak != 0 {
		t.Fatalf("no-progress streak = %d, want 0", got.LastScore.NoProgressStreak)
	}
	if got.LastScore.RepeatCycleStreak != 0 {
		t.Fatalf("repeat streak = %d, want 0", got.LastScore.RepeatCycleStreak)
	}
}

func TestEvaluateProgressExplorationUsesWindow(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore: ProgressScore{
			ExplorationStreak: 3,
			NoProgressStreak:  1,
		},
	}

	got := EvaluateProgress(state, ProgressInput{
		RunState: RunStatePlan,
		Evidence: []ProgressEvidenceRecord{
			{Kind: EvidenceNewInfoNonDup},
		},
		NoProgressLimit:  3,
		RepeatCycleLimit: 3,
	})

	if !got.LastScore.HasExplorationProgress {
		t.Fatalf("expected exploration progress")
	}
	if got.LastScore.ExplorationStreak != 4 {
		t.Fatalf("exploration streak = %d, want 4", got.LastScore.ExplorationStreak)
	}
	if got.LastScore.NoProgressStreak != 1 {
		t.Fatalf("no-progress streak = %d, want unchanged 1", got.LastScore.NoProgressStreak)
	}
}

func TestEvaluateProgressExplorationExhaustionStartsNoProgress(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore: ProgressScore{
			ExplorationStreak: 4,
			NoProgressStreak:  1,
		},
	}

	got := EvaluateProgress(state, ProgressInput{
		RunState: RunStatePlan,
		Evidence: []ProgressEvidenceRecord{
			{Kind: EvidenceNewInfoNonDup},
		},
		NoProgressLimit:  3,
		RepeatCycleLimit: 3,
	})

	if got.LastScore.NoProgressStreak != 2 {
		t.Fatalf("no-progress streak = %d, want 2", got.LastScore.NoProgressStreak)
	}
}

func TestEvaluateProgressRepeatCycleRequiresSameResultAndSubgoal(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore:              ProgressScore{RepeatCycleStreak: 2},
		LastToolSignature:      "sig",
		LastResultFingerprint:  "result",
		LastSubgoalFingerprint: "subgoal",
	}

	got := EvaluateProgress(state, ProgressInput{
		RunState:             RunStateExecute,
		CurrentToolSignature: "sig",
		ResultFingerprint:    "result",
		SubgoalFingerprint:   "subgoal",
		NoProgressLimit:      3,
		RepeatCycleLimit:     3,
	})

	if got.LastScore.RepeatCycleStreak != 3 {
		t.Fatalf("repeat streak = %d, want 3", got.LastScore.RepeatCycleStreak)
	}
	if got.LastScore.StalledProgressState != StalledProgressStalled {
		t.Fatalf("stalled state = %q, want %q", got.LastScore.StalledProgressState, StalledProgressStalled)
	}
	if got.LastScore.ReminderKind != ReminderKindRepeatCycle {
		t.Fatalf("reminder = %q, want %q", got.LastScore.ReminderKind, ReminderKindRepeatCycle)
	}
}

func TestEvaluateProgressUnknownSubgoalDoesNotAdvanceRepeat(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore:              ProgressScore{RepeatCycleStreak: 1},
		LastToolSignature:      "sig",
		LastResultFingerprint:  "result",
		LastSubgoalFingerprint: "subgoal",
	}

	got := EvaluateProgress(state, ProgressInput{
		RunState:             RunStateExecute,
		CurrentToolSignature: "sig",
		ResultFingerprint:    "result",
		SubgoalFingerprint:   "",
		NoProgressLimit:      3,
		RepeatCycleLimit:     3,
	})

	if got.LastScore.SameSubgoal != SubgoalRelationUnknown {
		t.Fatalf("same subgoal = %q, want %q", got.LastScore.SameSubgoal, SubgoalRelationUnknown)
	}
	if got.LastScore.RepeatCycleStreak != 0 {
		t.Fatalf("repeat streak = %d, want 0", got.LastScore.RepeatCycleStreak)
	}
}

func TestEvaluateProgressVerifyPassedAloneIsNotBusinessProgress(t *testing.T) {
	t.Parallel()

	got := EvaluateProgress(ProgressState{}, ProgressInput{
		RunState: RunStateVerify,
		Evidence: []ProgressEvidenceRecord{
			{Kind: EvidenceVerifyPassed},
		},
		NoProgressLimit:  3,
		RepeatCycleLimit: 3,
	})
	if got.LastScore.HasBusinessProgress {
		t.Fatalf("expected verify-passed alone to not count as business progress")
	}
	if got.LastScore.StrongEvidenceCount != 0 {
		t.Fatalf("strong evidence = %d, want 0", got.LastScore.StrongEvidenceCount)
	}
	if got.LastScore.MediumEvidenceCount != 1 {
		t.Fatalf("medium evidence = %d, want 1", got.LastScore.MediumEvidenceCount)
	}
}
