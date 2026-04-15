package controlplane

import "testing"

func TestApplyProgressEvidenceNoEvidenceIncrementsNoProgress(t *testing.T) {
	t.Parallel()
	got := ApplyProgressEvidence(ProgressState{}, nil, "")
	want := ProgressState{
		LastScore: ProgressScore{
			NoProgressStreak:  1,
			RepeatCycleStreak: 0,
		},
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestApplyProgressEvidenceOnlyNonDupResetsNoProgressStreak(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore: ProgressScore{NoProgressStreak: 3},
	}
	got := ApplyProgressEvidence(state, []ProgressEvidenceRecord{
		{Kind: EvidenceNewInfoNonDup},
	}, "sig1")
	want := ProgressState{
		LastScore: ProgressScore{
			ScoreDelta:        1,
			NoProgressStreak:  0,
			RepeatCycleStreak: 1,
		},
		LastSignature: "sig1",
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestApplyProgressEvidenceMixedResetsNoProgress(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore: ProgressScore{NoProgressStreak: 2},
	}
	got := ApplyProgressEvidence(state, []ProgressEvidenceRecord{
		{Kind: EvidenceNewInfoNonDup},
		{Kind: ProgressEvidenceKind("other_evidence")},
	}, "sig1")
	if got.LastScore.NoProgressStreak != 0 {
		t.Fatalf("expected streak reset, got %d", got.LastScore.NoProgressStreak)
	}
}

func TestApplyProgressEvidenceRepeatCycle(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore:     ProgressScore{NoProgressStreak: 1, RepeatCycleStreak: 2},
		LastSignature: "sig1",
	}
	got := ApplyProgressEvidence(state, []ProgressEvidenceRecord{
		{Kind: EvidenceNewInfoNonDup},
	}, "sig1")
	want := ProgressState{
		LastScore: ProgressScore{
			NoProgressStreak:  2,
			RepeatCycleStreak: 3,
		},
		LastSignature: "sig1",
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestApplyProgressEvidenceRepeatCycleOnFailureKeepsSignatureTracking(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore:     ProgressScore{NoProgressStreak: 2, RepeatCycleStreak: 1},
		LastSignature: "sig1",
	}

	got := ApplyProgressEvidence(state, nil, "sig1")
	want := ProgressState{
		LastScore: ProgressScore{
			NoProgressStreak:  3,
			RepeatCycleStreak: 2,
		},
		LastSignature: "sig1",
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}
