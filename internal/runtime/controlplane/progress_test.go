package controlplane

import "testing"

func TestEvaluateProgressRepeatCycleRequiresSameResultAndSubgoal(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore:              ProgressScore{RepeatCycleStreak: 2},
		LastToolSignature:      "sig",
		LastResultFingerprint:  "result",
		LastSubgoalFingerprint: "subgoal",
	}

	got := EvaluateProgress(state, ProgressInput{
		CurrentToolSignature: "sig",
		ResultFingerprint:    "result",
		SubgoalFingerprint:   "subgoal",
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
	if got.LastScore.ShouldTerminate {
		t.Fatal("first stalled repeat-cycle should warn before hard terminate")
	}
}

func TestEvaluateProgressRepeatCycleTerminatesAfterReminder(t *testing.T) {
	t.Parallel()

	state := ProgressState{
		LastScore: ProgressScore{
			RepeatCycleStreak:    3,
			StalledProgressState: StalledProgressStalled,
			ReminderKind:         ReminderKindRepeatCycle,
		},
		LastToolSignature:      "sig",
		LastResultFingerprint:  "result",
		LastSubgoalFingerprint: "subgoal",
	}

	got := EvaluateProgress(state, ProgressInput{
		CurrentToolSignature: "sig",
		ResultFingerprint:    "result",
		SubgoalFingerprint:   "subgoal",
		RepeatCycleLimit:     3,
	})

	if !got.LastScore.ShouldTerminate || got.LastScore.TerminateReason != StopReasonRepeatCycle {
		t.Fatalf("score = %+v, want repeat-cycle hard terminate", got.LastScore)
	}
}

func TestEvaluateProgressDifferentToolResultOrSubgoalResetsRepeat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input ProgressInput
		same  SubgoalRelation
	}{
		{
			name: "different tool",
			input: ProgressInput{
				CurrentToolSignature: "other",
				ResultFingerprint:    "result",
				SubgoalFingerprint:   "subgoal",
				RepeatCycleLimit:     3,
			},
			same: SubgoalRelationSame,
		},
		{
			name: "different result",
			input: ProgressInput{
				CurrentToolSignature: "sig",
				ResultFingerprint:    "other",
				SubgoalFingerprint:   "subgoal",
				RepeatCycleLimit:     3,
			},
			same: SubgoalRelationSame,
		},
		{
			name: "different subgoal",
			input: ProgressInput{
				CurrentToolSignature: "sig",
				ResultFingerprint:    "result",
				SubgoalFingerprint:   "other",
				RepeatCycleLimit:     3,
			},
			same: SubgoalRelationDifferent,
		},
		{
			name: "unknown subgoal",
			input: ProgressInput{
				CurrentToolSignature: "sig",
				ResultFingerprint:    "result",
				SubgoalFingerprint:   "",
				RepeatCycleLimit:     3,
			},
			same: SubgoalRelationUnknown,
		},
	}

	state := ProgressState{
		LastScore:              ProgressScore{RepeatCycleStreak: 2},
		LastToolSignature:      "sig",
		LastResultFingerprint:  "result",
		LastSubgoalFingerprint: "subgoal",
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := EvaluateProgress(state, tt.input)
			if got.LastScore.SameSubgoal != tt.same {
				t.Fatalf("same subgoal = %q, want %q", got.LastScore.SameSubgoal, tt.same)
			}
			if got.LastScore.RepeatCycleStreak != 0 {
				t.Fatalf("repeat streak = %d, want 0", got.LastScore.RepeatCycleStreak)
			}
			if got.LastScore.StalledProgressState != StalledProgressHealthy {
				t.Fatalf("stalled state = %q, want healthy", got.LastScore.StalledProgressState)
			}
		})
	}
}
