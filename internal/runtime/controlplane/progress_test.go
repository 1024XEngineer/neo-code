package controlplane

import "testing"

func TestApplyProgressEvidenceNoEvidenceIncrementsNoProgress(t *testing.T) {
	t.Parallel()
	state := ProgressState{}
	next := ApplyProgressEvidence(state, nil)
	if next.LastScore.NoProgressStreak != 1 {
		t.Fatalf("expected no_progress_streak 1, got %d", next.LastScore.NoProgressStreak)
	}
}

func TestApplyProgressEvidenceOnlyNonDupResetsNoProgressStreak(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore: ProgressScore{NoProgressStreak: 3},
	}
	next := ApplyProgressEvidence(state, []ProgressEvidenceRecord{
		{Kind: EvidenceNewInfoNonDup},
	})
	if next.LastScore.NoProgressStreak != 0 {
		t.Fatalf("expected streak reset to 0, got %d", next.LastScore.NoProgressStreak)
	}
	if next.LastScore.ScoreDelta != 1 {
		t.Fatalf("expected score_delta 1, got %d", next.LastScore.ScoreDelta)
	}
}

func TestApplyProgressEvidenceMixedResetsNoProgress(t *testing.T) {
	t.Parallel()
	state := ProgressState{
		LastScore: ProgressScore{NoProgressStreak: 2},
	}
	next := ApplyProgressEvidence(state, []ProgressEvidenceRecord{
		{Kind: EvidenceNewInfoNonDup},
		{Kind: ProgressEvidenceKind("other_evidence")},
	})
	if next.LastScore.NoProgressStreak != 0 {
		t.Fatalf("expected streak reset, got %d", next.LastScore.NoProgressStreak)
	}
}
